package core

import (
	"fmt"
	"strings"
	"time"
)

// State is the one status vocabulary. Only Attention computes it; clients
// never derive status themselves.
type State string

const (
	Asking   State = "asking"
	Failed   State = "failed"
	Blocked  State = "blocked"
	Done     State = "done"
	Working  State = "working"
	Waiting  State = "waiting"
	Seen     State = "seen"
	Snoozed  State = "snoozed"
	Settled  State = "settled"
	Archived State = "archived"
)

type Section string

const (
	NeedsYou    Section = "needs"
	WorkingSec  Section = "working"
	SeenSec     Section = "seen"
	SnoozedSec  Section = "snoozed"
	SettledSec  Section = "settled"
	ArchivedSec Section = "archived"
)

// Attention is the pure status function: one thread, one user's view, a time.
type Attention struct {
	State   State   `json:"state"`
	Section Section `json:"section"`
	Reason  string  `json:"reason,omitempty"`
	// Group is the owner's move in Needs you (V0.row-group-code, #454):
	// GroupAnswer, GroupApprove or GroupUnblock, set only in that section.
	// Clients group by it and never by the words of Reason.
	Group Group `json:"group,omitempty"`
	// Word and Subject are the two parts of a Reason "<a>: <b>" that a row
	// splits: Word goes in the state's place ("Run", "Settle", "Merge"),
	// Subject on the line under it (the why, the change's title). Set only
	// when the reason is a run request, a settle offer or a change's need.
	Word    string `json:"word,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// Group is the code of the owner's move in Needs you (V0.row-group-code).
type Group string

const (
	// GroupAnswer: a question to answer.
	GroupAnswer Group = "answer"
	// GroupApprove: something to say yes to: a tool approval, an effect, a
	// command to run, a settle offer, a ready PR.
	GroupApprove Group = "approve"
	// GroupUnblock: something broken: a failure, an agent not responding,
	// a change stopped on a failure.
	GroupUnblock Group = "unblock"
)

// ComputeWith returns the attention of t for the user who owns v at time now.
//
// Order: archived, settled, then the snooze (which a new question or failure
// breaks), then the thread's first claim (ClaimOrder), else done or seen.
//
// on is the auto-merge setting (V0.205): it says whether a repo merges with
// no press, so a merge ask Fyr will do by itself does not count as an ask of
// the owner. nil: no repo does.
func ComputeWith(t *Thread, v *View, now time.Time, on func(repo string) bool) Attention {
	if t.ArchivedAt != nil {
		return Attention{State: Archived, Section: ArchivedSec}
	}
	if t.Settle.At != nil {
		return Attention{State: Settled, Section: SettledSec}
	}
	asks := ownerAsks(t, on)
	quotaWait := ProviderWaitText(t, now)
	if until, since := SnoozeOf(v); until != nil && until.After(now) {
		// V0.185: until work is done never hides a request, whatever its age.
		bound := SnoozedWhenDone(v)
		woke := bound && WorkOverAt(t, now)
		for _, d := range asks {
			if d.CreatedAt.After(*since) {
				woke = true
			}
		}
		// V0.quota-wait, V0.quota-wait-midturn: a quota wait is not a failure
		// that needs the owner until its reset passes, so it breaks no snooze.
		if t.Failure != nil && quotaWait == "" && t.Failure.At.After(*since) {
			woke = true
		}
		if !woke {
			if bound {
				return Attention{State: Snoozed, Section: SnoozedSec, Reason: "until work is done"}
			}
			return Attention{State: Snoozed, Section: SnoozedSec, Reason: "until " + until.Format(time.RFC3339)}
		}
	}
	// V0.thread-claims: what the thread waits on is one ordered list; the row
	// shows its first claim. V0.101: with none, a finished run whose result
	// the owner has not seen is unread in Seen.
	if c := firstClaim(Claims(t, now, on)); c != nil {
		return c.attention()
	}
	if v == nil || t.ActivityAt.After(v.SeenAt) {
		return Attention{State: Done, Section: SeenSec}
	}
	return Attention{State: Seen, Section: SeenSec}
}

// UnresponsiveText is the one line that names an unresponsive agent
// (V0.143): "Agent not responding since 08:58: <reason>". The clock is
// the host's local time.
func UnresponsiveText(u *Unresponsive) string {
	return "Agent not responding since " + u.Since.Local().Format("15:04") + ": " + u.Reason
}

// MachineWaitText is the one line of a wait for the machine (V0.153).
func MachineWaitText(w *MachineWait) string {
	return "Waiting for the machine (" + w.Why + ")"
}

// GateHoldText is the one line of a heavy command held at the gate.
func GateHoldText(h *GateHold) string {
	return "Held: " + h.Why
}

// ciWait is the reason a thread waits for CI (V0.ci-wait-moves), or "": a
// merge ask held for CI (V0.136) names the checks it waits on, else an open
// PR of a change still going whose checks the poller read as running (a PR
// never read has no checks yet: a repo with no CI must not wait).
func ciWait(t *Thread) string {
	for i := range t.Changes {
		if n := heldNeed(t, &t.Changes[i]); n != nil {
			// "Waiting for CI on #846: 2 of 5 checks done": the row's word
			// is before the colon, the count after it.
			c := t.Changes[i].PR
			if rest, ok := strings.CutPrefix(n.Text, "Waiting for CI"); ok && c != nil && c.Number > 0 {
				return fmt.Sprintf("Waiting for CI on #%d%s", c.Number, rest)
			}
			// A held ask that waits on something else than checks ("Waiting
			// for GitHub to read whether it can merge") keeps its own words.
			return n.Text
		}
	}
	// A held merge ask on a PR Fyr has not read into a change yet: it waits
	// all the same (workGoing counted it; the row said Finished).
	for _, d := range t.HeldAsks {
		if d.Effect == nil {
			continue
		}
		if _, why, _ := mergeGate(t, d.Effect); why != "" {
			return why
		}
		return "Waiting for CI"
	}
	for _, c := range t.Changes {
		if c.PRWaitsOnCI() {
			if p := c.PR; p.Checks == ChecksFail {
				// V0.ci-failed-still-running: name the red check.
				return fmt.Sprintf("Waiting for CI on #%d: %d of %d checks done, %d failed", p.Number, p.ChecksDone, p.ChecksTotal, max(len(p.FailedChecks), 1))
			}
			return fmt.Sprintf("Waiting for CI on #%d", c.PR.Number)
		}
	}
	return ""
}

// SettleOfferText is the Your move line of a settle offer
// (V0.settle-offer-your-move): "Settle: <why>", the chip's words.
func SettleOfferText(a *SettleAsk) string {
	return "Settle: " + settleWhy(a)
}

func settleWhy(a *SettleAsk) string {
	if a.Why == "" {
		return "the agent is done"
	}
	return a.Why
}

// blocked is a Needs you attention whose reason a row splits into a word
// and a subject (V0.row-group-code).
func blocked(g Group, word, subject, reason string) Attention {
	return Attention{State: Blocked, Section: NeedsYou, Reason: reason, Group: g, Word: word, Subject: subject}
}

// askGroup is the move of the owner's open asks (V0.your-move): a question
// among them is one to answer; tool approvals and effects only are ones to
// say yes to.
func askGroup(asks []Decision) Group {
	for _, d := range asks {
		if d.Kind == DecisionAsk {
			return GroupAnswer
		}
	}
	return GroupApprove
}

// needGroup is the move of a thread blocked on its changes' needs: when
// every need in Needs you is a ready PR, the owner says yes to it (Merge);
// any other need (a failure) is one to unblock.
func needGroup(t *Thread, on func(repo string) bool) Group {
	for i := range t.Changes {
		if n := ChangeNeedWith(t, &t.Changes[i], on); n != nil && !n.Muted && n.Cause != CauseReady {
			return GroupUnblock
		}
	}
	return GroupApprove
}

// QueuedText is the one line of a thread queued for a heavy job
// (V0.slot-queued-waits), worded like "Waiting for CI: 2 of 5 checks done":
// the state before the colon, what it waits for after it.
const QueuedText = "Queued for a heavy job: waiting for room on the host"
