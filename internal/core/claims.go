package core

import (
	"slices"
	"time"
)

// V0.thread-claims: everything a thread waits on is one ordered list. The
// attention (ComputeWith), "does it need the owner" (NeedsOwnerAt) and "does
// its work go on by itself" (WorkGoingAt) all read this list, so a thread
// cannot be Finished on its row while Fyr counts its work as going on, nor
// the reverse.

// Side is whose move a claim is.
type Side string

const (
	// SideYours: the owner has to act (Your move).
	SideYours Side = "yours"
	// SideMoving: the thread moves without the owner (Moving without you).
	SideMoving Side = "moving"
)

// The kinds of a Claim, in the order ClaimOrder gives them.
const (
	ClaimAsk          = CauseAsk
	ClaimHandover     = "handover"
	ClaimQuota        = "quota"
	ClaimFailure      = "failure"
	ClaimUnresponsive = "unresponsive"
	ClaimNoProgress   = "no-progress"
	ClaimRun          = "run"
	ClaimStarting     = "starting"
	ClaimMachine      = "machine"
	ClaimDelivering   = "delivering"
	ClaimGate         = "gate"
	ClaimFrozen       = "frozen"
	ClaimTasks        = "tasks"
	ClaimSlot         = "slot"
	ClaimNeed         = "need"
	ClaimRunRequest   = "run-request"
	ClaimCommand      = "command"
	ClaimMerging      = "merging"
	ClaimSettleOffer  = "settle-offer"
	ClaimCI           = "ci"
	ClaimWatch        = "watch"
	ClaimWaking       = "waking"
	ClaimRule         = "rule"
)

// ClaimOrder is the precedence: the first claim a thread has is its
// attention. Claims returns them in this order.
var ClaimOrder = []string{
	ClaimAsk, ClaimHandover, ClaimQuota, ClaimFailure, ClaimUnresponsive, ClaimNoProgress,
	ClaimRun, ClaimStarting, ClaimMachine, ClaimDelivering, ClaimGate, ClaimFrozen, ClaimTasks, ClaimSlot,
	ClaimNeed, ClaimRunRequest, ClaimCommand, ClaimMerging, ClaimSettleOffer,
	ClaimCI, ClaimWatch, ClaimWaking, ClaimRule,
}

// Claim is one thing a thread waits on: the owner's move, or work that goes
// on by itself.
type Claim struct {
	Kind  string
	Side  Side
	State State
	// Group, Word and Subject are the Attention's (V0.row-group-code).
	Group   Group
	Reason  string
	Word    string
	Subject string
	// Quiet: the claim counts as work going on (a snooze until the work is
	// done keeps waiting) but does not hold the row in Moving without you: a
	// message not read yet, a notice about to wake the agent, an armed Rule
	// of the agent that is not a watch (V0.watch-waits).
	Quiet bool
}

// attention is the claim as the row reads it.
func (c Claim) attention() Attention {
	a := Attention{State: c.State, Section: WorkingSec, Reason: c.Reason, Group: c.Group, Word: c.Word, Subject: c.Subject}
	if c.Side == SideYours {
		a.Section = NeedsYou
	}
	return a
}

// StartingText is the reason of a thread whose agent is about to get a turn.
const StartingText = "Starting"

// Claims lists what t waits on at now, most important first (ClaimOrder).
// Each claim's condition holds on its own, not because an earlier one failed.
// on is the auto-merge setting (see ComputeWith); nil: no repo merges itself.
func Claims(t *Thread, now time.Time, on func(repo string) bool) []Claim {
	return claims(t, now, on, func(Rule) bool { return true })
}

// claims is Claims, counting only the armed Rules that counts takes (settle
// when done leaves out its own Rule, V0.one-predicate).
func claims(t *Thread, now time.Time, on func(repo string) bool, counts func(Rule) bool) []Claim {
	var out []Claim
	yours := func(kind string, st State, g Group, reason, word, subject string) {
		out = append(out, Claim{Kind: kind, Side: SideYours, State: st, Group: g, Reason: reason, Word: word, Subject: subject})
	}
	moving := func(kind string, st State, reason string) {
		out = append(out, Claim{Kind: kind, Side: SideMoving, State: st, Reason: reason})
	}
	quiet := func(kind, reason string) {
		out = append(out, Claim{Kind: kind, Side: SideMoving, State: Waiting, Reason: reason, Quiet: true})
	}
	quota := ProviderWaitText(t, now)

	if asks := ownerAsks(t, on); len(asks) > 0 {
		yours(ClaimAsk, Asking, askGroup(asks), string(asks[0].Kind), "", "")
	}
	if t.Handover != nil {
		// The thread moves to another provider; the usual reasons are a quota
		// stop or a failure, which it must not sit behind.
		moving(ClaimHandover, Waiting, HandoverText(t.Handover))
	}
	if quota != "" {
		// V0.quota-wait, V0.overload-retry: nothing the owner does moves it
		// before the reset or the next retry.
		moving(ClaimQuota, Waiting, quota)
	}
	if t.Failure != nil && quota == "" {
		yours(ClaimFailure, Failed, GroupUnblock, FailureReason(t, now), "", "")
	}
	if u := t.Unresponsive; u != nil {
		// V0.143: a live process that takes nothing Fyr writes.
		yours(ClaimUnresponsive, Blocked, GroupUnblock, UnresponsiveText(u), UnresponsiveWord, unresponsiveSubject(u))
	}
	if t.Running && t.NoProgress != nil {
		// V0.no-progress: a live run silent past NoProgressAfter.
		w, s := NoProgressWord(), NoProgressSubject(t.NoProgress)
		yours(ClaimNoProgress, Blocked, GroupUnblock, w+": "+s, w, s)
	}
	if t.Running {
		moving(ClaimRun, Working, "")
	}
	if t.Starting != nil && t.MachineWait == nil {
		// V0.starting-moves: the agent is starting.
		moving(ClaimStarting, Working, StartingText)
	}
	if t.MachineWait != nil {
		// V0.153: Fyr waits for the host before it starts the agent.
		moving(ClaimMachine, Waiting, MachineWaitText(t.MachineWait))
	}
	if len(t.Outbox) > 0 {
		// A message the agent has not read. Quiet: the row follows Starting,
		// which every send that starts a turn sets; a message a quota stop
		// left unread must not read as a start for ever.
		quiet(ClaimDelivering, "a message is on its way to the agent")
	}
	if t.GateHold != nil {
		// V0.gate-hold-visible: a heavy command the gate holds back.
		moving(ClaimGate, Waiting, GateHoldText(t.GateHold))
	}
	if t.Frozen != nil {
		// V0.freeze-visible: the memory guard froze the thread's work; above
		// its tasks, which would read as running.
		moving(ClaimFrozen, Waiting, FrozenText(t.Frozen))
	}
	if n := t.RunningTasks(); n > 0 {
		moving(ClaimTasks, Waiting, waitReason(t, n))
	}
	if t.SlotQueued {
		// V0.slot-queued-waits: the host holds a heavy job until it has room.
		moving(ClaimSlot, Waiting, QueuedText)
	}
	if c, n := threadNeed(t, on); n != nil {
		// V0.101: a change that is ready, or stopped on a failure.
		yours(ClaimNeed, Blocked, needGroup(t, on), c.Title+": "+n.Text, n.Text, c.Title)
	}
	if a := PendingRequest(t); a != nil {
		// V0.149 (5): a command the agent asked the owner to run.
		yours(ClaimRunRequest, Blocked, GroupApprove, "Run: "+a.Why, "Run", a.Why)
	}
	if a := RunningRequest(t); a != nil {
		// V0.waiting-for-command: the command the owner pressed Run on runs.
		moving(ClaimCommand, Waiting, "Waiting for command: "+a.Why)
	}
	if s := mergingText(t); s != "" {
		// V0.merge-in-flight: a merge Fyr started that GitHub has not
		// reported yet.
		moving(ClaimMerging, Waiting, s)
	}
	if t.SettleAsk != nil {
		// V0.settle-offer-your-move: the agent offers to settle.
		yours(ClaimSettleOffer, Blocked, GroupApprove, SettleOfferText(t.SettleAsk), "Settle", settleWhy(t.SettleAsk))
	}
	if r := ciWait(t); r != "" {
		// V0.ci-wait-moves: CI the thread waits on.
		moving(ClaimCI, Waiting, r)
	}
	if s := watchWait(t, now, counts); s != "" {
		// V0.watch-waits: the agent's own armed watch is work still coming.
		moving(ClaimWatch, Waiting, s)
	}
	if t.wakeable() && slices.ContainsFunc(t.Feed.Queue, func(n Notice) bool { return n.Urgency == Wake && !n.Self }) {
		// A notice that wakes the agent waits in the queue. Quiet: the take
		// that delivers it sets Starting, and the queue is not news for the
		// owner (#47).
		quiet(ClaimWaking, "a notice is about to wake the agent")
	}
	if slices.ContainsFunc(t.Rules, func(r Rule) bool { return quietRule(r) && counts(r) }) {
		quiet(ClaimRule, "waits for its own rule")
	}
	return out
}

// quietRule reports an armed Rule that counts as work going on without being
// one of the watches the row names: a Rule the agent armed for another
// purpose (its own "settle when merged"), or a watch nobody the row names
// armed.
func quietRule(r Rule) bool {
	if r.Status != RuleArmed || (r.CreatedBy != ByAgent && r.Purpose != PurposeWatch) {
		return false
	}
	return !namedWatch(r)
}

// UnresponsiveWord is the row's word for an agent that does not respond.
const UnresponsiveWord = "Not responding"

func unresponsiveSubject(u *Unresponsive) string {
	return "since " + u.Since.Local().Format("15:04") + ": " + u.Reason
}

// HandoverText is the one line of a thread that moves to another provider.
func HandoverText(h *Handover) string {
	if h.To == "" {
		return "Moving to another provider"
	}
	return "Moving to " + h.To
}

// firstClaim is the claim the row shows: the first that is not quiet.
func firstClaim(cs []Claim) *Claim {
	for i := range cs {
		if !cs[i].Quiet {
			return &cs[i]
		}
	}
	return nil
}

// hasClaim reports a claim of one of kinds.
func hasClaim(cs []Claim, kinds ...string) bool {
	return slices.ContainsFunc(cs, func(c Claim) bool { return slices.Contains(kinds, c.Kind) })
}

// ClaimKinds are the kinds of cs, for tests and logs.
func ClaimKinds(cs []Claim) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Kind
	}
	return out
}

// markStarting notes that a message Fyr is about to send starts the agent's
// turn (V0.starting-moves, V0.thread-claims): the thread reads Starting from
// now. Not on a thread that runs, starts already, or shows a failure (the
// send clears nothing the owner has to see first).
func markStarting(t *Thread, now time.Time) {
	if !t.Running && t.Starting == nil && t.Failure == nil && !t.Asleep() {
		t.Starting = &now
	}
}
