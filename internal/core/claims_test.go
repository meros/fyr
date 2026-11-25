package core

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// V0.thread-claims: the precedence, written out. A change of ClaimOrder is a
// change of where threads stand, so it is made here too.
func TestClaimOrderIsPinned(t *testing.T) {
	want := []string{"ask", "handover", "quota", "failure", "unresponsive", "no-progress", "run", "starting", "machine",
		"delivering", "gate", "tasks", "slot", "need", "run-request", "command", "merging", "settle-offer", "ci", "watch", "waking", "rule"}
	if !slices.Equal(ClaimOrder, want) {
		t.Fatalf("ClaimOrder = %v", ClaimOrder)
	}
}

type claimCase struct {
	kind  string
	state State
	sec   Section // SeenSec: a quiet claim, the row stays Finished
	build func(x *Thread)
}

func claimCases() []claimCase {
	now := at(10)
	return []claimCase{
		{ClaimAsk, Asking, NeedsYou, func(x *Thread) { x.Decisions = []Decision{{ID: "d", Kind: DecisionAsk, CreatedAt: now}} }},
		{ClaimHandover, Waiting, WorkingSec, func(x *Thread) { x.Handover = &Handover{To: "other", At: now} }},
		{ClaimFailure, Failed, NeedsYou, func(x *Thread) { x.Failure = &Failure{Message: "boom", At: now} }},
		{ClaimUnresponsive, Blocked, NeedsYou, func(x *Thread) { x.Unresponsive = &Unresponsive{Since: now, Reason: "no reply"} }},
		{ClaimStarting, Working, WorkingSec, func(x *Thread) { x.Starting = &now }},
		{ClaimMachine, Waiting, WorkingSec, func(x *Thread) { x.MachineWait = &MachineWait{Since: now, Why: "memory pressure"} }},
		{ClaimDelivering, Seen, SeenSec, func(x *Thread) { x.Outbox = []OutMsg{{ID: "m", State: "sent"}} }},
		{ClaimGate, Waiting, WorkingSec, func(x *Thread) { x.GateHold = &GateHold{Why: "memory is short"} }},
		{ClaimTasks, Waiting, WorkingSec, func(x *Thread) { x.Tasks = []Task{{ID: "k", Status: TaskRunning}} }},
		{ClaimSlot, Waiting, WorkingSec, func(x *Thread) { x.SlotQueued = true }},
		{ClaimNeed, Blocked, NeedsYou, func(x *Thread) {
			x.Stopped = true
			p := ready(headA)
			p.URL, p.Number = "https://x/pull/1", 1
			x.Changes = []Change{{ID: "c1", Kind: KindCode, Phase: PhaseReview, Title: "Fix", PR: &p}}
		}},
		{ClaimRunRequest, Blocked, NeedsYou, func(x *Thread) { x.Offers = []ActionSpec{{ID: "r1", Request: true, Why: "copy"}} }},
		{ClaimCommand, Waiting, WorkingSec, func(x *Thread) {
			x.Offers = []ActionSpec{{ID: "r2", Request: true, Why: "copy"}}
			x.Runs = []ActionRun{{Action: "r2", On: OnThread, Running: true}}
		}},
		{ClaimSettleOffer, Blocked, NeedsYou, func(x *Thread) { x.SettleAsk = &SettleAsk{Why: "PR merged"} }},
		{ClaimCI, Waiting, WorkingSec, func(x *Thread) { x.HeldAsks = []Decision{heldMerge()} }},
		{ClaimWatch, Waiting, WorkingSec, func(x *Thread) { x.Rules = append(x.Rules, agentWatch()) }},
		{ClaimWaking, Seen, SeenSec, func(x *Thread) { x.Feed.Queue = []Notice{{Key: "k", Urgency: Wake}} }},
		{ClaimRule, Seen, SeenSec, func(x *Thread) {
			x.Rules = append(x.Rules, Rule{ID: "s", Purpose: PurposeSettle, CreatedBy: ByAgent, Status: RuleArmed, When: When{Kind: WhenWorkDone}})
		}},
	}
}

// Each claim holds on its own, and the three readers agree on it: the row's
// section, "it needs the owner" and "its work goes on". This is the list the
// audit found in three copies that disagreed (a thread in a handover or with
// a command at the gate read one way on the row and another for the snooze).
func TestEachClaimAloneAgrees(t *testing.T) {
	now := at(10)
	v := &View{SeenAt: now}
	for _, c := range claimCases() {
		th := &Thread{ID: "t", Title: "x", ActivityAt: at(1)}
		c.build(th)
		cs := Claims(th, now, nil)
		if len(cs) != 1 || cs[0].Kind != c.kind {
			t.Errorf("%s: claims %v", c.kind, ClaimKinds(cs))
			continue
		}
		a := Compute(th, v, now)
		if a.State != c.state || a.Section != c.sec {
			t.Errorf("%s: attention %+v, want %s in %s", c.kind, a, c.state, c.sec)
		}
		moving := cs[0].Side == SideMoving
		if got := WorkGoingAt(th, now); got != moving {
			t.Errorf("%s: WorkGoingAt %v, want %v", c.kind, got, moving)
		}
		needs := cs[0].Side == SideYours && c.kind != ClaimSettleOffer
		if got := NeedsOwnerAt(th, now); got != needs {
			t.Errorf("%s: NeedsOwnerAt %v, want %v", c.kind, got, needs)
		}
		if a.Section == WorkingSec && a.Reason == "" && a.State == Waiting {
			t.Errorf("%s: a wait with no reason", c.kind)
		}
		if vs := CheckThread(th, v, now, nil); len(vs) > 0 && c.kind != ClaimRule && c.kind != ClaimNeed {
			for _, x := range vs {
				if x.ID == InvThreadAttentionPlacement {
					t.Errorf("%s: placement invariant: %s", c.kind, x.Evidence)
				}
			}
		}
	}
}

// With every claim at once, Claims lists them in ClaimOrder and the row shows
// the first.
func TestClaimsComeInOrder(t *testing.T) {
	now := at(10)
	th := &Thread{ID: "t", Title: "x"}
	for _, c := range claimCases() {
		c.build(th)
	}
	th.Running, th.RunStartedAt = true, &now
	th.NoProgress = &NoProgress{Since: now}
	got := ClaimKinds(Claims(th, now, nil))
	last := -1
	for _, k := range got {
		i := slices.Index(ClaimOrder, k)
		if i <= last {
			t.Fatalf("claims out of order: %v", got)
		}
		last = i
	}
	if len(got) < 15 {
		t.Fatalf("too few claims hold together: %v", got)
	}
	if a := Compute(th, &View{}, now); a.State != Asking {
		t.Fatalf("the first claim is the row: %+v", a)
	}
}

// The cases the audit found in the wrong place.
func TestClaimsFixWrongPlaces(t *testing.T) {
	now := at(10)
	v := &View{SeenAt: now}
	base := func() *Thread { return &Thread{ID: "t", Title: "x", ActivityAt: at(1)} }

	// A handover of a failed thread reads as moving, not Failed, and not Finished.
	th := base()
	th.Failure = &Failure{Message: "boom", At: now}
	th.Handover = &Handover{To: "other", At: now}
	if a := Compute(th, v, now); a.State != Waiting || a.Reason != "Moving to other" {
		t.Errorf("handover: %+v", a)
	}
	if NeedsOwnerAt(th, now) || !WorkGoingAt(th, now) {
		t.Errorf("handover: the Rules disagree with the row (needs %v, going %v)", NeedsOwnerAt(th, now), WorkGoingAt(th, now))
	}

	// A command at the gate, or one the owner started, is work going on: the
	// thread does not settle "when done" under it, and a snooze keeps waiting.
	for name, set := range map[string]func(*Thread){
		"gate": func(x *Thread) { x.GateHold = &GateHold{Why: "memory is short"} },
		"command": func(x *Thread) {
			x.Offers = []ActionSpec{{ID: "r", Request: true}}
			x.Runs = []ActionRun{{Action: "r", On: OnThread, Running: true}}
		},
	} {
		th := base()
		set(th)
		if !WorkGoingAt(th, now) || WorkOverAt(th, now) || WorkDoneAt(th, now) {
			t.Errorf("%s: going %v over %v done %v", name, WorkGoingAt(th, now), WorkOverAt(th, now), WorkDoneAt(th, now))
		}
	}

	// A held merge ask on a PR Fyr has not read into a change waits; it read Finished.
	th = base()
	th.HeldAsks = []Decision{heldMerge()}
	if a := Compute(th, v, now); a.State != Waiting || a.Reason == "" {
		t.Errorf("held ask without a change: %+v", a)
	}

	// An agent that does not respond has a word for the row and the chip.
	th = base()
	th.Unresponsive = &Unresponsive{Since: now, Reason: "no reply"}
	if a := Compute(th, v, now); a.Word != UnresponsiveWord || a.Subject == "" {
		t.Errorf("unresponsive: %+v", a)
	}
}

// A notice that wakes an idle agent starts its turn: the thread reads
// Starting from the take, not Finished until the turn reports.
func TestWakeReadsStarting(t *testing.T) {
	now := at(10)
	th := &Thread{ID: "t", Title: "x"}
	th.Feed.Queue = []Notice{{About: "thread", Key: "k", What: "CI failed", Urgency: Wake}}
	th, _, _, err := Reduce(th, &View{SeenAt: now}, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: FeedTake{At: AtIdle}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a := Compute(th, &View{SeenAt: now}, now); a.State != Working || a.Reason != StartingText {
		t.Fatalf("after the take: %+v (starting %v)", a, th.Starting)
	}
}

// The owner's Stop ends a run they watched: Finished, seen, not unread.
func TestInterruptedRunIsSeen(t *testing.T) {
	th, v := &Thread{ID: "t", Title: "x"}, &View{}
	var err error
	th, v, _, err = Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, at(1))
	if err != nil {
		t.Fatal(err)
	}
	th, v, _, err = Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{Data: map[string]any{"interrupted": true}}}, at(2))
	if err != nil {
		t.Fatal(err)
	}
	if a := Compute(th, v, at(3)); a.State != Seen {
		t.Fatalf("stopped by the owner: %+v", a)
	}
	// A run that ends by itself is unread.
	th, v, _, _ = Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, at(4))
	th, v, _, _ = Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, at(5))
	if a := Compute(th, v, at(6)); a.State != Done {
		t.Fatalf("ended by itself: %+v", a)
	}
}

// A held ask that waits on something else than checks keeps its own words.
func TestCIWaitTextIsNotGlued(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	th, _ := heldAsk(t, now)
	if r := ciWait(th); r != "Waiting for CI on #846: 2 of 5 checks done" {
		t.Fatalf("checks: %q", r)
	}
	th.Changes[0].PR.MergeState = "UNKNOWN"
	th.Changes[0].PR.Checks = ChecksPass
	if r := ciWait(th); strings.Contains(r, "#846W") || strings.Contains(r, "#846F") || r == "" {
		t.Fatalf("another gate: %q", r)
	}
}
