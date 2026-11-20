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
}

// Compute returns the attention of t for the user who owns v at time now.
//
// Order: archived, settled, then the snooze (which a new question or failure
// breaks), then asking, failed, working, blocked, done, seen.
func Compute(t *Thread, v *View, now time.Time) Attention {
	return ComputeWith(t, v, now, nil)
}

// ComputeWith is Compute with the auto-merge setting (V0.205): on says whether
// a repo merges with no press, so a merge ask Fyr will do by itself does not
// count as an ask of the owner. nil: no repo does.
func ComputeWith(t *Thread, v *View, now time.Time, on func(repo string) bool) Attention {
	if t.ArchivedAt != nil {
		return Attention{Archived, ArchivedSec, ""}
	}
	if t.Settle.At != nil {
		return Attention{Settled, SettledSec, ""}
	}
	asks := ownerAsks(t, on)
	asking := len(asks) > 0
	if until, since := SnoozeOf(v); until != nil && until.After(now) {
		// V0.185: until work is done never hides a request, whatever its age.
		bound := SnoozedWhenDone(v)
		woke := bound && WorkOver(t)
		for _, d := range asks {
			if d.CreatedAt.After(*since) {
				woke = true
			}
		}
		if t.Failure != nil && t.Failure.At.After(*since) {
			woke = true
		}
		if !woke {
			if bound {
				return Attention{Snoozed, SnoozedSec, "until work is done"}
			}
			return Attention{Snoozed, SnoozedSec, "until " + until.Format(time.RFC3339)}
		}
	}
	switch {
	case asking:
		return Attention{Asking, NeedsYou, string(asks[0].Kind)}
	case t.Failure != nil:
		return Attention{Failed, NeedsYou, t.Failure.Message}
	case t.Unresponsive != nil:
		// V0.143: a live process that takes nothing Fyr writes needs the
		// owner: Restart, or wait for the automatic one.
		return Attention{Blocked, NeedsYou, UnresponsiveText(t.Unresponsive)}
	case t.Running:
		return Attention{Working, WorkingSec, ""}
	}
	if t.MachineWait == nil && t.Starting != nil {
		// V0.starting-moves: the agent is starting; it moves without the
		// owner already.
		return Attention{Working, WorkingSec, "Starting"}
	}
	if t.MachineWait != nil {
		// V0.153: Fyr waits for the host before it starts the agent.
		return Attention{Waiting, WorkingSec, MachineWaitText(t.MachineWait)}
	}
	if n := t.RunningTasks(); n > 0 {
		return Attention{Waiting, WorkingSec, waitReason(t, n)}
	}
	// V0.101: Needs you is computed: Ready or Stopped on a failure (asks
	// are decisions, above). A finished run whose result the owner has not
	// seen is unread in Seen, not in Needs you.
	if c, n := threadNeed(t, on); n != nil {
		return Attention{Blocked, NeedsYou, c.Title + ": " + n.Text}
	}
	// V0.149 (5): a command the agent asked the owner to run.
	if a := PendingRequest(t); a != nil {
		return Attention{Blocked, NeedsYou, "Run: " + a.Why}
	}
	// V0.settle-offer-your-move: the agent offers to settle once its run
	// ended; the owner's move is Settle, or a message that clears it.
	if t.SettleAsk != nil {
		return Attention{Blocked, NeedsYou, SettleOfferText(t.SettleAsk)}
	}
	// V0.ci-wait-moves: CI the thread waits on moves without the owner, so
	// the thread is not Finished while it runs.
	if r := ciWait(t); r != "" {
		return Attention{Waiting, WorkingSec, r}
	}
	// V0.watch-waits: the agent's own armed watch is work still coming.
	if s := WatchWaitText(t, now); s != "" {
		return Attention{Waiting, WorkingSec, s}
	}
	if v == nil || t.ActivityAt.After(v.SeenAt) {
		return Attention{Done, SeenSec, ""}
	}
	return Attention{Seen, SeenSec, ""}
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

// ciWait is the reason a thread waits for CI (V0.ci-wait-moves), or "": a
// merge ask held for CI (V0.136) names the checks it waits on, else an open
// PR of a change still going whose checks the poller read as running (a PR
// never read has no checks yet: a repo with no CI must not wait).
func ciWait(t *Thread) string {
	for i := range t.Changes {
		if n := heldNeed(t, &t.Changes[i]); n != nil {
			// "Waiting for CI on #846: 2 of 5 checks done": the row's word
			// is before the colon, the count after it.
			if c := t.Changes[i].PR; c != nil && c.Number > 0 {
				return fmt.Sprintf("Waiting for CI on #%d%s", c.Number, strings.TrimPrefix(n.Text, "Waiting for CI"))
			}
			return n.Text
		}
	}
	for _, c := range t.Changes {
		if c.PR != nil && c.PR.State == "OPEN" && c.PR.Checks == ChecksPending && c.PR.CheckedAt != nil && !c.Dismissed && !c.IsDone() && !c.Phase.Ended() {
			return fmt.Sprintf("Waiting for CI on #%d", c.PR.Number)
		}
	}
	return ""
}

// SettleOfferText is the Your move line of a settle offer
// (V0.settle-offer-your-move): "Settle: <why>", the chip's words.
func SettleOfferText(a *SettleAsk) string {
	if a.Why == "" {
		return "Settle: the agent is done"
	}
	return "Settle: " + a.Why
}
