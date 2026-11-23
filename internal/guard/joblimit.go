package guard

// V0.job-limits (decision 0036 (C)), V0.guard-finish: a job sizes itself, and
// the policy says how. A job moved into job/ or undeclared/ gets a
// memory.high from its class's learned peak and a cpu.max from its learned
// demand; internal/cgroup reads the inputs and writes what JobLimit decides.
// There is no memory.max, so the limit never kills. memory.high with no swap
// room livelocks, so every job limit lifts while the agents' swap is nearly
// full (the valve of decision 0032 slice 3).

import (
	"fmt"
	"math"
	"strconv"

	"github.com/meros/fyr/internal/guard/resource"
)

// cpuPeriod is the cpu.max period of a job's limit.
const cpuPeriod = 100000

// SizeFor is z as a job is sized from it: under resource.LearnRuns runs a
// class's peak and demand are never below the prior's (1/n of budget and
// cores), as admission counts them (resource.NeedOf; decision 0029 section 2).
// V0.admission-memory: a first run its memory.high held back taught a low
// peak, and the next limit, sized from it, held the next run lower still.
// Without n (the host's slots setting) z is as learned.
func SizeFor(z resource.JobSize, n int, budget uint64, cores float64) resource.JobSize {
	if !z.Known || z.Runs >= resource.LearnRuns || n <= 0 {
		return z
	}
	p := resource.Prior(resource.Signals{CPU: cores, Memory: budget}, n)
	z.Peak, z.Demand = max(z.Peak, p.Mem), max(z.Demand, p.CPU)
	return z
}

// JobLimits are the memory.high and cpu.max for a job of size z: why says
// where they come from. budget and used are the agents' memory budget and
// use now; cores the agents' CPU; lifted is the swap valve; page the page size.
func JobLimits(z resource.JobSize, budget, used uint64, cores float64, lifted bool, page uint64) (high, cpu, why string) {
	if lifted {
		return "max", "max", "swap-full"
	}
	var mem uint64
	if z.Known && z.Peak > 0 {
		mem, why = uint64(resource.HoldMargin*float64(z.Peak)), "learned"
	} else if budget > 0 {
		free := uint64(0)
		if budget > used {
			free = budget - used
		}
		mem, why = min(free, uint64(resource.UnknownShare*float64(budget))), "unknown"
	}
	high = "max"
	if mem > 0 && (budget == 0 || mem < budget) {
		high = strconv.FormatUint(max(mem/page*page, page), 10)
	}
	cpu = "max"
	if z.Known && z.Demand > 0 {
		// The margin of the memory limit, so a job its cap held back can show
		// more demand next run: a cap at the demand itself would only ratchet down.
		c := max(1, math.Ceil(resource.HoldMargin*z.Demand))
		if cores <= 0 || c < cores {
			cpu = fmt.Sprintf("%d %d", int(c)*cpuPeriod, cpuPeriod)
		}
	}
	if why == "" {
		why = "no-budget"
	}
	return high, cpu, why
}

// unknownHigh is the memory.high kept for a job of a class not learned yet,
// given high, the agents' free memory at the move (JobLimits). V0.freeze-charge:
// on 19 Nov at 20:48 the guard moved work into undeclared/ while 73 MB were
// free, and that became its memory.high: after the thaw every allocation was
// throttled. Undeclared work is moved only by the guard, to freeze it, so
// always when memory is short and the free memory least: it gets no limit
// (the freeze bounds it, and after the thaw the guard's next tick does). Its
// pages stay charged to cli/ besides,
// so a limit there would count only what it allocates after the thaw. A job
// moved into job/ is never limited below the prior, 1/N of the agents, as
// admission counts it (decision 0029 section 2).
func unknownHigh(high string, n int, budget uint64, cores float64, undeclared bool, page uint64) (string, string) {
	if undeclared {
		return "max", "unknown-frozen"
	}
	if n <= 0 || budget == 0 || high == "max" {
		return high, "unknown"
	}
	p := resource.Prior(resource.Signals{CPU: cores, Memory: budget}, n).Mem
	if v, err := strconv.ParseUint(high, 10, 64); err == nil && v >= p {
		return high, "unknown"
	}
	if p >= budget {
		return "max", "prior"
	}
	return strconv.FormatUint(max(p/page*page, page), 10), "prior"
}

// grown is memory.high as the job's hits and its own stall leave it
// (V0.sim-fixes (2)). The load simulation (V0.load-sim) ran a class learned at
// 1.5 GB whose jobs grew to 7 GB: one ×1.2 on the class left each job a third
// of its memory, about 30× slower, holding the agents' stall critical.
//   - A hit raises the limit to the job's use × HoldMargin (the mechanism
//     keeps it in Raised), again at each hit; at or past the agents' budget
//     there is no limit.
//   - While the job's own memory full stall is critical, the limit lifts; its
//     use × HoldMargin is kept as the least it comes back at.
func grown(a resource.LimitAsk, high, why string, raised uint64) (string, string, uint64) {
	if a.Stalled {
		return "max", "own-stall", max(raised, uint64(resource.HoldMargin*float64(a.Current)))
	}
	v, err := strconv.ParseUint(high, 10, 64)
	if err != nil || raised <= v {
		return high, why, raised
	}
	if a.Budget > 0 && raised >= a.Budget {
		return "max", "raised", raised
	}
	return strconv.FormatUint(max(raised/a.Page*a.Page, a.Page), 10), "raised", raised
}

// JobLimit decides one job's limits on a tick (V0.job-limits). A class not
// learned yet is sized once, at the job's first ask, from the agents' free
// memory (V0.joblimit-slice: used counts the job itself, so sizing it again
// each tick would shrink its limit as it grows), and the answer's Keep is
// what later asks pass back as Kept; only the swap valve changes it after.
// The job's hits and stalls raise it (grown), and a job whose parts take
// turns has no memory.high (V0.guard-tidy).
func JobLimit(a resource.LimitAsk) resource.LimitAnswer {
	if a.Page == 0 {
		a.Page = 4096
	}
	z := SizeFor(a.Size, a.N, a.Budget, a.Cores) // V0.admission-memory: the 3-run prior
	var out resource.LimitAnswer
	kept := a.Kept
	if a.First {
		if h, c, w := JobLimits(z, a.Budget, a.Used, a.Cores, false, a.Page); w != "learned" {
			if w == "unknown" {
				h, w = unknownHigh(h, a.N, a.Budget, a.Cores, a.Undeclared, a.Page)
			}
			kept = &resource.Limit{High: h, CPU: c, Why: w}
			out.Keep = kept
		}
	}
	var l resource.Limit
	if kept != nil && !a.Lifted {
		l = *kept
	} else {
		l.High, l.CPU, l.Why = JobLimits(z, a.Budget, a.Used, a.Cores, a.Lifted, a.Page)
	}
	out.Raised = a.Raised
	if !a.Lifted && l.High != "max" {
		l.High, l.Why, out.Raised = grown(a, l.High, l.Why, a.Raised)
	}
	if a.Turns && l.High != "max" {
		l.High, l.Why = "max", "turns" // V0.guard-tidy (liftForTurns)
	}
	out.Limit = l
	return out
}
