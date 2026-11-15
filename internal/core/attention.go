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

// precedence orders Needs-you rows; a lower number sorts first.
var precedence = map[State]int{Asking: 0, Failed: 1, Blocked: 2, Done: 3}

func Precedence(s State) int {
	if p, ok := precedence[s]; ok {
		return p
	}
	return 9
}

// Compute returns the attention of t for the user who owns v at time now.
//
// Order: archived, settled, then the snooze (which a new question or failure
// breaks), then asking, failed, working, blocked, done, seen.
func Compute(t *Thread, v *View, now time.Time) Attention {
	if t.ArchivedAt != nil {
		return Attention{Archived, ArchivedSec, ""}
	}
	if t.SettledAt != nil {
		return Attention{Settled, SettledSec, ""}
	}
	asking := len(t.Decisions) > 0
	if v != nil && v.SnoozeUntil != nil && v.SnoozeUntil.After(now) {
		woke := false
		if v.SnoozedAt != nil {
			for _, d := range t.Decisions {
				if d.CreatedAt.After(*v.SnoozedAt) {
					woke = true
				}
			}
			if t.Failure != nil && t.Failure.At.After(*v.SnoozedAt) {
				woke = true
			}
		}
		if !woke {
			return Attention{Snoozed, SnoozedSec, "until " + v.SnoozeUntil.Format(time.RFC3339)}
		}
	}
	switch {
	case asking:
		return Attention{Asking, NeedsYou, t.Decisions[0].Kind}
	case t.Failure != nil:
		return Attention{Failed, NeedsYou, t.Failure.Message}
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
	if r := t.PR.Blocking(); r != "" {
		return Attention{Blocked, NeedsYou, r}
	}
	if r := ChangeBlocking(t); r != "" { // V0.59 (6)
		return Attention{Blocked, NeedsYou, r}
	}
	if v == nil || t.ActivityAt.After(v.SeenAt) {
		return Attention{Done, NeedsYou, ""}
	}
	return Attention{Seen, SeenSec, ""}
}
