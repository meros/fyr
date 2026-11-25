package core

import "fmt"

// V0.threads-row-pr: the PR an agent's `threads` list names for a thread is
// the one its inbox row shows (web/src/pr.ts prUrgency, web/src/offers.ts
// headerPrs): of the open PRs of changes still in flight, the most urgent
// (failing > conflicts > changes requested > review required > ready >
// running); with none open, the newest PR. The main change's PR is the first
// one the thread got and keeps its place after it merged, so it is not it.

// prUrgency orders open PRs as the row does; lower is more urgent.
func prUrgency(p *PR) int {
	switch {
	case p.Checks == ChecksFail:
		return 0
	case p.Conflict:
		return 1
	case p.Review == "CHANGES_REQUESTED":
		return 2
	case p.Review == "REVIEW_REQUIRED":
		return 3
	case p.Checks == ChecksPending:
		return 5
	}
	return 4
}

// prWord is the row chip's word for p.
func prWord(p *PR) string {
	switch {
	case p.State == "MERGED":
		return "merged"
	case p.State == "CLOSED":
		return "closed"
	case p.State == PRMissing:
		return "missing"
	case p.Conflict:
		return "conflicts"
	case p.Checks == ChecksFail:
		return "failing"
	case p.Review == "CHANGES_REQUESTED" || p.Review == "REVIEW_REQUIRED":
		return "review"
	case p.Checks == ChecksPending:
		return "running"
	}
	return "open"
}

// RowPR is the PR the thread's row shows among its shown changes, and how
// many other open PRs it has. Without an open one it is the newest PR of a
// change not dismissed, else main.
func RowPR(changes []Change, main *PR) (*PR, int) {
	var top, newest *Change
	open := 0
	for i := range changes {
		c := &changes[i]
		if c.PR == nil || c.Dismissed {
			continue
		}
		later := func(x *Change) bool { return !c.CreatedAt.Before(x.CreatedAt) }
		if newest == nil || later(newest) {
			newest = c
		}
		if !c.OpenPR() {
			continue
		}
		open++
		if top == nil || prUrgency(c.PR) < prUrgency(top.PR) || prUrgency(c.PR) == prUrgency(top.PR) && later(top) {
			top = c
		}
	}
	if top != nil {
		return top.PR, open - 1
	}
	if newest != nil {
		return newest.PR, 0
	}
	return main, 0
}

// RowPRText is RowPR as one line: "PR #12 conflicts +2 open", or "".
func RowPRText(changes []Change, main *PR) string {
	p, more := RowPR(changes, main)
	if p == nil {
		return ""
	}
	s := fmt.Sprintf("PR #%d %s", p.Number, prWord(p))
	if more > 0 {
		s += fmt.Sprintf(" +%d open", more)
	}
	return s
}
