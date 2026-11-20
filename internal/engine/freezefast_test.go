// V0.freeze-fast-class-name: when the PSI trigger fired and the agents' memory
// stalls critically, the guard freezes the work that grew most at once instead
// of waiting the detector's window (45 s) for a flag. On 20 Nov the trigger
// fired at 07:32:12 and the work was frozen at 07:33:01, as the desktop froze.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
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
	z.free, z.agents = 0.30, slots.StallCritical
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
	unit := 52.0
	z.free, z.agents, z.unit, z.oomd = 0.30, slots.StallElevated, &unit, 60
	e.GuardNow()
	if !z.frozen[a] {
		t.Fatalf("near oomd's limit after the trigger: frozen at once, got %v", z.frozen)
	}
}

func TestFreezeFastWithoutTheTriggerTheWindowHolds(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	asked := growing(t, e, u, a, now)
	z.free, z.agents = 0.30, slots.StallCritical
	tickFor(e, now, 30*time.Second)
	if len(z.frozen) != 0 || *asked != 0 {
		t.Fatalf("no trigger: the guard waits for the detector's flag, got frozen=%v asked=%d", z.frozen, *asked)
	}
}

func TestFreezeFastAnElevatedStallWaits(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	asked := growing(t, e, u, a, now)
	z.free, z.agents = 0.30, slots.StallElevated
	e.GuardNow()
	tickFor(e, now, 20*time.Second)
	if len(z.frozen) != 0 || *asked != 0 {
		t.Fatalf("the agents' stall is only elevated: the old wait holds, got frozen=%v asked=%d", z.frozen, *asked)
	}
}

func TestFreezeFastAnOldTriggerDoesNotCount(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	asked := growing(t, e, u, a, now)
	z.free, z.agents = 0.30, slots.StallElevated
	e.GuardNow()
	tickFor(e, now, TriggerFresh+10*time.Second)
	z.agents = slots.StallCritical // critical long after the trigger
	e.Tick()
	if len(z.frozen) != 0 || *asked != 0 {
		t.Fatalf("a trigger %s old: the old wait holds, got frozen=%v asked=%d", TriggerFresh, z.frozen, *asked)
	}
}

// The other rules hold: a service is never frozen.
func TestFreezeFastNeverAService(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	growing(t, e, u, a, now)
	e.GuardService = func(string) bool { return true }
	z.free, z.agents = 0.30, slots.StallCritical
	e.GuardNow()
	if z.frozen[a] {
		t.Fatal("froze a service")
	}
}
