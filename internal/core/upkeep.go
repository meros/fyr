package core

import "time"

// Keeping changes current (SPEC 0.5 V0.62): like the live view (V0.37,
// V0.52), the changes the agent owns are kept up to date. The agent feed's
// upkeep rules (feed.go) ask at the end of a run that did not touch them and
// remind a long turn. Changes that Fyr observes (git, GitHub) keep
// themselves current and do not count.

// OpenAgentChanges counts the changes the agent owns that are not ended:
// opened or last moved by the agent.
func OpenAgentChanges(t *Thread) int {
	n := 0
	for _, c := range t.Changes {
		if !c.Phase.Ended() && c.PhaseBy != ByObserved {
			n++
		}
	}
	return n
}

// ChangesChecked records a change tool call (change_unchanged included).
func ChangesChecked(t *Thread, now time.Time) { t.ChangesCheckedAt = &now }

// ChangesStale reports open agent changes and no change tool call in the
// current run.
func ChangesStale(t *Thread) bool {
	if OpenAgentChanges(t) == 0 || t.RunStartedAt == nil {
		return false
	}
	return t.ChangesCheckedAt == nil || t.ChangesCheckedAt.Before(*t.RunStartedAt)
}
