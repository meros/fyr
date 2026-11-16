package engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// SPEC V0.95 (1), (4): the record loop of 16 Nov, replayed. A squash-merged
// branch stays checked out with a changed file and nothing ahead of its base;
// a record of it from before has no revision. Run after run makes no new
// record, and nothing wakes the agent.
func TestRecordLoopReplay(t *testing.T) {
	e, _, _ := bare(t)
	g := &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	e.Git = g
	dir := t.TempDir()
	id := threadIn(t, e, dir)
	pr := core.PR{URL: "https://github.com/o/r/pull/13", Repo: "o/r", Number: 13, State: "MERGED", Checks: core.ChecksPass, Branch: "perf/x", Head: "h1", MergeSHA: "m1"}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsGitHub), Kind: core.KindPRObserved, Thread: id, Args: pr}); err != nil {
		t.Fatal(err)
	}
	key := core.ObservedKey("github.com/o/r", "perf/x")
	rec := &core.Change{ID: core.ChangeID(key), Key: key, Kind: core.KindCode, Title: "perf/x", Branch: "perf/x", Phase: core.PhaseWorking, PhaseBy: core.ByObserved,
		Targets: []string{"repo:github.com/o/r"}, Workspace: &core.Workspace{Repo: "github.com/o/r", Path: dir, Branch: "perf/x", Base: "origin/main"}, CreatedAt: e.Now(), UpdatedAt: e.Now()}
	e.mu.Lock()
	e.records[rec.ID] = rec
	e.threads[id].Refs = append(e.threads[id].Refs, core.Ref{ID: rec.ID, Repo: "github.com/o/r", Branch: "perf/x"})
	e.mu.Unlock()
	// Drop what the PR's merge queued; the loop is what follows.
	e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindFeedTake, Thread: id, Args: core.FeedTake{At: core.AtTool}})

	facts := core.GitFacts{Repo: "github.com/o/r", Path: dir, Branch: "perf/x", Base: "origin/main", Ahead: 0, Unpushed: 0, Dirty: 1, Head: "h1"}
	for range 5 {
		f := facts
		e.observeGit(id, &f)
	}
	e.mu.Lock()
	n := len(e.records)
	e.mu.Unlock()
	if n != 1 {
		t.Fatalf("records after 5 runs: %d", n)
	}
	out, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindFeedTake, Thread: id, Args: core.FeedTake{At: core.AtIdle}})
	if err != nil {
		t.Fatal(err)
	}
	if ns, _ := out.([]core.Notice); len(ns) != 0 {
		t.Fatalf("the loop woke the agent: %q", core.NoticeText(ns))
	}
}

// SPEC V0.95 (1): with the fake agent: an observed merge on an idle thread
// starts no run; CI failed on another PR does.
func TestObservedMergeDoesNotStartARun(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	gh := core.Observer(core.ObsGitHub)
	pr := core.PR{URL: "https://github.com/o/r/pull/13", Repo: "o/r", Number: 13, State: "OPEN", Checks: core.ChecksPass, Branch: "perf/x", Head: "h1"}
	for _, s := range []string{"OPEN", "MERGED"} {
		pr.State = s
		if _, err := h.e.Apply(core.Input{Source: gh, Kind: core.KindPRObserved, Thread: id, Args: pr}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if th, _ := h.e.Thread(id); th.RunEnds != 1 || th.Running {
		t.Fatalf("an observed merge started a run: runs %d, running %v", th.RunEnds, th.Running)
	}
	fail := core.PR{URL: "https://github.com/o/r/pull/14", Repo: "o/r", Number: 14, State: "OPEN", Checks: core.ChecksPending, Branch: "perf/y", Head: "y1"}
	h.e.Apply(core.Input{Source: gh, Kind: core.KindPRObserved, Thread: id, Args: fail})
	fail.Checks = core.ChecksFail
	h.e.Apply(core.Input{Source: gh, Kind: core.KindPRObserved, Thread: id, Args: fail})
	h.wait(id, "woken by CI failed", func(t core.Thread, _ core.Attention) bool { return t.RunEnds == 2 && !t.Running })
}

// SPEC V0.95 (1): a next notice goes out with the next turn: the owner's
// message carries it, before the owner's words, and a later tool call does
// not repeat it.
func TestNextNoticeRidesTheOwnersMessage(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	gh := core.Observer(core.ObsGitHub)
	pr := core.PR{URL: "https://github.com/o/r/pull/13", Repo: "o/r", Number: 13, State: "OPEN", Checks: core.ChecksPending, Branch: "perf/x", Head: "h1"}
	for _, s := range []string{"OPEN", "MERGED"} {
		pr.State = s
		if _, err := h.e.Apply(core.Input{Source: gh, Kind: core.KindPRObserved, Thread: id, Args: pr}); err != nil {
			t.Fatal(err)
		}
	}
	if r := h.cmd("thread.send", id, core.Args{"text": "hello again"}); !r.OK {
		t.Fatalf("send: %+v", r)
	}
	// The fake echoes its input; the summary is its first line.
	th := h.wait(id, "the owner's turn", func(t core.Thread, _ core.Attention) bool { return t.RunEnds == 2 && !t.Running })
	if th.Summary != "echo: [Fyr] PR #13: merged." || len(th.Feed.Queue) != 0 {
		t.Fatalf("the turn read %q, queue %+v", th.Summary, th.Feed.Queue)
	}
	if r := h.cmd("thread.send", id, core.Args{"text": "tool 300"}); !r.OK {
		t.Fatalf("send: %+v", r)
	}
	th = h.wait(id, "a turn with a tool call", func(t core.Thread, _ core.Attention) bool { return t.RunEnds == 3 && !t.Running })
	if !strings.HasPrefix(th.Summary, "context:") || strings.Contains(th.Summary, "merged") {
		t.Fatalf("the tool call repeated it: %q", th.Summary)
	}
}

// SPEC V0.95 (5): the load pass deletes the ~N records that ended and that no
// thread change follows, with the threads' references, and the leaked
// acme/app#88 fixture change; a second load changes nothing.
func TestLoadDropsLoopRecords(t *testing.T) {
	db := filepath.Join(t.TempDir(), "fyr.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	base := core.ObservedKey("github.com/o/r", "perf/x")
	mk := func(key string, phase core.Phase) *core.Change {
		return &core.Change{ID: core.ChangeID(key), Key: key, Kind: core.KindCode, Title: "perf/x", Branch: "perf/x", Phase: phase, PhaseBy: core.ByObserved,
			Targets: []string{"repo:github.com/o/r"}, CreatedAt: now, UpdatedAt: now}
	}
	first, loop2, loop3, followed, inFlight := mk(base, core.PhaseLanded), mk(base+"~2", core.PhaseLanded), mk(base+"~3", core.PhaseAbandoned), mk(base+"~4", core.PhaseLanded), mk(base+"~5", core.PhaseWorking)
	recs := []*core.Change{first, loop2, loop3, followed, inFlight}
	th := &core.Thread{ID: "t_loop", Title: "loop", Cwd: "/w/r", Owner: core.Owner, CreatedAt: now, UpdatedAt: now}
	for _, c := range recs {
		th.Refs = append(th.Refs, core.Ref{ID: c.ID, Repo: "github.com/o/r", Branch: "perf/x", Ended: !c.Phase.InFlight()})
	}
	th.Changes = []core.Change{
		{ID: "c_mine", Key: "perf", Kind: core.KindCode, Title: "Perf", Branch: "perf/x", Observed: followed.ID, Phase: core.PhaseLanded, PhaseBy: core.ByObserved, CreatedAt: now, UpdatedAt: now},
		{ID: "c_acme", Key: core.PRKey("acme/app", 88), Kind: core.KindCode, Title: "Listing cards v2", Phase: core.PhaseReview, PhaseBy: core.ByObserved, CreatedAt: now, UpdatedAt: now,
			PR: &core.PR{URL: "https://github.com/acme/app/pull/88", Repo: "acme/app", Number: 88, State: core.PRMissing}},
	}
	if _, err := st.Write(func(tx *store.Tx) error {
		for _, c := range recs {
			if err := tx.PutChange(c); err != nil {
				return err
			}
		}
		return tx.PutThread(th)
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		e := New(st)
		if err := e.Load(); err != nil {
			t.Fatal(err)
		}
		rs, _ := st.Changes()
		var ids []string
		for _, c := range rs {
			ids = append(ids, c.ID)
		}
		if want := sorted(first.ID, followed.ID, inFlight.ID); !slices.Equal(sorted(ids...), want) {
			t.Fatalf("records %v, want %v", sorted(ids...), want)
		}
		ts, _ := st.Threads()
		got := ts[0]
		var refs []string
		for _, r := range got.Refs {
			refs = append(refs, r.ID)
		}
		if want := sorted(first.ID, followed.ID, inFlight.ID); !slices.Equal(sorted(refs...), want) {
			t.Fatalf("refs %v, want %v", sorted(refs...), want)
		}
		if len(got.Changes) != 1 || got.Changes[0].ID != "c_mine" {
			t.Fatalf("thread changes: %+v", got.Changes)
		}
	}
}
