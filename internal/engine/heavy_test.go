// V0.209: heavy jobs run themselves. The gate admits a heavy command when it starts, its end gives the share back, and
// an admission nothing uses goes back; nobody calls anything.
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

func heavyEngine(t *testing.T, n int) (*Engine, *time.Time, string, string) {
	t.Helper()
	e, _, now := bare(t)
	p := slots.New(n)
	p.Now = e.Now // one clock for the pool and the engine
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	return e, now, a, b
}

func holds(e *Engine, id string) bool { _, ok := e.Slots.Holding(id); return ok }

func TestV209TheGateAdmitsAndTheEndReleases(t *testing.T) {
	e, _, a, _ := heavyEngine(t, 1)
	if r := e.HeavyGate(HeavyCall{Thread: a, ToolUse: "tu"}); r != "" || holds(e, a) {
		t.Fatalf("a quick command: %q", r)
	}
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"}); r != "" || !holds(e, a) {
		t.Fatalf("not admitted: %q", r)
	}
	e.HeavyDone(a, "other") // some other tool's end
	if !holds(e, a) {
		t.Fatal("another call released it")
	}
	e.HeavyDone(a, "tu1")
	if holds(e, a) {
		t.Fatal("not released at its end")
	}
}

func TestV209AFullHostQueuesThenTheThreadIsToldToRunItAgain(t *testing.T) {
	e, _, a, b := heavyEngine(t, 1)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"})
	r := e.HeavyGate(HeavyCall{Thread: b, Class: "go test ./...", ToolUse: "tu2"})
	if !strings.Contains(r, "queued") || !strings.Contains(r, "0 ahead") || !strings.Contains(r, "run the command again") || strings.Contains(r, "slot_") {
		t.Fatalf("refusal %q", r)
	}
	e.HeavyDone(a, "tu1")
	if !holds(e, b) || !told(e, b, "admitted now: run the command again") {
		t.Fatal("the queued thread was not admitted and told")
	}
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "go test ./...", ToolUse: "tu3"}); r != "" {
		t.Fatalf("the admitted thread was refused: %q", r)
	}
}

// A subagent never gets the [Fyr] admission, so its refusal says not to run
// the command again and to report it as queued (on 2026-11-19 one sent the
// same queued check four times).
func TestASubagentIsToldNotToRetryAQueuedJob(t *testing.T) {
	e, _, a, b := heavyEngine(t, 1)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"})
	r := e.HeavyGate(HeavyCall{Thread: b, Class: "make check", ToolUse: "tu2", Subagent: true})
	if !strings.Contains(r, "queued") || !strings.Contains(r, "Do not run this command again") || strings.Contains(r, "run the command again then") {
		t.Fatalf("refusal %q", r)
	}
}

func TestV209TheLastOfSeveralCallsReleases(t *testing.T) {
	e, _, a, _ := heavyEngine(t, 2)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"})
	e.HeavyGate(HeavyCall{Thread: a, Class: "nix build", ToolUse: "tu2"})
	e.HeavyDone(a, "tu1")
	if !holds(e, a) {
		t.Fatal("released with a call still running")
	}
	e.HeavyDone(a, "tu2")
	if holds(e, a) {
		t.Fatal("not released after the last")
	}
}

func TestV209ABackgroundJobIsReleasedWhenItGoesQuiet(t *testing.T) {
	e, now, a, _ := heavyEngine(t, 2)
	start := *now
	active := start
	e.SlotActive = func(owner string) (time.Time, bool) { return active, owner == a }
	guardDeps(t, e)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1", Background: true})
	e.HeavyDone(a, "tu1") // the call returns at once; the job goes on
	if !holds(e, a) {
		t.Fatal("released when the call returned")
	}
	active = start.Add(10 * time.Minute) // still using CPU
	*now = start.Add(11 * time.Minute)
	e.Tick()
	if !holds(e, a) {
		t.Fatal("released a busy job")
	}
	*now = active.Add(SlotIdle + time.Second)
	e.Tick()
	if holds(e, a) {
		t.Fatal("a quiet admission stayed")
	}
	if th, _ := e.Thread(a); len(th.Feed.Queue) != 0 {
		t.Fatalf("the agent was told: %+v", th.Feed.Queue)
	}
}

func TestV209ARunningCommandKeepsItsAdmissionWhileQuiet(t *testing.T) {
	e, now, a, _ := heavyEngine(t, 2)
	start := *now
	e.SlotActive = func(owner string) (time.Time, bool) { return start, owner == a }
	guardDeps(t, e)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"})
	*now = start.Add(HeavyCallStale - time.Minute)
	e.Tick()
	if !holds(e, a) {
		t.Fatal("released under a running command")
	}
	*now = start.Add(HeavyCallStale + time.Minute) // its result never came
	e.Tick()
	if holds(e, a) {
		t.Fatal("a lost result holds the share for ever")
	}
}

func TestV209ALearnedCommandNeverWaits(t *testing.T) {
	e, _, a, b := heavyEngine(t, 1)
	e.HeavySoft = func(class string) bool { return class == "gradle assemble" }
	guardDeps(t, e)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"})
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "gradle assemble", ToolUse: "tu2"}); r != "" {
		t.Fatalf("refused: %q", r)
	}
	if s := e.Slots.Snapshot(); len(s.Waiting) != 0 || holds(e, b) {
		t.Fatalf("queued or admitted over the budget: %+v", s)
	}
	e.HeavyDone(a, "tu1")
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "gradle assemble", ToolUse: "tu3"}); r != "" || !holds(e, b) {
		t.Fatalf("with room it is admitted: %q", r)
	}
}

func TestV209SlotsOffAdmitsNothing(t *testing.T) {
	e, _, a, _ := heavyEngine(t, 0)
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu"}); r != "" {
		t.Fatalf("%q", r)
	}
	e.HeavyDone(a, "tu")
}

// V0.guard-wake (decision 0036 E1): while the memory guard says memory is short, a new heavy command is held at the
// gate before anything that runs is frozen: also one from a thread that already holds an admission, and a learned
// one, which never queues. When memory recovers the thread is told to run it again.
func TestGuardWakeTheGateHoldsANewCommandWhileMemoryIsShort(t *testing.T) {
	e, now, a, b := heavyEngine(t, 2)
	e.HeavySoft = func(class string) bool { return class == "gradle assemble" }
	free := 0.5
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals {
		return slots.Signals{Provider: "cgroups", Headroom: &free, Stall: slots.StallOK, CPU: 8, Memory: 16 << 30}
	}
	e.SlotFreeze = func(string) bool { return true }
	e.SlotThaw = func(string) {}
	guardDeps(t, e)
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"}); r != "" || !holds(e, a) {
		t.Fatalf("not admitted while memory is fine: %q", r)
	}
	free = 0.04
	e.Tick() // the guard: memory short
	r := e.HeavyGate(HeavyCall{Thread: a, Class: "make e2e", ToolUse: "tu2"})
	if !strings.Contains(r, "memory is short on this host, so Fyr holds new heavy commands until it recovers") || !strings.Contains(r, "It did not run") {
		t.Fatalf("an admitted thread's new command ran: %q", r)
	}
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "gradle assemble", ToolUse: "tu3"}); !strings.Contains(r, "holds new heavy commands") {
		t.Fatalf("a learned command ran: %q", r)
	}
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "gradle assemble", ToolUse: "tu4", Subagent: true}); !strings.Contains(r, "You are a subagent") {
		t.Fatalf("subagent: %q", r)
	}
	free = 0.5
	*now = now.Add(10 * time.Second)
	e.Tick() // recovered
	for _, id := range []string{a, b} {
		if !told(e, id, "Memory has recovered on this host: run the heavy command Fyr held again.") {
			th, _ := e.Thread(id)
			t.Fatalf("%s not told: %+v", id, th.Feed.Queue)
		}
	}
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "make e2e", ToolUse: "tu5"}); r != "" {
		t.Fatalf("held after memory recovered: %q", r)
	}
}
