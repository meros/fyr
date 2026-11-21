package core

import (
	"errors"
	"slices"
	"time"
)

// Settling (SPEC 13.1 `e` and `m`, V0.59 (7), #33, #46, V0.203). Settle
// settles now. A thread settles later only when a settle Rule was armed for
// it: its work is done, or a change reached a phase ("when merged" is the main change reaching landed). The owner and
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
func settleRule(id, about string, w When, by string, also ...When) Rule {
	return Rule{ID: id, Purpose: PurposeSettle, About: about, When: All(append([]When{w, {Kind: WhenIdle}}, also...)...),
		Then: Then{Kind: KindRuleSettle}, After: AfterDisarm, CreatedBy: by}
}

// SettleDoneIdle is why "settle when done" is off on a thread with nothing
// left to wait for: Settle does it now.
const SettleDoneIdle = "Nothing to wait for"

// SettleWhenDone arms "settle when the work is done" (V0.203, WorkDone).
func SettleWhenDone(t *Thread, by string, now time.Time) {
	ArmRule(t, settleRule(settleDoneRule, "", When{Kind: WhenWorkDone}, by), now)
}

// WorkDone reports that nothing is left on the thread (V0.203): no work goes
// on, nothing waits for the owner, and no PR is open. The thread's own settle
// Rules do not count as work: they are what waits for this.
func WorkDone(t *Thread) bool { return WorkDoneAt(t, time.Now()) }

// WorkDoneAt is WorkDone at now (V0.quota-wait: a quota wait ends at its reset).
func WorkDoneAt(t *Thread, now time.Time) bool {
	if NeedsOwnerNowAt(t, now) || workGoing(t, func(r Rule) bool { return r.Purpose != PurposeSettle }, now) {
		return false
	}
	return !OpenPR(t)
}

// OpenPR reports a PR of the thread that is still open and still counts:
// not dismissed, not done, not ended.
func OpenPR(t *Thread) bool {
	return slices.ContainsFunc(t.Changes, func(c Change) bool {
		return c.PR != nil && c.PR.State == "OPEN" && !c.Dismissed && !c.IsDone() && !c.Phase.Ended()
	})
}

// settleWhenWorkDone fires "settle when done" when it holds. It runs last in
// Reduce, as wakeWhenDone does: what ends the work is read only after the
// merge asks are gated and the agent's notices are queued. A feed.take is
// skipped: the notice has left the queue and its turn has not begun.
func settleWhenWorkDone(r Rec, in Input, now time.Time) Result {
	var res Result
	x := FindRule(r.T, settleDoneRule)
	if in.Kind == KindFeedTake || x == nil || !x.When.doneBound() {
		return res
	}
	suspend(x, r.T, now)
	if x.Status == RuleArmed && x.When.eval(r.T, x.Armed, now) == yes {
		fire(r, x.ID, false, now, &res)
	}
	return res
}

// SettleWhenReached arms "settle when the change reaches phase", replacing
// the one on the same change.
func SettleWhenReached(t *Thread, changeID string, phase Phase, by string, now time.Time) {
	ArmRule(t, settleRule("settle:"+changeID, changeID, When{Kind: WhenReached, Change: changeID, Phase: phase}, by), now)
}

// SettleWhenMerged arms "settle when merged" (SPEC 13.1 `m`, #32): the
// thread settles when its main change lands and no other PR of the thread
// is open (V0.settle-merge-fixes (2)). It reports false when the thread has
// no open main PR.
func SettleWhenMerged(t *Thread, by string, now time.Time) bool {
	c := OpenMain(t)
	if c == nil {
		return false
	}
	ArmRule(t, settleRule("settle:"+c.ID, c.ID, When{Kind: WhenReached, Change: c.ID, Phase: PhaseLanded}, by, When{Kind: WhenNoOpenPR}), now)
	return true
}

// settleNow settles the thread and clears every settle Rule and the pin
// (V0.104).
func settleNow(t *Thread, now time.Time) {
	t.Settle.At = &now
	t.PinnedAt = nil
	t.SettleAsk = nil
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

// autoSettle is a settle Rule that the owner did not arm: the agent's
// watch (once also settle_when), or Fyr's own. It never fires while the thread is
// pinned, and unpin drops it (V0.106). The owner's Settle, now or deferred
// (when done, when merged), is the owner's press and still fires.
func autoSettle(r Rule) bool { return r.Purpose == PurposeSettle && r.CreatedBy != ByOwner }

// DropAutoSettle disarms the settle Rules the owner did not arm (V0.106).
func DropAutoSettle(t *Thread) { DropRules(t, autoSettle) }

// settlesWhenDoneByOwner reports a "settle when done" that will fire: the
// owner's, or anyone's on a thread that is not pinned (V0.106).
func settlesWhenDoneByOwner(t *Thread) bool {
	r := FindRule(t, settleDoneRule)
	return r != nil && (t.PinnedAt == nil || !autoSettle(*r))
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

// SettleAsk is the agent's word that its work is done (V0.settle-request):
// when it said so and why.
type SettleAsk struct {
	At  time.Time `json:"at"`
	Why string    `json:"why,omitempty"`
}

// SettleAskAuto and SettleAskShown are reduceSettleRequest's replies.
const (
	SettleAskAuto  = "auto"
	SettleAskShown = "shown"
)

// reduceSettleRequest is the agent's settle_request (V0.settle-request):
// its work is done and nothing it asked waits. On a project that settles on
// request (args.auto, which the engine reads from the project) it arms
// "settle when done" as the agent's, so the thread settles once the run ends
// and nothing waits for the owner; otherwise the owner sees a Settle chip.
func reduceSettleRequest(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	t := r.T
	if t.Settle.At != nil {
		return Result{}, errors.New("the thread is already settled")
	}
	if auto, _ := a["auto"].(bool); auto {
		t.SettleAsk = nil
		SettleWhenDone(t, ByAgent, now)
		return Result{Reply: SettleAskAuto}, nil
	}
	t.SettleAsk = &SettleAsk{At: now, Why: TruncHead(a.trimmed("why"), 200)}
	return Result{Reply: SettleAskShown}, nil
}
