// V0.guard-tidy: an empty frozen job is released, work that holds little of
// its thread's memory is not frozen, the hold past FreezeMax reads the agents'
// stall and ends at FreezeCeiling, `froze job` logs what `froze undeclared
// work` does and the hold is in the history, and a frozen thread gets one
// notice, not two.
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

// jobGuard is two running threads holding slots, a the oldest, with memory
// short, and what the guard froze.
func jobGuard(t *testing.T) (*Engine, *time.Time, string, string, *float64, *slots.Stall, map[string]bool) {
	e, _, now := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	p.Acquire(a, slots.Test)
	*now = now.Add(time.Minute)
	p.Acquire(b, slots.Test)
	free, agents := 0.02, slots.StallOK
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals {
		return slots.Signals{Provider: "cgroups", Headroom: &free, Stall: slots.StallOK, AgentsStall: agents}
	}
	e.SlotGrowth = func(string) uint64 { return 0 }
	frozen := map[string]bool{}
	e.SlotFreeze = func(o string) bool { frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(frozen, o) }
	return e, now, a, b, &free, &agents, frozen
}

// A frozen job whose cgroup emptied holds nothing: released at once, as
// undeclared work is, not kept for ten minutes.
func TestGuardTidyAnEmptyJobFreezeIsReleased(t *testing.T) {
	e, now, _, b, _, _, frozen := jobGuard(t)
	empty := map[string]bool{}
	e.SlotEmpty = func(o string) bool { return empty[o] }
	guardDeps(t, e)
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e.Tick()
	if !frozen[b] {
		t.Fatal("setup: not frozen")
	}
	empty[b] = true // the job ended while frozen
	*now = now.Add(10 * time.Second)
	e.Tick()
	if frozen[b] || e.first().owner != "" {
		t.Fatalf("an empty job freeze was kept: %v", frozen)
	}
	if !strings.Contains(out.String(), `guard: thawed job thread=`+b) || !strings.Contains(out.String(), `reason="its processes are gone"`) {
		t.Fatalf("no thaw line:\n%s", out.String())
	}
}

// 19 Nov 16:02: the parts read 0.0 GB while the thread's memory stayed
// charged to its cli/; freezing them frees nothing.
func TestGuardTidyWorkThatHoldsLittleIsNotFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)} // 2.8 GB flagged
	var roots uint64
	e.UndeclaredRootsMem = func(string) (uint64, bool) { return roots, true }
	guardDeps(t, e)
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	z.free = 0.04
	e.Tick()
	if z.frozen[a] {
		t.Fatal("work holding none of the thread's memory was frozen")
	}
	if !strings.Contains(out.String(), "guard: not freezing thread="+a+": the work holds little of the thread's memory") {
		t.Fatalf("no skip line:\n%s", out.String())
	}
	roots = 2e9
	*now = now.Add(10 * time.Second)
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("work holding most of the memory was not frozen")
	}
}

// Past FreezeMax the host's critical stall alone no longer holds the work (it
// can come from the desktop); the agents' does, up to FreezeCeiling, and the
// owner's history shows the hold.
func TestGuardTidyPastTheLimitTheAgentsStallHoldsUntilTheCeiling(t *testing.T) {
	t.Run("host", func(t *testing.T) {
		e, u, now, z, th := fzEngine(t)
		a, b := th[0], th[1]
		e.Slots.Acquire(b, slots.Test)
		u.flags = []slots.Undeclared{flagFor(a, *now)}
		z.free = 0.04
		e.Tick()
		z.stall = slots.StallCritical
		*now = now.Add(FreezeMax + time.Second)
		e.Tick()
		if z.frozen[a] {
			t.Fatal("kept past the limit on the host's stall alone")
		}
	})
	t.Run("agents", func(t *testing.T) {
		e, u, now, z, th := fzEngine(t)
		a, b := th[0], th[1]
		e.Slots.Acquire(b, slots.Test)
		u.flags = []slots.Undeclared{flagFor(a, *now)}
		z.free = 0.04
		var out bytes.Buffer
		log.SetOutput(&out)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })
		e.Tick()
		z.agents = slots.StallCritical
		*now = now.Add(FreezeMax + time.Second)
		e.Tick()
		if !z.frozen[a] {
			t.Fatal("thawed past the limit into the agents' critical stall")
		}
		kept := 0
		for _, ev := range e.GuardView().Events {
			if ev.Kind == "kept" && ev.Thread == a && ev.Work == "undeclared" {
				kept++
			}
		}
		*now = now.Add(5 * time.Minute)
		e.Tick()
		if kept != 1 {
			t.Fatalf("%d kept events, want 1", kept)
		}
		*now = now.Add(FreezeCeiling - FreezeMax - 5*time.Minute)
		e.Tick()
		if z.frozen[a] {
			t.Fatal("still frozen at the ceiling")
		}
		if !strings.Contains(out.String(), `reason="30 min ceiling, the agents' memory still stalls critically"`) {
			t.Fatalf("no ceiling thaw line:\n%s", out.String())
		}
		if !told(e, a, "Your heavy work runs again") {
			t.Fatal("the thread was not told it runs again")
		}
		last := e.GuardView().Events
		if ev := last[len(last)-1]; ev.Kind != "thaw" || ev.Reason != "ceiling" {
			t.Fatalf("last event %+v", ev)
		}
	})
}

// Decision 0032 "What the log gives for tuning": `froze job` has the fields
// `froze undeclared work` has.
func TestGuardTidyFrozeJobLogsLikeUndeclaredWork(t *testing.T) {
	e, _, _, b, _, _, frozen := jobGuard(t)
	guardDeps(t, e)
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e.Tick()
	if !frozen[b] {
		t.Fatal("setup: not frozen")
	}
	line := ""
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.Contains(l, "guard: froze job thread="+b) {
			line = l
		}
	}
	for _, want := range []string{"freeze=1/1", "episode_age=0s", `cmd="unknown command"`, "cores=", "headroom=2%", "running=2", "frozen_now=1"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %q", want, line)
		}
	}
}

// memoryGuard's notice ("nothing is stopped") went to the job the guard froze
// on the same tick, beside the pause notice.
func TestGuardTidyAFrozenJobGetsOneNotice(t *testing.T) {
	e, _, _, b, _, _, frozen := jobGuard(t)
	e.SlotGrowth = func(o string) uint64 {
		if o == b {
			return 2e9
		}
		return 0
	}
	guardDeps(t, e)
	e.Tick()
	if !frozen[b] || !told(e, b, "Fyr paused your heavy job") {
		t.Fatal("setup: not frozen and told")
	}
	if n := notices(e, b, "slot.memory"); len(n) != 0 {
		t.Fatalf("the frozen job was also told nothing is stopped: %+v", n)
	}
}
