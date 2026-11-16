package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// SPEC V0.95 (1): an observed "merged" on an idle thread is next and does not
// wake the agent; CI failed does.
func TestNextDoesNotWake(t *testing.T) {
	th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1"}, ct0)
	th, res := observePR(t, th, PR{State: "MERGED", Checks: ChecksPass, Head: "h1", MergeSHA: "m1"}, ct0.Add(time.Minute))
	if n := queued(t, th, "phase:"+th.Changes[0].ID); n.Urgency != Next || wakes(res) {
		t.Fatalf("merged: %+v wakes=%v", n, wakes(res))
	}
	if _, ns := take(t, th, FeedTake{At: AtIdle}, ct0.Add(time.Minute)); len(ns) != 0 {
		t.Fatalf("a next notice started a turn: %q", whats(ns))
	}
	if _, ns := take(t, th, FeedTake{At: AtTool}, ct0.Add(time.Minute)); len(ns) != 2 || ns[1].What != "PR #7: merged." {
		t.Fatalf("not taken at the next tool call: %q", whats(ns))
	}
	// A PR closed without a merge is done, not a failure: next.
	th, res = observePR(t, feedThread(), PR{State: "CLOSED", Checks: ChecksPending}, ct0)
	if n := queued(t, th, "phase:"+th.Changes[0].ID); n.Urgency != Next || wakes(res) {
		t.Fatalf("closed: %+v wakes=%v", n, wakes(res))
	}
	// The owner's rename is next too.
	th, res = fstep(t, feedThread(), Input{Source: FromOwner, Kind: "thread.rename", Args: Args{"title": "Login fix"}}, ct0)
	if n := queued(t, th, "owner:title"); n.Urgency != Next || wakes(res) {
		t.Fatalf("rename: %+v wakes=%v", n, wakes(res))
	}
	th, res = observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksFail, Head: "h1"}, ct0)
	if n := queued(t, th, "cond:"+th.Changes[0].ID+":checks"); n.Urgency != Wake || !wakes(res) {
		t.Fatalf("CI failed: %+v wakes=%v", n, wakes(res))
	}
	if _, ns := take(t, th, FeedTake{At: AtIdle}, ct0); len(ns) != 1 {
		t.Fatalf("CI failed did not start a turn: %q", whats(ns))
	}
}

// SPEC V0.95 (2): a fact said on one head is not said again on that head,
// across runs; a new head is a new notice.
func TestFactSaidOncePerHead(t *testing.T) {
	th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksFail, Head: "h1"}, ct0)
	th, ns := take(t, th, FeedTake{At: AtIdle}, ct0)
	if len(ns) != 1 {
		t.Fatalf("first failure: %q", whats(ns))
	}
	RunStarted(th, ct0)
	th, _ = take(t, th, FeedTake{At: AtTool}, ct0.Add(time.Minute))
	RunEnded(th, nil, ct0.Add(2*time.Minute))
	// The checks rerun on the same head and fail again.
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPending, Head: "h1"}, ct0.Add(3*time.Minute))
	th, res := observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: "h1"}, ct0.Add(4*time.Minute))
	if len(th.Feed.Queue) != 0 || wakes(res) {
		t.Fatalf("the same failure on the same head again: %+v", th.Feed.Queue)
	}
	// It survives a store round trip.
	th = CloneThread(th)
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPending, Head: "h1"}, ct0.Add(5*time.Minute))
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: "h1"}, ct0.Add(6*time.Minute))
	if len(th.Feed.Queue) != 0 {
		t.Fatalf("said again after a round trip: %+v", th.Feed.Queue)
	}
	// A new head is new news.
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPending, Head: "h2"}, ct0.Add(7*time.Minute))
	th, res = observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: "h2"}, ct0.Add(8*time.Minute))
	if n := queued(t, th, "cond:"+th.Changes[0].ID+":checks"); n.What != "CI on PR #7: failed." || !wakes(res) {
		t.Fatalf("new head: %+v", n)
	}
}

// failOn makes CI fail on PR #7 at a new head h: one observer wake.
func failOn(t *testing.T, th *Thread, h string, at time.Time) (*Thread, Result) {
	t.Helper()
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPending, Head: h}, at)
	return observePR(t, th, PR{State: "OPEN", Checks: ChecksFail, Head: h}, at)
}

// SPEC V0.95 (3): observers wake a thread at most 5 times in 10 minutes; the
// sixth wake is queued, its time is the thread's NextCheck, and the clock
// delivers it. The owner's message resets the budget; a result is not
// counted.
func TestWakeBudget(t *testing.T) {
	th := feedThread()
	var res Result
	for i := range 5 {
		at := ct0.Add(time.Duration(i) * time.Minute)
		th, res = failOn(t, th, "h"+string(rune('a'+i)), at)
		if !wakes(res) {
			t.Fatalf("wake %d did not wake", i+1)
		}
		var ns []Notice
		if th, ns = take(t, th, FeedTake{At: AtIdle}, at); len(ns) != 1 {
			t.Fatalf("wake %d: %q", i+1, whats(ns))
		}
	}
	six := ct0.Add(5 * time.Minute)
	th, res = failOn(t, th, "hf", six)
	if wakes(res) || len(th.Feed.Queue) != 1 {
		t.Fatalf("the sixth wake in 10 minutes woke: %+v", th.Feed.Queue)
	}
	if _, ns := take(t, th, FeedTake{At: AtIdle}, six.Add(time.Minute)); len(ns) != 0 {
		t.Fatalf("taken while the budget is spent: %q", whats(ns))
	}
	free := ct0.Add(10 * time.Minute)
	if at := NextCheck(th, nil, six); at == nil || !at.Equal(free) {
		t.Fatalf("NextCheck %v, want %v", at, free)
	}
	th, res = fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, free)
	if !wakes(res) {
		t.Fatal("the clock did not deliver the queued wake")
	}
	th, ns := take(t, th, FeedTake{At: AtIdle}, free)
	if len(ns) != 1 || ns[0].What != "CI on PR #7: failed." {
		t.Fatalf("delivered: %q", whats(ns))
	}
	if at := NextCheck(th, nil, free); at != nil {
		t.Fatalf("NextCheck after the delivery: %v", at)
	}

	// A result of the agent's ask is not counted.
	th = feedThread()
	for i := range 5 {
		th, _ = failOn(t, th, "h"+string(rune('a'+i)), ct0)
		th, _ = take(t, th, FeedTake{At: AtIdle}, ct0)
	}
	req := &EffectReq{Action: FyrMerge, Args: map[string]any{"pr": fprURL}}
	th2, res := fstep(t, th, Input{Source: FromEffect("fx_1"), Kind: KindEffectResult, Args: EffectResult{Effect: Effect{ID: "fx_1", Kind: EffectRun, Allow: true, Request: req}, OK: true}}, ct0)
	if !wakes(res) {
		t.Fatal("a result was held by the budget")
	}
	if _, ns := take(t, th2, FeedTake{At: AtIdle}, ct0); len(ns) != 1 {
		t.Fatalf("result: %q", whats(ns))
	}
	// The owner's message resets the budget.
	th, _ = fstep(t, th, Input{Source: FromOwner, Kind: "thread.send", Args: Args{"text": "go on"}}, ct0)
	if th, res = failOn(t, th, "hz", ct0.Add(time.Minute)); !wakes(res) {
		t.Fatalf("after the owner's message: %+v", th.Feed)
	}
}

// legacyRecord is a stored record as the loop left it: no revision.
func legacyRecord(t *testing.T, js string) *Change {
	t.Helper()
	c := &Change{}
	if err := json.Unmarshal([]byte(js), c); err != nil {
		t.Fatal(err)
	}
	return c
}

// SPEC V0.95 (4): a record needs commits ahead of its base on a branch that
// is not the default; it stores the head it read, so an ended record is
// found by its head even without a revision.
func TestRecordNeedsCommitsAhead(t *testing.T) {
	g := GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "perf/x", Base: "origin/main", Head: "h1"}
	for _, x := range []GitFacts{
		{Dirty: 3}, {Unpushed: 2}, {Dirty: 1, Unpushed: 1},
	} {
		f := g
		f.Dirty, f.Unpushed = x.Dirty, x.Unpushed
		if _, c := observe(t, nil, f, ct0); c != nil {
			t.Fatalf("nothing ahead made a record: %+v", f)
		}
	}
	main := g
	main.Branch, main.Ahead, main.Unpushed = "main", 2, 2
	if _, c := observe(t, nil, main, ct0); c != nil {
		t.Fatalf("the default branch made a record: %+v", c)
	}
	f := g
	f.Ahead = 1
	rs, rec := observe(t, nil, f, ct0)
	if rec == nil || len(rec.Revisions) != 1 {
		t.Fatalf("a branch with a commit: %+v", rec)
	}
	// The head stays on the record when the branch is no longer ahead.
	f.Ahead, f.Dirty = 0, 1
	_, rec = observe(t, rs, f, ct0)
	b, _ := json.Marshal(rec.Workspace)
	if !strings.Contains(string(b), `"head":"h1"`) {
		t.Fatalf("workspace without the head: %s", b)
	}

	// A record the loop left: landed, no revision, the head on its workspace.
	landed := legacyRecord(t, `{"id":"c_l","key":"git:github.com/o/r#perf/x~5","kind":"code","title":"perf/x","branch":"perf/x","phase":"landed","phaseBy":"observed",
		"targets":["repo:github.com/o/r"],"workspace":{"repo":"github.com/o/r","path":"/w/r","branch":"perf/x","base":"origin/main","head":"h1"}}`)
	f.Ahead, f.Dirty = 1, 0
	if _, c := observe(t, []*Change{landed}, f, ct0); c != nil {
		t.Fatalf("the ended head made a new record: %+v", c)
	}
	if MergedPRCovers(&PR{State: "MERGED", Head: "h9"}, landed) {
		t.Fatal("a merged PR at another head covers the record's head")
	}
	// An ended record at an unknown head: no new record without a head.
	unknown := legacyRecord(t, `{"id":"c_u","key":"git:github.com/o/r#perf/x~6","kind":"code","title":"perf/x","branch":"perf/x","phase":"landed","phaseBy":"observed","targets":["repo:github.com/o/r"]}`)
	f.Head = ""
	if _, c := observe(t, []*Change{unknown}, f, ct0); c != nil {
		t.Fatalf("an unknown head after an ended record made a new record: %+v", c)
	}
	f.Head = "h2"
	if _, c := observe(t, []*Change{landed}, f, ct0); c == nil {
		t.Fatal("a new head is new work")
	}
}
