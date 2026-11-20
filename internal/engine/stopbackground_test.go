package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
)

// V0.stop-ends-background, reproduced from thread t_409a8a489de9014b: a
// background subagent kept writing after the main turn ended; Fyr read its
// messages as a turn of the agent, so the thread showed Running and offered
// Stop. The owner's Stop then made the CLI kill every background agent, the
// agent was not told, and the strip read "finished".
func TestSubagentOutputIsNotARun(t *testing.T) {
	h := newHarness(t, 0)
	id := h.create("agentwork")
	h.wait(id, "the turn that launched the agent ended", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && t.Summary == "agent launched"
	})
	// The process handles the stream in order: once two of the subagent's
	// messages are in the log, the run they would have started is too.
	h.wait(id, "the subagent wrote twice", func(core.Thread, core.Attention) bool {
		return strings.Count(eventsText(h, id, "msg"), `"parent_tool_use_id":"tu_ag2"`) >= 2
	})
	th, _ := h.e.Thread(id)
	if th.Running {
		t.Fatal("a background subagent's message started a run of the thread")
	}
	if r := h.cmd("thread.interrupt", id, nil); r.OK {
		t.Fatalf("Stop is offered while only a background agent works: %+v", r)
	}
}

// V0.stop-ends-background: the owner's Stop during a run also ends the
// background agents (the CLI's interrupt kills them). The task reads as
// stopped, never finished, and the agent is told with its next message
// which one ended, so it can start it again.
func TestStopNamesTheBackgroundAgentsItEnded(t *testing.T) {
	h := newHarness(t, 0)
	id := h.create("agentwork")
	h.wait(id, "the turn that launched the agent ended", func(t core.Thread, _ core.Attention) bool {
		return !t.Running && t.Summary == "agent launched"
	})
	if r := h.cmd("thread.send", id, core.Args{"text": "hang"}); !r.OK {
		t.Fatalf("send: %+v", r)
	}
	h.wait(id, "the hanging turn runs", runLive)
	if r := h.cmd("thread.interrupt", id, nil); !r.OK {
		t.Fatalf("interrupt: %+v", r)
	}
	th := h.wait(id, "the background agent ended", func(t core.Thread, _ core.Attention) bool {
		for _, k := range t.Tasks {
			if k.ID == "ag2" {
				return k.Status != core.TaskRunning
			}
		}
		return false
	})
	for _, k := range th.Tasks {
		if k.ID == "ag2" && k.Status != core.TaskStopped {
			t.Fatalf("the killed agent reads %q, not stopped", k.Status)
		}
	}
	if strings.Contains(eventsText(h, id, "task"), `"status":"killed"`) {
		t.Fatal("the timeline carries the CLI's killed, which the strip reads as finished")
	}
	var told string
	for _, n := range th.Feed.Queue {
		if n.Key == "task.stopped:ag2" {
			told = n.What
		}
	}
	if !strings.Contains(told, `background agent "Audit sources"`) {
		t.Fatalf("the agent is not told which task the Stop ended: %+v", th.Feed.Queue)
	}
}

// eventsText joins the data of a thread's events of one type.
func eventsText(h *harness, id, typ string) string {
	var b strings.Builder
	for _, e := range h.events(id) {
		if e.Type == typ {
			b.Write(e.Data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
