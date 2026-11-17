package core

// Needs you is computed (SPEC V0.101, decision 0024 section 5, slice 5):
// the host decides why a change waits on the owner and sends it as
// `need {cause, head}`; the client only reads it. The causes are an open
// ask, Ready ("Merge", "Review and merge") and Stopped on a failure. A
// failure while the agent runs is "Agent is fixing CI", muted.

import (

	"strings"
	"time"
)

// StopAfter is how long after the last run a failure waits before it is
// the owner's: the agent may still pick it up (V0.101).
const StopAfter = 5 * time.Minute

// The causes of a Need.
const (
	CauseAsk     = string(DecisionAsk)
	CauseReady   = "ready"
	CauseFailure = "failure"
	// CauseFixing is a failure while the agent runs: shown muted, never in
	// Needs you.
	CauseFixing = "fixing"
)

// FixingCI is the muted line of a CI failure while the agent runs.
const FixingCI = "Agent is fixing CI"

// Need is why a change waits on the owner, set only in the thread DTO.
type Need struct {
	Cause string `json:"cause"`
	// Head is the commit the cause is about: the PR's head for Ready (the
	// Merge button is bound to it), the ask's head, the failure's head.
	Head string `json:"head,omitempty"`
	// Text is the cause in words: the Ready value, the ask's reason, the
	// failure.
	Text string `json:"text,omitempty"`
	// Ask and Action are the open ask's decision id and action.
	Ask    string `json:"ask,omitempty"`
	Action string `json:"action,omitempty"`
	// Muted: shown on the card, not in Needs you (CauseFixing, CauseWaiting).
	Muted bool `json:"muted,omitempty"`
}

// ChangeNeed is why c, a change t shows, waits on the owner, or nil. A
// done or ended change, and any change of an archived thread, needs
// nobody. The owner's Not now holds Ready and a failure until the head or
// the failing types change; an ask is declined instead.
func ChangeNeed(t *Thread, c *Change) *Need {
	if c == nil || c.IsDone() || c.Phase.Ended() || t.ArchivedAt != nil {
		return nil
	}
	for _, d := range t.Decisions {
		if askFor(t, d) == c.ID {
			head, _ := d.Effect.Args["head"].(string)
			text := strings.TrimSpace(d.Effect.Reason)
			if text == "" {
				text = askWord(d.Effect.Action)
			}
			return &Need{Cause: CauseAsk, Ask: d.ID, Action: d.Effect.Action, Head: head, Text: text}
		}
	}
	if n := heldNeed(t, c); n != nil { // V0.136: a merge ask waits for CI
		return n
	}
	running := t.Running || t.RunningTasks() > 0
	if r := Ready(c.PR, running); r != "" {
		if c.ParkHolds() {
			return nil
		}
		return &Need{Cause: CauseReady, Text: r, Head: c.PR.Head}
	}
	why := c.blocks()
	if why == "" {
		return nil
	}
	if running {
		text := FixingCI
		if condition(c, CondChecks) == nil || condition(c, CondChecks).Status != CondFail {
			text = "Agent is working on it: " + why
		}
		return &Need{Cause: CauseFixing, Text: text, Head: changeHead(c), Muted: true}
	}
	if !t.Stopped || c.ParkHolds() {
		return nil // the agent may still pick it up; or the owner said Not now
	}
	return &Need{Cause: CauseFailure, Text: why, Head: changeHead(c)}
}

// ThreadNeed is the first change of the thread, newest first, with a need
// that is not muted, and that need.
func ThreadNeed(t *Thread) (*Change, *Need) {
	for i := len(t.Changes) - 1; i >= 0; i-- {
		if n := ChangeNeed(t, &t.Changes[i]); n != nil && !n.Muted {
			return &t.Changes[i], n
		}
	}
	return nil, nil
}

// askFor is the id of the thread's change an open ask is about, or "": a
// change_request names it; an effect_request names its PR.
func askFor(t *Thread, d Decision) string {
	if d.Kind != DecisionEffect || d.Effect == nil {
		return ""
	}
	if id, _ := d.Effect.Args["change"].(string); id != "" {
		if c := FindChange(t, id); c != nil {
			return c.ID
		}
		return id
	}
	if url, _ := d.Effect.Args["pr"].(string); url != "" {
		if c := changeByPR(t, url); c != nil {
			return c.ID
		}
	}
	return ""
}

// askWord is an ask's action in words, when the agent gave no reason.
func askWord(action string) string {
	switch action {
	case FyrMerge:
		return "Asks you to merge"
	}
	if a, ok := strings.CutPrefix(action, "change."); ok {
		return "Asks you to " + a
	}
	return "Asks you"
}

// stoppedNow is the 5-minute rule at now: no run or task in progress, and
// the last run ended StopAfter ago or more (or never ran here).
func stoppedNow(t *Thread, now time.Time) bool {
	if t.Running || t.RunningTasks() > 0 {
		return false
	}
	return t.RunEndedAt == nil || !now.Before(t.RunEndedAt.Add(StopAfter))
}

// stopCheck is when the clock must re-reduce the thread so a failure that
// waits for the 5-minute rule turns into Stopped on a failure, or nil.
func stopCheck(t *Thread) *time.Time {
	if t.Stopped || t.Running || t.RunningTasks() > 0 || t.ArchivedAt != nil {
		return nil
	}
	failing := false
	for i := range t.Changes {
		c := &t.Changes[i]
		if !c.IsDone() && c.blocks() != "" {
			failing = true
			break
		}
	}
	if !failing {
		return nil
	}
	at := time.Time{}
	if t.RunEndedAt != nil {
		at = t.RunEndedAt.Add(StopAfter)
	}
	return &at
}

