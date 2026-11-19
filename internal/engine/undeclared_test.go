// V0.164 sections 7 and 12.5 (F), 12.2 (C9): the freeze takes undeclared work before a job that was admitted and only
// under memory pressure, and the owner's commands.
package engine

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/slots"
)

type und struct {
	flags []slots.Undeclared
}

func undEngine(t *testing.T) (*Engine, *und, *time.Time, string) {
	t.Helper()
	e, _, now := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	e.HostName = "desk"
	u := &und{}
	e.Undeclared = func() []slots.Undeclared { return u.flags }
	guardDeps(t, e)
	a := bareThread(t, e)
	running(e, a)
	return e, u, now, a
}

func flagFor(thread string, since time.Time) slots.Undeclared {
	return slots.Undeclared{Thread: thread, Since: since, CPU: 2.1, Mem: 2.8e9, CPUPct: 52, MemPct: 20, OverCPU: true, OverMem: true, MemOver: true,
		Command: "node test/layout.e2e.mjs", Kind: "e2e", Window: 45 * time.Second}
}

func notices(e *Engine, id, key string) []core.Notice {
	th, _ := e.Thread(id)
	var out []core.Notice
	for _, n := range th.Feed.Queue {
		if n.Key == key {
			out = append(out, n)
		}
	}
	return out
}

func endRun(e *Engine, id string, now time.Time) {
	coretest.Patch(e, id, func(th *core.Thread, _ time.Time) []core.Event { core.RunEnded(th, nil, now); return nil })
}

// ---- the freeze (section 7) ----

type fz struct {
	free    float64
	stall   slots.Stall
	agents  slots.Stall // the agents' own stall (V0.oomd-guard)
	mem     uint64      // the agents' memory (V0.guard-wake)
	slow    *float64
	unit    *float64        // the unit's full memory stall, oomd's reading (V0.guard-agents-stall)
	oomd    float64         // oomd's limit on the unit; 0 for none
	frozen  map[string]bool // undeclared
	slotted map[string]bool // admitted jobs
	moved   int
	empty   map[string]bool // frozen cgroups that hold no process
}

func fzEngine(t *testing.T) (*Engine, *und, *time.Time, *fz, []string) {
	e, u, now, a := undEngine(t)
	z := &fz{free: 0.30, stall: slots.StallOK, frozen: map[string]bool{}, slotted: map[string]bool{}, empty: map[string]bool{}}
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals {
		return slots.Signals{Provider: "cgroups", Headroom: &z.free, Stall: z.stall, AgentsStall: z.agents, AgentsMem: z.mem, Slowdown: z.slow, UnitStall: z.unit, OomdLimit: z.oomd}
	}
	e.SlotGrowth = func(string) uint64 { return 0 }
	e.SlotFreeze = func(o string) bool { z.slotted[o] = true; return true }
	e.SlotThaw = func(o string) { delete(z.slotted, o) }
	e.UndeclaredFreeze = func(o string) bool { z.frozen[o] = true; return true }
	e.UndeclaredThaw = func(o string) { delete(z.frozen, o) }
	e.UndeclaredEmpty = func(o string) bool { return z.empty[o] }
	guardDeps(t, e)
	b := bareThread(t, e)
	running(e, b)
	return e, u, now, z, []string{a, b}
}

func TestF1F2NoFreezeWhileMemoryIsFine(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.stall = 0.30, slots.StallElevated
	e.Tick()
	s := 5.0
	z.slow = &s
	e.Tick()
	if len(z.frozen) != 0 || len(z.slotted) != 0 {
		t.Fatalf("frozen %v %v", z.frozen, z.slotted)
	}
}

func TestF3UnderPressureTheUndeclaredWorkIsFrozenNotTheJobThatAsked(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.06
	e.Tick()
	if !z.frozen[a] || len(z.frozen) != 1 || len(z.slotted) != 0 {
		t.Fatalf("frozen %v %v", z.frozen, z.slotted)
	}
	ns := notices(e, a, "slot.pause")
	if len(ns) != 1 || !strings.Contains(ns[0].What, "Fyr paused your undeclared heavy work (`node test/layout.e2e.mjs`): memory is short on this host (memory: 6 % free). Its processes are frozen, not killed, and resume when memory has recovered for 30 s, or after 10 min unless memory still stalls critically then. A timeout inside the job may fire after it resumes; check its result. Ask with mcp__fyr__slot_acquire before a heavy job.") {
		t.Fatalf("%+v", ns)
	}
}

func TestF4TheNewestEpisodeIsFrozenAndOnlyOne(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	u.flags = []slots.Undeclared{flagFor(a, *now), flagFor(b, now.Add(time.Minute))}
	z.free = 0.04
	e.Tick()
	e.Tick()
	if !z.frozen[b] || len(z.frozen) != 1 {
		t.Fatalf("%v", z.frozen)
	}
}

func TestF5CPUOnlyIsNotFrozenTheJobsRuleApplies(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	c := bareThread(t, e)
	running(e, c)
	e.Slots.Acquire(b, slots.Test)
	*now = now.Add(time.Minute)
	e.Slots.Acquire(c, slots.Test)
	f := flagFor(a, *now)
	f.OverMem, f.MemOver = false, false
	u.flags = []slots.Undeclared{f}
	z.free = 0.04
	e.Tick()
	if len(z.frozen) != 0 || !z.slotted[c] {
		t.Fatalf("frozen %v slotted %v", z.frozen, z.slotted)
	}
}

// Decision 0032 slice 1 (amends V0.164 7): the only heavy work is frozen when
// another thread runs, since a thrashing cgroup starves that thread's agent;
// it is left alone only when nobody would gain.
func TestF6TheOnlyHeavyWorkIsFrozenWhenSomebodyElseRunsAndNotOtherwise(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	endRun(e, th[1], *now) // nobody else runs
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	if len(z.frozen) != 0 {
		t.Fatal("a freeze that helps nobody")
	}
	running(e, th[1]) // another thread runs: somebody to help
	e.Tick()
	if !z.frozen[th[0]] || len(z.frozen) != 1 {
		t.Fatalf("%v", z.frozen)
	}
}

func TestF7F8ThawByTheRuleAndNotFrozenAgainInTheEpisode(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	z.free = 0.5
	*now = now.Add(10 * time.Second)
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("thawed before 30 s")
	}
	*now = now.Add(31 * time.Second)
	e.Tick()
	if z.frozen[a] || !told(e, a, "Your heavy work runs again: Fyr thawed it.") {
		t.Fatalf("not thawed %v", z.frozen)
	}
	// Decision 0032 slice 1 (amends V0.164 7): short again in the same
	// episode it is frozen again, but only once the work has run RefreezeAfter.
	z.free = 0.03
	e.Tick()
	if len(z.frozen) != 0 {
		t.Fatal("frozen again before the slice ran")
	}
	*now = now.Add(RefreezeAfter + time.Second)
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen again in the long episode")
	}
}

// Decision 0032 slice 1: at most MaxFreezes freezes of one episode; then the
// work runs and the guard says so once.
func TestF14AnEpisodeIsFrozenAtMostMaxFreezesTimes(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	froze := 0
	for range MaxFreezes + 2 {
		z.free = 0.03
		e.Tick()
		if z.frozen[a] {
			froze++
		}
		z.free = 0.5
		*now = now.Add(time.Second)
		e.Tick() // memory is fine: the 30 s start
		*now = now.Add(31 * time.Second)
		e.Tick() // fine for 30 s: thawed
		*now = now.Add(RefreezeAfter + time.Second)
	}
	if froze != MaxFreezes {
		t.Fatalf("froze %d times, want %d", froze, MaxFreezes)
	}
	if !strings.Contains(buf.String(), "letting it run") {
		t.Fatalf("the cap was not logged:\n%s", buf.String())
	}
}

// Decision 0032 slice 1: a freeze whose cgroup is empty is released at once
// and does not block the next one.
func TestF13AFreezeThatHoldsNothingIsReleased(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen")
	}
	z.empty[a] = true // what was frozen ended
	*now = now.Add(5 * time.Second)
	e.Tick()
	if z.frozen[a] {
		t.Fatal("an empty freeze was kept")
	}
	held := e.first().owner
	if held != "" {
		t.Fatalf("still blocks other freezes: %q", held)
	}
	z.empty[a] = false
	*now = now.Add(RefreezeAfter + time.Second)
	e.Tick() // memory is still short and new work runs: frozen again
	if !z.frozen[a] {
		t.Fatal("not frozen again")
	}
}

// Decision 0032 slice 1: each decision of the guard leaves one line with the
// signals it acted on, so the thresholds can be tuned from the log.
func TestF15TheGuardLogsWhatItDidAndWhy(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	endRun(e, th[1], *now)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free, z.stall = 0.04, slots.StallCritical
	e.Tick()
	e.Tick() // the same skip on the next tick is not logged twice
	running(e, th[1])
	e.Tick()
	z.free, z.stall = 0.5, slots.StallOK
	*now = now.Add(time.Second)
	e.Tick() // recovered: the 30 s start
	*now = now.Add(31 * time.Second)
	e.Tick() // thawed
	out := buf.String()
	for _, want := range []string{
		"guard: memory short: headroom=4% stall=critical",
		"guard: not freezing thread=" + a + ": nobody else would gain",
		"guard: froze undeclared work thread=" + a + " freeze=1/8",
		"cmd=\"node test/layout.e2e.mjs\"",
		"others_running=1",
		"guard: thawed undeclared work thread=" + a,
		`reason="memory fine for 30 s"`,
		"guard: memory recovered:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "not freezing thread="); n != 1 {
		t.Errorf("the repeated skip was logged %d times", n)
	}
}

func TestF8TenMinutesAtMost(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	*now = now.Add(10*time.Minute + time.Second)
	e.Tick()
	if z.frozen[a] {
		t.Fatal("frozen after 10 min")
	}
	e.Tick()
	if len(z.frozen) != 0 {
		t.Fatal("frozen again")
	}
}

// V0.oomd-guard (decision 0036 E2): the 10 min limit does not thaw into a critical stall (V0.guard-tidy: the agents';
// the host's alone no longer holds it, TestGuardTidyPastTheLimitTheAgentsStallHoldsUntilTheCeiling); the thread is
// told once that its work waits, and it thaws at the limit once the stall is below critical.
func TestF8NoTimedThawIntoACriticalStall(t *testing.T) {
	for _, which := range []string{"agents"} {
		t.Run(which, func(t *testing.T) {
			e, u, now, z, th := fzEngine(t)
			a, b := th[0], th[1]
			e.Slots.Acquire(b, slots.Test)
			u.flags = []slots.Undeclared{flagFor(a, *now)}
			z.free = 0.04
			var out bytes.Buffer
			log.SetOutput(&out)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })
			e.Tick()
			if !z.frozen[a] {
				t.Fatal("not frozen")
			}
			if which == "host" {
				z.stall = slots.StallCritical
			} else {
				z.agents = slots.StallCritical
			}
			*now = now.Add(10*time.Minute + time.Second)
			e.Tick()
			if !z.frozen[a] {
				t.Fatal("thawed at the 10 min limit into a critical stall")
			}
			*now = now.Add(5 * time.Minute)
			e.Tick()
			if !z.frozen[a] {
				t.Fatal("thawed while the stall stays critical")
			}
			waits := 0
			for _, n := range notices(e, a, "slot.pause") {
				if strings.Contains(n.What, "Your heavy work stays frozen past 10 min: memory still stalls critically") {
					waits++
				}
			}
			if waits != 1 {
				t.Fatalf("told %d times that it waits", waits)
			}
			if !strings.Contains(out.String(), "guard: keeping thread="+a+" frozen past the 10m0s limit: the agents' memory stalls critically") {
				t.Fatalf("no log line for the kept freeze:\n%s", out.String())
			}
			z.stall, z.agents = slots.StallElevated, slots.StallOK
			e.Tick()
			if z.frozen[a] {
				t.Fatal("still frozen past the limit with the stall below critical")
			}
			if !strings.Contains(out.String(), `reason="10 min limit, stall no longer critical"`) {
				t.Fatalf("no thaw line:\n%s", out.String())
			}
		})
	}
}

func TestF9ThawedWhenTheEpisodeEndsOrTheThreadIsAdmitted(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	u.flags = nil // the episode ended (or the thread was admitted: it is no longer flagged)
	e.Tick()
	if z.frozen[a] {
		t.Fatal("still frozen after its episode")
	}
}

func TestF12WhereNothingCanBeFrozenNothingIsToldItWasPaused(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	e.UndeclaredFreeze = func(string) bool { return false } // no cgroups
	guardDeps(t, e)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	if told(e, a, "paused") || len(z.slotted) != 0 {
		t.Fatal("pretended to pause")
	}
}

// ---- the owner's commands ----

func TestC9TheCommandsAreTheOwnersOnly(t *testing.T) {
	e, _, _, _ := undEngine(t)
	var got []string
	e.HeavyRemove = func(p string) bool { got = append(got, p); return true }
	for _, k := range []string{core.KindHeavyRemove, core.KindUndeclaredSettings} {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: k, Args: map[string]any{"pattern": "x"}}); err == nil {
			t.Errorf("%s accepted from an observer", k)
		}
	}
	if r := e.Command(api.Cmd{CommandID: "c1", Action: core.KindHeavyRemove, Args: map[string]any{"pattern": "x"}}); !r.OK || len(got) != 1 {
		t.Fatalf("%+v %v", r, got)
	}
}

// V0.209: flagged work asks nothing of the agent: no notice, however long it runs.
func TestV209AFlagTellsTheAgentNothing(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	if th, _ := e.Thread(a); len(th.Feed.Queue) != 0 {
		t.Fatalf("%+v", th.Feed.Queue)
	}
}

// V0.190: a host whose guard has done nothing sends an empty list, not null; the
// front spreads it.
func TestF16ACalmGuardSendsAnEmptyEventList(t *testing.T) {
	e, _, _, z, _ := fzEngine(t)
	z.free = 0.9
	e.Tick()
	b, err := json.Marshal(e.GuardView())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"events":[]`) {
		t.Fatalf("%s", b)
	}
}

// Decision 0032: the front gets the guard's history and where each thread's
// freezes stand, from a published view that waits for nothing.
func TestF16TheGuardViewShowsTheHistoryAndEachThreadsFreezes(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	if e.GuardView() != nil && len(e.GuardView().Events) != 0 {
		t.Fatal("events before any tick")
	}
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	g := e.GuardView()
	if g == nil || !g.Short || g.ShortSince == nil || g.Max != MaxFreezes || g.RefreezeSec != 60 {
		t.Fatalf("%+v", g)
	}
	if len(g.Threads) != 1 || g.Threads[0].Thread != a || !g.Threads[0].Frozen || g.Threads[0].N != 1 || g.Threads[0].FrozenAt == nil {
		t.Fatalf("%+v", g.Threads)
	}
	kinds := []string{}
	for _, ev := range g.Events {
		kinds = append(kinds, ev.Kind+"/"+ev.Work)
	}
	if strings.Join(kinds, " ") != "short/ freeze/undeclared" {
		t.Fatalf("%v", kinds)
	}
	if g.Events[1].N != 1 || g.Events[1].Headroom == nil || *g.Events[1].Headroom != 0.04 {
		t.Fatalf("%+v", g.Events[1])
	}
	// Memory is fine for 30 s: thawed, and the next freeze waits for the slice.
	z.free = 0.5
	*now = now.Add(time.Second)
	e.Tick()
	*now = now.Add(31 * time.Second)
	e.Tick()
	g = e.GuardView()
	last := g.Events[len(g.Events)-1]
	if last.Kind != "thaw" || last.Reason != "memory" || g.Short {
		t.Fatalf("%+v short=%v", last, g.Short)
	}
	if th := g.Threads[0]; th.Frozen || th.NextAt == nil || th.Capped {
		t.Fatalf("%+v", th)
	}
}

// The history is bounded.
func TestF17TheGuardHistoryIsBounded(t *testing.T) {
	e, _, now, _, _ := fzEngine(t)
	for range guardKeep + 25 {
		e.guardRecord(slots.GuardEvent{At: *now, Kind: "short"}, slots.Signals{})
	}
	e.publishGuard(*now)
	if n := len(e.GuardView().Events); n != guardKeep {
		t.Fatalf("%d events", n)
	}
}
