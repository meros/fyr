package core

import (
	"slices"
	"time"
)

// The snooze "until work is done" (V0.185): the thread leaves the inbox while
// its agent works and comes back when the work is over. It is the View's
// snooze Rule with the Condition any(workOver, at(armed + SnoozeDoneMax)).

// SnoozeDoneMax is the safety net: however long the work seems to go on (a
// watch that never fires), the thread comes back after this time.
const SnoozeDoneMax = 24 * time.Hour

// Why the option is off: nothing is hidden silently.
const (
	SnoozeDoneIdle   = "Nothing is working"
	SnoozeDoneNeeded = "It needs you now"
)

// NeedsOwnerAt reports a thread that waits on the owner at now: a question,
// an approval or a merge ask, a failure, an agent that does not respond, a
// change that is ready or stopped on a failure, a Run card. A quota stop is
// the owner's only from its reset (V0.quota-wait), so the answer moves
// with the clock.
func NeedsOwnerAt(t *Thread, now time.Time) bool {
	return NeedsOwnerNowAt(t, now) || hasClaim(Claims(t, now, nil), ClaimNeed)
}

// ownerNow are the claims of NeedsOwnerNowAt. The settle offer is the
// owner's move on the row but is left out: "settle when done" must fire on a
// thread whose agent offers to settle.
var ownerNow = []string{ClaimAsk, ClaimFailure, ClaimUnresponsive, ClaimNoProgress, ClaimRunRequest}

// NeedsOwnerNowAt is NeedsOwner without the changes at now: a question, an
// approval or a merge ask, a failure, an agent that does not respond, a Run
// card. A failure the provider's quota caused is not the owner's before its
// reset (V0.quota-wait): the owner can do nothing about it, so the thread
// waits; past it, the failure is theirs.
//
// V0.thread-claims: read from the thread's claims, as the attention is. The
// Rules and Available that call this have no auto-merge setting, so a merge
// Fyr does by itself still counts as the owner's here.
func NeedsOwnerNowAt(t *Thread, now time.Time) bool {
	cs := Claims(t, now, nil)
	if hasClaim(cs, ClaimHandover) {
		// The failure a handover moves the thread away from is not the
		// owner's while it goes: the row says Moving, and so do the Rules.
		cs = slices.DeleteFunc(cs, func(c Claim) bool { return c.Kind == ClaimFailure })
	}
	return hasClaim(cs, ownerNow...)
}

// WorkGoingAt reports that the thread's work goes on by itself: a run or a
// background task is live, the agent is about to get a turn (an unread
// message, a notice that wakes it, a handover, a wait for the machine), or it
// waits on something it set up: a watch or settle condition of its own, a
// place in the slot queue, a merge ask held for CI, CI on an open PR, a
// command at the gate or one the owner started for it. A message that starts
// the agent counts from its send (V0.settle-merge-fixes (4)): the run it
// starts has not begun yet.
//
// V0.quota-wait: a thread Waiting for quota reads as work going on until the
// reset its notice names; the time is read at now, as the attention is.
func WorkGoingAt(t *Thread, now time.Time) bool {
	return workGoing(t, func(Rule) bool { return true }, now)
}

// workGoing is WorkGoingAt, counting the agent's armed Rules that counts
// takes. V0.thread-claims: any claim that moves without the owner, so what
// the row shows as Moving is never idle here.
func workGoing(t *Thread, counts func(Rule) bool, now time.Time) bool {
	return slices.ContainsFunc(claims(t, now, nil, counts), func(c Claim) bool { return c.Side == SideMoving })
}

// WorkOverAt is the Condition's value at now: the snooze has nothing left
// to wait for (V0.quota-wait: a quota wait ends at its reset).
func WorkOverAt(t *Thread, now time.Time) bool {
	return t.Asleep() || NeedsOwnerAt(t, now) || !WorkGoingAt(t, now)
}

// snoozeDoneReason is why the thread cannot take this snooze now, or "".
func snoozeDoneReason(t *Thread, now time.Time) string {
	switch {
	case NeedsOwnerAt(t, now):
		return SnoozeDoneNeeded
	case !WorkGoingAt(t, now):
		return SnoozeDoneIdle
	}
	return ""
}

// SnoozeWhenDone hides the thread until its work is over, at most
// SnoozeDoneMax.
func SnoozeWhenDone(v *View, now time.Time) {
	end := now.Add(SnoozeDoneMax)
	v.Rules = armIn(v.Rules, &Thread{}, Rule{ID: snoozeRule, Purpose: PurposeSnooze, When: Any(When{Kind: WhenWorkOver}, When{Kind: WhenAt, At: &end}),
		Then: Then{Kind: KindRuleWake}, After: AfterDisarm, CreatedBy: ByOwner}, now)
}

// workBound reports a Condition that reads workOver.
func (w When) workBound() bool { return w.has(WhenWorkOver) }

// doneBound reports a Condition that reads workDone.
func (w When) doneBound() bool { return w.has(WhenWorkDone) }

// SnoozedWhenDone reports that the armed snooze is the one until work is done.
func SnoozedWhenDone(v *View) bool {
	if v == nil {
		return false
	}
	r := findIn(v.Rules, snoozeRule)
	return r != nil && r.When.workBound()
}

// wakeWhenDone ends the snooze when its Condition holds. It runs last in
// Reduce, not with the other Rules: what ends the work is read only after the
// merge asks are gated and the agent's notices are queued, so a firing watch,
// which wakes the agent, does not count as the end. A feed.take is skipped:
// the notice has left the queue and the turn it starts has not begun.
// due skips every view Rule that reads workOver, so this takes each one
// (V0.one-predicate (2)).
func wakeWhenDone(r Rec, in Input, now time.Time) Result {
	var res Result
	if in.Kind == KindFeedTake {
		return res
	}
	var ids []string
	for _, x := range r.V.Rules {
		if x.When.workBound() {
			ids = append(ids, x.ID)
		}
	}
	for _, id := range ids {
		if x := findIn(r.V.Rules, id); x != nil && x.When.eval(r.T, x.Armed, now) == yes {
			fire(r, x.ID, true, now, &res)
		}
	}
	return res
}
