package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/release"
)

const oldSHA = "fedcba9876543210fedcba9876543210fedcba98"

// withAhead answers GitHub's compare with ahead_by n, and counts the calls.
func withAhead(r *Releases, n string, calls *int) *Releases {
	return onCompare(r, func(string) string {
		*calls++
		return `{"status":"diverged","ahead_by":` + n + `}`
	})
}

// install is one Fyr install for the side-by-side test: its own data
// folder, its own kv, its own watcher; smokeFails makes its smoke test fail.
func install(t *testing.T, smokeFails bool) (*Deployer, *Watcher) {
	d, _, steps := newDeployer(t, "")
	d.Live = release.Short(oldSHA)
	d.DB.SetKV("live_commit", oldSHA)
	calls := 0
	d.Releases = withAhead(fakeReleases(t, relSHA, false), "3", &calls)
	ok := releaseRunner(steps)
	d.Run = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if smokeFails && len(args) > 1 && args[1] == "selftest" {
			return []byte("panic: schema"), os.ErrInvalid
		}
		return ok(ctx, dir, args...)
	}
	now := time.Now()
	w := &Watcher{D: d, Quiet: func() time.Duration { return time.Minute }, Now: func() time.Time { return now }}
	w.Start = func(sha string) {
		r := core.EffectReq{Action: core.FyrDeploy, Args: map[string]any{"commit": sha, "trigger": "deployable"}}
		d.Deploy(context.Background(), "", r, "")
	}
	// The release is seen now and due once its quiet period has passed.
	w.Step(context.Background())
	now = now.Add(2 * time.Minute)
	return d, w
}

// #154: two installs side by side (decision 0021), each with its own data
// folder, update on their own: one whose smoke test fails marks the release
// bad and links nothing; the other installs it. Nothing of one lands in the
// other's folder, and the bad mark stays with the install it belongs to.
func TestTwoInstallsUpdateOnTheirOwn(t *testing.T) {
	a, wa := install(t, true)
	b, wb := install(t, false)
	if a.Data == b.Data {
		t.Fatal("the installs share a data folder")
	}
	wa.Step(context.Background())
	wb.Step(context.Background())

	if _, err := os.Lstat(filepath.Join(a.Data, "next")); err == nil {
		t.Fatal("a linked next although its smoke test failed")
	}
	if got := target(b.Data, "next"); got != release.Short(relSHA) {
		t.Fatalf("b's next = %q, want %s", got, release.Short(relSHA))
	}
	for _, d := range []*Deployer{a, b} {
		if _, err := os.Stat(filepath.Join(d.Data, "builds", release.Short(relSHA), "fyrd")); err != nil {
			t.Fatalf("%s: no download of its own: %v", d.Data, err)
		}
		ents, _ := os.ReadDir(filepath.Join(d.Data, "builds"))
		if len(ents) != 1 {
			t.Fatalf("%s: builds %v", d.Data, ents)
		}
	}
	if c, why := a.Bad(); c != relSHA || !strings.Contains(why, "smoke") {
		t.Fatalf("a's bad mark = %q %q", c, why)
	}
	if c, _ := b.Bad(); c != "" {
		t.Fatalf("b holds back %s, which only failed on a", c)
	}
	if p, _ := a.DB.GetKV("pending_deploy"); p != "" {
		t.Fatal("a waits for a switch it never made")
	}
	if p, _ := b.DB.GetKV("pending_deploy"); p == "" {
		t.Fatal("b switched without a pending deploy")
	}

	// a's next read skips the bad release: no second download, no smoke.
	a.Run = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("a tried the bad release again")
		return nil, nil
	}
	a.DB.SetKV(seenKey, "") // a fresh move of deployable to the same release
	wa.Now = func() time.Time { return time.Now().Add(time.Hour) }
	wa.Step(context.Background())
	wa.Step(context.Background())

	s := a.Summary()
	if s.Bad == nil || s.Bad.Commit != relSHA || s.Running != oldSHA || s.Wanted != relSHA || s.Behind != 3 {
		t.Fatalf("a's summary %+v", s)
	}
	if s.Last == nil || s.Last.Result != UpdateFailed || s.Last.Phases != nil {
		t.Fatalf("a's last update %+v", s.Last)
	}
	if s := b.Summary(); s.Bad != nil || s.Last == nil || s.Last.Result != UpdateRunning {
		t.Fatalf("b's summary %+v", s)
	}
}

// V0.host-updates: the bad mark shows only while deployable points at it;
// the owner's retry clears it, and a release that went live does too.
func TestBadMarkShowsWhileItHoldsBack(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	changes := 0
	d.OnChange = func() { changes++ }
	d.DB.SetKV(seenKey, relSHA)
	d.markBad(relSHA, "exited with code 1")
	if s := d.Summary(); s.Bad == nil || s.Bad.Why != "exited with code 1" || s.Bad.At.IsZero() {
		t.Fatalf("summary %+v", s)
	}
	d.DB.SetKV(seenKey, oldSHA)
	if s := d.Summary(); s.Bad != nil {
		t.Fatalf("a mark of a release deployable moved past still shows: %+v", s.Bad)
	}
	if !d.ClearBad() || d.ClearBad() {
		t.Fatal("ClearBad")
	}
	if changes != 2 {
		t.Fatalf("OnChange heard %d changes, want 2", changes)
	}

	// A release that rolled back before and now goes live ends its mark.
	d.markBad(relSHA, "rolled back")
	d.DB.SetKV("pending_deploy", `{"build":"0123456","commit":"`+relSHA+`"}`)
	markProbationOK(t, d, "0123456")
	d.ReportPending(t.Context())
	for i := 0; i < 400; i++ {
		if c, _ := d.Bad(); c == "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a release that went live kept its bad mark")
}

// V0.host-updates: behind is read once per pair of running and wanted, and
// 0 without a call when the host runs the wanted release.
func TestBehindIsReadOncePerMove(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	calls := 0
	d.Releases = withAhead(fakeReleases(t, relSHA, false), "5", &calls)
	d.DB.SetKV("live_commit", oldSHA)
	d.DB.SetKV(seenKey, relSHA)
	if s := d.Summary(); s.Behind != -1 {
		t.Fatalf("behind before any read = %d", s.Behind)
	}
	d.noteBehind(context.Background(), relSHA)
	d.noteBehind(context.Background(), relSHA)
	if s := d.Summary(); s.Behind != 5 || calls != 1 {
		t.Fatalf("behind %d after %d calls", s.Behind, calls)
	}
	d.DB.SetKV("live_commit", relSHA)
	d.noteBehind(context.Background(), relSHA)
	if s := d.Summary(); s.Behind != 0 || calls != 1 {
		t.Fatalf("behind %d after %d calls on the wanted release", s.Behind, calls)
	}
}
