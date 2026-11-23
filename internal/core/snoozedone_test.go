package core

import (
	"encoding/json"
	"testing"
	"time"
)

// V0.185: the snooze "until work is done".

const snoozeDone = "thread.snoozeWhenDone"

func clockIn() Input { return Input{Source: Observer(ObsClock), Kind: KindClock} }

func runEnd() Input {
	return Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}
}

// workingThread is a thread with a live run, snoozed until its work is done.
func workingThread(t *testing.T) (*Thread, *View) {
	t.Helper()
	th := &Thread{ID: "t_1", ActivityAt: ct0, Conversation: Conversation{Running: true}}
	v := &View{SeenAt: ct0}
	step(t, th, v, Input{Source: FromOwner, Kind: snoozeDone}, ct0)
	if !SnoozedWhenDone(v) || Compute(th, v, ct0).State != Snoozed {
		t.Fatalf("not snoozed: %+v %+v", v.Rules, Compute(th, v, ct0))
	}
	return th, v
}

func TestSnoozeDoneWakesWhenTheRunEnds(t *testing.T) {
	th, v := workingThread(t)
	at := ct0.Add(time.Minute)
	if a := Compute(th, v, at); a.Reason != "until work is done" {
		t.Fatalf("reason %q", a.Reason)
	}
	res := step(t, th, v, runEnd(), at)
	if SnoozedWhenDone(v) || fired(res, snoozeRule) != 1 {
		t.Fatalf("still snoozed: %+v %+v", v.Rules, res.Events)
	}
	if s := Compute(th, v, at).State; s != Done {
		t.Fatalf("state %s, want done", s)
	}
}

// Anything that needs the owner shows the thread at once, and the snooze is
// gone: it never hides a request, not even before a reduce has run.
func TestSnoozeDoneNeverHidesARequest(t *testing.T) {
	ready := []Change{{ID: "c1", Kind: KindCode, Phase: PhaseWorking, PR: &PR{State: "OPEN", Checks: ChecksPass, MergeState: "CLEAN", Head: "abc"}}}
	for name, set := range map[string]func(*Thread){
		"question": func(x *Thread) {
			x.Decisions = []Decision{{ID: "d", Kind: DecisionAsk, CreatedAt: ct0.Add(-time.Hour)}}
		},
		"failure":  func(x *Thread) { x.Failure = &Failure{Message: "no", At: ct0.Add(-time.Hour)} },
		"stuck":    func(x *Thread) { x.Unresponsive = &Unresponsive{Since: ct0, At: ct0, Reason: "no"} },
		"run card": func(x *Thread) { x.Offers = []ActionSpec{{ID: "a", Request: true, Why: "please"}} },
		"merge": func(x *Thread) {
			x.Running = false
			x.Changes = ready
			x.MainChange = "c1"
			x.Rules = []Rule{agentWatch()}
		},
	} {
		t.Run(name, func(t *testing.T) {
			th, v := workingThread(t)
			set(th)
			at := ct0.Add(time.Minute)
			if s := Compute(th, v, at).State; s == Snoozed {
				t.Fatal("hidden before the reduce")
			}
			step(t, th, v, clockIn(), at)
			if SnoozedWhenDone(v) {
				t.Fatalf("the snooze stayed: %+v", v.Rules)
			}
		})
	}
}

func agentWatch() Rule {
	later := ct0.Add(time.Hour)
	return Rule{ID: "w1", Purpose: PurposeWatch, CreatedBy: ByAgent, Status: RuleArmed, When: When{Kind: WhenAt, At: &later},
		Then: Then{Kind: KindRuleNotify, Args: map[string]any{"note": "look"}}, After: AfterDisarm}
}

// What the thread still waits on keeps it snoozed after the run; when that
// ends, it wakes.
func TestSnoozeDoneWaitsForWhatTheThreadArmed(t *testing.T) {
	read := ct0
	pending := []Change{{ID: "c1", Kind: KindCode, Phase: PhaseWorking, PR: &PR{State: "OPEN", Checks: ChecksPending, Head: "abc", CheckedAt: &read}}}
	for name, c := range map[string]struct{ set, clear func(*Thread) }{
		"slot":      {func(x *Thread) { x.SlotQueued = true }, func(x *Thread) { x.SlotQueued = false }},
		"task":      {func(x *Thread) { x.Tasks = []Task{{ID: "k", Status: TaskRunning}} }, func(x *Thread) { x.Tasks = nil }},
		"held ask":  {func(x *Thread) { x.HeldAsks = []Decision{heldMerge()} }, func(x *Thread) { x.HeldAsks = nil }},
		"CI":        {func(x *Thread) { x.Changes = pending; x.MainChange = "c1" }, func(x *Thread) { x.Changes = nil; x.MainChange = "" }},
		"unread":    {func(x *Thread) { x.Outbox = []OutMsg{{ID: "m", State: "sent"}} }, func(x *Thread) { x.Outbox = nil }},
		"agent due": {func(x *Thread) { x.Feed.Queue = []Notice{{Key: "k", Urgency: Wake}} }, func(x *Thread) { x.Feed.Queue = nil }},
	} {
		t.Run(name, func(t *testing.T) {
			th, v := workingThread(t)
			c.set(th)
			step(t, th, v, runEnd(), ct0.Add(time.Minute))
			if !SnoozedWhenDone(v) || Compute(th, v, ct0.Add(time.Minute)).State != Snoozed {
				t.Fatalf("woke early: %+v", Compute(th, v, ct0.Add(time.Minute)))
			}
			c.clear(th)
			step(t, th, v, clockIn(), ct0.Add(2*time.Minute))
			if SnoozedWhenDone(v) {
				t.Fatal("did not wake when the wait ended")
			}
		})
	}
}

func heldMerge() Decision {
	return Decision{ID: "h", Kind: DecisionEffect, Effect: &EffectReq{Action: "pr.merge", Args: map[string]any{"pr": "https://x/pull/1"}}}
}

// A merge ask held for CI keeps the snooze; when CI goes green the ask opens,
// and the same reduce wakes the thread.
func TestSnoozeDoneWakesWhenAHeldMergeAskOpens(t *testing.T) {
	th, v := workingThread(t)
	th.Changes = []Change{{ID: "c1", Kind: KindCode, Phase: PhaseWorking, PR: &PR{URL: "https://x/pull/1", State: "OPEN", Checks: ChecksPending, Head: "abc", MergeState: "BLOCKED"}}}
	th.MainChange = "c1"
	th.HeldAsks = []Decision{heldMerge()}
	step(t, th, v, runEnd(), ct0.Add(time.Minute))
	if !SnoozedWhenDone(v) || len(th.HeldAsks) != 1 {
		t.Fatalf("woke early: held %+v, %+v", th.HeldAsks, Compute(th, v, ct0.Add(time.Minute)))
	}
	th.Changes[0].PR.Checks, th.Changes[0].PR.MergeState = ChecksPass, "CLEAN"
	step(t, th, v, clockIn(), ct0.Add(2*time.Minute))
	if len(th.Decisions) != 1 || SnoozedWhenDone(v) || Compute(th, v, ct0.Add(2*time.Minute)).State != Asking {
		t.Fatalf("decisions %+v, rules %+v, %+v", th.Decisions, v.Rules, Compute(th, v, ct0.Add(2*time.Minute)))
	}
}

// A firing watch wakes the agent: the thread stays snoozed through the turn
// that follows, and wakes when that turn ends.
func TestSnoozeDoneWatchFiresThenTheTurnEnds(t *testing.T) {
	th, v := workingThread(t)
	at := ct0.Add(time.Hour)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w1", When: When{Kind: WhenAt, At: &at}, Then: "notify", Note: "look"}}, ct0)
	step(t, th, v, runEnd(), ct0.Add(time.Minute))
	if !SnoozedWhenDone(v) {
		t.Fatal("woke with a live watch")
	}
	step(t, th, v, clockIn(), at)
	if FindRule(th, "w1") != nil || !SnoozedWhenDone(v) {
		t.Fatalf("after the watch fired: rules %+v, view %+v", th.Rules, v.Rules)
	}
	// The manager takes the notice before the turn starts: not the end of the work.
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: FeedTake{At: AtIdle}}, at)
	if !SnoozedWhenDone(v) {
		t.Fatal("woke between the notice and its turn")
	}
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, at)
	step(t, th, v, runEnd(), at.Add(time.Minute))
	if SnoozedWhenDone(v) {
		t.Fatal("did not wake after the turn")
	}
}

// The safety net: a watch that never fires hides the thread for at most
// SnoozeDoneMax.
func TestSnoozeDoneHasAMaximum(t *testing.T) {
	th, v := workingThread(t)
	th.Rules = []Rule{agentWatch()}
	th.Rules[0].When = When{Kind: WhenIdleFor, Ms: int64(1000 * time.Hour / time.Millisecond)}
	step(t, th, v, runEnd(), ct0.Add(time.Minute))
	end := ct0.Add(SnoozeDoneMax)
	if at := NextCheck(th, v, ct0.Add(time.Minute)); at == nil || !at.Equal(end) {
		t.Fatalf("next check %v, want %v", at, end)
	}
	step(t, th, v, clockIn(), end.Add(-time.Second))
	if !SnoozedWhenDone(v) {
		t.Fatal("woke before the maximum")
	}
	step(t, th, v, clockIn(), end)
	if SnoozedWhenDone(v) || Compute(th, v, end).State == Snoozed {
		t.Fatalf("still hidden at the maximum: %+v", v.Rules)
	}
}

// A thread with no work going cannot take this snooze: the option is off with
// a reason, and the host refuses it, so nothing is hidden silently.
func TestSnoozeDoneNeedsWorkGoing(t *testing.T) {
	for name, c := range map[string]struct {
		th   Thread
		want string
	}{
		"idle":    {Thread{ID: "t"}, SnoozeDoneIdle},
		"asking":  {Thread{ID: "t", Conversation: Conversation{Running: true}, Decisions: []Decision{{ID: "d", Kind: DecisionAsk}}}, SnoozeDoneNeeded},
		"failed":  {Thread{ID: "t", Conversation: Conversation{Failure: &Failure{Message: "x", At: ct0}}}, SnoozeDoneNeeded},
		"settled": {Thread{ID: "t", Conversation: Conversation{Running: true}, Settle: Settle{At: &ct0}}, "Settled threads do not snooze"},
	} {
		t.Run(name, func(t *testing.T) {
			th, v := c.th, &View{SeenAt: ct0}
			if got := Registry[snoozeDone].Available(&th, v, ct0); got != c.want {
				t.Fatalf("reason %q, want %q", got, c.want)
			}
			if _, _, _, err := Reduce(&th, v, Input{Source: FromOwner, Kind: snoozeDone}, ct0); err == nil {
				t.Fatal("the host took it")
			}
		})
	}
}

// As every snooze (V0.104): it removes the pin, and a snoozed thread does not pin.
func TestSnoozeDoneRemovesThePin(t *testing.T) {
	th := &Thread{ID: "t_1", PinnedAt: &ct0, Conversation: Conversation{Running: true}}
	v := &View{SeenAt: ct0}
	step(t, th, v, Input{Source: FromOwner, Kind: snoozeDone}, ct0)
	if th.PinnedAt != nil {
		t.Fatal("still pinned")
	}
	if Registry["thread.pin"].Available(th, v, ct0) == "" {
		t.Fatal("a snoozed thread pins")
	}
	if Registry["thread.unsnooze"].Available(th, v, ct0) != "" {
		t.Fatal("it cannot be unsnoozed")
	}
}

// It is stored on the View as a Rule, so it survives a restart of fyrd.
func TestSnoozeDoneIsStored(t *testing.T) {
	th, v := workingThread(t)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	got := &View{}
	if err := json.Unmarshal(b, got); err != nil {
		t.Fatal(err)
	}
	until, _ := SnoozeOf(got)
	if !SnoozedWhenDone(got) || until == nil || !until.Equal(ct0.Add(SnoozeDoneMax)) {
		t.Fatalf("decoded %+v", got.Rules)
	}
	step(t, th, got, runEnd(), ct0.Add(time.Minute))
	if SnoozedWhenDone(got) {
		t.Fatal("did not wake after the reload")
	}
}

// The timed snooze keeps its meaning.
func TestTimedSnoozeIsNotWorkBound(t *testing.T) {
	th := &Thread{ID: "t_1", ActivityAt: ct0, Conversation: Conversation{Running: true}}
	v := &View{SeenAt: ct0}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.snooze", Args: Args{"until": ct0.Add(time.Hour).Format(time.RFC3339)}}, ct0)
	step(t, th, v, runEnd(), ct0.Add(time.Minute))
	if u, _ := SnoozeOf(v); u == nil || SnoozedWhenDone(v) {
		t.Fatalf("timed snooze changed: %+v", v.Rules)
	}
}
