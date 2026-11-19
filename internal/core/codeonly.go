package core

import (
	"fmt"
	"strings"
	"time"
)

// MigrateCodeOnly is the load pass of V0.changes-code-only (4) on one
// thread: a change that is not code and has not finished (not ended, not
// landed or live) ends as abandoned with CodeOnlyReason. It does not move
// the thread, so nothing about it reaches Your move; MigrateDone then files
// it under Done here. An open ask on such a change is closed, and the agent
// hears so once, at its next tool call or turn. It reports whether the
// thread changed.
func MigrateCodeOnly(t *Thread, now time.Time) bool {
	changed := false
	for i := range t.Changes {
		c := &t.Changes[i]
		if !endCodeOnly(c, now) {
			continue
		}
		changed = true
		for _, d := range t.Decisions {
			if d.Effect == nil {
				continue
			}
			if id, _ := d.Effect.Args["change"].(string); id != c.ID {
				continue
			}
			DecisionClosed(t, d.ID)
			t.Feed.Enqueue(Notice{About: "change:" + c.ID, What: codeOnlyNotice(d.Effect.Action, c), Urgency: Next, Key: "codeonly:" + d.ID, At: now})
		}
	}
	return changed
}

// MigrateRecordCodeOnly is the same pass on a host record.
func MigrateRecordCodeOnly(c *Change, now time.Time) bool { return endCodeOnly(c, now) }

func endCodeOnly(c *Change, now time.Time) bool {
	if c.Kind == "" || c.Kind == KindCode || c.Phase.Ended() || c.Phase.Reached(PhaseLanded) {
		return false
	}
	setPhase(nil, c, PhaseAbandoned, ByObserved, now)
	c.Label, c.Offers, c.Annotations = CodeOnlyReason, nil, nil
	return true
}

// codeOnlyNotice is the agent's one line about an ask Fyr closed: the ask,
// the change, and what to use instead.
func codeOnlyNotice(action string, c *Change) string {
	ask := strings.TrimPrefix(action, "change.")
	instead := goneAsks[ChangeAsk(ask)]
	if instead == "" {
		instead = goneKinds[c.Kind]
	}
	if instead == "" {
		instead = CodeOnlyReason + "."
	}
	return fmt.Sprintf("Fyr closed your %s ask on %q and ended the change as abandoned. %s", ask, c.Title, instead)
}
