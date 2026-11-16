package core

import "fmt"

// voidAsks closes the owner's effect asks whose precondition is gone
// (decision 0024 §5): an ask on a change that is abandoned or reverted, and
// a PR effect (merge, re-run) whose PR is no longer open, for example
// because it merged on GitHub. A deploy ask on a landed change stays: a
// deploy follows the merge. Each void ask is a decision.closed event with
// void: true, which the client shows and the agent feed reports.
func voidAsks(t *Thread) []Event {
	var evs []Event
	keep := t.Decisions[:0:0]
	for _, d := range t.Decisions {
		if why := askVoid(t, d); why != "" {
			evs = append(evs, Event{"decision.closed", map[string]any{
				"id": d.ID, "answer": "void: " + why, "void": true, "reason": why,
				"action": d.Effect.Action, "args": d.Effect.Args,
			}})
			continue
		}
		keep = append(keep, d)
	}
	if len(evs) == 0 {
		return nil
	}
	t.Decisions = keep
	if len(t.Decisions) == 0 {
		t.Decisions = nil
	}
	return evs
}

// askVoid is why the effect ask d no longer applies, or "".
func askVoid(t *Thread, d Decision) string {
	if d.Kind != DecisionEffect || d.Effect == nil {
		return ""
	}
	a := d.Effect.Args
	// V0.101: an archived thread's asks are void, and so is an ask on a done
	// change (a deploy ask follows the merge and stays).
	if t.ArchivedAt != nil {
		return "the thread is archived"
	}
	if id, _ := a["change"].(string); id != "" {
		if c := FindChange(t, id); c != nil && c.Phase.Ended() {
			return fmt.Sprintf("%q is %s", c.Title, c.Phase)
		} else if c != nil && c.IsDone() && d.Effect.Action != FyrDeploy {
			return fmt.Sprintf("%q is done", c.Title)
		}
	}
	if fx := FyrEffects[d.Effect.Action]; fx != nil && fx.OnPR {
		url, _ := a["pr"].(string)
		if c := changeByPR(t, url); url != "" && c != nil && !c.Phase.InFlight() {
			return fmt.Sprintf("%q is %s", c.Title, c.Phase)
		}
	}
	return ""
}
