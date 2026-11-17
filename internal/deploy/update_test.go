package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/release"
)

const relSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeReleases serves deployable = sha and a release of it signed with
// signKey; Key verifies with verifyKey.
func fakeReleases(t *testing.T, sha string, signWithOther bool) *Releases {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	sign := priv
	if signWithOther {
		sign = other
	}
	tgt := release.Here()
	return &Releases{Key: base64.StdEncoding.EncodeToString(pub), Gh: func(_ context.Context, args ...string) ([]byte, error) {
		a := strings.Join(args, " ")
		switch {
		case strings.Contains(a, "git/ref/tags/deployable"):
			return []byte(sha + "\n"), nil
		case strings.Contains(a, "/compare/"):
			return []byte("diverged"), nil
		case args[0] == "release" && args[1] == "download":
			var dir string
			for i, x := range args {
				if x == "-D" {
					dir = args[i+1]
				}
			}
			b, _ := json.Marshal(release.Info{Commit: sha, Build: release.Short(sha)})
			os.WriteFile(filepath.Join(dir, release.Manifest), b, 0o644)
			os.WriteFile(filepath.Join(dir, release.Asset("fyrd", tgt)), []byte("#!/bin/sh\n"), 0o755)
			os.WriteFile(filepath.Join(dir, release.Asset("fyr", tgt)), []byte("#!/bin/sh\n"), 0o755)
			sums, _ := release.SumFile(dir, []string{release.Asset("fyr", tgt), release.Asset("fyrd", tgt), release.Manifest})
			os.WriteFile(filepath.Join(dir, release.Sums), sums, 0o644)
			sig, _ := release.Sign(base64.StdEncoding.EncodeToString(sign.Seed()), sums)
			os.WriteFile(filepath.Join(dir, release.Sig), []byte(sig), 0o644)
			return nil, nil
		}
		return nil, errors.New("unexpected gh " + a)
	}}
}

// releaseRunner answers `fyrd version` with the release's id.
func releaseRunner(steps *[]string) Runner {
	return func(_ context.Context, dir string, args ...string) ([]byte, error) {
		*steps = append(*steps, strings.Join(args, " "))
		if len(args) == 2 && args[1] == "version" {
			return []byte(release.Short(relSHA) + "\n"), nil
		}
		return []byte("ok"), nil
	}
}

// #149, #150: an ask without a checkout downloads the deployable release,
// verifies it, smoke-tests it (no make, no tests) and links it as next.
func TestReleaseInstallSwitches(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false)
	d.Run = releaseRunner(steps)
	r := core.EffectReq{Action: "fyr.deploy", Args: map[string]any{}}
	if _, err := d.Deploy(context.Background(), "t1", r, ""); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	all := strings.Join(*steps, "\n")
	if strings.Contains(all, "make") || !strings.Contains(all, "selftest") {
		t.Fatalf("steps %v", *steps)
	}
	if target(d.Data, "next") != release.Short(relSHA) {
		t.Fatalf("next=%s", target(d.Data, "next"))
	}
	for _, f := range []string{"fyrd", "fyr", verifiedMark, release.Manifest} {
		if _, err := os.Stat(filepath.Join(d.Data, "builds", release.Short(relSHA), f)); err != nil {
			t.Fatalf("%s missing: %v", f, err)
		}
	}
	raw, _ := d.DB.GetKV("pending_deploy")
	var p pending
	json.Unmarshal([]byte(raw), &p)
	if p.Commit != relSHA || p.Local {
		t.Fatalf("pending %+v", p)
	}
}

// #149: a release signed with another key is refused; nothing is linked.
func TestReleaseInstallRefusesBadSignature(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, true)
	d.Run = releaseRunner(steps)
	_, err := d.Deploy(context.Background(), "t1", core.EffectReq{Action: "fyr.deploy", Args: map[string]any{}}, "")
	if err == nil || !strings.Contains(err.Error(), "did not verify") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Lstat(filepath.Join(d.Data, "next")); err == nil {
		t.Fatal("next linked for a bad release")
	}
	if _, err := os.Stat(filepath.Join(d.Data, "builds", release.Short(relSHA))); err == nil {
		t.Fatal("a bad release was kept as a build")
	}
}

// #159: a local build runs the smoke test, not make test, unless the ask
// says tests: true; once it has run its probation, auto-update pauses.
func TestLocalBuildSmokesAndPauses(t *testing.T) {
	d, h, steps := newDeployer(t, "")
	r := core.EffectReq{Action: "fyr.deploy", Args: map[string]any{"dir": "/src/fyr"}}
	if _, err := d.Deploy(context.Background(), "t1", r, "/src/fyr"); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	all := strings.Join(*steps, "\n")
	if strings.Contains(all, " test") && strings.Contains(all, "make -C /src/fyr test") || !strings.Contains(all, "selftest") {
		t.Fatalf("steps %v", *steps)
	}
	d.ReportPending()
	for i := 0; i < 200 && d.PausedBy() == nil; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if d.PausedBy() == nil {
		t.Fatal("a local build did not pause auto-update")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.reports) != 1 || !strings.Contains(h.reports[0], "auto-update is paused") {
		t.Fatalf("reports %v", h.reports)
	}
}

// #145: a failed test run names the test and keeps its log.
func TestFailedTestsNameTheTestAndKeepTheLog(t *testing.T) {
	d, _, _ := newDeployer(t, " test")
	_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
	if err == nil || !strings.Contains(err.Error(), "TestX") || !strings.Contains(err.Error(), "test.log") {
		t.Fatalf("err %v", err)
	}
	logs, _ := filepath.Glob(filepath.Join(d.Data, "builds", "*", "test.log"))
	if len(logs) != 1 {
		t.Fatalf("test logs %v", logs)
	}
	if line := strings.SplitN(err.Error(), "\n", 2)[0]; !strings.Contains(line, "TestX") {
		t.Fatalf("the first line (the notice) misses the test: %q", line)
	}
}

func TestFailingFindsGoTestsAndPackages(t *testing.T) {
	out := "=== RUN TestA\n--- FAIL: TestA (0.10s)\n    a_test.go:9: boom\nFAIL\nFAIL\tgithub.com/meros/fyr/internal/x\t0.2s\nok  \tgithub.com/meros/fyr/internal/y\t0.1s\n"
	got := Failing([]byte(out))
	if !strings.Contains(got, "TestA") || !strings.Contains(got, "internal/x") || strings.Contains(got, "internal/y") {
		t.Fatalf("got %q", got)
	}
	if Failing([]byte("nothing useful")) != "see the log" {
		t.Fatal("no fallback")
	}
}

// #153: deployable must stay put for the quiet period; then one install.
func TestWatcherWaitsForQuiet(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false)
	now := time.Now()
	var started []string
	w := &Watcher{D: d, Now: func() time.Time { return now }, Start: func(sha string) { started = append(started, sha) }}
	w.Step(context.Background())
	if len(started) != 0 {
		t.Fatal("installed before the quiet period")
	}
	now = now.Add(11 * time.Minute)
	w.Step(context.Background())
	w.Step(context.Background())
	if len(started) != 1 || started[0] != relSHA {
		t.Fatalf("started %v", started)
	}
}

// #159: while a local build runs, a release that does not contain it is
// not installed.
func TestWatcherLeavesALocalBuild(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false) // compare says "diverged"
	d.pause(Paused{Build: "local", Commit: "fedcba9876543210fedcba9876543210fedcba98", At: time.Now()})
	now := time.Now()
	started := 0
	w := &Watcher{D: d, Now: func() time.Time { return now }, Start: func(string) { started++ }}
	w.Step(context.Background())
	now = now.Add(time.Hour)
	w.Step(context.Background())
	if started != 0 {
		t.Fatal("auto-update replaced a local build")
	}
}

// #154: a release that rolled back here is not tried again.
func TestWatcherSkipsABadRelease(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false)
	d.markBad(relSHA, "not ready")
	now := time.Now()
	started := 0
	w := &Watcher{D: d, Now: func() time.Time { return now }, Start: func(string) { started++ }}
	w.Step(context.Background())
	now = now.Add(time.Hour)
	w.Step(context.Background())
	if started != 0 {
		t.Fatal("a release that rolled back here was installed again")
	}
}

// #147: prune keeps the linked builds and the newest others, and database
// snapshots only in current and previous.
func TestPruneKeepsLinkedAndNewest(t *testing.T) {
	data := t.TempDir()
	names := []string{"a", "b", "c", "d", "e", "f"}
	for i, n := range names {
		dir := filepath.Join(data, "builds", n)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "fyrd"), []byte("x"), 0o755)
		os.WriteFile(filepath.Join(dir, "pre.db"), []byte("snapshot"), 0o644)
		at := time.Now().Add(time.Duration(i) * time.Minute)
		os.Chtimes(dir, at, at)
	}
	os.MkdirAll(filepath.Join(data, "builds", ".dl-x-1"), 0o755)
	link(t, data, "current", "a")
	link(t, data, "previous", "b")
	prune(data, 2)
	left, _ := os.ReadDir(filepath.Join(data, "builds"))
	var got []string
	for _, e := range left {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != "a,b,e,f" {
		t.Fatalf("left %v", got)
	}
	for n, want := range map[string]bool{"a": true, "b": true, "e": false, "f": false} {
		_, err := os.Stat(filepath.Join(data, "builds", n, "pre.db"))
		if (err == nil) != want {
			t.Fatalf("pre.db in %s: %v", n, err == nil)
		}
	}
}

// #151: a release install leaves a record with its trigger, commit and
// phases; the new daemon's probation ends it as live.
func TestUpdateRecordFromStartToLive(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false)
	d.Run = releaseRunner(steps)
	r := core.EffectReq{Action: "fyr.deploy", Args: map[string]any{"trigger": "deployable"}}
	d.Deploy(context.Background(), "", r, "")
	u := d.Updates()
	if len(u) != 1 || u[0].Build != release.Short(relSHA) || u[0].Commit != relSHA || u[0].Trigger != "deployable" || u[0].Result != UpdateRunning {
		t.Fatalf("record %+v", u)
	}
	var names []string
	for _, p := range u[0].Phases {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "download,smoke,switch" {
		t.Fatalf("phases %v", names)
	}
	d.ReportPending()
	for i := 0; i < 200 && d.Updates()[0].Result == UpdateRunning; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if got := d.Updates()[0]; got.Result != UpdateLive || got.EndedAt.IsZero() {
		t.Fatalf("after probation %+v", got)
	}
	if b, err := d.UpdateLog(release.Short(relSHA), "smoke.log"); err != nil || len(b) == 0 {
		t.Fatalf("smoke log %q %v", b, err)
	}
	if _, err := d.UpdateLog("../x", "smoke.log"); err == nil {
		t.Fatal("a path outside builds was served")
	}
}

// A host that never updated itself sends an empty list, not null: the Automation page reads its length.
func TestUpdatesEmptyIsAList(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	b, _ := json.Marshal(d.Updates())
	if string(b) != "[]" {
		t.Fatalf("no updates encode as %s, want []", b)
	}
}

// #151: a failed local build is recorded as failed with its first line.
func TestUpdateRecordFailure(t *testing.T) {
	d, _, _ := newDeployer(t, " test")
	d.Deploy(context.Background(), "t1", req, "/src/fyr")
	u := d.Updates()
	if len(u) != 1 || u[0].Result != UpdateFailed || !u[0].Local || !strings.Contains(u[0].Reason, "TestX") {
		t.Fatalf("record %+v", u)
	}
	if len(u[0].Logs) == 0 {
		t.Fatal("the record lists no logs")
	}
}
