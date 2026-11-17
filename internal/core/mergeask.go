package core

import (
	"fmt"
	"strings"
	"time"
)

// A merge ask waits for green CI (V0.136): Fyr asks the owner to merge only
// when the merge can run now. An agent's merge ask on a head whose checks
// are pending is held (Thread.HeldAsks): no card, no Needs you, no push;
// the change shows "Waiting for CI". When the poller reads the head green,
// the ask opens as a Decision (and auto-merges where the repo's setting is
// on, V0.102). When CI fails, the PR conflicts, a review is required, or
// the head moves, the ask closes and the agent hears why. The gate reads
// only the PR snapshot the poller keeps; it adds no GitHub reads.

// The gate's answers.
type askGate int

const (
	gateOpen askGate = iota
	gateWait
	gateClose
)

// CauseWaiting is a change whose merge ask waits for CI (V0.136): shown
// muted on its card, never in Needs you.
const CauseWaiting = "waiting"

// isMergeAsk reports whether d is an agent's merge ask.
func isMergeAsk(d Decision) bool {
	return d.Kind == DecisionEffect && d.Effect != nil && d.Effect.Action == FyrMerge
}

// mergeGate is what the merge ask req may do now, with the words: open
// when the merge can run (checks pass, or the repo has no CI, and GitHub
// can merge); wait while CI has not passed on the asked head or GitHub has
// not computed the merge state; close when CI failed, the head moved, or
// GitHub will not merge it. A merge state Fyr never read ("") does not
// hold the ask: the runner reads GitHub again before it merges (V0.112).
func mergeGate(t *Thread, req *EffectReq) (askGate, string) {
	url, _ := req.Args["pr"].(string)
	head, _ := req.Args["head"].(string)
	c := changeByPR(t, url)
	if c == nil || c.PR == nil || c.PR.Head == "" {
		return gateWait, "Fyr has not read the PR yet"
	}
	p := c.PR
	base := p.Base
	if base == "" {
		base = "the base branch"
	}
	switch {
	case p.State != "OPEN":
		return gateClose, "the PR is not open"
	case head != "" && !SameHead(p.Head, head):
		return gateClose, fmt.Sprintf("the head moved to %s", shortSHA(p.Head))
	case p.Draft || p.MergeState == "DRAFT":
		return gateClose, "the PR is a draft"
	case p.Conflict || p.MergeState == "DIRTY":
		return gateClose, "it conflicts with " + base
	case p.Review == "CHANGES_REQUESTED":
		return gateClose, "a review asks for changes"
	case p.Checks == ChecksFail:
		return gateClose, fmt.Sprintf("CI failed on %s: %s", shortSHA(p.Head), failedNames(p.FailedChecks))
	case p.Checks == ChecksPending:
		return gateWait, waitingText(p)
	}
	switch p.MergeState {
	case "UNKNOWN":
		return gateWait, "Waiting for GitHub to read whether it can merge"
	case "BEHIND":
		return gateClose, "it is behind " + base + ": update the branch"
	case "BLOCKED":
		if p.Review == "REVIEW_REQUIRED" {
			return gateClose, "a review is required"
		}
		return gateClose, "the branch rules block it"
	}
	return gateOpen, ""
}

// waitingText is the card's line while CI runs.
func waitingText(p *PR) string {
	if p.ChecksTotal == 0 {
		return "Waiting for CI: no check has reported yet"
	}
	return fmt.Sprintf("Waiting for CI: %d of %d checks done", p.ChecksDone, p.ChecksTotal)
}

// failedNames names at most 5 failed checks.
func failedNames(names []string) string {
	switch {
	case len(names) == 0:
		return "a check failed"
	case len(names) > 5:
		return strings.Join(names[:5], ", ") + fmt.Sprintf(" and %d more", len(names)-5)
	}
	return strings.Join(names, ", ")
}

// openMergeAsk records the agent's merge ask: open when the merge can run
// now, held while CI runs, and refused with the reason when it cannot
// merge at all.
func openMergeAsk(t *Thread, d Decision) (Result, error) {
	switch g, why := mergeGate(t, d.Effect); g {
	case gateClose:
		return Result{}, fmt.Errorf("Fyr does not ask the owner to merge: %s. Fix it, push, and ask again", why)
	case gateWait:
		t.HeldAsks = append(t.HeldAsks, d)
		return Result{Events: []Event{{"decision.held", map[string]any{"decision": d, "why": why}}}}, nil
	}
	DecisionOpened(t, d)
	return Result{Events: []Event{{"decision.opened", map[string]any{"decision": d}}}}, nil
}

// gateMergeAsks moves the thread's merge asks by the PR Fyr read last,
// after each reduce: a held ask opens on green and closes on red; an open
// ask closes on red and goes back to held when CI runs again on its head.
// An ask that is void (0024 §5) closes as void.
func gateMergeAsks(t *Thread, now time.Time) []Event {
	var evs []Event
	closed := func(d Decision, why string) {
		evs = append(evs, Event{"decision.closed", map[string]any{
			"id": d.ID, "answer": "Not asked: " + why, "gate": true, "reason": why,
			"action": d.Effect.Action, "args": d.Effect.Args,
		}})
	}
	var held []Decision
	for _, d := range t.HeldAsks {
		if why := askVoid(t, d); why != "" {
			closed(d, why)
			continue
		}
		switch g, why := mergeGate(t, d.Effect); g {
		case gateWait:
			held = append(held, d)
		case gateClose:
			closed(d, why)
		default:
			d.CreatedAt = now // asked now: the owner sees it now
			DecisionOpened(t, d)
			evs = append(evs, Event{"decision.opened", map[string]any{"decision": d, "held": true}})
		}
	}
	keep := t.Decisions[:0:0]
	for _, d := range t.Decisions {
		if !isMergeAsk(d) {
			keep = append(keep, d)
			continue
		}
		switch g, why := mergeGate(t, d.Effect); g {
		case gateWait:
			held = append(held, d)
			evs = append(evs, Event{"decision.held", map[string]any{"decision": d, "why": why}})
		case gateClose:
			closed(d, why)
		default:
			keep = append(keep, d)
		}
	}
	if len(keep) != len(t.Decisions) {
		t.Decisions = keep
		if len(t.Decisions) == 0 {
			t.Decisions = nil
		}
	}
	t.HeldAsks = held
	return evs
}

// heldNeed is the waiting line of a change with a held merge ask, or nil.
func heldNeed(t *Thread, c *Change) *Need {
	for _, d := range t.HeldAsks {
		if askFor(t, d) != c.ID {
			continue
		}
		head, _ := d.Effect.Args["head"].(string)
		_, why := mergeGate(t, d.Effect)
		if why == "" {
			why = "Waiting for CI"
		}
		return &Need{Cause: CauseWaiting, Ask: d.ID, Action: d.Effect.Action, Head: head, Text: why, Muted: true}
	}
	return nil
}
