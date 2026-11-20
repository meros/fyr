// V0.sim-fixes: the violations the load simulation (V0.load-sim) found in the
// guard and admission, each as a focused case.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// simFixGuard is an engine with two running threads a (the older hold) and b,
// whose SlotActive moves only while their job is not frozen, and signals the
// test sets. how is how b's hold was taken: "call" (a foreground call),
// "background", or "restart" (a hold with no call known, as after fyrd restarts).
type simFixGuard struct {
	e          *Engine
	now        *time.Time
	a, b       string
	frozen     map[string]bool
	active     map[string]time.Time
	free       float64
	agents     slots.Stall
	mem        uint64
	threadMem  map[string]uint64
	unit       *float64
	oomd       float64
	thawReason []string
}

func newSimFixGuard(t *testing.T, how string) *simFixGuard {
	e, _, now := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	g := &simFixGuard{e: e, now: now, frozen: map[string]bool{}, active: map[string]time.Time{}, free: 0.5, agents: slots.StallOK, threadMem: map[string]uint64{}}
	g.a, g.b = bareThread(t, e), bareThread(t, e)
	running(e, g.a)
	running(e, g.b)
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals {
		return slots.Signals{Provider: "cgroups", Headroom: &g.free, Stall: slots.StallOK, AgentsStall: g.agents, AgentsMem: g.mem, UnitStall: g.unit, OomdLimit: g.oomd}
	}
	e.SlotGrowth = func(o string) uint64 {
		if o == g.b {
			return 2e9
		}
		return 1e9
	}
	e.SlotActive = func(o string) (time.Time, bool) {
		if !g.frozen[o] {
			g.active[o] = *g.now
		}
		at, ok := g.active[o]
		return at, ok
	}
	e.SlotFreeze = func(o string) bool { g.frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(g.frozen, o) }
	e.SlotMem = func(o string) uint64 { return g.threadMem[o] }
	guardDeps(t, e)
	e.HeavyGate(HeavyCall{Thread: g.a, Class: "pytest", ToolUse: "tu-a"})
	*now = now.Add(time.Minute)
	switch how {
	case "call":
		e.HeavyGate(HeavyCall{Thread: g.b, Class: "pytest", ToolUse: "tu-b"})
	case "background":
		e.HeavyGate(HeavyCall{Thread: g.b, Class: "pytest", ToolUse: "tu-b", Background: true})
	case "restart":
		p.Acquire(g.b, slots.Test)
	}
	if _, ok := p.Holding(g.b); !ok {
		t.Fatalf("%s: b not admitted", how)
	}
	return g
}

func (g *simFixGuard) holds(o string) bool {
	_, ok := g.e.Slots.Holding(o)
	return ok
}

// The two-growers, background and restart-frozen rows of V0.load-sim: b was
// frozen in the agents' critical stall, its SlotActive stood still, and
// sweepSlots dropped its hold (15 min for a call, 2 min for a background job or
// after a restart); the guard then thawed it as "episode or hold ended" into the
// same critical stall.
func TestSimFixesAFrozenJobKeepsItsHold(t *testing.T) {
	for _, how := range []string{"call", "background", "restart"} {
		g := newSimFixGuard(t, how)
		g.agents, g.mem = slots.StallCritical, 11e9
		g.e.Tick()
		if !g.frozen[g.b] {
			t.Fatalf("%s: b not frozen: %v", how, g.frozen)
		}
		tickFor(g.e, g.now, HeavyCallStale+time.Minute)
		if !g.frozen[g.b] || !g.holds(g.b) {
			t.Fatalf("%s: after %s in a critical stall: frozen %v, b holds %t", how, HeavyCallStale+time.Minute, g.frozen, g.holds(g.b))
		}
		// The stall falls: b wakes, and its idle clock starts at the thaw, not
		// at the last time it ran before the freeze.
		g.agents, g.mem = slots.StallOK, 4e9
		tickFor(g.e, g.now, ThawAfterOK+10*time.Second)
		if g.frozen[g.b] {
			t.Fatalf("%s: not woken", how)
		}
		g.active[g.b] = g.active[g.b].Add(-time.Hour) // what SlotActive read last: before the freeze
		g.frozen[g.b] = true                          // SlotActive does not move until its job runs again
		tickFor(g.e, g.now, 10*time.Second)
		delete(g.frozen, g.b)
		if !g.holds(g.b) {
			t.Fatalf("%s: the hold was dropped at the first sweep after its thaw", how)
		}
	}
}

// A hold that ends while its job is frozen (the call's result came) ends, but
// the job does not thaw while the agents' memory stalls critically.
func TestSimFixesAHoldThatEndsWhileFrozenThawsByTheStallRule(t *testing.T) {
	g := newSimFixGuard(t, "call")
	g.agents, g.mem = slots.StallCritical, 11e9
	g.e.Tick()
	if !g.frozen[g.b] {
		t.Fatalf("b not frozen: %v", g.frozen)
	}
	g.e.HeavyDone(g.b, "tu-b")
	if g.holds(g.b) {
		t.Fatal("the hold did not end with its call")
	}
	tickFor(g.e, g.now, time.Minute)
	if !g.frozen[g.b] {
		t.Fatal("thawed into the critical stall when its hold ended")
	}
	g.agents = slots.StallElevated
	tickFor(g.e, g.now, 10*time.Second)
	if g.frozen[g.b] {
		t.Fatal("still frozen once the stall is below critical")
	}
}

// The leak and swap-full rows of V0.load-sim: maxFrozen counted agents that
// run no heavy work, so the only heavy work was frozen for their sake and
// nothing ran until the 10 min thaw. The cap is now the threads with heavy
// work less one, and the last heavy work that runs is never frozen.
func TestSimFixesTheLastRunningHeavyWorkIsNeverFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	third(t, e)
	third(t, e)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.agents = 0.04, slots.StallCritical
	tickFor(e, now, 2*AddFreezeAfter)
	if len(z.frozen) != 0 {
		t.Fatalf("froze the only heavy work, for three idle agents: %v", z.frozen)
	}
	u.flags = append(u.flags, flagFor(b, now.Add(time.Second)))
	tickFor(e, now, 3*AddFreezeAfter)
	if !z.frozen[b] || z.frozen[a] {
		t.Fatalf("two threads with heavy work and two idle: the newer alone frozen, got %v", z.frozen)
	}
}

// The timed thaw at FreezeMax counts the frozen work's memory that is out of
// RAM: it comes back with the thaw. 2.8 GB of the 12 GB budget in swap and
// 15 % free would leave the agents under their 10 % reserve.
func TestSimFixesTheTimedThawCountsTheMemoryThatComesBack(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	e.Slots.Acquire(th[1], slots.Test)
	resident := uint64(2.8e9)
	e.SlotMem = func(string) uint64 { return resident }
	inner := e.SlotSignals
	e.SlotSignals = func() slots.Signals { s := inner(); s.Memory = 12e9; return s }
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.agents = 0.04, slots.StallCritical
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen")
	}
	resident = 0.2e9 // the reclaim moved 2.6 GB of it to swap
	z.free, z.agents = 0.15, slots.StallElevated
	tickFor(e, now, FreezeMax+time.Minute)
	if !z.frozen[a] {
		t.Fatal("thawed at the 10 min limit with no room for the 2.6 GB that comes back")
	}
	z.free = 0.40
	tickFor(e, now, 10*time.Second)
	if z.frozen[a] {
		t.Fatal("not thawed once the 2.6 GB fits")
	}
}

// The near-oomd and unknown-limit rows of V0.load-sim: the guard froze on the
// headroom alone while nothing stalled. The kernel was coping (page cache,
// swap); the frozen memory stayed charged and held the host short. Short
// headroom with no stall now holds new heavy commands at the gate and freezes
// nothing; a stall (elevated, the agents' or the host's) freezes as before.
func TestSimFixesNoFreezeOnTheHeadroomAloneWhileNothingStalls(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	e.Slots.Acquire(th[1], slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.stall, z.agents = 0.04, slots.StallOK, slots.StallOK
	tickFor(e, now, 2*AddFreezeAfter)
	if len(z.frozen) != 0 || len(z.slotted) != 0 {
		t.Fatalf("froze on the headroom alone: %v %v", z.frozen, z.slotted)
	}
	z.agents = slots.StallElevated
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen once the agents' memory stalls")
	}
}

// The turns-4x4GB row of V0.load-sim: the wake rule of work taking turns
// counted the thread's memory at the freeze, the part whose turn it is and the
// parts that ended since included ("agents=0.8 GB + its 11.2 GB >= cap=12.0 GB"
// with one part left). It counts the frozen parts' memory only.
func TestSimFixesTheWakeRuleOfTurnsCountsTheFrozenPartsOnly(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	a := th[0]
	e.Slots.Acquire(th[1], slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)} // 2.8 GB at the freeze
	resident := uint64(3.2e9)
	e.SlotMem = func(string) uint64 { return resident }
	z.free, z.agents, z.mem = 0.04, slots.StallCritical, 10e9 // the cap: 10 GB
	e.Tick()
	if e.first().active != 30 {
		t.Fatalf("no turns: %+v", e.first())
	}
	// Two parts end; the one whose turn it is runs on, nothing else is frozen.
	r.units = r.units[:1]
	resident = 1.4e9
	z.free, z.stall, z.agents, z.mem = 0.04, slots.StallOK, slots.StallOK, 9.0e9
	tickFor(e, now, ThawAfterOK+10*time.Second)
	if len(e.frozenOwners()) != 0 {
		t.Fatalf("not woken with no part frozen: 9.0 GB < cap 10 GB; frozen %+v", e.first())
	}
}

func (e *Engine) frozenOwners() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, f := range e.frozen {
		out = append(out, f.owner)
	}
	return out
}

// The near-oomd row: the two threads that ran ended and the frozen one waited
// for 30 s of calm with nothing else running. Frozen work that is all the
// heavy work left wakes at once, unless memory stalls critically.
func TestSimFixesFrozenWorkLeftAloneWakesAtOnce(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.agents = 0.04, slots.StallCritical
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen")
	}
	e.Slots.Release(b) // b's job ended
	tickFor(e, now, 10*time.Second)
	if !z.frozen[a] {
		t.Fatal("woke into a critical stall")
	}
	z.agents = slots.StallElevated
	tickFor(e, now, 10*time.Second)
	if z.frozen[a] {
		t.Fatal("still frozen with no other heavy work running")
	}
}

// shortStall is the host's stall a test reports with free headroom: elevated
// under the 10 % reserve, else ok (V0.sim-fixes: a freeze needs a stall).
func shortStall(free float64) slots.Stall {
	if free < slots.DefaultPolicy.MemoryReserve {
		return slots.StallElevated
	}
	return slots.StallOK
}
