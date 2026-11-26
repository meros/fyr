package guard

import (
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

// V0.freeze-controller: a thread that runs n heavy commands at once holds n
// needs, and a grow for another run asks for room for all of them.
func TestScaledCountsEveryRunAtOnce(t *testing.T) {
	one := resource.Need{CPU: 1.5, Mem: 3 << 30}
	for _, c := range []struct {
		n   int
		cpu float64
		mem uint64
	}{{0, 1.5, 3 << 30}, {1, 1.5, 3 << 30}, {2, 3, 6 << 30}, {4, 6, 12 << 30}} {
		got := scaled(one, c.n)
		if got.CPU != c.cpu || got.Mem != c.mem {
			t.Errorf("scaled(n=%d) = %+v, want cpu %v mem %v", c.n, got, c.cpu, c.mem)
		}
	}
}
