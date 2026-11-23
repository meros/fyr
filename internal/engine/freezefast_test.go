// V0.freeze-fast-class-name: when the PSI trigger fired and the agents' memory
// stalls critically, the guard freezes the work that grew most at once instead
// of waiting the detector's window (45 s) for a flag. On 20 Nov the trigger
// fired at 07:32:12 and the work was frozen at 07:33:01, as the desktop froze.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// growing gives the engine a thread whose work grew over the memory
// threshold and is not flagged yet: FlagNow flags it.
func growing(t *testing.T, e *Engine, u *und, thread string, now *time.Time) *int {
	t.Helper()
	asked := new(int)
	e.UndeclaredFlagNow = func() string {
		*asked++
		for _, f := range u.flags {
			if f.Thread == thread {
				return ""
			}
		}
		u.flags = append(u.flags, flagFor(thread, *now))
		return thread
	}
	guardDeps(t, e) // V0.182: never called under e.mu
	return asked
}

func TestFreezeFastTheTriggerWithACriticalStallFreezesAtOnce(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	growing(t, e, u, a, now)
	e.Slots.Acquire(th[1], resource.Test) // V0.sim-fixes (3): not the only heavy work
	z.free, z.agents = 0.30, resource.StallCritical
	*now = now.Add(time.Second)
	e.GuardNow()
	if !z.frozen[a] {
		t.Fatalf("the trigger fired and the agents stall critically: the growing work is frozen at once, got %v", z.frozen)
	}
}

func TestFreezeFastNearOomdsLimitWithTheTrigger(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	growing(t, e, u, a, now)
	e.Slots.Acquire(th[1], resource.Test) // V0.sim-fixes (3): not the only heavy work
	unit := 52.0
	z.free, z.agents, z.unit, z.oomd = 0.30, resource.StallElevated, &unit, 60
	e.GuardNow()
	if !z.frozen[a] {
		t.Fatalf("near oomd's limit after the trigger: frozen at once, got %v", z.frozen)
	}
}

func TestFreezeFastWithoutTheTriggerTheWindowHolds(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	asked := growing(t, e, u, a, now)
	z.free, z.agents = 0.30, resource.StallCritical
	tickFor(e, now, 30*time.Second)
	if len(z.frozen) != 0 || *asked != 0 {
		t.Fatalf("no trigger: the guard waits for the detector's flag, got frozen=%v asked=%d", z.frozen, *asked)
	}
}

func TestFreezeFastAnElevatedStallWithoutTheTriggerWaits(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	asked := growing(t, e, u, a, now)
	z.free, z.agents = 0.30, resource.StallElevated
	tickFor(e, now, 30*time.Second)
	if len(z.frozen) != 0 || *asked != 0 {
		t.Fatalf("no trigger, the agents' stall only elevated: the old wait holds, got frozen=%v asked=%d", z.frozen, *asked)
	}
}

func TestFreezeFastAnOldTriggerDoesNotCount(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	z.free, z.agents = 0.30, resource.StallElevated
	e.GuardNow() // nothing grows yet
	tickFor(e, now, resource.TriggerFresh+10*time.Second)
	asked := growing(t, e, u, a, now)
	z.agents = resource.StallCritical // critical by avg10, long after the trigger
	e.Tick()
	if len(z.frozen) != 0 || *asked != 0 {
		t.Fatalf("a trigger %s old: the old wait holds, got frozen=%v asked=%d", resource.TriggerFresh, z.frozen, *asked)
	}
}

// The other rules hold: a service is never frozen.
func TestFreezeFastNeverAService(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	growing(t, e, u, a, now)
	e.Slots.Acquire(th[1], resource.Test) // V0.sim-fixes (3): not the only heavy work
	e.GuardService = func(string) bool { return true }
	z.free, z.agents = 0.30, resource.StallCritical
	e.GuardNow()
	if z.frozen[a] {
		t.Fatal("froze a service")
	}
}

// V0.sim-fixes (3): the trigger does not lift "one always runs": the growing
// work flagged early is not frozen whole while it is the only heavy work.
func TestFreezeFastNeverTheOnlyHeavyWork(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	asked := growing(t, e, u, a, now)
	z.free, z.agents = 0.30, resource.StallCritical
	*now = now.Add(time.Second)
	e.GuardNow()
	if *asked == 0 {
		t.Fatal("the trigger did not flag the growing work")
	}
	if len(z.frozen) != 0 {
		t.Fatalf("froze the only heavy work that runs: %v", z.frozen)
	}
}

// The 20 Nov timeline: the trigger fired at 07:32:12 while avg10 still read
// "elevated" (the agents' stall went critical only at 07:33:01, headroom
// 64 %). A fresh trigger is a critical stall (10 %, the trigger's own level),
// so the growing work is frozen by the trigger plus one tick.
func TestFreezeFastTheTwentyNovemberTimeline(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	growing(t, e, u, a, now)
	e.Slots.Acquire(th[1], resource.Test) // held=1 that morning
	z.free, z.stall, z.agents = 0.64, resource.StallElevated, resource.StallElevated
	t0 := *now
	e.GuardNow()
	if !z.frozen[a] {
		tickFor(e, now, 10*time.Second)
	}
	if !z.frozen[a] {
		t.Fatalf("trigger at t0 with avg10 elevated: not frozen by t0+%s, got %v", now.Sub(t0), z.frozen)
	}
	if !e.GuardView().Short {
		t.Fatal("a fresh trigger is a critical stall: memory is short")
	}
}

// Past resource.TriggerFresh with avg10 still elevated, memory is no longer short by
// the trigger.
func TestFreezeFastTheTriggerCountsForTriggerFreshOnly(t *testing.T) {
	e, _, now, z, _ := fzEngine(t)
	z.free, z.agents = 0.64, resource.StallElevated
	e.GuardNow()
	if !e.GuardView().Short {
		t.Fatal("not short at the trigger")
	}
	tickFor(e, now, resource.TriggerFresh+10*time.Second)
	if e.GuardView().Short {
		t.Fatal("still short past resource.TriggerFresh with avg10 elevated")
	}
}
