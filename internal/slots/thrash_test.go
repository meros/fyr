package slots

import (
	"strings"
	"testing"
)

// SPEC 0.5 V0.153: the host thrashes at a critical memory stall or at twice
// the slowdown admission tolerates; elevated stalls and a mild slowdown are
// not thrashing.
func TestThrashing(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		s    Signals
		want bool
		why  string
	}{
		{Signals{Stall: StallCritical}, true, "memory pressure"},
		{Signals{Stall: StallElevated, Slowdown: f(1.8)}, false, ""},
		{Signals{Slowdown: f(3.2)}, true, "CPU pressure"},
		{Signals{}, false, ""},
	} {
		ok, why := Thrashing(c.s, DefaultPolicy)
		if ok != c.want || !strings.HasPrefix(why, c.why) {
			t.Errorf("%+v: %v %q", c.s, ok, why)
		}
	}
}
