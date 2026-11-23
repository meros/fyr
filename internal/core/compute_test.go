package core

import "time"

// Compute is the attention with no repo on auto-merge, for the tests.
func Compute(t *Thread, v *View, now time.Time) Attention {
	return ComputeWith(t, v, now, nil)
}
