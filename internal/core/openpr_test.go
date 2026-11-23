package core

import (
	"testing"
	"time"
)

// V0.one-predicate (1): "a PR that counts as open" and "a PR that waits on
// CI" are one predicate each, and every reader agrees: the open-PR checks
// of settle, the merge in text and the next main PR (OpenPR), the main PR
// that "Settle when merged" is offered on (OpenMain), and the work that
// goes on and the "Waiting for CI" line (PRWaitsOnCI).
func TestOnePRPredicate(t *testing.T) {
	read := ct0
	cases := []struct {
		name          string
		c             Change
		open, waitsCI bool
	}{
		{"open, checks run", Change{PR: &PR{State: "OPEN", Checks: ChecksPending, CheckedAt: &read}}, true, true},
		{"open, never read", Change{PR: &PR{State: "OPEN", Checks: ChecksPending}}, true, false},
		{"open, checks pass", Change{PR: &PR{State: "OPEN", Checks: ChecksPass, CheckedAt: &read}}, true, false},
		{"owner done", Change{PR: &PR{State: "OPEN", Checks: ChecksPending, CheckedAt: &read}, Done: &DoneMark{At: ct0, By: ByOwner, Reason: DoneByOwner}}, false, false},
		{"abandoned", Change{PR: &PR{State: "OPEN", Checks: ChecksPending, CheckedAt: &read}, Phase: PhaseAbandoned}, false, false},
		{"dismissed", Change{PR: &PR{State: "OPEN", Checks: ChecksPending, CheckedAt: &read}, Dismissed: true}, false, false},
		{"merged", Change{PR: &PR{State: "MERGED", Checks: ChecksPending, CheckedAt: &read}, Phase: PhaseLanded}, false, false},
		{"no PR", Change{}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.c
			c.ID, c.Kind, c.Title = "c1", KindCode, "x"
			if c.Phase == "" {
				c.Phase = PhaseReview
			}
			th := &Thread{ID: "t", Changes: []Change{c}, MainChange: "c1"}
			if got := th.Changes[0].OpenPR(); got != tc.open {
				t.Errorf("OpenPR = %v, want %v", got, tc.open)
			}
			if got := OpenPR(th); got != tc.open {
				t.Errorf("thread OpenPR = %v, want %v", got, tc.open)
			}
			if got := OpenMain(th) != nil; got != tc.open && !c.Dismissed {
				t.Errorf("OpenMain = %v, want %v", got, tc.open)
			}
			if got := th.nextPRChange(""); c.PR != nil && !c.Dismissed && (got != nil && got.OpenPR()) != tc.open {
				t.Errorf("nextPRChange open = %v, want %v", got != nil && got.OpenPR(), tc.open)
			}
			if got := th.Changes[0].PRWaitsOnCI(); got != tc.waitsCI {
				t.Errorf("PRWaitsOnCI = %v, want %v", got, tc.waitsCI)
			}
			if got := ciWait(th) != ""; got != tc.waitsCI {
				t.Errorf("ciWait = %q, want waiting %v", ciWait(th), tc.waitsCI)
			}
			if got := WorkGoingAt(th, ct0.Add(time.Minute)); got != tc.waitsCI {
				t.Errorf("WorkGoingAt = %v, want %v", got, tc.waitsCI)
			}
		})
	}
}

// V0.one-predicate (1): "Settle when merged" is not offered on a main PR the
// owner marked done, which OpenPR does not count as open.
func TestSettleWhenMergedNotOnADoneMainPR(t *testing.T) {
	read := ct0
	th := &Thread{ID: "t", MainChange: "c1", Changes: []Change{{ID: "c1", Kind: KindCode, Title: "x", Phase: PhaseReview,
		PR: &PR{State: "OPEN", Checks: ChecksPass, CheckedAt: &read}, Done: &DoneMark{At: ct0, By: ByOwner, Reason: DoneByOwner}}}}
	if why := Registry["thread.settleWhenMerged"].Available(th, &View{}, ct0); why == "" {
		t.Fatal("Settle when merged is offered on an owner-done main PR")
	}
	if SettleWhenMerged(th, ByOwner, ct0) {
		t.Fatal("Settle when merged armed on an owner-done main PR")
	}
}

// V0.one-predicate (3): every Fyr effect a change_request asks for is found
// by its ask, and no ask finds another.
func TestFyrEffectForEveryAsk(t *testing.T) {
	for id, fx := range FyrEffects {
		if fx.Ask == "" {
			continue
		}
		if got := FyrEffectFor(fx.Ask); got == nil || got.ID != id {
			t.Errorf("ask %q: got %+v, want %s", fx.Ask, got, id)
		}
	}
	if FyrEffectFor("") != nil || FyrEffectFor("nonesuch") != nil {
		t.Error("an ask that names no effect found one")
	}
}
