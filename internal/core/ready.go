package core

import (
	"fmt"
	"strings"
)

// The values of Ready (V0.97, decision 0024 section 5).
const (
	ReadyMerge  = "Merge"
	ReadyReview = "Review and merge"
)

// HeadMoved is the refusal of a merge whose head is not the PR's head any
// more (V0.97): nothing was merged.
const HeadMoved = "The head moved. Look again."

// Ready is what a PR waits for from the owner (V0.97): "Merge" when it is
// open, not a draft, its checks pass (none is not pass), it has no conflict,
// GitHub's merge state is CLEAN or HAS_HOOKS, no auto-merge is set and the
// thread has no run or task running; "Review and merge" when the same holds
// except that only a required review blocks it; "" otherwise.
func Ready(pr *PR, threadRunning bool) string {
	if pr == nil || pr.State != "OPEN" || pr.Draft || pr.Checks != ChecksPass || pr.Conflict || pr.AutoMerge || threadRunning {
		return ""
	}
	switch pr.MergeState {
	case "CLEAN", "HAS_HOOKS":
		return ReadyMerge
	case "BLOCKED":
		if pr.Review == "REVIEW_REQUIRED" {
			return ReadyReview
		}
	}
	return ""
}

// checksWait is why a merge of args.pr waits for CI, or "" (V0.112): Fyr
// reads its checks as pending, which is also an empty check list on a repo
// with CI (the poller decides that). A repo with no CI reads none and does
// not wait; failed checks keep V0.43's rule, and GitHub refuses a merge its
// branch rules block. The runner reads GitHub again before it merges.
func checksWait(t *Thread, args map[string]any) string {
	url, _ := args["pr"].(string)
	c := changeByPR(t, url)
	if c == nil || c.PR == nil || c.PR.State != "OPEN" || c.PR.Checks != ChecksPending {
		return ""
	}
	on := "this PR"
	if c.PR.Head != "" {
		on = shortSHA(c.PR.Head)
	}
	return "CI has not passed on " + on + " yet: Merge waits for the checks"
}

// SameHead reports whether head, the full SHA or a prefix of at least 7
// characters that the owner saw, names the commit sha (V0.97).
func SameHead(sha, head string) bool {
	sha, head = strings.ToLower(strings.TrimSpace(sha)), strings.ToLower(strings.TrimSpace(head))
	return sha != "" && len(head) >= 7 && strings.HasPrefix(sha, head)
}

// headMoved is why a merge bound to args.head cannot run, from what Fyr last
// read of args.pr (V0.97): HeadMoved when Fyr reads another head, or "".
// GitHub has the last word; the runner reads the head again.
func headMoved(t *Thread, args map[string]any) string {
	url, _ := args["pr"].(string)
	head, _ := args["head"].(string)
	if c := changeByPR(t, url); c != nil && c.PR != nil && c.PR.Head != "" && head != "" && !SameHead(c.PR.Head, head) {
		return HeadMoved
	}
	return ""
}

// fixHead sets args.head of an agent's merge request to the head Fyr reads
// now, when the agent gave none (V0.97): the owner approves that commit.
func fixHead(t *Thread, args map[string]any) {
	if h, _ := args["head"].(string); h != "" {
		return
	}
	url, _ := args["pr"].(string)
	if c := changeByPR(t, url); c != nil && c.PR != nil && c.PR.Head != "" {
		args["head"] = c.PR.Head
	}
}

// needHead is the head check of a merge's args (V0.97).
func needHead(a map[string]any) error {
	h, _ := a["head"].(string)
	if len(strings.TrimSpace(h)) < 7 {
		return fmt.Errorf("args.head must be the PR's head commit the owner saw (Fyr has not read it yet: try again after the next poll)")
	}
	return nil
}
