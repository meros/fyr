package slots

// Decision 0029 slice S2 (#186): admission by a budget of costs instead of
// a count. The pool's queue and order stay (V0.86, V0.137); only "may the
// head start now" changes. Every threshold is a share or a slowdown, so the
// same rules hold on any host; a host without a provider keeps the fixed
// count.

import (
	"fmt"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// CheapShare: a job whose shares are both at or under it ignores the
// slowdown and may always backfill (decision 0029's 0.25 of 2 slots).
const CheapShare = 0.125

// IdleShare is the CPU an idle hold counts (no heavy command and no busy
// reading for IdleAfter); its memory is what it holds now.
const IdleShare = 0.05

// IdleAfter is how long a hold must be idle to count as idle.
const IdleAfter = 3 * time.Minute

// Resources a wait can name.
const (
	ResCPU = "cpu"
	ResMem = "memory"
	// ResHeadroom: memory, held back by the host's real headroom (memory
	// outside the budget: the owner's browser, the running jobs' growth),
	// not by the memory budget. V0.admission-memory: the AIMD controller
	// grows the memory budget only for ResMem, since a larger budget does
	// not make the host's free memory any larger; the snapshot sends it as
	// ResMem, so the front reads the same two values.
	ResHeadroom = "headroom"
)

// Wire is binding as the snapshot sends it: ResHeadroom is memory.
func Wire(binding string) string {
	if binding == ResHeadroom {
		return ResMem
	}
	return binding
}

// Decide says whether head may start beside held, why not, and which
// resource binds (V0.163). In order: without a provider (or capacity) the
// fixed count n; memory short holds every job but a cheap one (V0.sim-fixes)
// and a critical stall every job; with
// nothing held the head starts (one job always runs); its CPU must fit the
// free CPU budget and its memory the free memory (the memory budget less
// the held jobs' needs, and no more than the real headroom above the
// reserve); a job that is not cheap waits while new jobs run slow.
func Decide(p resource.Policy, s resource.Signals, f resource.Factors, n int, held []resource.Need, head resource.Need) (ok bool, why, binding string) {
	return DecideWith(p, s, f, n, held, nil, "", head)
}

// request stands in for. The wait reason names the largest when it is what
// binds: "needs ≈ 30 % CPU, 10 % free (Map perf uses 50 % undeclared)".
func DecideWith(p resource.Policy, s resource.Signals, f resource.Factors, n int, held []resource.Need, extra []resource.Extra, headOwner string, head resource.Need) (ok bool, why, binding string) {
	if s.Provider == "" || s.CPU <= 0 || s.Memory == 0 {
		if len(held) < n {
			return true, "", ""
		}
		return false, fmt.Sprintf("%d of %d heavy jobs run (this host counts jobs: no load measures)", len(held), n), ""
	}
	// V0.sim-fixes (2): while memory is short a cheap job still backfills
	// unless the stall is critical or near oomd's limit (below), or the swap
	// is full (V0.guard-swap: then what reads as free is the code the agents
	// run): decision 0029 section 4, a job of CheapShare or less barely
	// touches the head's room. The load simulation (V0.load-sim) had a 0.5 GB
	// job wait 239 min behind one job larger than the budget.
	backfill := false
	if s.Headroom != nil && *s.Headroom < s.Reserve(p) {
		if !Cheap(head, s) || s.SwapFull {
			return false, s.MemoryWhy(), ResHeadroom
		}
		backfill = true
	}
	if crit, why := s.Critical(); crit {
		return false, why, ResMem
	}
	if why := s.OomdWhy(); why != "" { // V0.guard-agents-stall: as the guard
		return false, why, ResMem
	}
	if len(held) == 0 {
		return true, "", ""
	}
	var used resource.Need
	for _, h := range held {
		used = used.Plus(h)
	}
	var others []resource.Extra
	for _, x := range extra {
		if x.Owner != headOwner || headOwner == "" {
			others = append(others, x)
			used = used.Plus(x.Need)
		}
	}
	cpuFree := f.CPU*s.CPU - used.CPU
	if head.CPU > cpuFree+1e-9 {
		return false, fmt.Sprintf("needs ≈ %.0f %% CPU, %.0f %% free", resource.Pct(head.CPU/s.CPU), max(0, resource.Pct(cpuFree/s.CPU))) + undeclaredWhy(others, s, ResCPU), ResCPU
	}
	// V0.admission-memory: the budget binds (ResMem) or the real headroom
	// does (ResHeadroom), less what the running jobs may still grow.
	memFree, memBind := f.Mem*float64(s.Memory)-float64(used.Mem), ResMem
	if s.Headroom != nil {
		if room := (*s.Headroom-s.Reserve(p))*float64(s.Memory) - float64(s.Growth); room < memFree {
			memFree, memBind = room, ResHeadroom
		}
	}
	if float64(head.Mem) > memFree+1 && !backfill {
		grow := ""
		if memBind == ResHeadroom && s.Growth > 0 {
			grow = fmt.Sprintf(" (the running jobs may still grow %s)", resource.GB(float64(s.Growth)))
		}
		return false, fmt.Sprintf("needs ≈ %s memory, %s free", resource.GB(float64(head.Mem)), resource.GB(max(0, memFree))) + grow + undeclaredWhy(others, s, ResMem), memBind
	}
	c, m := head.Shares(s)
	if (c > CheapShare || m > CheapShare) && s.Slowdown != nil && *s.Slowdown >= p.SlowdownMax {
		return false, fmt.Sprintf("CPU: new jobs run %.1f× slower", *s.Slowdown), ResCPU
	}
	return true, "", ""
}

// undeclaredWhy names the largest undeclared use in res, with "+N" for the
// others, or "" when none uses any of it.
func undeclaredWhy(others []resource.Extra, s resource.Signals, res string) string {
	var big *resource.Extra
	var bigShare float64
	k := 0
	for i := range others {
		c, m := others[i].Need.Shares(s)
		sh := c
		if res == ResMem {
			sh = m
		}
		if sh <= 0 {
			continue
		}
		k++
		if sh > bigShare {
			big, bigShare = &others[i], sh
		}
	}
	if big == nil {
		return ""
	}
	use := fmt.Sprintf("%.0f %%", resource.Pct(bigShare))
	if res == ResMem {
		use = resource.GB(float64(big.Need.Mem))
	}
	more := ""
	if k > 1 {
		more = fmt.Sprintf(" +%d", k-1)
	}
	if big.Service {
		return fmt.Sprintf(" (%s's services hold %s%s)", big.Name, use, more)
	}
	return fmt.Sprintf(" (%s uses %s undeclared%s)", big.Name, use, more)
}

// Cheap says whether a need ignores the slowdown and may always backfill.
func Cheap(a resource.Need, s resource.Signals) bool {
	c, m := a.Shares(s)
	return c <= CheapShare && m <= CheapShare
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
func EASY(now time.Time, cheap bool, wall float64, ends []time.Time) bool {
	if cheap {
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

// ThrashFactor: the slowdown, as a multiple of the policy's SlowdownMax,
// at which the host thrashes (V0.153); admission holds new jobs well before.
const ThrashFactor = 2.0

// Thrashing says whether the host is under so much pressure that a slow
// agent CLI is slow because of it, and why (V0.153): memory stalls at the
// critical level, or new jobs run ThrashFactor × SlowdownMax slower.
func Thrashing(s resource.Signals, p resource.Policy) (bool, string) {
	if s.Stall == resource.StallCritical {
		return true, "memory pressure"
	}
	if s.Slowdown != nil && p.SlowdownMax > 0 && *s.Slowdown >= ThrashFactor*p.SlowdownMax {
		return true, fmt.Sprintf("CPU pressure: new jobs run %.1f× slower", *s.Slowdown)
	}
	return false, ""
}
