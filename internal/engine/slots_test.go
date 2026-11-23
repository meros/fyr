// SPEC 0.5 V0.86: a queued thread is told when the slot is its own, and a
// held slot goes back when the run ends. (The agent's slot_acquire and
// slot_release went with V0.one-tool-vocabulary: the gate admits.)
package engine

import (
	"github.com/meros/fyr/internal/guard"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

func running(e *Engine, id string) {
	coretest.Patch(e, id, func(th *core.Thread, now time.Time) []core.Event {
		core.RunStarted(th, now)
		return nil
	})
}

func told(e *Engine, id, what string) bool {
	th, _ := e.Thread(id)
	return slices.ContainsFunc(th.Feed.Queue, func(n core.Notice) bool { return strings.Contains(n.What, what) })
}

func TestV086SlotToolsAndRunEnd(t *testing.T) {
	e, _, now := bare(t)
	e.UseSlots(slots.New(1))
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)

	if out := acquire(e, a, "test"); out != "admitted" {
		t.Fatalf("a: %q", out)
	}
	if out := acquire(e, b, "test"); !strings.Contains(out, "(0 ahead)") {
		t.Fatalf("b: %q", out)
	}
	// b's run ends while it waits: it keeps its place.
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: b, Args: core.RunEndedArgs{}}); err != nil {
		t.Fatal(err)
	}
	if s := e.Slots.Snapshot(); len(s.Waiting) != 1 || s.Waiting[0].Owner != b {
		t.Fatalf("b lost its place: %+v", s)
	}
	// a's run ends: its slot goes to b, and b is told.
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: a, Args: core.RunEndedArgs{}}); err != nil {
		t.Fatal(err)
	}
	if s := e.Slots.Snapshot(); len(s.Held) != 1 || s.Held[0].Owner != b {
		t.Fatalf("after a's run: %+v", s)
	}
	if !told(e, b, "Your test job is admitted now") {
		th, _ := e.Thread(b)
		t.Fatalf("b was not told: %+v", th.Feed.Queue)
	}
	// b's grant woke no run: after 2 minutes the tick takes the slot back.
	*now = now.Add(time.Minute)
	e.Tick()
	if len(e.Slots.Snapshot().Held) != 1 {
		t.Fatal("released too early")
	}
	*now = now.Add(61 * time.Second)
	e.Tick()
	if s := e.Slots.Snapshot(); len(s.Held) != 0 {
		t.Fatalf("an idle grant kept its slot: %+v", s)
	}

}

func TestV086SlotsOff(t *testing.T) {
	e, _, _ := bare(t)
	a := bareThread(t, e)
	if out := acquire(e, a, "test"); out != "off" {
		t.Fatalf("%q", out)
	}
	// SPEC 0.5 V0.86 (1): slots 0 answers that heavy jobs are not limited and queues nothing.
	e.UseSlots(slots.New(0))
	b := bareThread(t, e)
	for _, id := range []string{a, b} {
		if out := acquire(e, id, "test"); out != "off" {
			t.Fatalf("%q", out)
		}
	}
	if s := e.Slots.Snapshot(); len(s.Held)+len(s.Waiting) != 0 {
		t.Fatalf("%+v", s)
	}
}

// SPEC 0.5 V0.86 (3): after a restart a thread that held a slot without a
// live run gives it back after 2 minutes; a queued one is told on its turn.
func TestV086SlotsAfterRestart(t *testing.T) {
	e, st, now := bare(t)
	a, b := bareThread(t, e), bareThread(t, e)
	for _, id := range []string{a, b} {
		running(e, id)
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{}}); err != nil {
			t.Fatal(err)
		}
	}
	p := slots.Open(1, st)
	p.Acquire(a, resource.Test)
	p.Acquire(b, resource.Test)

	e.UseSlots(slots.Open(1, st)) // the restarted daemon; neither thread runs
	e.Tick()
	*now = now.Add(2*time.Minute + time.Second)
	e.Tick()
	if s := e.Slots.Snapshot(); len(s.Held) != 1 || s.Held[0].Owner != b {
		t.Fatalf("after the idle holder: %+v", s)
	}
	if !told(e, b, "Your test job is admitted now") {
		t.Fatal("b was not told")
	}
}

// SPEC V0.137: the owner reorders the queue with host.slotOrder (owner only,
// with a receipt), and the next free slot goes to the thread now first; the
// self-deploy's priority wait stays ahead and an agent cannot reorder.
func TestV0137SlotOrderDecidesTheNextGrant(t *testing.T) {
	e, _, _ := bare(t)
	e.UseSlots(slots.New(1))
	a, b, c := bareThread(t, e), bareThread(t, e), bareThread(t, e)
	for _, id := range []string{a, b, c} {
		running(e, id)
		acquire(e, id, "test")
	}
	r := e.Command(api.Cmd{CommandID: "o1", Action: "host.slotOrder", Args: map[string]any{"owners": []any{c, b}}})
	if !r.OK {
		t.Fatalf("reorder: %+v", r)
	}
	if s, ok := r.Result.(resource.Snapshot); !ok || len(s.Waiting) != 2 || s.Waiting[0].Owner != c {
		t.Fatalf("the receipt is not the new queue: %#v", r.Result)
	}
	if out := acquire(e, b, "test"); !strings.Contains(out, "1 ahead") {
		t.Fatalf("b's place after the reorder: %q", out)
	}
	// An agent's input is refused.
	if _, err := e.Apply(core.Input{Source: core.FromAgent, Thread: b, Kind: "host.slotOrder", Args: map[string]any{"owners": []any{b, c}}}); err == nil {
		t.Fatal("an agent reordered the queue")
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: a, Args: core.RunEndedArgs{}}); err != nil {
		t.Fatal(err)
	}
	if s := e.Slots.Snapshot(); len(s.Held) != 1 || s.Held[0].Owner != c {
		t.Fatalf("the next slot did not go to c: %+v", s)
	}
	if !told(e, c, "Your test job is admitted now") || told(e, b, "is admitted now") {
		t.Fatal("the wrong thread was told")
	}
}

// V0.201: a slot outlives the run while a background task may carry the job,
// and goes back guard.SlotIdle after the task is done. A Monitor or a command gone
// quiet holds nothing.
func TestV201AdmissionOutlivesTheRunWhileTheJobRuns(t *testing.T) {
	quiet := time.Unix(1, 0)
	for _, c := range []struct {
		name string
		task core.Task
		keep bool
	}{
		{"background command", core.Task{ID: "b", Type: "local_bash", Background: true, Status: core.TaskRunning}, true},
		{"subagent", core.Task{ID: "a", Type: "local_agent", Status: core.TaskRunning}, true},
		{"monitor", core.Task{ID: "m", Type: "local_bash", Tool: "Monitor", Background: true, Status: core.TaskRunning}, false},
		{"quiet command", core.Task{ID: "q", Type: "local_bash", Background: true, Status: core.TaskRunning, QuietSince: &quiet}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _, now := bare(t)
			e.UseSlots(slots.New(1))
			a := bareThread(t, e)
			running(e, a)
			acquire(e, a, "test")
			coretest.Patch(e, a, func(th *core.Thread, _ time.Time) []core.Event {
				th.Tasks = []core.Task{c.task}
				return nil
			})
			if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: a, Args: core.RunEndedArgs{}}); err != nil {
				t.Fatal(err)
			}
			held := func() bool { return len(e.Slots.Snapshot().Held) == 1 }
			if held() != c.keep {
				t.Fatalf("held after the run: %v, want %v", held(), c.keep)
			}
			if !c.keep {
				return
			}
			*now = now.Add(10 * time.Minute)
			e.Tick()
			*now = now.Add(10 * time.Minute)
			e.Tick()
			if !held() {
				t.Fatal("the slot went back while the task ran")
			}
			coretest.Patch(e, a, func(th *core.Thread, _ time.Time) []core.Event {
				th.Tasks[0].Status = core.TaskCompleted
				return nil
			})
			e.Tick()
			*now = now.Add(guard.SlotIdle + time.Second)
			e.Tick()
			if held() {
				t.Fatal("the slot stayed after the task ended")
			}
		})
	}
}
