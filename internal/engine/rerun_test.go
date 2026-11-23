// V0.rerun-classes (decision 0036 (F)): under the guard a job of a class the owner marked rerunnable is killed instead
// of frozen when it was frozen and could not wake within 2 min, or when it would be frozen and runs under 5 min; its
// thread is told to run it again, and the gate admits the rerun again; a class killed 3 times in an hour is frozen
// instead for the hour. Nothing is rerunnable unless the owner says so.
package engine

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

type rerunRec struct {
	killed []string
	raised []string
	wall   time.Duration
}

func useRerun(t *testing.T, e *Engine, entries ...string) *rerunRec {
	r := &rerunRec{}
	e.RerunMatch = func(class, cmd string) string {
		for _, x := range entries {
			if class == x || strings.Contains(cmd, x) {
				return x
			}
		}
		return ""
	}
	e.JobKill = func(th string, und bool) int { r.killed = append(r.killed, th); return 3 }
	e.RerunWall = func(string) (time.Duration, bool) { return r.wall, r.wall > 0 }
	e.RerunRaise = func(th string) { r.raised = append(r.raised, th) }
	guardDeps(t, e)
	return r
}

func TestRerunAFrozenRerunnableWorkIsKilledAfterTwoMinutes(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	r := useRerun(t, e, "layout.e2e")
	e.Slots.Acquire(b, resource.Test)
	u.flags = []resource.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen first: its wall time is not known")
	}
	*now = now.Add(guard.RerunAfter - 10*time.Second)
	e.Tick()
	if len(r.killed) != 0 {
		t.Fatal("killed before 2 min")
	}
	*now = now.Add(10 * time.Second)
	e.Tick()
	if len(r.killed) != 1 || r.killed[0] != a || e.first().Owner == a {
		t.Fatalf("killed %v, still held %q", r.killed, e.first().Owner)
	}
	if !told(e, a, "Fyr killed your heavy work (`layout.e2e`) to free memory on this host: the owner marked it rerunnable.") {
		th, _ := e.Thread(a)
		t.Fatalf("not told: %+v", th.Feed.Queue)
	}
	if !strings.Contains(out.String(), "guard: killed undeclared work thread="+a+` class="layout.e2e" why="frozen 2m0s and could not wake" procs=3`) {
		t.Fatalf("no kill line:\n%s", out.String())
	}
	if !e.RerunWaiting(a) {
		t.Fatal("the rerun is not waited for")
	}
}

func TestRerunNothingIsKilledUnlessMarked(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	r := useRerun(t, e) // no entries
	e.Slots.Acquire(b, resource.Test)
	u.flags = []resource.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	tickFor(e, now, 5*time.Minute)
	if len(r.killed) != 0 || !z.frozen[a] {
		t.Fatalf("killed %v frozen %v", r.killed, z.frozen)
	}
}

func TestRerunAServiceIsNeverKilled(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	r := useRerun(t, e, "layout.e2e")
	e.GuardService = func(string) bool { return true }
	guardDeps(t, e)
	e.Slots.Acquire(b, resource.Test)
	u.flags = []resource.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	tickFor(e, now, 5*time.Minute)
	if len(r.killed) != 0 {
		t.Fatalf("a service was killed: %v", r.killed)
	}
}

// slotEngine is two running threads each holding a slot, the newer one in a rerunnable class, and memory short.
func slotEngine(t *testing.T) (*Engine, *time.Time, string, string, map[string]bool, *float64) {
	e, _, now := bare(t)
	p := slots.New(3)
	p.Now = e.Now
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	free := 0.5
	e.SlotPolicy = resource.DefaultPolicy
	e.SlotSignals = func() resource.Signals {
		return resource.Signals{Provider: "cgroups", Headroom: &free, Stall: shortStall(free), CPU: 8, Memory: 16 << 30}
	}
	e.SlotGrowth = func(string) uint64 { return 0 }
	frozen := map[string]bool{}
	e.SlotFreeze = func(o string) bool { frozen[o] = true; return true }
	e.SlotThaw = func(o string) { delete(frozen, o) }
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "go test ./...", ToolUse: "a1"}); r != "" {
		t.Fatal(r)
	}
	*now = now.Add(time.Minute)
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "make test", ToolUse: "b1"}); r != "" {
		t.Fatal(r)
	}
	// The pool's admission as on a host with signals: memory short holds every job (decision 0029 S2).
	p.Admit = func(resource.Holder, []resource.Holder) (bool, string, string) {
		if free < resource.DefaultPolicy.MemoryReserve {
			return false, "memory short", resource.ResMem
		}
		return true, "", ""
	}
	free = 0.04
	return e, now, a, b, frozen, &free
}

func TestRerunAShortJobIsKilledInsteadOfFrozenAndItsRerunIsAdmittedAgain(t *testing.T) {
	e, now, a, b, frozen, free := slotEngine(t)
	e.HeavySoft = func(string) bool { return true } // learned: it would never queue
	r := useRerun(t, e, "make test")
	r.wall = 3 * time.Minute
	e.Tick()
	if frozen[a] || len(r.killed) != 1 || r.killed[0] != b {
		t.Fatalf("frozen %v killed %v: the short rerunnable job (b) is killed, the oldest (a) runs", frozen, r.killed)
	}
	if len(r.raised) != 1 || holds(e, b) {
		t.Fatalf("raised %v, b still holds: %t", r.raised, holds(e, b))
	}
	// The rerun while memory is short: queued, even though its class is learned.
	if got := e.HeavyGate(HeavyCall{Thread: b, Class: "make test", ToolUse: "b2"}); !strings.Contains(got, "queued") {
		t.Fatalf("the rerun ran while memory is short: %q", got)
	}
	*free = 0.5
	*now = now.Add(10 * time.Second)
	e.Tick()
	if !holds(e, b) || !told(e, b, "admitted now: run the command again") {
		t.Fatal("the rerun was not admitted once memory was fine")
	}
	if got := e.HeavyGate(HeavyCall{Thread: b, Class: "make test", ToolUse: "b3"}); got != "" || e.RerunWaiting(b) {
		t.Fatalf("the admitted rerun: %q waiting=%t", got, e.RerunWaiting(b))
	}
}

func TestRerunALongJobIsFrozenFirst(t *testing.T) {
	e, _, _, b, frozen, _ := slotEngine(t)
	r := useRerun(t, e, "make test")
	r.wall = 20 * time.Minute
	e.Tick()
	if !frozen[b] || len(r.killed) != 0 {
		t.Fatalf("frozen %v killed %v", frozen, r.killed)
	}
}

func TestRerunAClassKilledThreeTimesInAnHourIsFrozenInstead(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e, now, _, b, frozen, free := slotEngine(t)
	r := useRerun(t, e, "make test")
	r.wall = time.Minute
	for i := range guard.RerunCap {
		*free = 0.04
		e.Tick()
		if len(r.killed) != i+1 {
			t.Fatalf("kill %d: killed %v", i+1, r.killed)
		}
		*free = 0.5
		*now = now.Add(time.Minute)
		e.Tick() // recovered: the rerun is admitted
		if got := e.HeavyGate(HeavyCall{Thread: b, Class: "make test", ToolUse: "r" + string(rune('a'+i))}); got != "" {
			t.Fatalf("rerun %d: %q", i+1, got)
		}
	}
	*free = 0.04
	e.Tick()
	if len(r.killed) != guard.RerunCap || !frozen[b] {
		t.Fatalf("killed %d times, frozen %v: the fourth time it is frozen", len(r.killed), frozen)
	}
	g := e.GuardView()
	if len(g.Rerun) != 1 || g.Rerun[0].Kills != guard.RerunCap || g.Rerun[0].CappedUntil == nil {
		t.Fatalf("view %+v", g.Rerun)
	}
	capped := false
	for _, ev := range g.Events {
		capped = capped || (ev.Kind == "capped" && ev.Class == "make test")
	}
	if !capped || !strings.Contains(out.String(), `guard: class="make test" killed 3 times in an hour: frozen instead of killed until`) {
		t.Fatalf("the owner is not told: %+v\n%s", g.Events, out.String())
	}
	// An hour after the first kill it may be killed again.
	*now = now.Add(time.Hour)
	if len(guard.Recent(e.gs.RerunKills["make test"], *now)) >= guard.RerunCap {
		t.Fatal("still capped after the hour")
	}
}

// The owner's add and remove reach the host's list; an agent or an observer cannot change it.
func TestRerunClassesAreTheOwnersCommand(t *testing.T) {
	e, _, _, _ := undEngine(t)
	var got []map[string]any
	e.RerunSet = func(a map[string]any) error { got = append(got, a); return nil }
	guardDeps(t, e)
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindRerunClasses, Args: map[string]any{"add": "make test"}}); err == nil {
		t.Fatal("accepted from an observer")
	}
	for _, bad := range []map[string]any{{}, {"add": "a", "remove": "b"}, {"add": "  "}} {
		if r := e.Command(api.Cmd{CommandID: "bad", Action: core.KindRerunClasses, Args: bad}); r.OK {
			t.Fatalf("accepted %v", bad)
		}
	}
	if r := e.Command(api.Cmd{CommandID: "c1", Action: core.KindRerunClasses, Args: map[string]any{"add": " make test "}}); !r.OK || len(got) != 1 || got[0]["add"] != "make test" {
		t.Fatalf("%+v %v", r, got)
	}
	if r := e.Command(api.Cmd{CommandID: "c2", Action: core.KindRerunClasses, Args: map[string]any{"remove": "make test"}}); !r.OK || len(got) != 2 {
		t.Fatalf("%+v %v", r, got)
	}
}
