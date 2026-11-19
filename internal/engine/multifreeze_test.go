// V0.multi-freeze: while memory stays short the guard freezes one more thread
// each AddFreezeAfter, at most MaxFrozen; it thaws them one at a time, the
// newest first; and the turns of a frozen thread go to its heavy parts.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// tickFor ticks every 10 s for d.
func tickFor(e *Engine, now *time.Time, d time.Duration) {
	for end := now.Add(d); now.Before(end); {
		*now = now.Add(10 * time.Second)
		e.Tick()
	}
}

func TestMultiStillShortAMinuteLaterASecondThreadIsFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Second))}
	z.free = 0.04
	e.Tick()
	if !z.frozen[b] || len(z.frozen) != 1 {
		t.Fatalf("first freeze: the newest episode alone, got %v", z.frozen)
	}
	tickFor(e, now, AddFreezeAfter-10*time.Second)
	if len(z.frozen) != 1 {
		t.Fatalf("a second freeze before AddFreezeAfter: %v", z.frozen)
	}
	tickFor(e, now, 10*time.Second)
	if !z.frozen[a] || !z.frozen[b] {
		t.Fatalf("still short a minute later: both frozen, got %v", z.frozen)
	}
}

func TestMultiNoSecondFreezeOnceMemoryIsFine(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Second))}
	z.free = 0.04
	e.Tick()
	z.free = 0.12 // not short, not fine either
	tickFor(e, now, 2*AddFreezeAfter)
	if z.frozen[a] || !z.frozen[b] {
		t.Fatalf("one freeze is enough when memory is no longer short: %v", z.frozen)
	}
}

func TestMultiAtMostMaxFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	c, d := bareThread(t, e), bareThread(t, e)
	running(e, c)
	running(e, d)
	ids := append(th, c, d)
	for i, id := range ids {
		u.flags = append(u.flags, flagFor(id, now.Add(time.Duration(i)*time.Second)))
	}
	z.free = 0.04
	e.Tick()
	tickFor(e, now, 5*AddFreezeAfter)
	if len(z.frozen) != MaxFrozen || z.frozen[ids[0]] {
		t.Fatalf("frozen %v, want the %d newest", z.frozen, MaxFrozen)
	}
}

func TestMultiThawOneAtATimeNewestFirst(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Second))}
	z.free = 0.04
	e.Tick()
	tickFor(e, now, AddFreezeAfter)
	if len(z.frozen) != 2 {
		t.Fatalf("setup: %v", z.frozen)
	}
	z.free = 0.30
	e.Tick()
	tickFor(e, now, ThawAfterOK)
	if z.frozen[a] || !z.frozen[b] {
		t.Fatalf("the newest freeze (a) thaws first, alone: %v", z.frozen)
	}
	tickFor(e, now, ThawAfterOK-10*time.Second)
	if !z.frozen[b] {
		t.Fatal("the second thaw came before another ThawAfterOK of fine memory")
	}
	tickFor(e, now, 10*time.Second)
	if len(z.frozen) != 0 {
		t.Fatalf("still frozen: %v", z.frozen)
	}
}

func TestMultiTurnsSkipSmallParts(t *testing.T) {
	units := []slots.WorkUnit{{PID: 30, RSS: 1 << 30}, {PID: 31, RSS: 1 << 20}, {PID: 32, RSS: 1 << 30}}
	e, u, now, z, th, r := turnEngine(t, units...)
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	if r.last() != 30 {
		t.Fatalf("first turn %v", r.active)
	}
	*now = now.Add(TurnMin)
	e.Tick()
	if r.last() != 32 {
		t.Fatalf("the turn went to a 1 MB part: %v", r.active)
	}
}

func TestMultiSmallPartsOnlyStillTakeTurns(t *testing.T) {
	units := []slots.WorkUnit{{PID: 30, RSS: 1 << 20}, {PID: 31, RSS: 1 << 20}}
	if got := turnTakers(units); len(got) != 2 {
		t.Fatalf("no heavy part: all take turns, got %v", got)
	}
}
