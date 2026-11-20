package cgroup

import (
	"testing"

	"github.com/meros/fyr/internal/slots"
)

// V0.limit-raised-settled: a limit raised on a class learned from LearnRuns
// runs is as settled as the learned one; the raise follows a learned limit,
// so it is no sizing below the prior (limit-under-prior).
func TestLimitSettled(t *testing.T) {
	for _, c := range []struct {
		why  string
		runs int
		want bool
	}{
		{"learned", slots.LearnRuns, true},
		{"raised", slots.LearnRuns, true}, // 20 Nov: 921 MB learned, then 1097 MB raised
		{"raised", slots.LearnRuns + 4, true},
		{"raised", slots.LearnRuns - 1, false},
		{"learned", slots.LearnRuns - 1, false},
		{"unknown", slots.LearnRuns, false},
	} {
		if got := LimitSettled(c.why, c.runs); got != c.want {
			t.Errorf("LimitSettled(%q, %d) = %v, want %v", c.why, c.runs, got, c.want)
		}
	}
}
