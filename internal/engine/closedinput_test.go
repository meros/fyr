package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.133: Fyr never closes the CLI's input while it has background
// work. claude 2.1.322 keeps running for a background task after stdin
// closes; the turn it then starts by itself cannot reach Fyr, and every
// tool call in it comes back as "The user doesn't want to take this action
// right now" although nobody refused it.
func TestIdleStopWaitsForBackgroundWork(t *testing.T) {
	h := newHarness(t, 100*time.Millisecond) // idle stop long before the task ends
	id := h.create("bgwait 700")
	th := h.wait(id, "the self-started turn read its task's output", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && (t.Summary == "background read ok" || t.Summary == "I've stopped as you asked.")
	})
	if th.Summary != "background read ok" {
		t.Fatalf("the turn after the background task was refused: summary %q", th.Summary)
	}
	if hasEvent(h.events(id), "tool.refused", "") {
		t.Fatal("a refusal was recorded")
	}
	// The idle stop still comes once the task is over.
	deadline := time.Now().Add(5 * time.Second)
	for h.m.Live(id) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if h.m.Live(id) {
		t.Fatal("the process did not stop when idle after its background work")
	}
}

// V0.133: a live view switched on while a background task runs ends the
// process only after the task, not at once.
func TestViewRefreshWaitsForBackgroundWork(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("bgwait 700")
	h.wait(id, "the first turn ended", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && t.Summary == "waiting for the build"
	})
	if r := h.cmd("thread.setView", id, core.Args{"on": true}); !r.OK {
		t.Fatalf("setView: %+v", r)
	}
	th := h.wait(id, "the self-started turn ended", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && (t.Summary == "background read ok" || t.Summary == "I've stopped as you asked.")
	})
	if th.Summary != "background read ok" {
		t.Fatalf("the turn after the background task was refused: summary %q", th.Summary)
	}
}

// V0.133: a tool call the CLI refuses with its generic reject text, which
// Fyr never sends, is recorded as a refusal that names the tool, so the
// owner does not read "stopped at your request" as their own doing.
func TestCLIRefusalIsRecorded(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("refused")
	h.wait(id, "done", state(core.Done))
	for _, e := range h.events(id) {
		if e.Type != "tool.refused" {
			continue
		}
		d := string(e.Data)
		if !strings.Contains(d, `"tool":"Bash"`) || !strings.Contains(d, "not the owner") {
			t.Fatalf("refusal %s", d)
		}
		return
	}
	t.Fatal("no tool.refused event")
}
