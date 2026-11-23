package guard

// V0.job-limits, V0.guard-finish: a job's limits as the policy decides them,
// without a cgroup. internal/cgroup's tests write them through the job groups.

import (
	"strconv"
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestJobLimitsFromTheLearnedClass(t *testing.T) {
	const page = 4096
	cases := []struct {
		name           string
		z              resource.JobSize
		budget, used   uint64
		cores          float64
		lifted         bool
		high, cpu, why string
	}{
		{"learned", resource.JobSize{Class: "fyr: make test-go", Peak: 2 * gb, Demand: 2.9, Known: true}, 12 * gb, 4 * gb, 8, false, "2684354560", "400000 100000", "learned"},
		{"at least a core", resource.JobSize{Peak: gb, Demand: 0.2, Known: true}, 12 * gb, 0, 8, false, "1342177280", "100000 100000", "learned"},
		{"demand above the agents' cores", resource.JobSize{Peak: gb, Demand: 7, Known: true}, 12 * gb, 0, 8, false, "1342177280", "max", "learned"},
		{"unknown: the free budget", resource.JobSize{}, 12 * gb, 9 * gb, 8, false, "3221225472", "max", "unknown"},
		{"unknown: at most half the budget", resource.JobSize{}, 12 * gb, gb, 8, false, "6442450944", "max", "unknown"},
		{"swap nearly full", resource.JobSize{Peak: gb, Demand: 1, Known: true}, 12 * gb, 0, 8, true, "max", "max", "swap-full"},
		{"no budget", resource.JobSize{}, 0, 0, 8, false, "max", "max", "no-budget"},
	}
	for _, c := range cases {
		high, cpu, why := JobLimits(c.z, c.budget, c.used, c.cores, c.lifted, page)
		if c.high != "max" { // whole pages, as the kernel keeps it
			n, _ := strconv.ParseUint(c.high, 10, 64)
			c.high = strconv.FormatUint(n/page*page, 10)
		}
		if high != c.high || cpu != c.cpu || why != c.why {
			t.Errorf("%s: high=%s cpu=%s why=%s, want %s %s %s", c.name, high, cpu, why, c.high, c.cpu, c.why)
		}
	}
}

// V0.admission-memory: under three runs a class sizes no smaller than the prior.
func TestSizeForAClassUnderThreeRunsSizesWithThePrior(t *testing.T) {
	// 12 GB and 8 cores for the agents, two slots: the prior is half of each.
	one := resource.JobSize{Class: "fyr: make test-go", Peak: gb, Demand: 0.5, Known: true, Runs: 1}
	if z := SizeFor(one, 2, 12*gb, 8); z.Peak != 6*gb || z.Demand != 4 {
		t.Fatalf("one run sizes with its learned peak, not the prior: %+v", z)
	}
	big := one
	big.Peak, big.Demand = 8*gb, 6
	if z := SizeFor(big, 2, 12*gb, 8); z.Peak != 8*gb || z.Demand != 6 {
		t.Fatalf("a learned peak above the prior was lowered: %+v", z)
	}
	three := one
	three.Runs = resource.LearnRuns
	if z := SizeFor(three, 2, 12*gb, 8); z != three {
		t.Fatalf("three runs size with the learned peak: %+v", z)
	}
	if z := SizeFor(resource.JobSize{}, 2, 12*gb, 8); z != (resource.JobSize{}) {
		t.Fatalf("an unknown class stays unknown: %+v", z)
	}
	if z := SizeFor(one, 0, 12*gb, 8); z != one {
		t.Fatalf("without N a class sizes as learned: %+v", z)
	}
}

// JobLimit on its own: what it keeps for a class not learned yet, the valve,
// the raises and the stall, and the turns.
func TestJobLimit(t *testing.T) {
	const page = 4096
	pages := func(n uint64) string { return strconv.FormatUint(n/page*page, 10) }
	learned := resource.JobSize{Class: "c", Peak: 2 * gb, Demand: 1, Known: true, Runs: 5}
	base := resource.LimitAsk{N: 4, Budget: 12 * gb, Used: 4 * gb, Cores: 8, Page: page}

	a := base
	a.Size, a.First = learned, true
	got := JobLimit(a)
	if got.High != pages(uint64(2.5*gb)) || got.CPU != "200000 100000" || got.Why != "learned" || got.Keep != nil {
		t.Errorf("a learned class: %+v", got)
	}

	a = base
	a.First = true
	got = JobLimit(a)
	if got.Keep == nil || got.Why != "unknown" || got.High != pages(6*gb) || *got.Keep != got.Limit {
		t.Fatalf("an unknown class is sized once and kept: %+v", got)
	}
	kept := got.Keep
	a = base
	a.Used, a.Kept = 11*gb, kept
	if again := JobLimit(a); again.Limit != *kept || again.Keep != nil {
		t.Errorf("a later ask keeps the first size, though the job grew: %+v", again)
	}
	a.Lifted = true
	if l := JobLimit(a); l.High != "max" || l.CPU != "max" || l.Why != "swap-full" {
		t.Errorf("the swap valve lifts a kept limit: %+v", l)
	}

	a = base
	a.First, a.Used = true, 11*gb
	if l := JobLimit(a); l.High != pages(3*gb) || l.Why != "prior" {
		t.Errorf("a job/ of an unknown class is not limited below the prior (1/N): %+v", l)
	}
	a.Undeclared = true
	if l := JobLimit(a); l.High != "max" || l.Why != "unknown-frozen" {
		t.Errorf("undeclared work the guard moved gets no limit: %+v", l)
	}

	a = base
	a.Size, a.Raised = learned, 4*gb
	if l := JobLimit(a); l.High != pages(4*gb) || l.Why != "raised" || l.Raised != 4*gb {
		t.Errorf("a hit raises the limit: %+v", l)
	}
	a.Raised = 13 * gb
	if l := JobLimit(a); l.High != "max" || l.Why != "raised" {
		t.Errorf("a raise past the budget is no limit: %+v", l)
	}
	a.Raised, a.Stalled, a.Current = 0, true, 3*gb
	if l := JobLimit(a); l.High != "max" || l.Why != "own-stall" || l.Raised != uint64(3.75*gb) {
		t.Errorf("a job's own stall lifts its limit and keeps use×margin: %+v", l)
	}
	a = base
	a.Size, a.Turns = learned, true
	if l := JobLimit(a); l.High != "max" || l.Why != "turns" || l.CPU != "200000 100000" {
		t.Errorf("parts taking turns have no memory.high: %+v", l)
	}
}
