// V0.watch-stale: a watch never stays armed on a condition its change can no
// longer report. Landing (or ending) drops checks, review and conflict
// (decision 0018), so a watch on one of those would look armed forever and
// never fire.
package core

import "testing"

// Arming a watch on a change that already landed is refused, rather than
// arming a watch that can never fire.
func TestWatchOnLandedChangeIsRefused(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass}}, ct0)
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "MERGED"}}, ct0)
	c := th.Changes[0]
	if c.Phase != PhaseLanded || condition(&c, CondChecks) != nil {
		t.Fatalf("landing did not drop checks: %s %+v", c.Phase, c.Conditions)
	}
	_, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify", Note: "n",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0)
	if err == nil {
		t.Fatal("a watch on a landed change's checks was armed; it can never fire")
	}
	if FindRule(th, "w_1") != nil {
		t.Fatalf("the refused watch stayed armed: %+v", th.Rules)
	}
}

// A watch armed before the landing is dropped when the change lands, so it
// never sits armed on a condition that is gone.
func TestWatchIsDroppedWhenItsChangeLands(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPending}}, ct0)
	c := th.Changes[0]
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify", Note: "n",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0)
	if FindRule(th, "w_1") == nil {
		t.Fatal("the watch was not armed in the first place")
	}
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "MERGED"}}, ct0)
	if FindRule(th, "w_1") != nil {
		t.Fatalf("the watch stayed armed after its change landed: %+v", th.Rules)
	}
}

// A watch that landing cannot make unwatchable stays: a phase watch, and a
// thread condition, are still readable after the change lands.
func TestLandingKeepsOtherWatches(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPending}}, ct0)
	c := th.Changes[0]
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_reached", Then: "notify", Note: "n",
		When: When{Kind: WhenReached, Change: c.Key, Phase: PhaseLive}}}, ct0)
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "MERGED"}}, ct0)
	if FindRule(th, "w_reached") == nil {
		t.Fatalf("a phase watch was dropped by landing: %+v", th.Rules)
	}
}

// A brand-new change is in flight, so a watch on its checks arms as usual.
func TestWatchOnANewChangeArms(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	c, _, err := OpenChange(th, ChangeOpen{Key: "k", Kind: KindCode}, ByAgent, ct0)
	if err != nil {
		t.Fatal(err)
	}
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify", Note: "n",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0)
	if FindRule(th, "w_1") == nil {
		t.Fatalf("a watch on a new change was refused: %+v", th.Rules)
	}
}
