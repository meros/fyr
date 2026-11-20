// V0.freeze-fast-class-name: when the PSI trigger fired and the agents' memory
// stalls critically the guard does not wait the window; FlagNow flags the
// thread whose working set grew most above its idle level at once.
package cgroup

import (
	"testing"
	"time"
)

func TestFlagNowTakesTheThreadThatGrewMost(t *testing.T) {
	d := &Detector{}
	now := time.Date(2026, 11, 20, 7, 32, 0, 0, time.UTC)
	tick := func(work map[string]float64, adm map[string]bool) {
		var rs []Reading
		for th, w := range work {
			c := 0.1
			rs = append(rs, Reading{Thread: th, CPU: &c, Work: uint64(w)})
		}
		d.Observe(now, DetectInput{Capacity: &desk, Config: DefaultUndeclared, Reads: rs, Admitted: adm})
		now = now.Add(5 * time.Second)
	}
	idle := map[string]float64{"t_a": 0.9 * gb, "t_b": 0.9 * gb, "t_c": 0.9 * gb, "t_job": 0.9 * gb}
	for range 4 {
		tick(idle, nil)
	}
	// 10 s in: t_a grew 2.1 GB, t_b 1.3 GB, t_c 0.5 GB (under the 1.12 GB
	// threshold), t_job 4 GB but admitted. The window (45 s) is far off.
	grown := map[string]float64{"t_a": 3.0 * gb, "t_b": 2.2 * gb, "t_c": 1.4 * gb, "t_job": 4.9 * gb}
	adm := map[string]bool{"t_job": true}
	tick(grown, adm)
	tick(grown, adm)
	if len(d.Flags()) != 0 {
		t.Fatalf("setup: flagged before the window: %+v", d.Flags())
	}
	if got := d.FlagNow(now); got != "t_a" {
		t.Fatalf("FlagNow = %q, want t_a (grew most, not admitted)", got)
	}
	fl := d.Flags()
	if len(fl) != 1 || fl[0].Thread != "t_a" || !fl[0].MemOver || !fl[0].Since.Equal(now) || fl[0].Mem < uint64(2*gb) {
		t.Fatalf("flags %+v", fl)
	}
	if got := d.FlagNow(now); got != "t_b" {
		t.Fatalf("second FlagNow = %q, want t_b", got)
	}
	if got := d.FlagNow(now); got != "" {
		t.Fatalf("third FlagNow = %q: t_c is under the threshold, t_job admitted", got)
	}
	// The early episode goes on with the readings and is kept as any other.
	tick(grown, adm)
	if len(d.Flags()) != 2 {
		t.Fatalf("the early flags did not hold: %+v", d.Flags())
	}
}
