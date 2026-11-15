package core

import (
	"slices"
	"time"
)

// Settling (SPEC 13.1 `e` and `m`, V0.59 (7), #33). A thread settles now, or
// when a condition holds: its run is done, or a change reached a phase ("when
// merged" is the main change reaching landed). The owner and the agent may
// arm several conditions; any one that holds settles the thread, and
// settling clears them all. A condition that holds while a run is live waits
// for the run to end, so a working thread is never hidden, and a failed run
// keeps the thread in the inbox until it is dismissed or retried. CheckSettle is
// the one evaluator; it runs after every observation that can satisfy a
// condition (a run ends, a change moves) and when one is armed.

// Settle is when the thread settled and what settles it.
type Settle struct {
	At   *time.Time   `json:"at,omitempty"`
	When []SettleCond `json:"when,omitempty"`
}

// SettleCond is one armed condition: the run is done, or Change reached
// Phase.
type SettleCond struct {
	Kind   SettleKind `json:"kind"`
	Change string     `json:"change,omitempty"`
	Phase  Phase      `json:"phase,omitempty"`
}

// Has reports an armed condition of this kind.
func (s *Settle) Has(k SettleKind) bool {
	return slices.ContainsFunc(s.When, func(c SettleCond) bool { return c.Kind == k })
}

// Drop disarms the conditions of this kind.
func (s *Settle) Drop(k SettleKind) {
	s.When = slices.DeleteFunc(s.When, func(c SettleCond) bool { return c.Kind == k })
	if len(s.When) == 0 {
		s.When = nil
	}
}

// ArmSettle adds a condition, replacing one of the same kind on the same
// change, and evaluates at once.
func ArmSettle(t *Thread, c SettleCond, now time.Time) {
	t.Settle.arm(c)
	CheckSettle(t, now)
}

func (s *Settle) arm(c SettleCond) {
	i := slices.IndexFunc(s.When, func(x SettleCond) bool { return x.Kind == c.Kind && x.Change == c.Change })
	if i >= 0 {
		s.When[i] = c
	} else {
		s.When = append(s.When, c)
	}
}

// CheckSettle settles the thread when any armed condition holds, no run is
// live and the last run did not fail. It reports whether it settled.
func CheckSettle(t *Thread, now time.Time) bool {
	if t.Settle.At != nil || t.Running || t.Failure != nil || len(t.Settle.When) == 0 {
		return false
	}
	for _, c := range t.Settle.When {
		if settleHolds(t, c) {
			t.Settle = Settle{At: &now}
			return true
		}
	}
	return false
}

func settleHolds(t *Thread, c SettleCond) bool {
	switch c.Kind {
	case SettleDone:
		// The run ended (CheckSettle waits for that) and asked nothing.
		return len(t.Decisions) == 0
	case SettleChange:
		ch := FindChange(t, c.Change)
		return ch != nil && ch.Phase.Reached(c.Phase)
	}
	return false
}

// SettleWhenMerged arms "settle when merged" (SPEC 13.1 `m`, #32): the
// thread settles when its main change lands. It reports false when the
// thread has no open main PR.
func SettleWhenMerged(t *Thread, now time.Time) bool {
	c := OpenMain(t)
	if c == nil {
		return false
	}
	ArmSettle(t, SettleCond{Kind: SettleChange, Change: c.ID, Phase: PhaseLanded}, now)
	return true
}

// SettlesWhenMerged reports that the thread settles when its main PR merges.
func SettlesWhenMerged(t *Thread) bool {
	m := t.Main()
	return m != nil && slices.ContainsFunc(t.Settle.When, func(c SettleCond) bool {
		return c.Kind == SettleChange && c.Phase == PhaseLanded && FindChange(t, c.Change) == m
	})
}

// SettleWhenChange is the armed change condition a client shows, the one
// that is not "when merged" first, or nil.
func SettleWhenChange(t *Thread) *SettleCond {
	var first *SettleCond
	m := t.Main()
	for i := range t.Settle.When {
		c := &t.Settle.When[i]
		if c.Kind != SettleChange {
			continue
		}
		if m == nil || c.Phase != PhaseLanded || FindChange(t, c.Change) != m {
			return c
		}
		if first == nil {
			first = c
		}
	}
	return first
}
