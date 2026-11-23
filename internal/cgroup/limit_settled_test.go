package cgroup

import (
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
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
		{"learned", resource.LearnRuns, true},
		{"raised", resource.LearnRuns, true}, // 20 Nov: 921 MB learned, then 1097 MB raised
		{"raised", resource.LearnRuns + 4, true},
		{"raised", resource.LearnRuns - 1, false},
		{"learned", resource.LearnRuns - 1, false},
		{"unknown", resource.LearnRuns, false},
	} {
		if got := LimitSettled(c.why, c.runs); got != c.want {
			t.Errorf("LimitSettled(%q, %d) = %v, want %v", c.why, c.runs, got, c.want)
		}
	}
}
