package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.settle-merge-fixes (4): another agent's message to a settled thread
// unsettles it for the answer, and the thread settles again once that run
// is over and nothing is left; the settle Rule that settled it had fired.
func TestAgentMessageToSettledThreadSettlesAgain(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("coordinator")
	b := h.create("worker")
	h.wait(b, "done", state(core.Done))
	if r := h.cmd("thread.settle", b, nil); !r.OK {
		t.Fatalf("settle: %+v", r)
	}
	if _, isErr := callAs(t, h.e, a, "thread_send", map[string]any{"id": b, "text": "please rebase"}); isErr {
		t.Fatal("send failed")
	}
	h.wait(b, "answered and settled again", func(th core.Thread, _ core.Attention) bool {
		return th.RunEnds >= 2 && !th.Running && th.Settle.At != nil
	})
	if !hasEvent(h.events(b), "user", "please rebase") {
		t.Fatal("the message was not delivered")
	}

	// The owner's message to a settled thread is the owner's: it stays open.
	h.cmd("thread.send", b, core.Args{"text": "one more thing"})
	h.wait(b, "owner's turn", func(th core.Thread, _ core.Attention) bool { return th.RunEnds >= 3 && !th.Running })
	if th, _ := h.e.Thread(b); th.Settle.At != nil || core.SettlesWhenDone(&th) {
		t.Fatalf("the owner's message settled the thread again: %v", th.Settle.At)
	}
}

// V0.settle-merge-fixes (1): a run that wrote "merge" at the start of a text
// on a thread with an open PR and no merge ask wakes the agent with the call
// it should have made. Prose ("Merged.") does not.
func TestMergeInTextIsNudged(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("worker")
	h.wait(a, "done", state(core.Done))
	observePRIn(t, h.e, a, core.PR{URL: "https://github.com/o/r/pull/9", State: "OPEN", Checks: core.ChecksPass, Head: "abc1234"})
	h.cmd("thread.send", a, core.Args{"text": "hi"}) // takes the CI notice, which would go before saymerge
	h.wait(a, "notice taken", func(th core.Thread, _ core.Attention) bool { return th.RunEnds >= 2 && !th.Running })
	h.cmd("thread.send", a, core.Args{"text": "saymerge"})
	h.wait(a, "nudged", func(th core.Thread, _ core.Attention) bool { return th.RunEnds >= 4 && !th.Running })
	if !hasEvent(h.events(a), "user", "mcp__fyr__change_request") {
		t.Fatal("the agent was not told to call change_request")
	}

	for _, x := range []struct {
		texts []string
		want  bool
	}{
		{[]string{"merge"}, true},
		{[]string{"Opened the PR.", "merge\n\nCI passed."}, true},
		{[]string{"mergeI've opened PR #370"}, true},
		{[]string{"Merged. Next is the check."}, false},
		{[]string{"merges cleanly", "I asked Fyr to merge it."}, false},
	} {
		if got := mergeInText(x.texts); got != x.want {
			t.Errorf("%q: %v", x.texts, got)
		}
	}
}
