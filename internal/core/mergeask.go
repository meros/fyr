package core

import (
	"fmt"
	"slices"
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

// mergeGate is what the merge ask req may do now, why, and the agent's
// next step for a close: open when the merge can run (checks pass, or the
// repo has no CI, and GitHub can merge); wait while CI has not passed on
// the asked head or GitHub has not computed the merge state; close when CI
// failed, the head moved, or GitHub will not merge it (a conflict, a
// review, the branch rules). A merge state Fyr never read ("") does not
// hold the ask: the runner reads GitHub again before it merges (V0.112).
func mergeGate(t *Thread, req *EffectReq) (askGate, string, string) {
	url, _ := req.Args["pr"].(string)
	head, _ := req.Args["head"].(string)
	c := changeByPR(t, url)
	if c == nil || c.PR == nil || c.PR.Head == "" {
		return gateWait, "Fyr has not read the PR yet", ""
	}
	p := c.PR
	base := baseName(p)
	switch {
	case p.State != "OPEN":
		return gateClose, "the PR is not open", "nothing; it is not open any more"
	case head != "" && !SameHead(p.Head, head):
		return gateClose, fmt.Sprintf("the head moved to %s", shortSHA(p.Head)), "ask again for the new head; Fyr shows it when CI is green on it"
	case p.Draft || p.MergeState == "DRAFT":
		return gateClose, "the PR is a draft", "mark it ready with gh pr ready"
	case p.Conflict || p.MergeState == "DIRTY":
		return gateClose, "it conflicts with " + base, "rebase on " + base + ", resolve the conflicts and push"
	case p.Review == "CHANGES_REQUESTED":
		return gateClose, "a review asks for changes", "address the review comments and push"
	case p.Checks == ChecksFail:
		return gateClose, fmt.Sprintf("CI failed on %s: %s", shortSHA(p.Head), failedNames(p.FailedChecks)), "fix the failing checks and push"
	case p.Checks == ChecksPending:
		return gateWait, waitingText(p), ""
	}
	switch p.MergeState {
	case "UNKNOWN":
		return gateWait, "Waiting for GitHub to read whether it can merge", ""
	case "BEHIND":
		return gateClose, "it is behind " + base + " and the branch rules require it to be up to date", "rebase on " + base + " and push"
	case "BLOCKED":
		why, next := blockedWhy(p)
		return gateClose, why, next
	}
	return gateOpen, "", ""
}

func baseName(p *PR) string {
	if p.Base == "" {
		return "the base branch"
	}
	return p.Base
}

// blockedWhy names what blocks a PR GitHub reads as BLOCKED while its
// checks are green, from the review decision and the base's rules the
// poller read, with the agent's next step. The first rule that applies
// wins; Merge as admin is the owner's, on GitHub.
func blockedWhy(p *PR) (string, string) {
	base := baseName(p)
	has := func(r string) bool { return slices.Contains(p.Rules, r) }
	switch {
	case p.Review == "REVIEW_REQUIRED" || (has("pull_request") && p.Review != "APPROVED"):
		return "a review is required by the rules of " + base, "request a review from someone who can approve (gh pr edit --add-reviewer <login>), or ask the owner to approve it"
	case len(p.MissingChecks) > 0:
		return "a required check has not reported on " + shortSHA(p.Head) + ": " + failedNames(p.MissingChecks), "make the required checks run on this head (push again, or check that the workflow runs for this branch)"
	case has("required_signatures"):
		return "the rules of " + base + " require signed commits", "sign the commits (git rebase --exec 'git commit --amend --no-edit -S' " + base + ") and push"
	case has("required_linear_history"):
		return "the rules of " + base + " require a linear history", "rebase on " + base + " instead of merging it in, and push"
	case has("required_deployments"):
		return "the rules of " + base + " require a successful deployment first", "deploy to the required environment, or ask the owner"
	case has("merge_queue"):
		return base + " merges through a merge queue", "ask the owner to add the PR to the merge queue on GitHub"
	case len(p.Rules) > 0:
		return "the rules of " + base + " block it (" + strings.Join(p.Rules, ", ") + ")", "ask the owner to check the rules, or to merge it on GitHub with admin rights"
	case p.Protected:
		return "the branch protection of " + base + " blocks it", "ask the owner to check the branch protection, or to merge it on GitHub with admin rights"
	}
	return "GitHub's branch rules block it", "ask the owner to check the branch rules, or to merge it on GitHub with admin rights"
}

// refusedNext is the agent's next step after GitHub refused a merge Fyr
// ran, from GitHub's words and the PR Fyr read last (nil: not tracked).
func refusedNext(p *PR, text string) string {
	l := strings.ToLower(text)
	switch {
	case strings.Contains(l, strings.ToLower(HeadMoved)) || strings.Contains(l, "head branch was modified"):
		return "ask again for the new head; Fyr shows it when CI is green on it"
	case strings.Contains(l, "approv") || strings.Contains(l, "review is required") || strings.Contains(l, "review required"):
		return "request a review from someone who can approve (gh pr edit --add-reviewer <login>), or ask the owner to approve it"
	case strings.Contains(l, "sign"):
		return "sign the commits and push"
	case strings.Contains(l, "status check") || strings.Contains(l, "ci has not passed") || strings.Contains(l, "no check has reported"):
		return "wait for the required checks to pass on this head, then ask again"
	case strings.Contains(l, "merge queue"):
		return "ask the owner to add the PR to the merge queue on GitHub"
	case strings.Contains(l, "merge conflict") || strings.Contains(l, "conflicts") || strings.Contains(l, "not mergeable"):
		return "rebase on " + baseName(p) + ", resolve the conflicts and push"
	case strings.Contains(l, "policy") || strings.Contains(l, "protect") || strings.Contains(l, "rule"):
		if p != nil && (len(p.Rules) > 0 || p.Protected || p.Review != "") {
			_, next := blockedWhy(p)
			return next
		}
		return "ask the owner to check the branch rules, or to merge it on GitHub with admin rights"
	}
	return "read GitHub's error, fix the cause, and ask again"
}

// mergeRefused records a merge Fyr ran that failed (V0.136), refused by
// GitHub or by Fyr's last read: a mergeable condition on the PR's change
// with those words, so the card shows it
// and Stopped on a failure applies, and one message to the agent with the
// next step. A push or the merge clears the condition.
func mergeRefused(t *Thread, url, text string, now time.Time) []Event {
	c := changeByPR(t, url)
	first := strings.TrimSpace(text)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = strings.TrimSpace(first[:i])
	}
	first = strings.TrimPrefix(first, "not merged: ")
	if first == "" {
		first = "the merge failed"
	}
	var p *PR
	id := ""
	if c != nil {
		p, id = c.PR, c.ID
		setCondition(c, CondMerge, CondFail, "Merge refused: "+first, ByObserved, now)
		c.UpdatedAt = now
	}
	return []Event{{"merge.refused", map[string]any{"pr": url, "change": id, "reason": first, "next": refusedNext(p, text)}}}
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
	switch g, why, next := mergeGate(t, d.Effect); g {
	case gateClose:
		return Result{}, fmt.Errorf("Fyr does not ask the owner to merge: %s. Next: %s; then ask again", why, next)
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
	closed := func(d Decision, why, next string) {
		evs = append(evs, Event{"decision.closed", map[string]any{
			"id": d.ID, "answer": "Not asked: " + why, "gate": true, "reason": why, "next": next,
			"action": d.Effect.Action, "args": d.Effect.Args,
		}})
	}
	var held []Decision
	for _, d := range t.HeldAsks {
		if why := askVoid(t, d); why != "" {
			closed(d, why, "nothing; the work it was about is over")
			continue
		}
		switch g, why, next := mergeGate(t, d.Effect); g {
		case gateWait:
			held = append(held, d)
		case gateClose:
			closed(d, why, next)
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
		switch g, why, next := mergeGate(t, d.Effect); g {
		case gateWait:
			held = append(held, d)
			evs = append(evs, Event{"decision.held", map[string]any{"decision": d, "why": why}})
		case gateClose:
			closed(d, why, next)
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
		_, why, _ := mergeGate(t, d.Effect)
		if why == "" {
			why = "Waiting for CI"
		}
		return &Need{Cause: CauseWaiting, Ask: d.ID, Action: d.Effect.Action, Head: head, Text: why, Muted: true}
	}
	return nil
}
