// V0.load-invariants: each invariant of the load guard, driven with the fake
// deps; a violation writes one log line and opens one card, a repeat within
// a day opens none, Dismiss holds the next for a day, and Fix it starts a
// thread in the Fyr checkout with the evidence.
package engine

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

// invLog captures the log, teed into the engine's ring as fyrd does.
func invLog(t *testing.T, e *Engine) *bytes.Buffer {
	var out bytes.Buffer
	log.SetOutput(io.MultiWriter(&out, e.LogTap()))
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &out
}

// invLines counts the `invariant: id` lines.
func invLines(out *bytes.Buffer, id string) int {
	return strings.Count(out.String(), "invariant: "+id+" ")
}

// invCards are the open cards of id, as the front reads them.
func invCards(e *Engine, id string) []resource.InvariantCard {
	var out []resource.InvariantCard
	for _, c := range e.invariantView().Cards {
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}

func invCount(e *Engine, id string) int {
	for _, s := range e.invariantView().Stats {
		if s.ID == id {
			return s.Count
		}
	}
	return -1
}

func TestInvariantThawIntoCritical(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	a := th[0]
	out := invLog(t, e)
	e.Slots.Acquire(th[1], resource.Test) // V0.sim-fixes (3): not the only heavy work
	u.flags = []resource.Undeclared{flagFor(a, *now)}
	z.free = 0.04
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("setup: not frozen")
	}
	// V0.sim-fixes (1): an episode that ends while the agents' memory stalls
	// critically keeps its work frozen, so the guard itself gives no violation.
	u.flags = nil
	z.agents = resource.StallCritical
	*now = now.Add(10 * time.Second)
	e.Tick()
	if !z.frozen[a] || invLines(out, InvThawIntoCritical) != 0 {
		t.Fatalf("thawed into the critical stall: frozen=%v\n%s", z.frozen, out)
	}
	// A thaw into it (a regression of that rule) is the violation.
	crit := resource.Signals{Provider: "cgroups", AgentsStall: resource.StallCritical}
	e.guardRecord(resource.GuardEvent{At: *now, Kind: "thaw", Work: "undeclared", Thread: a, Reason: "ended"}, crit)
	e.Tick() // checkInvariants runs on it and the view is published
	if n := invLines(out, InvThawIntoCritical); n != 1 {
		t.Fatalf("%d invariant lines:\n%s", n, out)
	}
	cs := invCards(e, InvThawIntoCritical)
	if len(cs) != 1 || !strings.Contains(cs[0].Evidence, "thread="+a) || !strings.Contains(cs[0].Evidence, "agents_stall=critical") {
		t.Fatalf("card: %+v", cs)
	}
	// The card shows the guard's lines of the ten minutes before: the freeze.
	if !strings.Contains(strings.Join(cs[0].Lines, "\n"), "guard: froze undeclared work thread="+a) {
		t.Fatalf("no guard lines on the card: %q", cs[0].Lines)
	}
	if g := e.GuardView(); g == nil || g.Invariants == nil || len(g.Invariants.Cards) != 1 {
		t.Fatalf("the published view has no card: %+v", g)
	}
}

// A thaw at the ceiling or because the processes are gone is no violation.
func TestInvariantThawIntoCriticalNotForGoneOrCeiling(t *testing.T) {
	e, _, now, _, _ := fzEngine(t)
	out := invLog(t, e)
	crit := resource.Signals{Provider: "cgroups", AgentsStall: resource.StallCritical}
	e.guardRecord(resource.GuardEvent{At: *now, Kind: "thaw", Thread: "t1", Reason: "gone"}, crit)
	e.guardRecord(resource.GuardEvent{At: *now, Kind: "thaw", Thread: "t2", Reason: "ceiling"}, crit)
	e.checkInvariants()
	if invLines(out, InvThawIntoCritical) != 0 {
		t.Fatalf("a violation for gone or ceiling:\n%s", out)
	}
}

func TestInvariantFreezeHeldLong(t *testing.T) {
	e, _, now, _, th := fzEngine(t)
	out := invLog(t, e)
	e.mu.Lock()
	e.gs.Frozen = []guard.Frozen{{Owner: th[0], At: now.Add(-InvHeldLong), Undeclared: true}}
	e.mu.Unlock()
	e.checkInvariants()
	if invLines(out, InvFreezeHeldLong) != 0 {
		t.Fatal("a violation at exactly 30 min")
	}
	*now = now.Add(time.Second)
	e.checkInvariants()
	*now = now.Add(10 * time.Second)
	e.checkInvariants()
	if n := invLines(out, InvFreezeHeldLong); n != 1 || len(invCards(e, InvFreezeHeldLong)) != 1 {
		t.Fatalf("%d lines, want one line and one card for one freeze:\n%s", n, out)
	}
}

func TestInvariantFreezeCyclingFourThawsInAnHour(t *testing.T) {
	e, _, now, _, _ := fzEngine(t)
	out := invLog(t, e)
	sig := resource.Signals{Provider: "cgroups"}
	thaw := func(at time.Time) {
		e.guardRecord(resource.GuardEvent{At: at, Kind: "thaw", Thread: "t1", Reason: "memory"}, sig)
		e.checkInvariants()
	}
	for i := range 3 {
		thaw(now.Add(time.Duration(i) * 10 * time.Minute))
	}
	if invLines(out, InvFreezeCycling) != 0 {
		t.Fatal("3 thaws are not more than 3")
	}
	thaw(now.Add(30 * time.Minute))
	thaw(now.Add(40 * time.Minute))
	if n := invLines(out, InvFreezeCycling); n != 1 || len(invCards(e, InvFreezeCycling)) != 1 {
		t.Fatalf("%d lines:\n%s", n, out)
	}
	// Spread out, thaws are no cycle: a thread thawed every 20 min.
	thaw2 := func(at time.Time) {
		e.guardRecord(resource.GuardEvent{At: at, Kind: "thaw", Thread: "t2", Reason: "memory"}, sig)
	}
	for i := range 6 {
		thaw2(now.Add(time.Duration(i) * 20 * time.Minute))
	}
	e.checkInvariants()
	if strings.Contains(out.String(), "thread=t2") {
		t.Fatalf("thaws 20 min apart cycle:\n%s", out)
	}
}

func TestInvariantProbeSilent(t *testing.T) {
	e, _, now, z, _ := fzEngine(t)
	out := invLog(t, e)
	tickFor(e, now, InvProbeSilentAfter)
	if invLines(out, InvProbeSilent) != 0 {
		t.Fatal("a violation at 5 min")
	}
	tickFor(e, now, time.Minute)
	if n := invLines(out, InvProbeSilent); n != 1 {
		t.Fatalf("%d lines:\n%s", n, out)
	}
	s := 1.2
	z.slow = &s // the probe reads again
	tickFor(e, now, time.Minute)
	z.slow = nil
	tickFor(e, now, 6*time.Minute)
	if n := invLines(out, InvProbeSilent); n != 2 || len(invCards(e, InvProbeSilent)) != 1 {
		t.Fatalf("a second silence: %d lines (want 2), one card while it is open:\n%s", n, out)
	}
}

func TestInvariantLimitUnderPrior(t *testing.T) {
	e, _, _, _, _ := fzEngine(t)
	out := invLog(t, e)
	e.JobLimitSet("t1", "go-test", "learned", 3e9, 2e9, false) // above the prior
	e.JobLimitSet("t1", "go-test", "learned", 1e9, 2e9, true)  // a class learned from 3 runs
	e.JobLimitSet("t1", "e2e", "unknown", 0, 2e9, false)       // "max"
	e.checkInvariants()
	if invLines(out, InvLimitUnderPrior) != 0 {
		t.Fatalf("a violation:\n%s", out)
	}
	e.JobLimitSet("t1", "e2e", "unknown", 5e8, 2e9, false)
	e.checkInvariants()
	if n := invLines(out, InvLimitUnderPrior); n != 1 || !strings.Contains(out.String(), `class="e2e" high=0.5 GB prior=2.0 GB why=unknown`) {
		t.Fatalf("%d lines:\n%s", n, out)
	}
}

func TestInvariantPSITriggerMissing(t *testing.T) {
	for _, armed := range []bool{false, true} {
		e, _, now, _, _ := fzEngine(t)
		out := invLog(t, e)
		e.PSITrigger(true, false)
		if armed {
			e.PSITrigger(true, true) // set on a later try
		}
		e.checkInvariants()
		*now = now.Add(InvPSIWithin - time.Minute)
		e.checkInvariants()
		if invLines(out, InvPSITriggerMissing) != 0 {
			t.Fatal("a violation before the hour")
		}
		*now = now.Add(2 * time.Minute)
		e.checkInvariants()
		*now = now.Add(time.Minute)
		e.checkInvariants()
		if want := map[bool]int{false: 1, true: 0}[armed]; invLines(out, InvPSITriggerMissing) != want {
			t.Fatalf("armed=%t: want %d lines:\n%s", armed, want, out)
		}
	}
}

func TestInvariantShortNothingFrozen(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	out := invLog(t, e)
	a := th[0]
	u.flags = []resource.Undeclared{flagFor(a, *now)}
	e.UndeclaredRootsMem = func(string) (uint64, bool) { return 1e6, true } // its work holds little: never frozen
	z.free = 0.04
	// V0.sim-fixes (3): alone, the only heavy work that runs is never frozen
	// whole, so nothing frozen keeps the rule.
	e.Tick()
	tickFor(e, now, InvShortAfter+20*time.Second)
	if invLines(out, InvShortNothingFroze) != 0 {
		t.Fatalf("a violation for the only heavy work:\n%s", out)
	}
	// Beside another thread's job it could be frozen: the clock starts over.
	z.free = 0.5
	e.Tick()
	e.Slots.Acquire(th[1], resource.Test)
	z.free = 0.04
	e.Tick()
	tickFor(e, now, InvShortAfter)
	if invLines(out, InvShortNothingFroze) != 0 {
		t.Fatal("a violation at 10 min")
	}
	tickFor(e, now, 20*time.Second)
	if z.frozen[a] {
		t.Fatal("setup: frozen")
	}
	if n := invLines(out, InvShortNothingFroze); n != 1 || len(invCards(e, InvShortNothingFroze)) != 1 {
		t.Fatalf("%d lines:\n%s", n, out)
	}
}

// V0.sim-fixes (3): short on the headroom alone, with no stall, freezes
// nothing, and nothing frozen is no violation.
func TestInvariantShortNothingFrozenNotWithoutAStall(t *testing.T) {
	e, u, now, z, th := fzEngine(t)
	out := invLog(t, e)
	e.Slots.Acquire(th[1], resource.Test)
	u.flags = []resource.Undeclared{flagFor(th[0], *now)}
	z.free, z.stall = 0.04, resource.StallOK
	e.Tick()
	tickFor(e, now, InvShortAfter+time.Minute)
	if len(z.frozen) != 0 || invLines(out, InvShortNothingFroze) != 0 {
		t.Fatalf("frozen=%v\n%s", z.frozen, out)
	}
}

// One card per invariant a day: a repeat while it is open opens none; after
// Dismiss none comes for a day; then one comes again.
func TestInvariantCardOncePerDayAndDismiss(t *testing.T) {
	e, _, now, _, _ := fzEngine(t)
	out := invLog(t, e)
	v := violation{id: InvLimitUnderPrior, evidence: "thread=t1"}
	e.violate(v, *now)
	*now = now.Add(time.Hour)
	e.violate(v, *now)
	if len(invCards(e, InvLimitUnderPrior)) != 1 || invCount(e, InvLimitUnderPrior) != 2 || invLines(out, InvLimitUnderPrior) != 2 {
		t.Fatalf("cards %d count %d:\n%s", len(invCards(e, InvLimitUnderPrior)), invCount(e, InvLimitUnderPrior), out)
	}
	if r := e.Command(api.Cmd{CommandID: "d1", Action: core.KindInvariantDismiss, Args: map[string]any{"id": InvLimitUnderPrior}}); !r.OK {
		t.Fatalf("dismiss: %+v", r)
	}
	if len(invCards(e, InvLimitUnderPrior)) != 0 {
		t.Fatal("dismissed card still open")
	}
	*now = now.Add(23 * time.Hour)
	e.violate(v, *now)
	if len(invCards(e, InvLimitUnderPrior)) != 0 {
		t.Fatal("a card within a day of Dismiss")
	}
	*now = now.Add(2 * time.Hour)
	e.violate(v, *now)
	if len(invCards(e, InvLimitUnderPrior)) != 1 {
		t.Fatal("no card a day after Dismiss")
	}
	if r := e.Command(api.Cmd{CommandID: "d2", Action: core.KindInvariantDismiss, Args: map[string]any{"id": InvProbeSilent}}); r.OK {
		t.Fatal("dismissed a card that is not open")
	}
}

// The record outlives a restart: the same store, a new engine.
func TestInvariantCardsAreKept(t *testing.T) {
	e, st, now := bare(t)
	e.violate(violation{id: InvProbeSilent, evidence: "x"}, *now)
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if len(invCards(e2, InvProbeSilent)) != 1 || invCount(e2, InvProbeSilent) != 1 {
		t.Fatal("the card did not outlive the restart")
	}
}

// fyrRepo makes a folder that looks like a Fyr checkout.
func fyrRepo(t *testing.T) string {
	d := t.TempDir()
	for _, p := range []string{"cmd/fyrd", "docs/changes"} {
		if err := os.MkdirAll(filepath.Join(d, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(d, "docs/changes/README.md"), []byte("#"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestInvariantFixStartsAThreadWithThePrompt(t *testing.T) {
	e, _, now := bare(t)
	e.HostName = "desk"
	ag := &sends{}
	e.Agents = ag
	e.LogTap().Write([]byte("2026/11/15 09:58:00 fyrd guard: froze job thread=t9\n"))
	e.violate(violation{id: InvFreezeHeldLong, evidence: "thread=t9 frozen=31m0s"}, *now)
	fix := func(id string) api.Reply {
		return e.Command(api.Cmd{CommandID: NewID("c"), Action: core.KindInvariantFix, Args: map[string]any{"id": id}})
	}
	// No Fyr checkout: the button says so.
	if r := fix(InvFreezeHeldLong); r.OK || !strings.Contains(r.Error, "Fyr checkout") {
		t.Fatalf("without a checkout: %+v", r)
	}
	if v := e.invariantView(); v.FixDir != "" {
		t.Fatalf("fixDir %q", v.FixDir)
	}
	other := t.TempDir()
	if r := e.Command(api.Cmd{CommandID: "p0", Action: "project.create", Args: map[string]any{"name": "Other", "folder": other}}); !r.OK {
		t.Fatalf("project: %+v", r)
	}
	dir := fyrRepo(t)
	if r := e.Command(api.Cmd{CommandID: "p1", Action: "project.create", Args: map[string]any{"name": "Fyr", "folder": dir}}); !r.OK {
		t.Fatalf("project: %+v", r)
	}
	if v := e.invariantView(); v.FixDir != dir {
		t.Fatalf("fixDir %q, want %q", v.FixDir, dir)
	}
	r := fix(InvFreezeHeldLong)
	if !r.OK || r.ThreadID == "" {
		t.Fatalf("fix: %+v", r)
	}
	th, ok := e.Thread(r.ThreadID)
	if !ok || th.Cwd != dir || !strings.Contains(th.Title, InvFreezeHeldLong) {
		t.Fatalf("thread: %+v", th)
	}
	text := sentTo(t, ag, r.ThreadID)
	for _, want := range []string{"`freeze-held-long`", "on desk", "thread=t9 frozen=31m0s", "guard: froze job thread=t9", "0029-adaptive-slots.md", "0032-bounded-agents.md", "0036-memory-layers.md", "docs/changes/", "test first", "scripts/ci-local.sh", "open a PR"} {
		if !strings.Contains(text, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, text)
		}
	}
	if len(invCards(e, InvFreezeHeldLong)) != 0 {
		t.Fatal("the card stays open after Fix it")
	}
	if r := fix(InvFreezeHeldLong); r.OK {
		t.Fatal("a second Fix it of a closed card")
	}
}

// sentTo is the first message the engine handed the agent of thread id.
func sentTo(t *testing.T, ag *sends, id string) string {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		ag.mu.Lock()
		for _, g := range ag.got {
			if s, ok := strings.CutPrefix(g, id+" "); ok {
				ag.mu.Unlock()
				return s
			}
		}
		ag.mu.Unlock()
	}
	t.Fatalf("no message to %s", id)
	return ""
}
