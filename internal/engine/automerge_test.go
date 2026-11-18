package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

// SPEC V0.102: the owner opts a repo in to auto-merge; then an agent's merge
// ask merges with no press once its head is green, and the setting outlives
// a restart. A repo not in the setting waits for the owner as before.
func TestAutoMergeByRepoSetting(t *testing.T) {
	const head = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	url := "https://github.com/acme/app/pull/9"
	observe := func(checks core.Checks) {
		coretest.Patch(h.e, id, func(t *core.Thread, now time.Time) []core.Event {
			core.PRObserved(t, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: checks, Head: head, MergeState: "CLEAN"}, now)
			return nil
		})
	}
	observe(core.ChecksPending)
	c := changeOf(h, id, "pr:acme/app#9")
	if out, isErr := callAs(t, h.e, id, "change_request", map[string]any{"change": c.ID, "ask": "merge", "reason": "green soon"}); isErr {
		t.Fatalf("change_request: %s", out)
	}
	if got := h.e.AutoMergeRepos(); len(got) != 1 || got[0].Repo != "acme/app" || got[0].On {
		t.Fatalf("repos %+v", got)
	}
	// The setting is off: green checks leave the ask for the owner.
	observe(core.ChecksPass)
	time.Sleep(100 * time.Millisecond)
	if th, _ := h.e.Thread(id); len(th.Decisions) != 1 || h.r.n() != 0 {
		t.Fatalf("merged without the setting: %d decisions, %d runs", len(th.Decisions), h.r.n())
	}
	for _, d := range h.e.Snapshot() {
		if d.ID == id && d.PushQuiet {
			t.Fatal("an ask the owner must answer is quiet")
		}
	}
	// The owner opts the repo in: the waiting ask merges at once.
	if r := h.cmd(core.KindAutoMergeSet, "", core.Args{"repo": "Acme/App", "on": true}); !r.OK {
		t.Fatalf("set: %+v", r)
	}
	eventually(t, "the auto-approved merge ran", func() bool { return h.r.n() == 1 })
	h.r.mu.Lock()
	call := h.r.calls[0]
	h.r.mu.Unlock()
	if call.Action != core.FyrMerge || call.Args["head"] != head || call.Args["auto"] != "acme/app" {
		t.Fatalf("ran %+v", call)
	}
	eventually(t, "the thread says it", func() bool { return hasEvent(h.events(id), "effect.result", "Auto-approved") })
	// V0.200: the merge sends no push, whatever state it leaves.
	eventually(t, "the merged thread is quiet", func() bool {
		for _, d := range h.e.Snapshot() {
			if d.ID == id {
				return d.PushQuiet
			}
		}
		return false
	})
	if !hasEvent(h.events(id), "decision.closed", "Auto-approved") {
		t.Fatal("no auto-approved answer in the thread")
	}
	if c := changeOf(h, id, c.ID); c.AutoApproved == nil || c.AutoApproved.Repo != "acme/app" {
		t.Fatalf("the board does not say it: %+v", c.AutoApproved)
	}
	// A second ask in an opted-in repo merges when its checks pass.
	observe(core.ChecksPending)
	callAs(t, h.e, id, "change_request", map[string]any{"change": c.ID, "ask": "merge", "reason": "again"})
	time.Sleep(100 * time.Millisecond)
	if h.r.n() != 1 {
		t.Fatalf("merged with pending checks: %d", h.r.n())
	}
	observe(core.ChecksPass)
	eventually(t, "the second merge ran on green", func() bool { return h.r.n() == 2 })

	// The setting is stored: a new engine on the same database has it.
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	if got := h2.e.AutoMergeRepos(); len(got) == 0 || !got[0].On {
		t.Fatalf("after restart %+v", got)
	}
	if r := h2.cmd(core.KindAutoMergeSet, "", core.Args{"repo": "acme/app", "on": false}); !r.OK {
		t.Fatalf("unset: %+v", r)
	}
	for _, r := range h2.e.AutoMergeRepos() {
		if r.On {
			t.Fatalf("still on: %+v", r)
		}
	}
}

// SPEC V0.205 (1), (2): while the agent runs, an ask in a repo of the setting
// is not in Needs you, and it is the owner's again when the repo is turned off.
func TestSelfMergingAskReadsWorking(t *testing.T) {
	const head = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	url := "https://github.com/acme/app/pull/9"
	coretest.Patch(h.e, id, func(t *core.Thread, now time.Time) []core.Event {
		core.PRObserved(t, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: core.ChecksPass, Head: head, MergeState: "CLEAN"}, now)
		t.Running = true
		return nil
	})
	c := changeOf(h, id, "pr:acme/app#9")
	if r := h.cmd(core.KindAutoMergeSet, "", core.Args{"repo": "acme/app", "on": true}); !r.OK {
		t.Fatalf("set: %+v", r)
	}
	if out, isErr := callAs(t, h.e, id, "change_request", map[string]any{"change": c.ID, "ask": "merge", "reason": "green"}); isErr {
		t.Fatalf("change_request: %s", out)
	}
	attention := func() core.State {
		for _, d := range h.e.Snapshot() {
			if d.ID == id {
				return d.Attention.State
			}
		}
		return ""
	}
	eventually(t, "the ask reads working", func() bool { return attention() == core.Working })
	if h.r.n() != 0 {
		t.Fatalf("merged during the run: %d", h.r.n())
	}
	if r := h.cmd(core.KindAutoMergeSet, "", core.Args{"repo": "acme/app", "on": false}); !r.OK {
		t.Fatalf("unset: %+v", r)
	}
	eventually(t, "the ask is the owner's again", func() bool { return attention() == core.Asking })
}
