// Decision 0029 slice S5 (#189): under the memory guard the newest slotted job is frozen (never the oldest: one job
// always runs), one at a time; it thaws when memory has recovered for 30 s, after 10 min at most, or when its slot
// goes back; its thread is told both times.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

func TestS5MemoryGuardFreezesTheNewestJob(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(3)
	p.Now = e.Now
	e.UseSlots(p)
	a, b, c := bareThread(t, e), bareThread(t, e), bareThread(t, e)
	for _, id := range []string{a, b, c} {
		running(e, id)
	}
	start := *now
	p.Acquire(a, slots.Test)
	*now = start.Add(time.Minute)
	p.Acquire(b, slots.Test)
	*now = start.Add(2 * time.Minute)
	p.Acquire(c, slots.E2E)
	free := 0.05
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals { return slots.Signals{Provider: "cgroups", Headroom: &free, Stall: slots.StallOK} }
	e.SlotGrowth = func(string) uint64 { return 0 }
	frozen := map[string]bool{}
	e.SlotFreeze = func(o string) bool { frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(frozen, o) }

	e.Tick()
	if !frozen[c] || len(frozen) != 1 || !told(e, c, "Fyr paused your heavy job") {
		th, _ := e.Thread(c)
		t.Fatalf("frozen %v (c %s) feed %+v", frozen, c, th.Feed.Queue)
	}
	e.Tick()
	if len(frozen) != 1 {
		t.Fatalf("a second job frozen %v", frozen)
	}
	// Memory recovers: not before 30 s of it.
	free = 0.5
	*now = now.Add(10 * time.Second)
	e.Tick()
	if !frozen[c] {
		t.Fatal("thawed before 30 s of recovered memory")
	}
	*now = now.Add(31 * time.Second)
	e.Tick()
	if frozen[c] || !told(e, c, "Your heavy job runs again") {
		t.Fatalf("not thawed after 30 s %v", frozen)
	}
}

func TestS5AFreezeEndsAfterTenMinutesAndNeverTakesTheLastJob(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	p.Acquire(a, slots.Test)
	free := 0.02
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals { return slots.Signals{Provider: "cgroups", Headroom: &free} }
	e.SlotGrowth = func(string) uint64 { return 0 }
	frozen := map[string]bool{}
	e.SlotFreeze = func(o string) bool { frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(frozen, o) }
	e.Tick()
	if len(frozen) != 0 {
		t.Fatal("the only running job was frozen")
	}
	*now = now.Add(time.Minute)
	p.Acquire(b, slots.Test) // memory is short, but one job always runs; b was admitted before (here: by hand)
	e.Tick()
	if !frozen[b] {
		t.Fatal("the newest of two was not frozen")
	}
	*now = now.Add(10*time.Minute + time.Second)
	e.Tick()
	if frozen[b] {
		t.Fatal("still frozen after 10 min")
	}
}
