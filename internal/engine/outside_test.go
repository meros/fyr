package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/t3"
)

// #38 (moved from fyrd): the Fyr effects that reach past the host go to
// GitHub or the deployer; a merge forgets the PR tab's cached detail.
func TestOutsideRunsFyrEffects(t *testing.T) {
	url := "https://github.com/acme/app/pull/7"
	views := 0
	gh := func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "pr" && args[1] == "view" {
			views++
			return []byte(`{"number":7,"state":"OPEN"}`), nil
		}
		return []byte(`{}`), nil
	}
	details := &github.Details{Gh: gh}
	var deployed []string
	x := Outside{Gh: gh, PR: details, Deploy: func(ctx context.Context, threadID string, req core.EffectReq, dir string) (string, error) {
		deployed = append(deployed, threadID+" "+dir)
		return "deploying", nil
	}}
	ctx := context.Background()
	details.Get(ctx, url, false)
	details.Get(ctx, url, false)
	if views != 1 {
		t.Fatalf("the detail is not cached: %d reads", views)
	}
	if _, err := x.Run(ctx, "t1", core.EffectReq{Action: core.FyrMerge, Args: map[string]any{"pr": url, "method": "bogus"}}); err == nil || !strings.Contains(err.Error(), "merge method") {
		t.Fatalf("merge with a bad method: %v", err)
	}
	details.Get(ctx, url, false)
	if views != 2 {
		t.Fatalf("a merge did not forget the PR's detail: %d reads", views)
	}
	if out, err := x.Run(ctx, "t1", core.EffectReq{Action: core.FyrDeploy, Args: map[string]any{"dir": "/src/fyr"}}); err != nil || out != "deploying" || len(deployed) != 1 || deployed[0] != "t1 /src/fyr" {
		t.Fatalf("deploy: %q %v %v", out, err, deployed)
	}
	if _, err := x.Run(ctx, "t1", core.EffectReq{Action: "nope"}); err == nil || !strings.Contains(err.Error(), "unknown effect") {
		t.Fatalf("unknown effect: %v", err)
	}
}

// #38 (moved from fyrd): the poller watches the PRs of threads that are not
// archived, and its observations come back as Inputs; a provider counts the
// threads on it that are not archived.
func TestPRWatchAndThreadsOn(t *testing.T) {
	e, _, _ := bare(t)
	r := e.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": t.TempDir(), "text": "hi", "profile": "p1"}})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	id := r.ThreadID
	url := "https://github.com/acme/app/pull/9"
	coretest.Patch(e, id, func(th *core.Thread, now time.Time) []core.Event {
		core.PRObserved(th, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: core.ChecksPending}, now)
		return nil
	})
	w := PRWatch{E: e}
	if s := w.Snapshot(); len(s) != 1 || s[0] != (github.PRThread{ID: id, URL: url}) {
		t.Fatalf("snapshot %+v", s)
	}
	w.Observe(id, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: "fail"})
	if th, _ := e.Thread(id); th.MainPR() == nil || th.MainPR().Checks != "fail" {
		t.Fatalf("observed PR %+v", th.MainPR())
	}
	if n := e.ThreadsOn("p1"); n != 1 {
		t.Fatalf("threads on p1: %d", n)
	}
	if r := e.Command(api.Cmd{CommandID: "c2", Action: "thread.archive", ThreadID: id}); !r.OK {
		t.Fatalf("archive: %+v", r)
	}
	if s := w.Snapshot(); len(s) != 0 {
		t.Fatalf("an archived thread is watched: %+v", s)
	}
	if n := e.ThreadsOn("p1"); n != 0 {
		t.Fatalf("an archived thread counts: %d", n)
	}
}

// #38 (moved from fyrd), SPEC 0.5 V0.9: a T3 thread is adopted once, on its
// profile or the default one, with its PR and the last assistant text as
// its summary.
func TestT3Adopt(t *testing.T) {
	e, _, _ := bare(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-src-app")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(
		`{"type":"user","message":{"role":"user","content":"Fix the lasso"},"timestamp":"2026-11-13T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Fixed the lasso."}]},"timestamp":"2026-11-13T10:00:05Z"}
`), 0o644)
	x := T3{E: e, Home: home, Profiles: func() []string { return []string{"claudeAlt", "claudeAcme"} }}
	at := time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)
	cs := []t3.Candidate{
		{T3ID: "a", Title: "A", Cwd: "/src/app", Profile: "gone", SessionID: "s1", PRURL: "https://github.com/acme/app/pull/4", UpdatedAt: at},
		{T3ID: "b", Title: "B", Cwd: "/src/app", Profile: "claudeAcme", SessionID: "s2", UpdatedAt: at},
	}
	if n, err := x.adopt(cs, []string{"a", "b"}); err != nil || n != 2 {
		t.Fatalf("adopt: %d %v", n, err)
	}
	if n, _ := x.adopt(cs, []string{"a", "b"}); n != 0 {
		t.Fatalf("adopted again: %d", n)
	}
	got := map[string]api.Thread{}
	for _, d := range e.Snapshot() {
		got[d.T3ID] = d
	}
	a, b := got["a"], got["b"]
	if a.Thread == nil || a.Profile != "claudeAlt" || a.Source != "t3" || a.PR == nil || a.PR.Number != 4 || a.Summary != "Fixed the lasso." {
		t.Fatalf("a: %+v", a.Thread)
	}
	if b.Thread == nil || b.Profile != "claudeAcme" || b.PR != nil {
		t.Fatalf("b: %+v", b.Thread)
	}
	// #48: adopted through the path, it keeps T3's time and its history's.
	evs, _ := e.Events(a.ID, 0, 100)
	if !a.UpdatedAt.Equal(at) || len(evs) == 0 || !evs[0].At.Equal(time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("times: updated %v, history %+v", a.UpdatedAt, evs)
	}
}

// #48: t3.adopt is an Input kind on the one path: its reply waits for the
// adoption, and one commandId adopts once.
func TestT3AdoptCommand(t *testing.T) {
	e, _, _ := bare(t)
	if r := e.Command(api.Cmd{CommandID: "a0", Action: "t3.adopt"}); r.OK || r.Status != 409 {
		t.Fatalf("without T3: %+v", r)
	}
	calls := 0
	e.Adopt = func(ids []string) (int, error) { calls++; return len(ids), nil }
	for range 2 {
		r := e.Command(api.Cmd{CommandID: "a1", Action: "t3.adopt", Args: core.Args{"t3Ids": []any{"x", "y"}}})
		if n, _ := r.Result.(map[string]any)["adopted"]; !r.OK || fmt.Sprint(n) != "2" {
			t.Fatalf("adopt: %+v", r)
		}
	}
	if calls != 1 {
		t.Fatalf("one command adopted %d times", calls)
	}
}

// V0.72: a PR that GitHub says does not exist, and a change the owner ended,
// are no longer polled.
func TestEndedPRIsNotPolled(t *testing.T) {
	e, _, _ := bare(t)
	r := e.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": t.TempDir(), "text": "hi"}})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	id := r.ThreadID
	gone, mine := "https://github.com/acme/app/pull/88", "https://github.com/acme/app/pull/89"
	w := PRWatch{E: e}
	w.Observe(id, core.PR{URL: gone, Repo: "acme/app", Number: 88, State: "OPEN"})
	w.Observe(id, core.PR{URL: mine, Repo: "acme/app", Number: 89, State: "OPEN"})
	if s := w.Snapshot(); len(s) != 2 {
		t.Fatalf("snapshot %+v", s)
	}
	now := e.Now()
	w.Observe(id, core.PR{URL: gone, Repo: "acme/app", Number: 88, State: core.PRMissing, CheckedAt: &now})
	if s := w.Snapshot(); len(s) != 1 || s[0].URL != mine {
		t.Fatalf("a missing PR is still polled: %+v", s)
	}
	r = e.Command(api.Cmd{CommandID: "c2", Action: core.KindChangeEnd, ThreadID: id, Args: core.Args{"change": "pr:acme/app#89", "reason": "not mine"}})
	if !r.OK {
		t.Fatalf("end: %+v", r)
	}
	if s := w.Snapshot(); len(s) != 0 {
		t.Fatalf("an ended change is still polled: %+v", s)
	}
}
