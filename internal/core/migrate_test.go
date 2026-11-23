package core

import (
	"testing"
	"time"
)

// V0.structure-cost (2): a "settle when done" Rule stored before V0.203 reads the
// retired noDecisions kind, which no longer holds; the load pass gives it
// the Condition it has now, drops any other Rule on it, and a second pass
// changes nothing.
func TestMigrateRetiredWhens(t *testing.T) {
	old := When{All: []When{{Kind: WhenRunEnded}, {Kind: retiredNoDecisions}, {Kind: WhenIdle}}}
	th := &Thread{ID: "t", Rules: []Rule{
		{ID: settleDoneRule, Purpose: PurposeSettle, When: old, Then: Then{Kind: KindRuleSettle}, After: AfterDisarm, CreatedBy: ByOwner, Status: RuleArmed},
		{ID: "w_old", Purpose: PurposeWatch, When: When{Not: &When{Kind: retiredNoDecisions}}, Then: Then{Kind: KindRuleNotify}, CreatedBy: ByAgent, Status: RuleArmed},
	}}
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if !Migrate(th, now) {
		t.Fatal("the pass changed nothing")
	}
	if len(th.Rules) != 1 || th.Rules[0].ID != settleDoneRule {
		t.Fatalf("rules: %+v", th.Rules)
	}
	if w := th.Rules[0].When; w.has(retiredNoDecisions) || !w.doneBound() {
		t.Fatalf("settle when done reads %+v", w)
	}
	if Migrate(th, now) {
		t.Fatal("a second pass changed the thread")
	}
}
