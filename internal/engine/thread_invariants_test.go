// V0.thread-invariants: engine-level tests for thread state, transition,
// restart, temporal, broadcast-drift, slot-queue-drift, and process-consistency
// invariants, plus Needs-you card, transition trace, Dismiss, and Fix it.
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/slots"
)

func TestThreadInvariantApplyCapturesStateViolationAndTransitionLines(t *testing.T) {
	e, _, now := bare(t)
	out := invLog(t, e)
	id := bareThread(t, e)

	// Run a normal transition first so the thread ring records history.
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	// Inject a contradictory state via test.patch (running=true without runStartedAt).
	coretest.Patch(e, id, func(th *core.Thread, _ time.Time) []core.Event {
		th.RunStartedAt = nil
		return nil
	})

	if n := invLines(out, core.InvThreadRunStateContradict); n != 1 {
		t.Fatalf("want 1 invariant line, got %d:\n%s", n, out)
	}
	cs := invCards(e, core.InvThreadRunStateContradict)
	if len(cs) != 1 || cs[0].Kind != InvKindThread || !strings.Contains(cs[0].Evidence, "thread="+id) {
		t.Fatalf("card: %+v", cs)
	}
	joined := strings.Join(cs[0].Lines, "\n")
	if !strings.Contains(joined, "run.started") {
		t.Fatalf("expected recent transition lines on card, got %q", cs[0].Lines)
	}
}

func TestThreadInvariantRestartLeftLiveWork(t *testing.T) {
	e, st, now := bare(t)
	id := bareThread(t, e)
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
		t.Fatal(err)
	}
	// Normal restart recovery cleans up live work -> zero violations.
	e2 := New(st)
	e2.Now = func() time.Time { return *now }
	out := invLog(t, e2)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if n := invLines(out, InvThreadRestartLiveWork); n != 0 {
		t.Fatalf("normal Load reported restart violation:\n%s", out)
	}

	// Simulate a broken restart recovery where a non-kept thread stayed Running.
	e2.mu.Lock()
	e2.threads[id].Running = true
	e2.threads[id].RunStartedAt = now
	e2.mu.Unlock()
	e2.checkThreadLoad()
	if n := invLines(out, InvThreadRestartLiveWork); n != 1 {
		t.Fatalf("expected 1 %s line, got %d:\n%s", InvThreadRestartLiveWork, n, out)
	}
}

func TestThreadInvariantTemporalStuckStates(t *testing.T) {
	t.Run("starting-stuck", func(t *testing.T) {
		e, _, now := bare(t)
		e.Agents = &sends{}
		out := invLog(t, e)
		id := bareThread(t, e) // sets Starting = *now
		*now = now.Add(InvStartingStuck)
		e.Tick()
		if invLines(out, InvThreadStartingStuck) != 0 {
			t.Fatal("fired at exact threshold")
		}
		*now = now.Add(time.Second)
		e.Tick()
		*now = now.Add(time.Minute)
		e.Tick()
		if n := invLines(out, InvThreadStartingStuck); n != 1 || len(invCards(e, InvThreadStartingStuck)) != 1 {
			t.Fatalf("want 1 line + 1 card for one stuck Starting episode, got %d:\n%s", n, out)
		}
		_ = id
	})

	t.Run("handover-stuck", func(t *testing.T) {
		e, _, now := bare(t)
		e.Agents = &sends{}
		out := invLog(t, e)
		id := bareThread(t, e)
		e.mu.Lock()
		e.threads[id].Starting = nil
		e.threads[id].Handover = &core.Handover{To: "agy", At: *now}
		e.mu.Unlock()
		*now = now.Add(InvHandoverStuck + time.Second)
		e.Tick()
		if n := invLines(out, InvThreadHandoverStuck); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadHandoverStuck, n, out)
		}
	})

	t.Run("machine-wait-stuck", func(t *testing.T) {
		e, _, now := bare(t)
		out := invLog(t, e)
		id := bareThread(t, e)
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindMachineWait, Thread: id, Args: core.MachineWaitArgs{Why: "memory critical"}}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(InvMachineWaitStuck + time.Second)
		e.Tick()
		if n := invLines(out, InvThreadMachineWaitStuck); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadMachineWaitStuck, n, out)
		}
	})

	t.Run("settled-still-running", func(t *testing.T) {
		e, _, now := bare(t)
		e.Agents = &sends{}
		out := invLog(t, e)
		id := bareThread(t, e)
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
			t.Fatal(err)
		}
		if r := e.Command(api.Cmd{CommandID: "settle1", Action: "thread.settle", ThreadID: id}); !r.OK {
			t.Fatalf("settle: %+v", r)
		}
		// Within grace: no violation.
		*now = now.Add(InvSettledRunningGrace)
		e.Tick()
		if invLines(out, InvThreadSettledStillRuns) != 0 {
			t.Fatal("fired within grace")
		}
		*now = now.Add(time.Second)
		e.Tick()
		if n := invLines(out, InvThreadSettledStillRuns); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadSettledStillRuns, n, out)
		}
	})

	t.Run("outbox-stuck", func(t *testing.T) {
		e, _, now := bare(t)
		e.Agents = &sends{}
		out := invLog(t, e)
		id := bareThread(t, e)
		e.mu.Lock()
		e.threads[id].Starting = nil
		e.threads[id].Outbox = []core.OutMsg{{ID: "m1", State: "sent"}}
		e.mu.Unlock()
		e.Tick()
		*now = now.Add(InvOutboxStuck)
		e.Tick()
		if invLines(out, InvThreadOutboxStuck) != 0 {
			t.Fatal("fired at exact grace")
		}
		*now = now.Add(time.Second)
		e.Tick()
		if n := invLines(out, InvThreadOutboxStuck); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadOutboxStuck, n, out)
		}
	})

	t.Run("waking-feed-stalled", func(t *testing.T) {
		e, _, now := bare(t)
		e.Agents = &sends{}
		out := invLog(t, e)
		id := bareThread(t, e)
		e.mu.Lock()
		e.threads[id].Starting = nil
		e.threads[id].Feed.Queue = []core.Notice{{About: "thread", Key: "k1", What: "wake up", Urgency: core.Wake, At: *now}}
		e.mu.Unlock()
		*now = now.Add(InvFeedStuck + time.Second)
		e.Tick()
		if n := invLines(out, InvThreadWakingFeedStalled); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadWakingFeedStalled, n, out)
		}
	})

	t.Run("action-run-stuck", func(t *testing.T) {
		e, _, now := bare(t)
		out := invLog(t, e)
		id := bareThread(t, e)
		e.mu.Lock()
		e.threads[id].Starting = nil
		e.threads[id].Runs = []core.ActionRun{{On: core.OnThread, Action: "run-1", Label: "Run", By: core.ByOwner, At: *now, Running: true}}
		e.mu.Unlock()
		*now = now.Add(InvActionRunStuck + time.Second)
		e.Tick()
		if n := invLines(out, InvThreadActionRunStuck); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadActionRunStuck, n, out)
		}
	})
}

func TestThreadInvariantOverdueRuleAttentionAndSlotDrift(t *testing.T) {
	t.Run("rule-overdue", func(t *testing.T) {
		e, _, now := bare(t)
		out := invLog(t, e)
		id := bareThread(t, e)
		// Arm a rule whose condition holds now (WhenIdle) directly in memory without firing,
		// and call checkThreadTick directly as if Tick's clock pass missed it.
		e.mu.Lock()
		e.threads[id].Starting = nil
		e.threads[id].Rules = []core.Rule{{
			ID: "w_overdue", Purpose: core.PurposeWatch,
			When:   core.When{Kind: core.WhenIdle},
			Then:   core.Then{Kind: core.KindRuleNotify},
			After:  core.AfterDisarm,
			Status: core.RuleArmed,
		}}
		e.mu.Unlock()
		e.checkThreadTick(*now)
		if n := invLines(out, InvThreadRuleOverdue); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadRuleOverdue, n, out)
		}
	})

	t.Run("attention-drift", func(t *testing.T) {
		e, _, now := bare(t)
		out := invLog(t, e)
		id := bareThread(t, e)
		e.mu.Lock()
		e.last[id] = core.Settled // deliberately stale vs Working(Starting)
		e.mu.Unlock()
		e.checkThreadTick(*now)
		if n := invLines(out, InvThreadAttentionDrift); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadAttentionDrift, n, out)
		}
	})

	t.Run("slot-queue-drift", func(t *testing.T) {
		e, st, _ := bare(t)
		e.UseSlots(slots.Open(1, st))
		out := invLog(t, e)
		id := bareThread(t, e)
		e.mu.Lock()
		e.threads[id].SlotQueued = true // pool has nobody waiting
		e.mu.Unlock()
		e.checkSlotQueueInvariant()
		if n := invLines(out, InvThreadSlotQueueDrift); n != 1 {
			t.Fatalf("want 1 %s, got %d:\n%s", InvThreadSlotQueueDrift, n, out)
		}
	})
}

func TestThreadInvariantProcessConsistency(t *testing.T) {
	e, _, now := bare(t)
	e.Agents = &sends{}
	snap := map[string]AgentState{}
	e.AgentInspect = func() map[string]AgentState { return snap }
	guardDeps(t, e)
	out := invLog(t, e)

	tPhantomRun := bareThread(t, e)
	tGhostRun := bareThread(t, e)
	tPhantomTasks := bareThread(t, e)
	tPhantomDec := bareThread(t, e)
	tPhantomOut := bareThread(t, e)

	e.mu.Lock()
	// 1. Engine says Running=true, agent has no live session.
	e.threads[tPhantomRun].Starting = nil
	e.threads[tPhantomRun].Running = true
	e.threads[tPhantomRun].RunStartedAt = now

	// 2. Engine says Running=false, agent says TurnLive=true.
	e.threads[tGhostRun].Starting = nil
	snap[tGhostRun] = AgentState{Live: true, TurnLive: true}

	// 3. Engine has running task, agent has no live session.
	e.threads[tPhantomTasks].Starting = nil
	e.threads[tPhantomTasks].Tasks = []core.Task{{ID: "k1", Status: core.TaskRunning, UpdatedAt: *now}}

	// 4. Engine has process-bound ask, agent is live but not waiting on it.
	e.threads[tPhantomDec].Starting = nil
	e.threads[tPhantomDec].Running = true
	e.threads[tPhantomDec].RunStartedAt = now
	e.threads[tPhantomDec].Decisions = []core.Decision{{ID: "d_lost", Kind: core.DecisionAsk, Assignee: core.Owner, CreatedAt: *now}}
	snap[tPhantomDec] = AgentState{Live: true, TurnLive: true, Asks: nil}

	// 5. Engine has unread outbox, agent has no live or starting session.
	e.threads[tPhantomOut].Starting = nil
	e.threads[tPhantomOut].Outbox = []core.OutMsg{{ID: "m_lost", State: "sent"}}
	e.mu.Unlock()

	// First tick starts grace clocks; no violations yet.
	e.Tick()
	*now = now.Add(InvProcessDriftGrace)
	e.Tick()
	for _, id := range []string{InvThreadPhantomRunning, InvThreadGhostRunning, InvThreadPhantomTasks, InvThreadPhantomDecision, InvThreadPhantomOutbox} {
		if invLines(out, id) != 0 {
			t.Fatalf("%s fired within grace:\n%s", id, out)
		}
	}

	// Past grace: each fires exactly once.
	*now = now.Add(time.Second)
	e.Tick()
	*now = now.Add(10 * time.Second)
	e.Tick()
	for _, id := range []string{InvThreadPhantomRunning, InvThreadGhostRunning, InvThreadPhantomTasks, InvThreadPhantomDecision, InvThreadPhantomOutbox} {
		if n := invLines(out, id); n != 1 {
			t.Errorf("%s: want 1 line, got %d:\n%s", id, n, out)
		}
	}

	// Resolving and re-breaking starts a fresh episode.
	snap[tGhostRun] = AgentState{Live: true, TurnLive: false}
	*now = now.Add(time.Second)
	e.Tick()
	snap[tGhostRun] = AgentState{Live: true, TurnLive: true}
	*now = now.Add(time.Second)
	e.Tick()
	*now = now.Add(InvProcessDriftGrace + time.Second)
	e.Tick()
	if n := invLines(out, InvThreadGhostRunning); n != 2 {
		t.Fatalf("second episode of %s: want 2 lines, got %d:\n%s", InvThreadGhostRunning, n, out)
	}
}

func TestThreadInvariantFixStartsThreadWithTailoredPrompt(t *testing.T) {
	e, _, now := bare(t)
	e.HostName = "desk"
	ag := &sends{}
	e.Agents = ag
	dir := fyrRepo(t)
	if r := e.Command(api.Cmd{CommandID: "p1", Action: "project.create", Args: map[string]any{"name": "Fyr", "folder": dir}}); !r.OK {
		t.Fatalf("project: %+v", r)
	}
	e.recordThreadTransition("t_broken", *now, "thread.settle (owner) working→settled running=true tasks=0 dec=0 out=0 settled=true archived=false")
	e.violate(violation{id: InvThreadSettledStillRuns, evidence: "thread=t_broken running 3m0s after settle", thread: "t_broken"}, *now)

	view := e.InvariantView()
	if len(view.Cards) != 1 || view.Cards[0].Kind != InvKindThread || view.Cards[0].ID != InvThreadSettledStillRuns {
		t.Fatalf("cards: %+v", view.Cards)
	}
	r := e.Command(api.Cmd{CommandID: "fix1", Action: core.KindInvariantFix, Args: map[string]any{"id": InvThreadSettledStillRuns}})
	if !r.OK || r.ThreadID == "" {
		t.Fatalf("fix: %+v", r)
	}
	th, ok := e.Thread(r.ThreadID)
	if !ok || th.Title != "Fix thread invariant "+InvThreadSettledStillRuns {
		t.Fatalf("fix thread: %+v", th)
	}
	text := sentTo(t, ag, r.ThreadID)
	for _, want := range []string{
		"Fyr's thread invariant `thread-settled-still-running`",
		"on desk",
		"thread=t_broken running 3m0s after settle",
		"thread.settle (owner) working→settled",
		"docs/SPEC.md",
		"internal/core/thread_invariants.go",
		"docs/changes/thread-invariants.md",
		"scripts/ci-local.sh",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt missing %q:\n%s", want, text)
		}
	}
	if len(invCards(e, InvThreadSettledStillRuns)) != 0 {
		t.Fatal("card remained open after Fix it")
	}
}

// V0.thread-self-wake-churn: a thread whose CLI starts turns by itself that
// the provider (or the adapter's settle) ends with nothing behind them is a
// loop the owner must see; a thread the owner or the feed starts is not.
func TestThreadInvariantSelfWakeChurn(t *testing.T) {
	e, _, now := bare(t)
	out := invLog(t, e)
	id := bareThread(t, e)

	// A run the agent manager opened for itself: KindRunStarted, with no send
	// behind it, and the CLI's result carries no measured duration.
	selfRun := func() {
		t.Helper()
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(2 * time.Second)
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{Data: map[string]any{"interrupted": false}}}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(30 * time.Second)
	}

	// Two inside the window are not yet a loop.
	selfRun()
	selfRun()
	if n := invLines(out, InvThreadSelfWakeChurn); n != 0 {
		t.Fatalf("fired below the threshold:\n%s", out)
	}
	selfRun()
	if n := invLines(out, InvThreadSelfWakeChurn); n != 1 {
		t.Fatalf("want 1 %s, got %d:\n%s", InvThreadSelfWakeChurn, n, out)
	}
	cs := invCards(e, InvThreadSelfWakeChurn)
	if len(cs) != 1 || cs[0].Kind != InvKindThread || !strings.Contains(cs[0].Evidence, "thread="+id) {
		t.Fatalf("card: %+v", cs)
	}

	// The owner's own turns never count, and a run the owner interrupted
	// never does either, whatever its duration.
	e2, _, now2 := bare(t)
	out2 := invLog(t, e2)
	id2 := bareThread(t, e2)
	for range InvSelfWakeRuns + 1 {
		if _, err := e2.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id2, Args: core.RunEndedArgs{Data: map[string]any{"interrupted": false}}}); err != nil {
			t.Fatal(err)
		}
		*now2 = now2.Add(30 * time.Second)
	}
	if n := invLines(out2, InvThreadSelfWakeChurn); n != 0 {
		t.Fatalf("fires without a self-start:\n%s", out2)
	}
	// A quota-refused self-start counts even with a measured duration.
	for range InvSelfWakeRuns {
		if _, err := e2.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id2}); err != nil {
			t.Fatal(err)
		}
		if _, err := e2.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id2, Args: core.RunEndedArgs{
			Failure: &core.Failure{Class: core.ClassQuota, Message: "Individual quota reached. Resets in 3h32m5s."},
			Data:    map[string]any{"interrupted": false, "durationMs": float64(800)},
		}}); err != nil {
			t.Fatal(err)
		}
		*now2 = now2.Add(30 * time.Second)
	}
	if n := invLines(out2, InvThreadSelfWakeChurn); n != 1 {
		t.Fatalf("quota-refused self-starts did not count:\n%s", out2)
	}
}

func TestThreadInvariantDuplicateSend(t *testing.T) {
	e, _, now := bare(t)
	e.Agents = &sends{}
	out := invLog(t, e)
	id := bareThread(t, e)

	send := func(cmdID, text, actor string) {
		t.Helper()
		r := e.submit(core.Input{Source: core.FromOwner, Actor: actor, Kind: "thread.send", Thread: id, Args: core.Args{"text": text}, CmdID: cmdID})
		if !r.OK {
			t.Fatalf("send %s: %+v", cmdID, r)
		}
	}

	send("c1", "Status?", "device:d1")
	// Different text immediately is fine.
	*now = now.Add(500 * time.Millisecond)
	send("c2", "Any news?", "device:d1")
	if n := invLines(out, InvThreadDuplicateSend); n != 0 {
		t.Fatalf("fired on distinct text:\n%s", out)
	}
	// Identical text with a new commandId within InvDuplicateSendWindow fires.
	*now = now.Add(2 * time.Millisecond)
	send("c3", "Any news?", "device:d1")
	if n := invLines(out, InvThreadDuplicateSend); n != 1 {
		t.Fatalf("want 1 %s, got %d:\n%s", InvThreadDuplicateSend, n, out)
	}
	// Past window does not fire.
	*now = now.Add(InvDuplicateSendWindow + time.Second)
	send("c4", "Any news?", "device:d1")
	if n := invLines(out, InvThreadDuplicateSend); n != 1 {
		t.Fatalf("fired past window:\n%s", out)
	}
}

func TestThreadInvariantFailureChurn(t *testing.T) {
	e, _, now := bare(t)
	e.Agents = &sends{}
	out := invLog(t, e)
	id := bareThread(t, e)

	failRun := func(class, msg string) {
		t.Helper()
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(2 * time.Second)
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{
			Failure: &core.Failure{Class: class, Message: msg},
			Data:    map[string]any{"interrupted": false, "durationMs": float64(500)},
		}}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(20 * time.Second)
	}

	// Owner manual retry between failures resets the streak and never fires.
	failRun("api", "API 500")
	failRun("api", "API 500")
	if r := e.Command(api.Cmd{CommandID: "retry1", Action: "thread.retry", ThreadID: id}); !r.OK {
		t.Fatalf("retry: %+v", r)
	}
	failRun("api", "API 500")
	if n := invLines(out, InvThreadFailureChurn); n != 0 {
		t.Fatalf("fired across owner retry:\n%s", out)
	}

	// Three consecutive unattended failures within window fire once.
	failRun(core.ClassQuota, "Individual quota reached. Resets in 1h.")
	failRun(core.ClassQuota, "Individual quota reached. Resets in 1h.")
	if n := invLines(out, InvThreadFailureChurn); n != 1 {
		t.Fatalf("want 1 %s, got %d:\n%s", InvThreadFailureChurn, n, out)
	}
}

func TestThreadInvariantCleanEndQuotaNotice(t *testing.T) {
	e, _, now := bare(t)
	out := invLog(t, e)
	id := bareThread(t, e)

	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindDigest, Thread: id, Args: core.DigestArgs{
		Summary: "⚠ Individual quota reached. Please upgrade your subscription. Resets in 1h.",
	}}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{
		Data: map[string]any{"interrupted": false, "durationMs": float64(2000)},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := invLines(out, InvThreadQuotaMissedOnEnd); n != 1 {
		t.Fatalf("want 1 %s for missed mid-turn quota notice, got %d:\n%s", InvThreadQuotaMissedOnEnd, n, out)
	}
}

func TestThreadInvariantGhostDecisionLiveOutboxGhostBGAndSilentRun(t *testing.T) {
	e, _, now := bare(t)
	e.Agents = &sends{}
	snap := map[string]AgentState{}
	e.AgentInspect = func() map[string]AgentState { return snap }
	guardDeps(t, e)
	out := invLog(t, e)

	tGhostDec := bareThread(t, e)
	tLiveOutbox := bareThread(t, e)
	tGhostBG := bareThread(t, e)
	tSilent := bareThread(t, e)

	e.mu.Lock()
	e.threads[tGhostDec].Starting = nil
	snap[tGhostDec] = AgentState{Live: true, TurnLive: true, Asks: []string{"d_orphaned"}}

	e.threads[tLiveOutbox].Starting = nil
	e.threads[tLiveOutbox].Outbox = []core.OutMsg{{ID: "m_orphan", State: "queued"}}
	snap[tLiveOutbox] = AgentState{Live: true, TurnLive: false, Outbox: nil}

	e.threads[tGhostBG].Starting = nil
	snap[tGhostBG] = AgentState{Live: true, TurnLive: false, BG: 2}

	e.threads[tSilent].Starting = nil
	e.threads[tSilent].Running = true
	start := *now
	e.threads[tSilent].RunStartedAt = &start
	e.threads[tSilent].Activity = "Fyr thread rename"
	snap[tSilent] = AgentState{Live: true, TurnLive: true, LastOut: start, OpenTools: 0, Judging: 0}
	e.mu.Unlock()

	e.Tick()
	*now = now.Add(InvProcessDriftGrace + time.Second)
	e.Tick()
	for _, id := range []string{InvThreadGhostDecision, InvThreadPhantomOutbox, InvThreadPhantomTasks} {
		if n := invLines(out, id); n != 1 {
			t.Errorf("%s: want 1 line, got %d:\n%s", id, n, out)
		}
	}
	if invLines(out, InvThreadRunSilentStuck) != 0 {
		t.Fatalf("%s fired before threshold:\n%s", InvThreadRunSilentStuck, out)
	}

	// Open tool suppresses silent-run invariant even past threshold.
	snap[tSilent] = AgentState{Live: true, TurnLive: true, LastOut: start, OpenTools: 1}
	*now = start.Add(InvRunSilentStuck + time.Minute)
	e.Tick()
	if invLines(out, InvThreadRunSilentStuck) != 0 {
		t.Fatalf("%s fired while a tool was open:\n%s", InvThreadRunSilentStuck, out)
	}

	// With no open tools and silence past InvRunSilentStuck, it fires once.
	snap[tSilent] = AgentState{Live: true, TurnLive: true, LastOut: start, OpenTools: 0}
	e.Tick()
	*now = now.Add(time.Minute)
	e.Tick()
	if n := invLines(out, InvThreadRunSilentStuck); n != 1 {
		t.Fatalf("want 1 %s, got %d:\n%s", InvThreadRunSilentStuck, n, out)
	}
}
