package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

// SPEC V0.97: every merge path carries the head the owner saw. The agent's
// merge ask fixes the head when it is made and runs with it after the head
// moved (GitHub then refuses it); the PR tab's Merge and a change's Merge
// button refuse a head Fyr no longer sees, and pass the current one on.
func TestMergeBoundToHead(t *testing.T) {
	const headA, headB = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "b2c3d4e5f60718293a4b5c6d7e8f901234567890"
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	url := "https://github.com/acme/app/pull/9"
	set := func(head string) {
		coretest.Patch(h.e, id, func(t *core.Thread, now time.Time) []core.Event {
			core.PRObserved(t, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: "pass", Head: head, MergeState: "CLEAN"}, now)
			return nil
		})
	}
	set(headA)
	c := changeOf(h, id, "pr:acme/app#9")
	if out, isErr := callAs(t, h.e, id, "change_request", map[string]any{"change": c.ID, "ask": "merge", "reason": "green"}); isErr {
		t.Fatalf("change_request: %s", out)
	}
	th, _ := h.e.Thread(id)
	d := th.Decisions[0]
	if d.Effect.Action != core.FyrMerge || d.Effect.Args["head"] != headA {
		t.Fatalf("the ask did not fix the head: %+v", d.Effect)
	}
	if r := h.cmd("thread.answer", id, core.Args{"decisionId": d.ID, "allow": true}); !r.OK {
		t.Fatalf("answer: %+v", r)
	}
	eventually(t, "the ask ran", func() bool { return h.r.n() == 1 })
	h.r.mu.Lock()
	got := h.r.calls[0].Args["head"]
	h.r.mu.Unlock()
	if got != headA {
		t.Fatalf("the ask ran with head %v, want the ask-time head %s", got, headA)
	}
	// An ask made before the head moved is refused when the owner approves it.
	callAs(t, h.e, id, "change_request", map[string]any{"change": c.ID, "ask": "merge", "reason": "again"})
	th, _ = h.e.Thread(id)
	d = th.Decisions[len(th.Decisions)-1]
	if d.Effect.Args["head"] != headA {
		t.Fatalf("second ask: %+v", d.Effect)
	}
	set(headB)
	h.cmd("thread.answer", id, core.Args{"decisionId": d.ID, "allow": true})
	eventually(t, "the stale ask was refused", func() bool { return hasEvent(h.events(id), "effect.result", core.HeadMoved) })
	if h.r.n() != 1 {
		t.Fatalf("a stale ask ran: %d", h.r.n())
	}

	if r := h.cmd("pr.merge", id, core.Args{"method": "squash", "head": headA}); r.OK || !strings.Contains(r.Error, core.HeadMoved) {
		t.Fatalf("PR tab with a stale head: %+v", r)
	}
	if r := h.cmd("pr.merge", id, core.Args{"method": "squash"}); r.OK {
		t.Fatalf("PR tab with no head: %+v", r)
	}
	if h.r.n() != 1 {
		t.Fatalf("a refused merge ran: %d", h.r.n())
	}
	if r := h.cmd("pr.merge", id, core.Args{"method": "squash", "head": headB}); !r.OK {
		t.Fatalf("PR tab with the current head: %+v", r)
	}
	eventually(t, "the PR tab's merge ran", func() bool { return h.r.n() == 2 })
	h.r.mu.Lock()
	got = h.r.calls[1].Args["head"]
	h.r.mu.Unlock()
	if got != headB {
		t.Fatalf("PR tab merged head %v, want %s", got, headB)
	}

	m := findOffer(changeOf(h, id, c.ID).Offers, "fyr.merge")
	if r := h.cmd("action.run", id, core.Args{"on": c.ID, "action": "fyr.merge", "digest": m.Digest, "head": headA}); r.OK || !strings.Contains(r.Error, core.HeadMoved) {
		t.Fatalf("press with a stale head: %+v", r)
	}
	if r := h.cmd("action.run", id, core.Args{"on": c.ID, "action": "fyr.merge", "digest": m.Digest, "head": headB}); !r.OK {
		t.Fatalf("press with the current head: %+v", r)
	}
	eventually(t, "the press ran", func() bool { return h.r.n() == 3 })
	h.r.mu.Lock()
	got = h.r.calls[2].Args["head"]
	h.r.mu.Unlock()
	if got != headB {
		t.Fatalf("the press merged head %v, want %s", got, headB)
	}
}

// SPEC V0.97: each change in the thread DTO carries its Ready value.
func TestReadyInDTO(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	coretest.Patch(h.e, id, func(t *core.Thread, now time.Time) []core.Event {
		core.PRObserved(t, core.PR{URL: "https://github.com/acme/app/pull/9", Repo: "acme/app", Number: 9, State: "OPEN", Checks: "pass", Head: "abc1234", MergeState: "CLEAN"}, now)
		return nil
	})
	d := dtoOf(h.e, id)
	if len(d.Changes) != 1 || d.Changes[0].Ready != core.ReadyMerge {
		t.Fatalf("changes %+v", d.Changes)
	}
}
