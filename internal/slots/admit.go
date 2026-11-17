package slots

// Decision 0029 slice S2 (#186): admission by a budget of costs instead of
// a count. The pool's queue and order stay (V0.86, V0.137); only "may the
// head start now" changes. Every threshold is a share or a slowdown, so the
// same rules hold on any host; a host without a provider keeps the fixed
// count.

import (
	"fmt"
	"time"
)

// Signals are the host's measures the rules read (V0.142).
type Signals struct {
	// Provider is "" without one: then the fixed count holds.
	Provider string
	// Slowdown is s, how much slower a new job runs now; nil without a
	// fresh probe.
	Slowdown *float64
	// Headroom is the share of the agents' memory left; nil when unknown.
	Headroom *float64
	// Stall is the memory stall level; "" when unknown.
	Stall Stall
	// CPU and Memory are the agents' capacity (V0.142); 0 when unknown.
	CPU    float64
	Memory uint64
}

// Stall is a memory stall level (V0.142).
type Stall string

// The levels.
const (
	StallOK       Stall = "ok"
	StallElevated Stall = "elevated"
	StallCritical Stall = "critical"
)

// Policy is the host's thresholds.
type Policy struct {
	// SlowdownMax: at or above it a job costing more than CheapCost waits.
	SlowdownMax float64
	// MemoryReserve: the share of the agents' memory that must stay free.
	MemoryReserve float64
}

// DefaultPolicy is decision 0029's: 1.5× and 10 %.
var DefaultPolicy = Policy{SlowdownMax: 1.5, MemoryReserve: 0.10}

// CheapCost is the cost at or under which a job ignores the slowdown.
const CheapCost = 0.25

// IdleCost is the cost of a held slot that runs no job: no heavy command
// and no busy reading for IdleAfter.
const IdleCost = 0.1

// IdleAfter is how long a hold must be idle to cost IdleCost.
const IdleAfter = 3 * time.Minute

// Decide says whether a job of cost head may start beside held (their
// costs) under budget, and why not. In order: without a provider the fixed
// count; memory short or stalling holds every job; with nothing held the
// head starts (one job always runs); the budget; the slowdown.
func Decide(p Policy, s Signals, budget float64, held []float64, head float64) (bool, string) {
	if s.Provider == "" {
		if float64(len(held)) < budget {
			return true, ""
		}
		return false, fmt.Sprintf("all %d slots in use", int(budget))
	}
	if s.Headroom != nil && *s.Headroom < p.MemoryReserve {
		return false, fmt.Sprintf("memory: %.0f %% free", *s.Headroom*100)
	}
	// Only a critical stall holds: reclaiming page cache at memory.high
	// alone keeps PSI full near 5 % (elevated) with plenty of memory free.
	if s.Stall == StallCritical {
		return false, fmt.Sprintf("memory stalls (%s)", s.Stall)
	}
	if len(held) == 0 {
		return true, ""
	}
	used := 0.0
	for _, c := range held {
		used += c
	}
	if used+head > budget+1e-9 {
		return false, fmt.Sprintf("%.1f of %.1f in use", used, budget)
	}
	if head > CheapCost && s.Slowdown != nil && *s.Slowdown >= p.SlowdownMax {
		return false, fmt.Sprintf("CPU: new jobs run %.1f× slower", *s.Slowdown)
	}
	return true, ""
}

// Decision 0029 S3 (#187): learned costs.

// MinCost and MaxCost bound a learned cost; LearnRuns is how many runs a
// class needs before its cost may fall under the prior 1.0.
const (
	MinCost   = 0.1
	MaxCost   = 2.0
	LearnRuns = 3
)

// CostOf is a job's cost from its raw measures (demand in cores, peak in
// bytes): the larger of its shares of the host's capacity over n, bounded
// MinCost–MaxCost; under LearnRuns runs never below 1.0. Without a capacity
// it is the prior 1.0.
func CostOf(demand float64, peak uint64, runs int, cpu float64, mem uint64, n float64) float64 {
	if cpu <= 0 || mem == 0 || n <= 0 {
		return 1
	}
	c := max(demand/(cpu/n), float64(peak)/(float64(mem)/n))
	c = min(MaxCost, max(MinCost, c))
	if runs < LearnRuns {
		c = max(c, 1)
	}
	return float64(int(c*100+0.5)) / 100
}

// The backfill cap (EASY): a head is passed at most BackfillPasses times,
// and not at all once it has waited BackfillFor.
const (
	BackfillPasses = 3
	BackfillFor    = 10 * time.Minute
)

// EASY says whether a job that already fits may pass the queue's head
// (decision 0029 S3): a cheap one always; else only when its p90 wall time
// (seconds) ends by the earliest expected end of the running jobs, so the
// head starts no later. A zero end is a holder without an estimate: no
// promise can be kept.
func EASY(now time.Time, cost, wall float64, ends []time.Time) bool {
	if cost <= CheapCost {
		return true
	}
	if wall <= 0 || len(ends) == 0 {
		return false
	}
	earliest := ends[0]
	for _, e := range ends {
		if e.IsZero() {
			return false
		}
		if e.Before(earliest) {
			earliest = e
		}
	}
	return !now.Add(time.Duration(wall * float64(time.Second))).After(earliest)
}
