package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

// snoozedRun is a thread with a live run, snoozed until its work is done, and
// a new engine on the same store: fyrd after a restart. kept: the run outlived it.
func snoozedRun(t *testing.T, kept bool) (*Engine, string) {
	t.Helper()
	e, st, _ := bare(t)
	id := bareThread(t, e)
	running(e, id)
	if _, err := e.Apply(core.Input{Source: core.FromOwner, Kind: "thread.snoozeWhenDone", Thread: id}); err != nil {
		t.Fatal(err)
	}
	if d := dtoOf(e, id); !d.View.SnoozeWhenDone || d.View.SnoozeUntil == nil || d.Attention.State != core.Snoozed {
		t.Fatalf("view %+v attention %+v", d.View, d.Attention)
	}
	e2 := New(st)
	e2.Now = func() time.Time { return e.Now().Add(time.Minute) }
	e2.Kept = map[string]bool{id: kept}
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	return e2, id
}

// V0.185: the wire says "until work is done", and the snooze survives a
// restart of fyrd that keeps the agent's turn.
func TestSnoozeDoneWireAndRestart(t *testing.T) {
	e, id := snoozedRun(t, true)
	if d := dtoOf(e, id); !d.View.SnoozeWhenDone || d.Attention.State != core.Snoozed || d.Attention.Reason != "until work is done" {
		t.Fatalf("after the restart: %+v %+v", d.View, d.Attention)
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{}}); err != nil {
		t.Fatal(err)
	}
	if d := dtoOf(e, id); d.View.SnoozeWhenDone || d.Attention.State == core.Snoozed {
		t.Fatalf("after the run: %+v %+v", d.View, d.Attention)
	}
}

// A run that a restart lost is a failure: the thread wakes and says so.
func TestSnoozeDoneWakesWhenARestartLosesTheRun(t *testing.T) {
	e, id := snoozedRun(t, false)
	if d := dtoOf(e, id); d.View.SnoozeWhenDone || d.Attention.State != core.Failed {
		t.Fatalf("after the restart: %+v %+v", d.View, d.Attention)
	}
}

// The wake is an attention change like any snooze's end: done, so it pushes.
func TestSnoozeDoneWakeNotifies(t *testing.T) {
	e, _, now := bare(t)
	id := bareThread(t, e)
	running(e, id)
	if _, err := e.Apply(core.Input{Source: core.FromOwner, Kind: "thread.snoozeWhenDone", Thread: id}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	var prev, next core.State
	e.OnAttention = func(d api.Thread, p core.State) {
		if d.ID == id {
			prev, next = p, d.Attention.State
		}
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{}}); err != nil {
		t.Fatal(err)
	}
	if prev != core.Snoozed || next != core.Done {
		t.Fatalf("attention %q -> %q", prev, next)
	}
}

// The queue lives in the slot pool; the thread's record follows it, so a
// queued thread stays snoozed after its run and wakes when its turn is over.
func TestSnoozeDoneFollowsTheSlotQueue(t *testing.T) {
	e, _, _ := bare(t)
	e.UseSlots(slots.New(1))
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	acquire(e, a, "test")
	acquire(e, b, "test")
	if th, _ := e.Thread(b); !th.SlotQueued {
		t.Fatal("the record does not say queued")
	}
	if _, err := e.Apply(core.Input{Source: core.FromOwner, Kind: "thread.snoozeWhenDone", Thread: b}); err != nil {
		t.Fatal(err)
	}
	end := func(id string) {
		t.Helper()
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{}}); err != nil {
			t.Fatal(err)
		}
	}
	end(b)
	if d := dtoOf(e, b); !d.View.SnoozeWhenDone {
		t.Fatalf("woke while queued: %+v", d.Attention)
	}
	// Leaving the queue by any path is noticed at the tick.
	e.Slots.Release(b)
	e.Tick()
	if th, _ := e.Thread(b); th.SlotQueued {
		t.Fatal("the record still says queued")
	}
	if d := dtoOf(e, b); d.View.SnoozeWhenDone {
		t.Fatalf("still snoozed after the queue: %+v", d.Attention)
	}
}
