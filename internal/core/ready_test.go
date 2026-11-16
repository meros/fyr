package core

import "testing"

// SPEC V0.97, decision 0024 section 5: Ready is "Merge" for an open, green,
// clean PR, "Review and merge" when only a required review blocks it, and
// "" otherwise.
func TestReady(t *testing.T) {
	green := PR{State: "OPEN", Checks: ChecksPass, MergeState: "CLEAN", Head: "abc"}
	with := func(f func(*PR)) *PR { p := green; f(&p); return &p }
	cases := []struct {
		name    string
		pr      *PR
		running bool
		want    string
	}{
		{"clean", &green, false, ReadyMerge},
		{"has hooks", with(func(p *PR) { p.MergeState = "HAS_HOOKS" }), false, ReadyMerge},
		{"blocked by a required review", with(func(p *PR) { p.MergeState = "BLOCKED"; p.Review = "REVIEW_REQUIRED" }), false, ReadyReview},
		{"blocked by something else", with(func(p *PR) { p.MergeState = "BLOCKED" }), false, ""},
		{"blocked, changes requested", with(func(p *PR) { p.MergeState = "BLOCKED"; p.Review = "CHANGES_REQUESTED" }), false, ""},
		{"no PR", nil, false, ""},
		{"merged", with(func(p *PR) { p.State = "MERGED" }), false, ""},
		{"closed", with(func(p *PR) { p.State = "CLOSED" }), false, ""},
		{"draft", with(func(p *PR) { p.Draft = true }), false, ""},
		{"checks none", with(func(p *PR) { p.Checks = ChecksNone }), false, ""},
		{"checks pending", with(func(p *PR) { p.Checks = ChecksPending }), false, ""},
		{"checks fail", with(func(p *PR) { p.Checks = ChecksFail }), false, ""},
		{"conflict", with(func(p *PR) { p.Conflict = true }), false, ""},
		{"behind", with(func(p *PR) { p.MergeState = "BEHIND" }), false, ""},
		{"unstable", with(func(p *PR) { p.MergeState = "UNSTABLE" }), false, ""},
		{"unknown merge state", with(func(p *PR) { p.MergeState = "" }), false, ""},
		{"auto-merge on", with(func(p *PR) { p.AutoMerge = true }), false, ""},
		{"a run or task running", &green, true, ""},
	}
	for _, c := range cases {
		if got := Ready(c.pr, c.running); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
