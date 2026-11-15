package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// lastUser is the text of the last message the agent was sent.
func lastUser(h *harness, id string) string {
	text := ""
	for _, ev := range h.events(id) {
		if ev.Type == "user" {
			var m map[string]any
			json.Unmarshal(ev.Data, &m)
			text, _ = m["text"].(string)
		}
	}
	return text
}

// #47: a notice that arrives while the agent runs is taken at its next tool
// call as context, not as a message; nothing is left to start another turn.
func TestFeedRunningAtToolCall(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("tool 800")
	h.wait(id, "running", func(t core.Thread, _ core.Attention) bool { return t.Running })
	if r := h.cmd("thread.rename", id, core.Args{"title": "Login fix"}); !r.OK {
		t.Fatalf("rename: %+v", r)
	}
	th := h.wait(id, "done", state(core.Done))
	if th.Summary != `context: [Fyr] The owner renamed this thread to "Login fix".` {
		t.Fatalf("the agent read %q", th.Summary)
	}
	time.Sleep(300 * time.Millisecond)
	if th, _ := h.e.Thread(id); th.RunEnds != 1 || len(th.Feed.Queue) != 0 {
		t.Fatalf("runs %d, queue %+v", th.RunEnds, th.Feed.Queue)
	}
}

// #47: a settled thread's notices queue without waking the agent; when the
// owner revives it, they start one turn together.
func TestFeedSettledQueuesUntilRevived(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	if r := h.cmd("thread.settle", id, core.Args{"now": true}); !r.OK {
		t.Fatalf("settle: %+v", r)
	}
	if r := h.cmd("thread.rename", id, core.Args{"title": "Login fix"}); !r.OK {
		t.Fatalf("rename: %+v", r)
	}
	time.Sleep(300 * time.Millisecond)
	if th, _ := h.e.Thread(id); th.RunEnds != 1 || th.Running || len(th.Feed.Queue) != 2 {
		t.Fatalf("a settled thread was woken: runs %d, queue %+v", th.RunEnds, th.Feed.Queue)
	}
	if r := h.cmd("thread.unsettle", id, nil); !r.OK {
		t.Fatalf("unsettle: %+v", r)
	}
	h.wait(id, "woken once", func(t core.Thread, _ core.Attention) bool { return t.RunEnds == 2 && !t.Running })
	want := "[Fyr] The owner set this thread: settled → not settled.\n[Fyr] The owner renamed this thread to \"Login fix\"."
	if got := lastUser(h, id); got != want {
		t.Fatalf("the agent was sent %q", got)
	}
	time.Sleep(300 * time.Millisecond)
	if th, _ := h.e.Thread(id); th.RunEnds != 2 || len(th.Feed.Queue) != 0 {
		t.Fatalf("runs %d, queue %+v", th.RunEnds, th.Feed.Queue)
	}
}

// #47: fyr_state returns the thread's records in one compact form, and one
// change by its key.
func TestFyrStateTool(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("pr")
	h.wait(id, "pr", func(t core.Thread, _ core.Attention) bool { return t.MainPR() != nil })
	h.wait(id, "done", state(core.Done))
	out, isErr := callAs(t, h.e, id, "fyr_state", map[string]any{})
	if isErr || !strings.Contains(out, "thread "+id) || !strings.Contains(out, "changes:") || !strings.Contains(out, "PR #") || !strings.Contains(out, "view: off") {
		t.Fatalf("state: %s", out)
	}
	th, _ := h.e.Thread(id)
	one, isErr := callAs(t, h.e, id, "fyr_state", map[string]any{"about": th.Changes[0].Key})
	if isErr || strings.Contains(one, "thread "+id) || !strings.Contains(one, th.Changes[0].ID) {
		t.Fatalf("one change: %s", one)
	}
	if out, isErr := callAs(t, h.e, id, "fyr_state", map[string]any{"about": "nope"}); !isErr {
		t.Fatalf("unknown about: %s", out)
	}
	t.Logf("fyr_state: %d characters\n%s", len(out), out)
	if len(out) > 2000 {
		t.Fatalf("not compact: %d characters", len(out))
	}
}
