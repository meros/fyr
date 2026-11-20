// Decision 0032 slice 2: the parts of a thread's undeclared work take turns.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

type turns struct {
	units  []slots.WorkUnit
	active []int // the active pid of every rotate call
	faults uint64
	refuse bool
}

func turnEngine(t *testing.T, units ...slots.WorkUnit) (*Engine, *und, *time.Time, *fz, []string, *turns) {
	e, u, now, z, th := fzEngine(t)
	r := &turns{units: units}
	e.UndeclaredUnits = func(string) []slots.WorkUnit { return r.units }
	e.UndeclaredRotate = func(_ string, _ []slots.WorkUnit, active int) bool {
		if r.refuse {
			return false
		}
		r.active = append(r.active, active)
		return true
	}
	e.UndeclaredFaults = func(string) uint64 { return r.faults }
	guardDeps(t, e)
	return e, u, now, z, th, r
}

func (r *turns) last() int { return r.active[len(r.active)-1] }

var threeUnits = []slots.WorkUnit{{PID: 30, RSS: 1 << 30}, {PID: 31, RSS: 1 << 30}, {PID: 32, RSS: 1 << 30}}

func TestT1SeveralPartsTakeTurnsInsteadOfAllStopping(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	if len(z.frozen) != 0 || len(r.active) != 1 || r.last() != 30 {
		t.Fatalf("whole freeze %v, turns %v", z.frozen, r.active)
	}
	if v := e.first(); v.owner != th[0] || !v.undeclared || v.active != 30 {
		t.Fatalf("%+v", v)
	}
}

func TestT2TheTurnMovesWhenItIsOverAndWrapsAround(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	*now = now.Add(TurnMin - time.Second)
	e.Tick()
	if r.last() != 30 {
		t.Fatalf("moved early: %v", r.active)
	}
	for _, want := range []int{31, 32, 30} {
		*now = now.Add(TurnMin)
		e.Tick()
		if r.last() != want {
			t.Fatalf("turns %v, want %d next", r.active, want)
		}
	}
}

func TestT3APartThatEndedGivesUpItsTurnAtOnce(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	r.units = threeUnits[1:]
	*now = now.Add(time.Second)
	e.Tick()
	if r.last() != 31 {
		t.Fatalf("%v", r.active)
	}
}

func TestT4ATurnPaysForTheSwapIn(t *testing.T) {
	if got := turnLength(1<<30, 0); got != TurnMin {
		t.Fatalf("no rate: %s", got)
	}
	if got := turnLength(1<<30, 100<<20); got != 30720*time.Millisecond {
		t.Fatalf("1 GB at 100 MB/s: %s", got)
	}
	if got := turnLength(1<<20, 1<<30); got != TurnMin {
		t.Fatalf("floor: %s", got)
	}
	if got := turnLength(1<<40, 1<<20); got != TurnMax {
		t.Fatalf("ceiling: %s", got)
	}
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	*now = now.Add(TurnMin)
	r.faults = 1 << 30 // 1 GB read back in one turn of 30 s: the next 1 GB part gets three times that
	e.Tick()
	if d := e.first().turn - 90*time.Second; d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("turn %s", e.first().turn)
	}
}

func TestT5OnePartIsFrozenWhole(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits[0])
	e.Slots.Acquire(th[1], slots.Test) // V0.sim-fixes (3): not the only heavy work
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	if !z.frozen[th[0]] || len(r.active) != 0 || e.first().active != 0 {
		t.Fatalf("frozen %v turns %v", z.frozen, r.active)
	}
}

func TestT6RefusedTurnsFallBackToTheWholeFreeze(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	r.refuse = true
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	if !z.frozen[th[0]] || e.first().active != 0 {
		t.Fatalf("frozen %v, %+v", z.frozen, e.frozen)
	}
}

func TestT7TheThawEndsTheTurns(t *testing.T) {
	e, u, now, z, th, r := turnEngine(t, threeUnits...)
	thawed := 0
	e.UndeclaredThaw = func(string) { thawed++ }
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	z.free = 0.30
	e.Tick()
	*now = now.Add(ThawAfterOK)
	calls := len(r.active)
	e.Tick()
	if thawed != 1 || e.first().owner != "" || len(r.active) != calls {
		t.Fatalf("thawed %d, frozen %+v, turns %v", thawed, e.frozen, r.active)
	}
}

// first is the guard's oldest freeze, or none.
func (e *Engine) first() frozenJob {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.frozen) == 0 {
		return frozenJob{}
	}
	return e.frozen[0]
}
