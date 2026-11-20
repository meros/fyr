package core

import (
	"testing"
	"time"
)

// V0.one-tool-vocabulary: an agent that retries a change_request or a watch
// whose reply it lost gets the ask or the watch it made, not a second one.

func TestChangeRequestRetryReturnsTheOpenAsk(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	th, _ := heldAsk(t, now)
	th, _, res, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_again", Change: th.Changes[0].ID, Ask: AskMerge, Reason: "retry"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := res.Reply.(ChangeRequestReply)
	if !r.Again || r.ID != "d_merge" || len(th.HeldAsks) != 1 || len(th.Decisions) != 0 {
		t.Fatalf("a retry made a second ask: %+v held %+v", r, th.HeldAsks)
	}
	// Another method is another ask.
	th, _, res, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_rebase", Change: th.Changes[0].ID, Ask: AskMerge, Args: map[string]any{"method": "rebase"}}}, now)
	if r, _ := res.Reply.(ChangeRequestReply); err != nil || r.Again || len(th.HeldAsks) != 2 {
		t.Fatalf("a different ask was taken for a retry: %v %+v", err, r)
	}
}

// A merge ask may name an untracked PR by its URL: Fyr tracks it as a change
// and holds the ask for CI (it was effect_request pr.merge).
func TestChangeRequestOnAnUntrackedPR(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	th, _, res, err := Reduce(&Thread{ID: "t1"}, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d1", Change: "https://github.com/o/r/pull/9", Ask: AskMerge, Args: map[string]any{"head": "abc1234def"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Changes) != 1 || th.Changes[0].PRURL() != "https://github.com/o/r/pull/9" || len(th.HeldAsks) != 1 || hasEvent(res, "pr.linked") == nil {
		t.Fatalf("changes %+v held %+v", th.Changes, th.HeldAsks)
	}
}

func TestWatchRetryReturnsTheArmedWatch(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1"}
	at := now.Add(time.Hour)
	arm := func(id, note string) Rule {
		t.Helper()
		var res Result
		var err error
		th, _, res, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindRuleArm, Thread: "t1",
			Args: RuleArmArgs{ID: id, When: When{Kind: WhenAt, At: &at}, Then: "notify", Note: note}}, now)
		if err != nil {
			t.Fatal(err)
		}
		return res.Reply.(Rule)
	}
	if r := arm("w_1", "check CI"); r.ID != "w_1" {
		t.Fatalf("first %+v", r)
	}
	if r := arm("w_2", "check CI"); r.ID != "w_1" || len(th.Rules) != 1 {
		t.Fatalf("a retry armed a second watch: %+v %+v", r, th.Rules)
	}
	if r := arm("w_3", "check the deploy"); r.ID != "w_3" || len(th.Rules) != 2 {
		t.Fatalf("another note is another watch: %+v", th.Rules)
	}
}

// settle_when done is watch {when: {kind: workDone}, then: settle}: the
// thread's one "settle when done", and only with then settle, alone.
func TestWatchWorkDoneSettles(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1"}
	RunStarted(th, now) // work goes on: the rule waits
	arm := func(w When, then string) error {
		_, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindRuleArm, Thread: "t1",
			Args: RuleArmArgs{ID: "w_1", When: w, Then: then}}, now)
		return err
	}
	if arm(When{Kind: WhenWorkDone}, "notify") == nil {
		t.Fatal("workDone with notify was taken")
	}
	if arm(All(When{Kind: WhenWorkDone}, When{Kind: WhenIdleFor, Ms: 1}), "settle") == nil {
		t.Fatal("a nested workDone was taken")
	}
	th2, _, res, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindRuleArm, Thread: "t1",
		Args: RuleArmArgs{ID: "w_1", When: When{Kind: WhenWorkDone}, Then: "settle"}}, now)
	if err != nil || !SettlesWhenDone(th2) || res.Reply.(Rule).ID != settleDoneRule {
		t.Fatalf("%v %+v", err, th2.Rules)
	}
}
