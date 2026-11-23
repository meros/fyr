package core

import (
	"testing"
	"time"
)

// V0.one-predicate (2): every reader of a Condition's structure looks
// through All, Any and Not alike.
func TestWhenWalkSeesEveryBranch(t *testing.T) {
	later := ct0.Add(time.Hour)
	cases := []struct {
		name                       string
		w                          When
		anchored, workOver, doneOn bool
	}{
		{"not run ended", When{Not: &When{Kind: WhenRunEnded}}, true, false, false},
		{"all with work over", All(When{Kind: WhenWorkOver}, When{Kind: WhenIdle}), false, true, false},
		{"any with work done", Any(When{Kind: WhenWorkDone}, When{Kind: WhenAt, At: &later}), true, false, true},
		{"not work done", When{Not: &When{Kind: WhenWorkDone}}, false, false, true},
		{"plain idle", When{Kind: WhenIdle}, false, false, false},
	}
	for _, c := range cases {
		if got := c.w.anchored(); got != c.anchored {
			t.Errorf("%s: anchored %v, want %v", c.name, got, c.anchored)
		}
		if got := c.w.workBound(); got != c.workOver {
			t.Errorf("%s: workBound %v, want %v", c.name, got, c.workOver)
		}
		if got := c.w.doneBound(); got != c.doneOn {
			t.Errorf("%s: doneBound %v, want %v", c.name, got, c.doneOn)
		}
	}
	nested := When{Not: &When{Kind: WhenAt, At: &later}}
	if at := nested.nextCheck(&Thread{}); at == nil || !at.Equal(later) {
		t.Errorf("nextCheck under Not: %v", at)
	}
	if c := (When{Not: &When{Kind: WhenReached, Change: "c9"}}).changeOf(); c != "c9" {
		t.Errorf("changeOf under Not: %q", c)
	}
}

// V0.one-predicate (2): due skips every thread Rule that reads workDone,
// so the end of Reduce fires each one, not only the owner's "settle when
// done" (settleDoneRule): another settle Rule on workDone fires too.
func TestEveryWorkDoneRuleFires(t *testing.T) {
	th, v := &Thread{ID: "t", Title: "x", ActivityAt: ct0}, &View{SeenAt: ct0}
	ArmRule(th, Rule{ID: "settle:other", Purpose: PurposeSettle, CreatedBy: ByOwner, When: All(When{Kind: WhenWorkDone}, When{Kind: WhenIdle}),
		Then: Then{Kind: KindRuleSettle}, After: AfterDisarm}, ct0)
	if !WorkDoneAt(th, ct0.Add(time.Minute)) {
		t.Fatal("setup: the thread has work")
	}
	res := step(t, th, v, clockIn(), ct0.Add(time.Minute))
	if fired(res, "settle:other") != 1 {
		t.Fatalf("the workDone settle Rule did not fire: %+v", res.Events)
	}
}
