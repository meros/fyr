package core

import (
	"fmt"
	"sort"
	"time"
)

// V0.wakeups-via-watch: an agent's wake-up is its watch on a time
// ({kind: "at"}, then notify). The owner sees it on the thread and the
// inbox row, and cancels it with thread.cancelWake.

// KindCancelWake is the owner's cancel of an agent's wake-up.
const KindCancelWake = "thread.cancelWake"

// WakeUp is one armed wake-up as clients and notices read it.
type WakeUp struct {
	Rule string    `json:"rule"`
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
}

// WakeOf is the wake-up a Rule is, or nil.
func WakeOf(r Rule) *WakeUp {
	if r.Purpose != PurposeWatch || r.CreatedBy != ByAgent || r.Status == "suspended" || r.When.Kind != WhenAt || r.When.At == nil {
		return nil
	}
	note, _ := r.Then.Args["note"].(string)
	return &WakeUp{Rule: r.ID, At: *r.When.At, Note: note}
}

// Wakes are the thread's armed wake-ups, soonest first.
func Wakes(t *Thread) []WakeUp {
	var out []WakeUp
	for _, r := range t.Rules {
		if w := WakeOf(r); w != nil {
			out = append(out, *w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func init() {
	register(&Action{Name: KindCancelWake, Label: "Cancel wake-up",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if len(Wakes(t)) == 0 {
				return "No wake-up is set"
			}
			return ""
		},
		// rule: the watch to cancel; "" cancels the soonest.
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			id := a.trimmed("rule")
			ws := Wakes(t)
			if id == "" && len(ws) > 0 {
				id = ws[0].Rule
			}
			x := FindRule(t, id)
			if x == nil || WakeOf(*x) == nil {
				return nil, reject(RejectNotFound, "no wake-up %q on this thread", id)
			}
			DropRules(t, func(y Rule) bool { return y.ID == id })
			return nil, nil
		}})
}

// cancelledWakes are the owner's notices for wake-ups an owner's input
// dropped (V0.wakeups-via-watch): the agent must not count on them.
func cancelledWakes(b, a *Thread) []Notice {
	var out []Notice
	for _, w := range Wakes(b) {
		if x := FindRule(a, w.Rule); x != nil && WakeOf(*x) != nil {
			continue
		}
		what := fmt.Sprintf("The owner cancelled your wake-up %s at %s", w.Rule, w.At.UTC().Format(time.RFC3339))
		if w.Note != "" {
			what += fmt.Sprintf(" (%q)", w.Note)
		}
		out = append(out, Notice{About: "thread", Key: "owner:wake:" + w.Rule, What: what + "; it will not fire. Do not set it again unless the owner asks."})
	}
	return out
}
