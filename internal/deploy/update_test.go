package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/release"
)

const relSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeReleases serves deployable = sha and a release of it signed with
// another key than Key when signWithOther, from an httptest GitHub
// (V0.release-without-gh: plain HTTPS, no gh).
func fakeReleases(t *testing.T, sha string, signWithOther bool) *Releases {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	sign := priv
	if signWithOther {
		sign = other
	}
	gh := newFakeGitHub(t, sha, sign)
	r := gh.releases()
	r.Key = base64.StdEncoding.EncodeToString(pub)
	return r
}

// fakeGitHub is GitHub for the repo meros/fyr: the API's tag ref and
// compare, and the download URLs of one release, build-<sha12>.
type fakeGitHub struct {
	sha   string
	files map[string][]byte
	srv   *httptest.Server
	mu    sync.Mutex
	asked []*http.Request
	// handle, when set, answers first; false passes the request on.
	handle func(w http.ResponseWriter, r *http.Request) bool
}

func newFakeGitHub(t *testing.T, sha string, sign ed25519.PrivateKey) *fakeGitHub {
	t.Helper()
	tgt := release.Here()
	dir := t.TempDir()
	b, _ := json.Marshal(release.Info{Commit: sha, Build: release.Short(sha)})
	os.WriteFile(filepath.Join(dir, release.Manifest), b, 0o644)
	os.WriteFile(filepath.Join(dir, release.Asset("fyrd", tgt)), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, release.Asset("fyr", tgt)), []byte("#!/bin/sh\n"), 0o755)
	sums, _ := release.SumFile(dir, []string{release.Asset("fyr", tgt), release.Asset("fyrd", tgt), release.Manifest})
	os.WriteFile(filepath.Join(dir, release.Sums), sums, 0o644)
	sig, _ := release.Sign(base64.StdEncoding.EncodeToString(sign.Seed()), sums)
	os.WriteFile(filepath.Join(dir, release.Sig), []byte(sig), 0o644)
	g := &fakeGitHub{sha: sha, files: map[string][]byte{}}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		g.files[e.Name()], _ = os.ReadFile(filepath.Join(dir, e.Name()))
	}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.asked = append(g.asked, r.Clone(context.Background()))
	h := g.handle
	g.mu.Unlock()
	if h != nil && h(w, r) {
		return
	}
	dl := "/meros/fyr/releases/download/" + release.Tag(g.sha) + "/"
	switch p := r.URL.Path; {
	case p == "/repos/meros/fyr/git/ref/tags/deployable":
		fmt.Fprintf(w, `{"ref":"refs/tags/deployable","object":{"sha":%q,"type":"commit"}}`, g.sha)
	case strings.HasPrefix(p, "/repos/meros/fyr/compare/"):
		fmt.Fprint(w, `{"status":"diverged","ahead_by":0,"total_commits":0,"commits":[]}`)
	case strings.HasPrefix(p, dl) && g.files[strings.TrimPrefix(p, dl)] != nil:
		w.Write(g.files[strings.TrimPrefix(p, dl)])
	default:
		http.NotFound(w, r)
	}
}

// requests is what the fake was asked, as "METHOD path".
func (g *fakeGitHub) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, r := range g.asked {
		out = append(out, r.Method+" "+r.URL.Path)
	}
	return out
}

// releases reads this fake: the API and downloads from it, deployable by
// git ls-remote from it too.
func (g *fakeGitHub) releases() *Releases {
	return &Releases{API: g.srv.URL, Web: g.srv.URL, HTTP: g.srv.Client(), Git: func(_ context.Context, args ...string) ([]byte, error) {
		return []byte(g.sha + "\trefs/tags/deployable\n"), nil
	}}
}

// rtFunc is an http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// onCompare answers GitHub's compare with answer(path) and passes the rest on.
func onCompare(r *Releases, answer func(path string) string) *Releases {
	next := r.HTTP.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	r.HTTP = &http.Client{Transport: rtFunc(func(q *http.Request) (*http.Response, error) {
		if strings.Contains(q.URL.Path, "/compare/") {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(answer(q.URL.Path))), Request: q}, nil
		}
		return next.RoundTrip(q)
	})}
	return r
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
	markProbationOK(t, d, "")
	d.ReportPending(t.Context())
	for i := 0; i < 200 && d.PausedBy() == nil; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if d.PausedBy() == nil {
		t.Fatal("a local build did not pause auto-update")
	}
	// The pause comes before the report (V0.deploy-report-flake): wait
	// for the report itself.
	if r := h.reportsAfter(1); len(r) != 1 || !strings.Contains(r[0], "auto-update is paused") {
		t.Fatalf("reports %v", r)
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

// Polls are hourly, so a release seen between them wakes the watcher when
// its quiet period ends instead of waiting for the next poll.
func TestWatcherWakesWhenTheQuietEnds(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false)
	now := time.Now()
	w := &Watcher{D: d, Now: func() time.Time { return now }, Start: func(string) {}}
	if w.every() != time.Hour || w.untilDue() != 0 {
		t.Fatalf("every %v, due %v before any release", w.every(), w.untilDue())
	}
	w.Step(context.Background())
	if got := w.untilDue(); got != 10*time.Minute+time.Second {
		t.Fatalf("due in %v, want the quiet period", got)
	}
	now = now.Add(11 * time.Minute)
	if got := w.untilDue(); got != 0 {
		t.Fatalf("due in %v after the quiet period", got)
	}
}

// V0.update-now-fresh: the host's summary says when a release that waits
// out its quiet period installs by itself, so the fronts can offer Update
// now before then; once it runs, nothing is due.
func TestSummarySaysWhenTheWaitingReleaseIsDue(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.Releases = fakeReleases(t, relSHA, false)
	d.DB.SetKV("live_commit", oldSHA)
	now := time.Now()
	w := &Watcher{D: d, Now: func() time.Time { return now }, Start: func(string) {}}
	if !d.Summary().Due.IsZero() {
		t.Fatal("due before any release was seen")
	}
	w.Step(context.Background())
	if got := d.Summary().Due; !got.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("due %v, want the end of the quiet period %v", got, now.Add(10*time.Minute))
	}
	d.DB.SetKV("live_commit", relSHA)
	if got := d.Summary().Due; !got.IsZero() {
		t.Fatalf("due %v while the release runs", got)
	}
}

// A Kick before Run is kept, not lost: the first Step after it is at once.
func TestKickBeforeRunIsKept(t *testing.T) {
	w := &Watcher{}
	w.Kick()
	w.Kick() // a second one while the first waits is folded into it
	select {
	case <-w.kicks():
	default:
		t.Fatal("the kick was lost")
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
	markProbationOK(t, d, "")
	d.ReportPending(t.Context())
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

// V0.update-changelog: a release's record lists the commits since the build that runs, newest first, by one compare
// from the live build's commit.
func TestUpdateRecordHasItsChangelog(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	d.Live = "fedcba987654"
	var asked string
	c := func(n int) string { return strings.Repeat(string(rune('a'+n)), 40) }
	r := onCompare(fakeReleases(t, relSHA, false), func(path string) string {
		asked = path
		return `{"status":"ahead","ahead_by":3,"total_commits":3,"commits":[` +
			`{"sha":"` + c(0) + `","commit":{"message":"First (#1)\n\nbody"}},` +
			`{"sha":"` + c(1) + `","commit":{"message":"Second"}},` +
			`{"sha":"` + c(2) + `","commit":{"message":"Third (#3)"}}]}`
	})
	d.Releases = r
	d.Run = releaseRunner(steps)
	d.Deploy(context.Background(), "", core.EffectReq{Action: "fyr.deploy", Args: map[string]any{"trigger": "deployable"}}, "")
	u := d.Updates()
	if len(u) != 1 {
		t.Fatalf("records %+v", u)
	}
	if asked != "/repos/meros/fyr/compare/fedcba987654..."+relSHA {
		t.Fatalf("compare asked %q", asked)
	}
	got := u[0]
	if got.From != "fedcba987654" || got.ChangesAll != 3 || len(got.Changes) != 3 || got.Changes[0].Title != "Third (#3)" || got.Changes[2].Commit != c(0) {
		t.Fatalf("changelog %+v", got)
	}
}

// V0.update-changelog: the list keeps the newest changesKeep and counts them all; lines that are not a commit drop.
func TestParseChangesKeepsTheNewest(t *testing.T) {
	var b strings.Builder
	b.WriteString("noise\n")
	for i := 0; i < changesKeep+5; i++ {
		b.WriteString(strings.Repeat("0", 38) + string("0123456789abcdef"[i/16]) + string("0123456789abcdef"[i%16]) + "\tc" + strings.Repeat("x", i) + "\n")
	}
	cs, n := parseChanges(b.String(), true)
	if n != changesKeep+5 || len(cs) != changesKeep || cs[0].Title != "c"+strings.Repeat("x", changesKeep+4) {
		t.Fatalf("n=%d len=%d first=%q", n, len(cs), cs[0].Title)
	}
}
