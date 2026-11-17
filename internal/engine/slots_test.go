// SPEC 0.5 V0.86: slot_acquire answers at once, a queued thread is told
// when the slot is its own, and a held slot goes back when the run ends.
package engine

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
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

	out, isErr := callAs(t, e, a, "slot_acquire", map[string]any{"kind": "test"})
	if isErr || !strings.Contains(out, "You hold a test slot") {
		t.Fatalf("a: %v %q", isErr, out)
	}
	out, isErr = callAs(t, e, b, "slot_acquire", map[string]any{"kind": "test"})
	if isErr || !strings.Contains(out, "Queued for a test slot: 0 ahead") {
		t.Fatalf("b: %v %q", isErr, out)
	}
	if _, isErr := callAs(t, e, b, "slot_acquire", map[string]any{"kind": "nap"}); !isErr {
		t.Fatal("an unknown kind was accepted")
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
	if !told(e, b, "A test slot is yours now") {
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

	// slot_release gives it back at once.
	running(e, a)
	callAs(t, e, a, "slot_acquire", map[string]any{"kind": "build"})
	out, isErr = callAs(t, e, a, "slot_release", map[string]any{})
	if isErr || len(e.Slots.Snapshot().Held) != 0 {
		t.Fatalf("release: %v %q", isErr, out)
	}
}

func TestV086SlotsOff(t *testing.T) {
	e, _, _ := bare(t)
	a := bareThread(t, e)
	out, isErr := callAs(t, e, a, "slot_acquire", map[string]any{"kind": "test"})
	if isErr || !strings.Contains(out, "no slots") {
		t.Fatalf("%v %q", isErr, out)
	}
	// SPEC 0.5 V0.86 (1): slots 0 answers "no limit" and queues nothing.
	e.UseSlots(slots.New(0))
	b := bareThread(t, e)
	for _, id := range []string{a, b} {
		out, isErr := callAs(t, e, id, "slot_acquire", map[string]any{"kind": "test"})
		if isErr || !strings.Contains(out, "no limit") {
			t.Fatalf("%v %q", isErr, out)
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
	p.Acquire(a, slots.Test)
	p.Acquire(b, slots.Test)

	e.UseSlots(slots.Open(1, st)) // the restarted daemon; neither thread runs
	e.Tick()
	*now = now.Add(2*time.Minute + time.Second)
	e.Tick()
	if s := e.Slots.Snapshot(); len(s.Held) != 1 || s.Held[0].Owner != b {
		t.Fatalf("after the idle holder: %+v", s)
	}
	if !told(e, b, "A test slot is yours now") {
		t.Fatal("b was not told")
	}
}

// SPEC V0.136: the owner reorders the queue with host.slotOrder (owner only,
// with a receipt), and the next free slot goes to the thread now first; the
// self-deploy's priority wait stays ahead and an agent cannot reorder.
func TestV0136SlotOrderDecidesTheNextGrant(t *testing.T) {
	e, _, _ := bare(t)
	e.UseSlots(slots.New(1))
	a, b, c := bareThread(t, e), bareThread(t, e), bareThread(t, e)
	for _, id := range []string{a, b, c} {
		running(e, id)
		callAs(t, e, id, "slot_acquire", map[string]any{"kind": "test"})
	}
	r := e.Command(api.Cmd{CommandID: "o1", Action: "host.slotOrder", Args: map[string]any{"owners": []any{c, b}}})
	if !r.OK {
		t.Fatalf("reorder: %+v", r)
	}
	if s, ok := r.Result.(slots.Snapshot); !ok || len(s.Waiting) != 2 || s.Waiting[0].Owner != c {
		t.Fatalf("the receipt is not the new queue: %#v", r.Result)
	}
	out, _ := callAs(t, e, b, "slot_acquire", map[string]any{"kind": "test"})
	if !strings.Contains(out, "1 ahead") {
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
	if !told(e, c, "A test slot is yours now") || told(e, b, "slot is yours") {
		t.Fatal("the wrong thread was told")
	}
}
