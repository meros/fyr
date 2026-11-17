package core

import (
	"fmt"
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
	if t.ArchivedAt != nil {
		return Attention{Archived, ArchivedSec, ""}
	}
	if t.Settle.At != nil {
		return Attention{Settled, SettledSec, ""}
	}
	asking := len(t.Decisions) > 0
	if until, since := SnoozeOf(v); until != nil && until.After(now) {
		woke := false
		for _, d := range t.Decisions {
			if d.CreatedAt.After(*since) {
				woke = true
			}
		}
		if t.Failure != nil && t.Failure.At.After(*since) {
			woke = true
		}
		if !woke {
			return Attention{Snoozed, SnoozedSec, "until " + until.Format(time.RFC3339)}
		}
	}
	switch {
	case asking:
		return Attention{Asking, NeedsYou, string(t.Decisions[0].Kind)}
	case t.Failure != nil:
		return Attention{Failed, NeedsYou, t.Failure.Message}
	case t.Unresponsive != nil:
		// V0.143: a live process that takes nothing Fyr writes needs the
		// owner: Restart, or wait for the automatic one.
		return Attention{Blocked, NeedsYou, UnresponsiveText(t.Unresponsive)}
	case t.Running:
		return Attention{Working, WorkingSec, ""}
	}
	if n := t.RunningTasks(); n > 0 {
		reason := "1 agent"
		if n > 1 {
			reason = fmt.Sprintf("%d agents", n)
		}
		return Attention{Waiting, WorkingSec, reason}
	}
	// V0.101: Needs you is computed: Ready or Stopped on a failure (asks
	// are decisions, above). A finished run whose result the owner has not
	// seen is unread in Seen, not in Needs you.
	if c, n := ThreadNeed(t); n != nil {
		return Attention{Blocked, NeedsYou, c.Title + ": " + n.Text}
	}
	// V0.149 (5): a command the agent asked the owner to run.
	if a := PendingRequest(t); a != nil {
		return Attention{Blocked, NeedsYou, "Run: " + a.Why}
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
