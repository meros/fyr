package core

import (
	"slices"
	"time"
)

// Settling (SPEC 13.1 `e` and `m`, V0.59 (7), #33, #46). A thread settles
// now, or when a settle Rule fires: its run is done, or a change reached a
// phase ("when merged" is the main change reaching landed). The owner and
// the agent may arm several; the first that fires settles the thread, and
// settling clears them all. Each settle Rule also waits for idle: a
// condition that holds while a run is live waits for the run to end, so a
// working thread is never hidden, and a failed run keeps the thread in the
// inbox until it is dismissed or retried.

// Settle is when the thread settled. What settles it are its settle Rules.
type Settle struct {
	At *time.Time `json:"at,omitempty"`
}

const settleDoneRule = "settle:done"

// settleRule is a settle Rule: when w holds and the thread is idle, it
// settles.
func settleRule(id, about string, w When, by string) Rule {
	return Rule{ID: id, Purpose: PurposeSettle, About: about, When: All(w, When{Kind: WhenIdle}),
		Then: Then{Kind: KindRuleSettle}, After: AfterDisarm, CreatedBy: by}
}

// SettleWhenDone arms "settle when the current run ends without an open
// question".
func SettleWhenDone(t *Thread, by string, now time.Time) {
	ArmRule(t, settleRule(settleDoneRule, "", All(When{Kind: WhenRunEnded}, When{Kind: WhenNoDecisions}), by), now)
}

// SettleWhenReached arms "settle when the change reaches phase", replacing
// the one on the same change.
func SettleWhenReached(t *Thread, changeID string, phase Phase, by string, now time.Time) {
	ArmRule(t, settleRule("settle:"+changeID, changeID, When{Kind: WhenReached, Change: changeID, Phase: phase}, by), now)
}

// SettleWhenMerged arms "settle when merged" (SPEC 13.1 `m`, #32): the
// thread settles when its main change lands. It reports false when the
// thread has no open main PR.
func SettleWhenMerged(t *Thread, by string, now time.Time) bool {
	c := OpenMain(t)
	if c == nil {
		return false
	}
	SettleWhenReached(t, c.ID, PhaseLanded, by, now)
	return true
}

// settleNow settles the thread and clears every settle Rule and the pin
// (V0.104).
func settleNow(t *Thread, now time.Time) {
	t.Settle.At = &now
	t.PinnedAt = nil
	DropSettleRules(t)
}

// DropSettleRules disarms every settle Rule, the agent's too.
func DropSettleRules(t *Thread) {
	DropRules(t, func(r Rule) bool { return r.Purpose == PurposeSettle })
}

// dropSettleDone disarms "when done": it was for the run that ended or the
// one a message replaces.
func dropSettleDone(t *Thread) {
	DropRules(t, func(r Rule) bool { return r.ID == settleDoneRule })
}

// Settling reports an armed settle Rule.
func Settling(t *Thread) bool { return len(RulesOf(t, PurposeSettle)) > 0 }

// SettlesWhenDone reports an armed "settle when done".
func SettlesWhenDone(t *Thread) bool { return FindRule(t, settleDoneRule) != nil }

// SettleOn is a change settle Rule as clients read it (settleWhen).
type SettleOn struct {
	Kind   string `json:"kind"`
	Change string `json:"change"`
	Phase  Phase  `json:"phase"`
}

// reached is the change and phase of a change settle Rule.
func (r Rule) reached() (SettleOn, bool) {
	if r.Purpose != PurposeSettle || len(r.When.All) == 0 || r.When.All[0].Kind != WhenReached {
		return SettleOn{}, false
	}
	w := r.When.All[0]
	return SettleOn{Kind: "change", Change: w.Change, Phase: w.Phase}, true
}

// SettlesWhenMerged reports that the thread settles when its main PR merges.
func SettlesWhenMerged(t *Thread) bool {
	m := t.Main()
	return m != nil && slices.ContainsFunc(t.Rules, func(r Rule) bool {
		s, ok := r.reached()
		return ok && s.Phase == PhaseLanded && FindChange(t, s.Change) == m
	})
}

// SettleWhenChange is the armed change settle Rule a client shows, the one
// that is not "when merged" first, or nil.
func SettleWhenChange(t *Thread) *SettleOn {
	var first *SettleOn
	m := t.Main()
	for _, r := range t.Rules {
		s, ok := r.reached()
		if !ok {
			continue
		}
		if m == nil || s.Phase != PhaseLanded || FindChange(t, s.Change) != m {
			return &s
		}
		if first == nil {
			first = &s
		}
	}
	return first
}
