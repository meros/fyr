package core

import (
	"testing"
	"time"
)

// V0.review-correctness (1): the checks and the review of a PR are what GitHub
// reports for its current head. A PR read with no checks (a new head before
// CI starts, or a repo without CI) or with no review decision keeps no
// condition from an earlier read.
func TestPRFactsFollowTheSnapshot(t *testing.T) {
	cond := func(th *Thread, typ CondType) *Condition {
		t.Helper()
		c := changeByPR(th, fprURL)
		if c == nil {
			t.Fatal("no main change")
		}
		return condition(c, typ)
	}
	cases := []struct {
		name      string
		before    PR
		after     PR
		checksNil bool
		reviewNil bool
	}{
		{"fixed push, checks not started", PR{State: "OPEN", Checks: ChecksFail, Head: "aaaa"}, PR{State: "OPEN", Checks: ChecksNone, Head: "bbbb"}, true, true},
		{"same head, checks gone", PR{State: "OPEN", Checks: ChecksFail, Head: "aaaa"}, PR{State: "OPEN", Checks: ChecksNone, Head: "aaaa"}, true, true},
		{"review dismissed", PR{State: "OPEN", Checks: ChecksPass, Review: "CHANGES_REQUESTED", Head: "aaaa"}, PR{State: "OPEN", Checks: ChecksPass, Head: "aaaa"}, false, true},
		{"new head, review kept by GitHub", PR{State: "OPEN", Checks: ChecksFail, Review: "APPROVED", Head: "aaaa"}, PR{State: "OPEN", Checks: ChecksPending, Review: "APPROVED", Head: "bbbb"}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, _ := observePR(t, feedThread(), tc.before, ct0)
			if tc.before.Checks == ChecksFail {
				if b := changeByPR(th, fprURL).Blocking(); b != "CI failed" {
					t.Fatalf("before: blocking %q, want CI failed", b)
				}
			}
			th, _ = observePR(t, th, tc.after, ct0.Add(time.Minute))
			if got := cond(th, CondChecks) == nil; got != tc.checksNil {
				t.Errorf("checks condition gone = %v, want %v (%+v)", got, tc.checksNil, changeByPR(th, fprURL).Conditions)
			}
			if got := cond(th, CondReview) == nil; got != tc.reviewNil {
				t.Errorf("review condition gone = %v, want %v (%+v)", got, tc.reviewNil, changeByPR(th, fprURL).Conditions)
			}
			if b := changeByPR(th, fprURL).Blocking(); b != "" {
				t.Errorf("after: blocking %q, want nothing", b)
			}
			if a := changeByPR(th, fprURL).Annotations; len(a) != 0 {
				t.Errorf("after: annotations %v", a)
			}
		})
	}
}

// The owner's own word on a condition is not a fact Fyr read: a PR read
// without a review decision leaves it.
func TestPRFactsKeepTheOwnersCondition(t *testing.T) {
	th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "aaaa"}, ct0)
	c := changeByPR(th, fprURL)
	c.Conditions = append(c.Conditions, Condition{Type: CondReview, Status: CondOK, Source: ByOwner, At: ct0})
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksNone, Head: "bbbb"}, ct0.Add(time.Minute))
	if k := condition(changeByPR(th, fprURL), CondReview); k == nil || k.Source != ByOwner {
		t.Fatalf("the owner's review condition went: %+v", changeByPR(th, fprURL).Conditions)
	}
}
