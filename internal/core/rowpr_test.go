package core

import (
	"testing"
	"time"
)

// V0.threads-row-pr: the threads tool names the PR the inbox row shows.
func TestRowPR(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ch := func(n int, at int, ph Phase, p PR) Change {
		p.Number = n
		return Change{ID: "c" + string(rune('a'+at)), Phase: ph, CreatedAt: t0.Add(time.Duration(at) * time.Minute), PR: &p}
	}
	merged := PR{State: "MERGED", Checks: ChecksPass}
	cases := []struct {
		name    string
		changes []Change
		want    string
	}{
		{"an open conflicting PR beats the first, merged one", []Change{
			ch(1678, 0, PhaseLive, merged),
			ch(13569, 1, PhaseReview, PR{State: "OPEN", Checks: ChecksPass, Conflict: true}),
		}, "PR #13569 conflicts"},
		{"failing beats newer open PRs, +N counts the other open ones", []Change{
			ch(1158, 0, PhaseLanded, merged),
			ch(1168, 1, PhaseReview, PR{State: "OPEN", Checks: ChecksFail}),
			ch(1169, 2, PhaseReview, PR{State: "OPEN", Checks: ChecksPending}),
			ch(1170, 3, PhaseReview, PR{State: "OPEN", Checks: ChecksPass}),
		}, "PR #1168 failing +2 open"},
		{"all merged: the newest", []Change{
			ch(1615, 0, PhaseLanded, merged),
			ch(1672, 2, PhaseLive, merged),
			ch(1664, 1, PhaseLive, merged),
		}, "PR #1672 merged"},
		{"an abandoned change's PR is not open", []Change{
			ch(10, 0, PhaseLanded, merged),
			ch(11, 1, PhaseAbandoned, PR{State: "OPEN", Checks: ChecksPass}),
		}, "PR #11 open"},
		{"a dismissed change is left out", []Change{
			ch(20, 0, PhaseReview, PR{State: "OPEN", Checks: ChecksPass}),
			func() Change {
				c := ch(21, 1, PhaseReview, PR{State: "OPEN", Checks: ChecksFail})
				c.Dismissed = true
				return c
			}(),
		}, "PR #20 open"},
		{"a closed PR reads closed", []Change{
			ch(30, 0, PhaseReview, PR{State: "CLOSED"}),
		}, "PR #30 closed"},
	}
	for _, c := range cases {
		if got := RowPRText(c.changes, c.changes[0].PR); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := RowPRText(nil, nil); got != "" {
		t.Errorf("no PR: %q", got)
	}
	if got := RowPRText(nil, &PR{Number: 5, State: "OPEN"}); got != "PR #5 open" {
		t.Errorf("main only: %q", got)
	}
}
