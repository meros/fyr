// Decision 0029 slice S5 (#189), V0.load-tuning: under the memory guard the slotted job that grew most is frozen (the
// newest without a reading, and then never the oldest: one job always runs), one at a time; it thaws when memory has recovered for 30 s, after 10 min unless memory stalls critically (V0.oomd-guard), or when its slot
// goes back; its thread is told both times.
package engine

import (
	"bytes"
	"log"
	"os"
	"strings"
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
	guardDeps(t, e)

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
	guardDeps(t, e)
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
	// V0.oomd-guard (decision 0036 E2): not while memory stalls critically.
	stall := slots.StallCritical
	e.SlotSignals = func() slots.Signals { return slots.Signals{Provider: "cgroups", Headroom: &free, Stall: stall} }
	*now = now.Add(10*time.Minute + time.Second)
	e.Tick()
	if !frozen[b] || !told(e, b, "Your heavy job stays frozen past 10 min") {
		t.Fatal("thawed at 10 min into a critical stall")
	}
	stall = slots.StallElevated
	e.Tick()
	if frozen[b] {
		t.Fatal("still frozen after 10 min")
	}
}

// V0.load-tuning: the job whose memory grew most is frozen. V0.guard-wake (decision 0036 E4): never the oldest, which
// always runs, even when it grew most.
func TestGuardFreezesTheJobThatGrewMostButNeverTheOldest(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(3)
	p.Now = e.Now
	e.UseSlots(p)
	big, mid, idle := bareThread(t, e), bareThread(t, e), bareThread(t, e)
	for _, id := range []string{big, mid, idle} {
		running(e, id)
	}
	p.Acquire(big, slots.Test)
	*now = now.Add(time.Minute)
	p.Acquire(mid, slots.Test)
	*now = now.Add(time.Minute)
	p.Acquire(idle, slots.Test)
	free := 0.05
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals { return slots.Signals{Provider: "cgroups", Headroom: &free, Stall: slots.StallOK} }
	e.SlotGrowth = func(o string) uint64 {
		switch o {
		case big:
			return 3 << 30
		case mid:
			return 1 << 30
		}
		return 0
	}
	frozen := map[string]bool{}
	e.SlotFreeze = func(o string) bool { frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(frozen, o) }
	e.SlotMem = func(string) uint64 { return 2 << 30 }
	e.FrozenUsec = func(string, bool) (uint64, bool) { return 0, true }
	guardDeps(t, e)
	e.Tick()
	if !frozen[mid] || frozen[big] || frozen[idle] {
		t.Fatalf("frozen %v, want the job that grew most of the younger ones (%s), not the oldest (%s)", frozen, mid, big)
	}
}

// V0.guard-wake (decision 0036 (D), E4): a service is never frozen.
func TestGuardNeverFreezesAService(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(3)
	p.Now = e.Now
	e.UseSlots(p)
	a, svc, c := bareThread(t, e), bareThread(t, e), bareThread(t, e)
	for _, id := range []string{a, svc, c} {
		running(e, id)
	}
	p.Acquire(a, slots.Test)
	*now = now.Add(time.Minute)
	p.Acquire(c, slots.Test)
	*now = now.Add(time.Minute)
	p.Acquire(svc, slots.E2E) // the newest: it would be frozen first
	free := 0.05
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals { return slots.Signals{Provider: "cgroups", Headroom: &free, Stall: slots.StallOK} }
	e.SlotGrowth = func(string) uint64 { return 0 }
	e.GuardService = func(th string) bool { return th == svc }
	frozen := map[string]bool{}
	e.SlotFreeze = func(o string) bool { frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(frozen, o) }
	guardDeps(t, e)
	e.Tick()
	if frozen[svc] || !frozen[c] {
		t.Fatalf("frozen %v: the service %s must run, %s is next", frozen, svc, c)
	}
}

// V0.load-tuning: an admission after a wait says how long the job waited.
func TestAdmissionAfterAQueueLogsTheWait(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(1)
	p.Now = e.Now
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	p.Acquire(a, slots.Test)
	p.Acquire(b, slots.E2E)
	e.syncSlotQueue()
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	*now = now.Add(90 * time.Second)
	p.Release(a)
	e.syncSlotQueue()
	if want := "slots: admitted thread=" + b + " kind=e2e waited=1m30s"; !strings.Contains(out.String(), want) {
		t.Fatalf("log %q, want %q", out.String(), want)
	}
}
