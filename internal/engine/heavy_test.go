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
