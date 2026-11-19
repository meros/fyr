// V0.multi-freeze: while memory stays short the guard freezes one more thread
// each AddFreezeAfter; and the turns of a frozen thread go to its heavy parts.
// V0.guard-wake: at most the running threads less one, the oldest wakes first
// by Buck2's rule, and the thaw says how long the work was frozen.
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

// tickFor ticks every 10 s for d.
func tickFor(e *Engine, now *time.Time, d time.Duration) {
	for end := now.Add(d); now.Before(end); {
		*now = now.Add(10 * time.Second)
		e.Tick()
	}
}

// third adds a thread that runs and does no heavy work: with three running, two may be frozen (V0.guard-wake).
func third(t *testing.T, e *Engine) string {
	c := bareThread(t, e)
	running(e, c)
	return c
}

func TestMultiStillShortAMinuteLaterASecondThreadIsFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	third(t, e)
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
	third(t, e)
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Second))}
	z.free = 0.04
	e.Tick()
	z.free = 0.12 // not short, not fine either
	tickFor(e, now, 2*AddFreezeAfter)
	if z.frozen[a] || !z.frozen[b] {
		t.Fatalf("one freeze is enough when memory is no longer short: %v", z.frozen)
	}
}

// V0.guard-wake (decision 0036 E4): at most the running threads less one are frozen; it was 3.
func TestMultiAtMostTheRunningThreadsLessOne(t *testing.T) {
	for _, n := range []int{2, 4, 6} {
		e, u, now, z, th := fzEngine(t)
		ids := th
		for len(ids) < n {
			ids = append(ids, third(t, e))
		}
		for i, id := range ids {
			u.flags = append(u.flags, flagFor(id, now.Add(time.Duration(i)*time.Second)))
		}
		z.free = 0.04
		e.Tick()
		tickFor(e, now, time.Duration(n+1)*AddFreezeAfter)
		if len(z.frozen) != n-1 || z.frozen[ids[0]] {
			t.Fatalf("%d running: frozen %v, want the %d newest", n, z.frozen, n-1)
		}
	}
}

// V0.guard-wake (decision 0036 E3): the oldest freeze wakes first, then the next at most WakeEvery later.
func TestMultiWakeOldestFirstOnePerWakeEvery(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	third(t, e)
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Second))}
	z.free = 0.04
	e.Tick() // b, the newest episode, is frozen first
	tickFor(e, now, AddFreezeAfter)
	if len(z.frozen) != 2 {
		t.Fatalf("setup: %v", z.frozen)
	}
	z.free = 0.30
	e.Tick()
	tickFor(e, now, ThawAfterOK)
	if z.frozen[b] || !z.frozen[a] {
		t.Fatalf("the oldest freeze (b) wakes first, alone: %v", z.frozen)
	}
	*now = now.Add(WakeEvery - time.Second)
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("the second wake came before WakeEvery")
	}
	*now = now.Add(time.Second)
	e.Tick()
	if len(z.frozen) != 0 {
		t.Fatalf("still frozen: %v", z.frozen)
	}
}

// V0.guard-wake (decision 0036 E3, Buck2's rule): the cap is the agents' memory when the stall first went critical; a
// frozen job wakes only when the agents' memory plus its own at the freeze is under it, and the stall has been below
// elevated for 30 s.
func TestWakeRuleTheCapFromTheFirstCriticalStall(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e, u, now, z, th := fzEngine(t)
	a := th[0] // flagFor: 2.8 GB
	e.Slots.Acquire(th[1], slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.stall, z.mem = 0.04, slots.StallCritical, 10e9
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen")
	}
	z.stall, z.mem = slots.StallCritical, 12e9 // a later, higher reading does not move the cap
	tickFor(e, now, 20*time.Second)
	if !strings.Contains(out.String(), "guard: wake cap agents=10.0 GB") {
		t.Fatalf("no cap line:\n%s", out.String())
	}
	z.free, z.stall, z.mem = 0.04, slots.StallOK, 8e9 // 8 + 2.8 >= 10
	tickFor(e, now, 2*ThawAfterOK)
	if !z.frozen[a] {
		t.Fatal("woke while it does not fit under the cap")
	}
	if !strings.Contains(out.String(), "not waking thread="+a+" yet: agents=8.0 GB + its 2.8 GB >= cap=10.0 GB") {
		t.Fatalf("no line for the wait:\n%s", out.String())
	}
	z.mem, z.stall = 7e9, slots.StallElevated // fits, but the stall is elevated
	tickFor(e, now, 2*ThawAfterOK)
	if !z.frozen[a] {
		t.Fatal("woke while the stall is elevated")
	}
	z.stall = slots.StallOK
	e.Tick() // the 30 s start
	tickFor(e, now, ThawAfterOK-10*time.Second)
	if !z.frozen[a] {
		t.Fatal("woke before 30 s below elevated")
	}
	tickFor(e, now, 10*time.Second)
	if z.frozen[a] {
		t.Fatal("not woken: 7 + 2.8 < 10 and 30 s below elevated")
	}
	if !strings.Contains(out.String(), "fits: agents=7.0 GB + its 2.8 GB < cap=10.0 GB") {
		t.Fatalf("no wake line:\n%s", out.String())
	}
}

// V0.guard-wake (decision 0036 E6): the thaw notice names how long the work was frozen, from frozen_usec where the
// kernel has it, else the wall time.
func TestThawSaysHowLongItWasFrozen(t *testing.T) {
	for _, kernel := range []bool{true, false} {
		e, u, now, z, th := fzEngine(t)
		a := th[0]
		e.Slots.Acquire(th[1], slots.Test)
		usec := uint64(5_000_000)
		e.FrozenUsec = func(string, bool) (uint64, bool) { return usec, kernel }
		u.flags = []slots.Undeclared{flagFor(a, *now)}
		z.free = 0.04
		e.Tick()
		usec += uint64((2*time.Minute + 3*time.Second) / time.Microsecond)
		*now = now.Add(2 * time.Minute)
		z.free = 0.5
		e.Tick() // fine: the 30 s start
		*now = now.Add(30 * time.Second)
		e.Tick() // woken
		want := "It was frozen for 2 min 3 s"
		if !kernel {
			want = "It was frozen for 2 min 30 s"
		}
		if !told(e, a, "Your heavy work runs again: Fyr thawed it. "+want+"; a timeout or a slow step in that time came from the freeze, not from the work.") {
			th, _ := e.Thread(a)
			t.Fatalf("kernel=%t: %+v", kernel, th.Feed.Queue)
		}
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
