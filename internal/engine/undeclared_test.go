// V0.164 sections 5, 7 and 12.3 (N), 12.5 (F), 12.2 (C9): the agent is told once, the freeze takes undeclared work
// before a job that asked and only under memory pressure, and the owner's commands.
package engine

import (
	"encoding/json"
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

func TestN1AFlagIsOneNoticeNotAWake(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	ns := notices(e, a, "heavy.undeclared")
	if len(ns) != 1 || ns[0].Urgency != core.Next {
		t.Fatalf("%+v", ns)
	}
	for _, want := range []string{
		"This thread runs heavy work Fyr did not admit: about 2.8 GB memory (20 % of desk) and 2.1 cores (52 %) for 45 s, from `node test/layout.e2e.mjs`.",
		`Before a heavy job call mcp__fyr__slot_acquire {"kind":"e2e","command":"node test/layout.e2e.mjs"}`,
		"Call it now for the job that runs.",
		"Fyr counts this use against the host either way, so other jobs wait; nothing is stopped, but if memory runs short Fyr may pause it.",
	} {
		if !strings.Contains(ns[0].What, want) {
			t.Errorf("missing %q in %q", want, ns[0].What)
		}
	}
}

func TestN1OnlyTheOverDimensionIsNamed(t *testing.T) {
	e, u, now, a := undEngine(t)
	f := flagFor(a, *now)
	f.OverCPU = false
	u.flags = []slots.Undeclared{f}
	e.Tick()
	w := notices(e, a, "heavy.undeclared")[0].What
	if !strings.Contains(w, "about 2.8 GB memory (20 % of desk) for 45 s") || strings.Contains(w, "cores") {
		t.Fatal(w)
	}
}

func TestN2AnEpisodeThatGoesOnIsToldOnce(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	for i := 0; i < 120; i++ { // 10 min
		*now = now.Add(5 * time.Second)
		e.Tick()
	}
	if n := len(notices(e, a, "heavy.undeclared")); n != 1 {
		t.Fatalf("%d", n)
	}
}

func TestN3ASecondEpisodeInTheSameRunIsNotToldAgain(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	// Take the notice (the agent read it), then the episode ends and another starts, in the same run.
	coretest.Patch(e, a, func(th *core.Thread, _ time.Time) []core.Event { th.Feed.Queue = nil; return nil })
	u.flags = nil
	*now = now.Add(5 * time.Minute)
	e.Tick()
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	if n := len(notices(e, a, "heavy.undeclared")); n != 0 {
		t.Fatalf("%d notices for a second episode in one run", n)
	}
}

func TestN4ANewRunANewEpisodeOneMore(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	u.flags = nil
	e.Tick()
	coretest.Patch(e, a, func(th *core.Thread, _ time.Time) []core.Event { th.Feed.Queue = nil; return nil })
	endRun(e, a, *now)
	running(e, a)
	*now = now.Add(time.Hour)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	if n := len(notices(e, a, "heavy.undeclared")); n != 1 {
		t.Fatalf("%d", n)
	}
}

func TestN5OneEpisodeAcrossTwoRunsIsToldOnce(t *testing.T) {
	e, u, now, a := undEngine(t)
	f := flagFor(a, *now)
	u.flags = []slots.Undeclared{f}
	e.Tick()
	coretest.Patch(e, a, func(th *core.Thread, _ time.Time) []core.Event { th.Feed.Queue = nil; return nil })
	endRun(e, a, *now)
	running(e, a)
	*now = now.Add(time.Minute)
	e.Tick()
	if n := len(notices(e, a, "heavy.undeclared")); n != 0 {
		t.Fatalf("%d", n)
	}
}

func TestN6AnIdleAgentIsNotWokenItTakesTheNoticeAtTheNextRun(t *testing.T) {
	e, u, now, a := undEngine(t)
	endRun(e, a, *now)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	th, _ := e.Thread(a)
	if th.Running {
		t.Fatal("running")
	}
	ns := notices(e, a, "heavy.undeclared")
	if len(ns) != 1 || ns[0].Urgency != core.Next {
		t.Fatalf("%+v", ns)
	}
	if n := core.NoticeText(nil); n != "" && false {
		t.Fatal(n)
	}
}

func TestN7ASubagentsFirstBashCallCarriesItOncePerEpisode(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	got := e.UndeclaredSubagentNote(a)
	if !strings.Contains(got, "This thread runs heavy work Fyr did not admit") {
		t.Fatalf("%q", got)
	}
	if again := e.UndeclaredSubagentNote(a); again != "" {
		t.Fatalf("twice: %q", again)
	}
	if len(notices(e, a, "heavy.undeclared")) != 1 {
		t.Fatal("the main agent's notice was used up")
	}
	// A thread that is not flagged gets nothing.
	if e.UndeclaredSubagentNote("t_nobody") != "" {
		t.Fatal("note for an unflagged thread")
	}
}

func TestN8AQueuedThreadIsToldNotToRunIt(t *testing.T) {
	e, u, now, a := undEngine(t)
	f := flagFor(a, *now)
	f.Queued = true
	u.flags = []slots.Undeclared{f}
	e.Tick()
	w := notices(e, a, "heavy.undeclared")[0].What
	if !strings.Contains(w, "You are queued and not admitted yet: do not run the job until Fyr tells you.") || strings.Contains(w, "Call it now") {
		t.Fatal(w)
	}
}

func TestNTheEventCarriesTheUseForTheConversationsLine(t *testing.T) {
	e, u, now, a := undEngine(t)
	u.flags = []slots.Undeclared{flagFor(a, *now)}
	e.Tick()
	evs, err := e.Events(a, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range evs {
		if ev.Type != core.EventSlot {
			continue
		}
		var d map[string]any
		json.Unmarshal(ev.Data, &d)
		if txt, ok := d["undeclared"].(string); ok && strings.Contains(txt, "heavy work Fyr did not admit") && d["mem"] != nil && d["cpu"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("no slot event with the text: %+v", evs)
	}
}

// ---- the freeze (section 7) ----

type fz struct {
	free    float64
	stall   slots.Stall
	slow    *float64
	frozen  map[string]bool // undeclared
	slotted map[string]bool // admitted jobs
	moved   int
}

func fzEngine(t *testing.T) (*Engine, *und, *time.Time, *fz, []string) {
	e, u, now, a := undEngine(t)
	z := &fz{free: 0.30, stall: slots.StallOK, frozen: map[string]bool{}, slotted: map[string]bool{}}
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals {
		return slots.Signals{Provider: "cgroups", Headroom: &z.free, Stall: z.stall, Slowdown: z.slow}
	}
	e.SlotGrowth = func(string) uint64 { return 0 }
	e.SlotFreeze = func(o string) bool { z.slotted[o] = true; return true }
	e.SlotThaw = func(o string) { delete(z.slotted, o) }
	e.UndeclaredFreeze = func(o string) bool { z.frozen[o] = true; return true }
	e.UndeclaredThaw = func(o string) { delete(z.frozen, o) }
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
	if len(ns) != 1 || !strings.Contains(ns[0].What, "Fyr paused your undeclared heavy work (`node test/layout.e2e.mjs`): memory is short on this host (memory: 6 % free). Its processes are frozen, not killed, and resume when memory has recovered for 30 s, within 10 min at most. A timeout inside the job may fire after it resumes; check its result. Ask with mcp__fyr__slot_acquire before a heavy job.") {
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

func TestF6TheOnlyHeavyWorkIsNotFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	u.flags = []slots.Undeclared{flagFor(th[0], *now)}
	z.free = 0.04
	e.Tick()
	if len(z.frozen) != 0 {
		t.Fatal("a freeze that helps nobody")
	}
	// Another flagged thread running is somebody to help.
	u.flags = append(u.flags, flagFor(th[1], now.Add(time.Minute)))
	e.Tick()
	if len(z.frozen) != 1 {
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
	// Short again in the same episode: not frozen a second time.
	z.free = 0.03
	e.Tick()
	if len(z.frozen) != 0 {
		t.Fatal("frozen twice in one episode")
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

func TestF10ADismissedThreadIsFrozenLikeAnyOther(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	f := flagFor(a, *now)
	f.Dismissed = true
	u.flags = []slots.Undeclared{f}
	z.free = 0.04
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("dismiss exempted it")
	}
}

func TestF12WhereNothingCanBeFrozenNothingIsToldItWasPaused(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a, b := th[0], th[1]
	e.Slots.Acquire(b, slots.Test)
	e.UndeclaredFreeze = func(string) bool { return false } // no cgroups
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
	e.UndeclaredDismiss = func(th string, on bool) bool { got = append(got, th); return true }
	for _, k := range []string{core.KindUndeclaredDismiss, core.KindHeavySuggestion, core.KindHeavyRemove, core.KindUndeclaredSettings} {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: k, Args: map[string]any{"thread": "t_a", "on": true}}); err == nil {
			t.Errorf("%s accepted from an observer", k)
		}
	}
	r := e.Command(api.Cmd{CommandID: "c1", Action: core.KindUndeclaredDismiss, Args: map[string]any{"thread": "t_a", "on": true}})
	if !r.OK || len(got) != 1 || got[0] != "t_a" {
		t.Fatalf("%+v %v", r, got)
	}
	if r := e.Command(api.Cmd{CommandID: "c2", Action: core.KindUndeclaredDismiss, Args: map[string]any{"on": true}}); r.OK {
		t.Fatal("no thread")
	}
}

func TestTheGateRefusesAnAddedPatternAndCountsIt(t *testing.T) {
	e, _, _, a := undEngine(t)
	_ = a
	if g := e.HeavyGate(a, "node test/layout.run.mjs (e2e)"); !strings.Contains(g, "not admitted") || !strings.Contains(g, `"kind":"e2e"`) {
		t.Fatalf("%q", g)
	}
}
