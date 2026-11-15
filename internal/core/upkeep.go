package core

import "time"

// Keeping changes current (SPEC 0.5 V0.62): like the live view (V0.37,
// V0.52), the changes the agent owns are kept up to date. At the end of a
// run that did not touch them the Stop hook asks once; a long turn gets a
// reminder. Changes that Fyr observes (git, GitHub) keep themselves current
// and do not count.

// ChangesNudgeReason is the mid-turn reminder for changes.
const ChangesNudgeReason = "Your open changes in Fyr have not been updated for a few minutes while you work. Report what moved with change_status (phase, conditions), change_revise or change_attach, or call change_unchanged with a reason, then continue."

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

// StopReason is the Stop hook's one reason for what is stale.
func StopReason(view, changes bool) string {
	switch {
	case view && changes:
		return "Update this thread's live view (view_state or view_publish) and your open changes (change_status, change_revise or change_attach), or call view_unchanged and change_unchanged with a reason, then stop."
	case changes:
		return "Update your open changes in Fyr (change_status for phase and conditions, change_revise, change_attach), or call change_unchanged with a reason, then stop."
	}
	return ViewStopReason
}
