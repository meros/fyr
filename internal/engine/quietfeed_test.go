package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
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
