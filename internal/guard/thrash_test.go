package guard

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

// SPEC 0.5 V0.153: the host thrashes at a critical memory stall or at twice
// the slowdown admission tolerates; elevated stalls and a mild slowdown are
// not thrashing.
func TestThrashing(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		s    resource.Signals
		want bool
		why  string
	}{
		{resource.Signals{Stall: resource.StallCritical}, true, "memory pressure"},
		{resource.Signals{Stall: resource.StallElevated, Slowdown: f(1.8)}, false, ""},
		{resource.Signals{Slowdown: f(3.2)}, true, "CPU pressure"},
		{resource.Signals{}, false, ""},
	} {
		ok, why := Thrashing(c.s, resource.DefaultPolicy)
		if ok != c.want || !strings.HasPrefix(why, c.why) {
			t.Errorf("%+v: %v %q", c.s, ok, why)
		}
	}
}
