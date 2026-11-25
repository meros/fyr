package core

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// V0.watch-waits: a thread whose agent has an armed watch of its own (a
// wake-up at a time, a wait for a change to go live) still has work coming:
// Attention puts it in Working as Waiting, with the watch as the reason,
// instead of in Seen as if it were finished. workGoing already counts it.

// agentWatches are the thread's armed notify watches (agent or owner quota
// wakes), soonest wake-up first.
func agentWatches(t *Thread) []Rule {
	var at, other []Rule
	for _, r := range t.Rules {
		if !namedWatch(r) {
			continue
		}
		if (r.When.Kind == WhenAt || r.When.Kind == WhenQuota) && r.When.At != nil {
			at = append(at, r)
		} else {
			other = append(other, r)
		}
	}
	slices.SortStableFunc(at, func(a, b Rule) int { return a.When.At.Compare(*b.When.At) })
	return append(at, other...)
}

// namedWatch reports an armed notify watch the row names: the agent's, or the
// owner's quota wake.
func namedWatch(r Rule) bool {
	return r.Purpose == PurposeWatch && (r.CreatedBy == ByAgent || r.CreatedBy == ByOwner) && r.Status == RuleArmed
}

// WatchWaitText is the Waiting reason of a thread with armed agent watches,
// or "" when it has none: "wakes 07:00", "waits for #1009 to be live",
// with "+N more" for the rest. Times are the host's local time, as
// UnresponsiveText's; the date shows when it is not today.
func WatchWaitText(t *Thread, now time.Time) string {
	return watchWait(t, now, func(Rule) bool { return true })
}

// watchWait is WatchWaitText over the watches counts takes.
func watchWait(t *Thread, now time.Time, counts func(Rule) bool) string {
	ws := slices.DeleteFunc(agentWatches(t), func(r Rule) bool { return !counts(r) })
	if len(ws) == 0 {
		return ""
	}
	var parts []string
	for _, r := range ws[:min(2, len(ws))] {
		parts = append(parts, watchText(t, r.When, now))
	}
	s := strings.Join(parts, " · ")
	if n := len(ws) - len(parts); n > 0 {
		s += fmt.Sprintf(" · +%d more", n)
	}
	return s
}

// watchText is one watch's condition in a few words.
func watchText(t *Thread, w When, now time.Time) string {
	switch w.Kind {
	case WhenAt:
		if w.At == nil {
			break
		}
		return "wakes " + wakeClock(*w.At, now)
	case WhenQuota:
		if w.At == nil {
			break
		}
		when := wakeClock(*w.At, now)
		if w.Window != "" {
			return fmt.Sprintf("wakes %s, when the %s window resets", when, w.Window)
		}
		return fmt.Sprintf("wakes %s, when quota resets", when)
	case WhenReached:
		return "waits for " + watchedChange(t, w.Change) + " to be " + string(w.Phase)
	case WhenCondition:
		return fmt.Sprintf("waits for %s %s on %s", w.Type, w.Status, watchedChange(t, w.Change))
	case WhenRunEnded:
		return "waits for the run to end"
	case WhenIdleFor:
		return fmt.Sprintf("wakes after %d min idle", max(1, w.Ms/60_000))
	}
	return "watching"
}

// wakeClock formats at in the host's local time, adding the date when it is
// not today.
func wakeClock(t, now time.Time) string {
	at, n := t.Local(), now.Local()
	if at.Year() == n.Year() && at.YearDay() == n.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("2 Jan 15:04")
}

// watchedChange names a watched change with changeName, or its ref when
// the thread does not have it.
func watchedChange(t *Thread, ref string) string {
	if c := FindChange(t, ref); c != nil {
		return changeName(c)
	}
	return ref
}
