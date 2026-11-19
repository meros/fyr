package core

import (
	"testing"
	"time"
)

// V0.settle-merge-fixes (2): "settle when merged" waits until no PR of the
// thread is open, not only the main one.
func TestSettleWhenMergedWaitsForEveryPR(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const one, two = "https://github.com/o/r/pull/1", "https://github.com/o/r/pull/2"
	pr := func(url, state string) Input {
		return Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: state, Checks: ChecksPass}}
	}
	PRObserved(th, PR{URL: one, State: "OPEN", Checks: ChecksPass}, ct0)
	PRObserved(th, PR{URL: two, State: "OPEN", Checks: ChecksPass}, ct0)
	if !SettleWhenMerged(th, ByAgent, ct0) {
		t.Fatal("no open main PR")
	}
	if !SettlesWhenMerged(th) {
		t.Fatalf("the client does not read it as when merged: %+v", th.Rules)
	}
	if m := th.Main(); m == nil || m.PR.URL != two {
		t.Fatalf("the main PR: %+v", m)
	}
	step(t, th, v, pr(two, "MERGED"), ct0)
	if th.Settle.At != nil {
		t.Fatal("the main PR merging settled the thread while another PR is open")
	}
	step(t, th, v, pr(one, "MERGED"), ct0.Add(time.Minute))
	if th.Settle.At == nil || Settling(th) {
		t.Fatalf("the last PR merging did not settle: %+v %+v", th.Settle, th.Rules)
	}
}

// V0.settle-merge-fixes (3): a watch that fires once and that resumes on a
// state that came true while the thread was settled fires then, instead of
// staying armed for good (which kept WorkDone false and the thread open).
func TestOneShotWatchFiresOnResume(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/7"
	PRObserved(th, PR{URL: url, State: "OPEN", Checks: ChecksPass}, ct0)
	c := th.Changes[0]
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify", Note: "#7 landed: go on",
		When: When{Kind: WhenReached, Change: c.Key, Phase: PhaseLanded}}}, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle", Args: Args{"now": true}}, ct0)
	if r := FindRule(th, "w_1"); r == nil || r.Status != RuleSuspended {
		t.Fatalf("settling did not suspend the watch: %+v", th.Rules)
	}
	res := step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "MERGED", Checks: ChecksPass}}, ct0)
	if fired(res, "w_1") != 0 {
		t.Fatal("a suspended watch fired")
	}
	res = step(t, th, v, Input{Source: FromOwner, Kind: "thread.unsettle"}, ct0.Add(time.Minute))
	if fired(res, "w_1") != 1 || FindRule(th, "w_1") != nil {
		t.Fatalf("the watch did not fire once on resume: %+v %+v", res.Events, th.Rules)
	}

	// One stored by the old resume (armed, its value true, never fired)
	// fires at the next Input.
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_2", Then: "notify", Note: "again",
		When: When{Kind: WhenReached, Change: c.Key, Phase: PhaseLanded}}}, ct0.Add(2*time.Minute))
	th.Rules = append(th.Rules, Rule{ID: "w_3", Purpose: PurposeWatch, About: c.ID, When: When{Kind: WhenReached, Change: c.ID, Phase: PhaseLanded},
		Then: Then{Kind: KindRuleNotify, Args: map[string]any{"note": "stuck"}}, After: AfterDisarm, CreatedBy: ByAgent, Status: RuleArmed, LastValue: true})
	res = step(t, th, v, Input{Source: FromOwner, Kind: "thread.markSeen"}, ct0.Add(3*time.Minute))
	if fired(res, "w_3") != 1 || FindRule(th, "w_3") != nil {
		t.Fatalf("a stuck watch did not fire: %+v %+v", res.Events, th.Rules)
	}
}

// V0.settle-merge-fixes (1): "merge" in text is read only on a thread with an
// open PR and no merge ask; the reading wakes the agent with the call.
func TestMergeTextOnlyWithoutAnAsk(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	th.RunEnds = 3
	in := Input{Source: Observer(ObsMergeText), Kind: KindMergeText, Args: MergeTextArgs{Run: 3}}
	if res := step(t, th, v, in, ct0); len(res.Events) != 0 {
		t.Fatalf("no PR, yet: %+v", res.Events)
	}
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/1", State: "OPEN", Checks: ChecksPass}, ct0)
	if res := step(t, th, v, in, ct0); !hasType(res.Events, "mergeText.seen") {
		t.Fatalf("an open PR and no ask: %+v", res.Events)
	}
	th.HeldAsks = []Decision{{ID: "d_1", Kind: DecisionEffect, Effect: &EffectReq{Action: FyrMerge}}}
	if res := step(t, th, v, in, ct0); hasType(res.Events, "mergeText.seen") {
		t.Fatal("a merge ask held for CI, yet the agent is told to ask")
	}
}

func hasType(evs []Event, typ string) bool {
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}
