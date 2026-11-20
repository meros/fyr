// V0.guard-agents-stall: the agents' own memory stall, the figure oomd
// compares against its limit, makes memory short on its own; and near
// oomd's limit the next freeze does not wait AddFreezeAfter.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// Decision 0032's incident: the agents stalled at 53 % while the host read 0.1 %.
func TestAgentsStallCriticalFreezesWithTheHostFine(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	e.Slots.Acquire(th[1], slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.stall, z.agents = 0.30, slots.StallOK, slots.StallCritical
	e.Tick()
	if !z.frozen[a] {
		t.Fatalf("the agents' stall is critical: the undeclared work is frozen, got %v", z.frozen)
	}
	if !e.GuardView().Short {
		t.Fatal("the guard does not say memory is short")
	}
}

func TestAgentsStallElevatedIsNotFine(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	e.Slots.Acquire(th[1], slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.agents = 0.30, slots.StallCritical
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("setup: not frozen")
	}
	z.agents = slots.StallElevated // the host is fine, the agents still stall
	tickFor(e, now, 3*ThawAfterOK)
	if !z.frozen[a] {
		t.Fatal("woke while the agents' stall is elevated")
	}
	z.agents = slots.StallOK
	tickFor(e, now, ThawAfterOK+10*time.Second)
	if z.frozen[a] {
		t.Fatal("still frozen with both stalls ok")
	}
}

func TestNearOomdsLimitTheNextFreezeDoesNotWait(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	c := third(t, e)
	e.Slots.Acquire(c, slots.Test) // V0.sim-fixes (3): a third thread with heavy work, so two may be frozen
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Second))}
	unit := 30.0
	z.free, z.agents, z.unit, z.oomd = 0.30, slots.StallCritical, &unit, 60
	e.Tick()
	if !z.frozen[b] || len(z.frozen) != 1 {
		t.Fatalf("first freeze: %v", z.frozen)
	}
	tickFor(e, now, 10*time.Second)
	if len(z.frozen) != 1 {
		t.Fatalf("20 points under oomd's limit: the second freeze waits AddFreezeAfter, got %v", z.frozen)
	}
	unit = 52 // within 10 points of 60
	tickFor(e, now, 10*time.Second)
	if !z.frozen[a] || !z.frozen[b] {
		t.Fatalf("near oomd's limit: the second freeze at the next tick, got %v", z.frozen)
	}
}
