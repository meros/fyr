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
	// AgentsStall is the level of the agents' own memory stall; "" where it
	// cannot be read apart from the host's (V0.load-tuning).
	AgentsStall Stall
	// CPU and Memory are the agents' capacity (V0.142); 0 when unknown.
	CPU    float64
	Memory uint64
	// SwapFull: the agents' swap is at its cap (V0.guard-swap), so the
	// kernel can reclaim only their page cache.
	SwapFull bool
	// AgentsMem is what the agents hold now (agents/memory.current, or the
	// sum of the process trees); 0 when unknown. V0.guard-wake: the guard
	// wakes a frozen job when it fits under the cap learned from it.
	AgentsMem uint64
	// Growth is what the admitted jobs may still grow: the sum over the
	// holders of max(0, predicted peak − what they grew since their grant).
	// Admission fills it, not the host (V0.admission-memory): decision 0029
	// section 3 rule 3 takes it off the headroom, so a job admitted at 3 % of
	// its 45 % peak does not leave the other 42 % to the next one.
	Growth uint64
}

// Reserve is the share of the agents' memory that must stay free: twice
// p's while their swap is full (V0.guard-swap). Then nothing they hold can
// move to swap and what reads as free is the code they run; on 19 Nov the
// agents re-read it at 2.4 GB/s with 12 % "free".
func (s Signals) Reserve(p Policy) float64 {
	if s.SwapFull {
		return 2 * p.MemoryReserve
	}
	return p.MemoryReserve
}

// MemoryWhy says why memory is short by its headroom: "memory: 12 % free",
// with "(swap full)" when the full swap raised the reserve.
func (s Signals) MemoryWhy() string {
	if s.Headroom == nil {
		return ""
	}
	why := fmt.Sprintf("memory: %.0f %% free", *s.Headroom*100)
	if s.SwapFull {
		why += " (swap full)"
	}
	return why
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
	// SlowdownMax: at or above it a job that is not Cheap waits.
	SlowdownMax float64
	// MemoryReserve: the share of the agents' memory that must stay free.
	MemoryReserve float64
}

// DefaultPolicy is decision 0029's: 1.5× and 10 %.
var DefaultPolicy = Policy{SlowdownMax: 1.5, MemoryReserve: 0.10}

// V0.163: a job needs two things of its host, CPU and memory, checked
// separately, so a CPU-heavy job runs next to a memory-heavy one. Every
// share is of what the host gives its agents (its capacity, V0.142 (1)).

// Need is a job's learned (or live) use: demand in cores, peak in bytes.
type Need struct {
	CPU float64 `json:"cpu"`
	Mem uint64  `json:"mem"`
}

// Plus is the sum of two needs.
func (a Need) Plus(b Need) Need { return Need{a.CPU + b.CPU, a.Mem + b.Mem} }

// Max is the larger of two needs, per resource.
func (a Need) Max(b Need) Need { return Need{max(a.CPU, b.CPU), max(a.Mem, b.Mem)} }

// Shares are a need's shares of the host's capacity (0–1, and more).
func (a Need) Shares(s Signals) (cpu, mem float64) {
	if s.CPU > 0 {
		cpu = a.CPU / s.CPU
	}
	if s.Memory > 0 {
		mem = float64(a.Mem) / float64(s.Memory)
	}
	return cpu, mem
}

// CheapShare: a job whose shares are both at or under it ignores the
// slowdown and may always backfill (decision 0029's 0.25 of 2 slots).
const CheapShare = 0.125

// IdleShare is the CPU an idle hold counts (no heavy command and no busy
// reading for IdleAfter); its memory is what it holds now.
const IdleShare = 0.05

// IdleAfter is how long a hold must be idle to count as idle.
const IdleAfter = 3 * time.Minute

// Prior is the need of a job Fyr has not learned: 1/n of the host in each
// resource (n is the host's `slots` setting, default 2: half the host).
func Prior(s Signals, n int) Need {
	if n <= 0 {
		n = 1
	}
	return Need{s.CPU / float64(n), uint64(float64(s.Memory) / float64(n))}
}

// Factors are the budget per resource as a share of the host's capacity
// (1 is all of it); the AIMD controller moves them (V0.154, V0.163).
type Factors struct {
	CPU float64 `json:"cpu"`
	Mem float64 `json:"mem"`
}

// Full is the budget before AIMD moves it.
var Full = Factors{1, 1}

// Pct is a share as a whole percentage.
func Pct(share float64) float64 { return float64(int(share*100 + 0.5)) }

// GB is bytes as "2.6 GB".
func GB(b float64) string { return fmt.Sprintf("%.1f GB", b/1e9) }

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
// fixed count n; memory short or critically stalling holds every job; with
// nothing held the head starts (one job always runs); its CPU must fit the
// free CPU budget and its memory the free memory (the memory budget less
// the held jobs' needs, and no more than the real headroom above the
// reserve); a job that is not cheap waits while new jobs run slow.
func Decide(p Policy, s Signals, f Factors, n int, held []Need, head Need) (ok bool, why, binding string) {
	return DecideWith(p, s, f, n, held, nil, "", head)
}

// Extra is the use of a thread that runs heavy work Fyr did not admit
// (V0.164): one more Need in the budget beside the admitted jobs, and not one
// of them: "one job always runs" looks at the admitted jobs only. A thread's
// own use does not count against its own request (Owner is the thread).
type Extra struct {
	Owner string
	Name  string // what the wait reason calls it: the thread's title
	Need  Need
	// Service: a running service's reservation (V0.services-not-jobs), not
	// undeclared work; the wait reason says so.
	Service bool
}

// DecideWith is Decide with the undeclared use of other threads counted
// (V0.164): added to what the held jobs use, except headOwner's own, which its
// request stands in for. The wait reason names the largest when it is what
// binds: "needs ≈ 30 % CPU, 10 % free (Map perf uses 50 % undeclared)".
func DecideWith(p Policy, s Signals, f Factors, n int, held []Need, extra []Extra, headOwner string, head Need) (ok bool, why, binding string) {
	if s.Provider == "" || s.CPU <= 0 || s.Memory == 0 {
		if len(held) < n {
			return true, "", ""
		}
		return false, fmt.Sprintf("%d of %d heavy jobs run (this host counts jobs: no load measures)", len(held), n), ""
	}
	if s.Headroom != nil && *s.Headroom < s.Reserve(p) {
		return false, s.MemoryWhy(), ResHeadroom
	}
	if s.Stall == StallCritical {
		return false, "memory stalls (critical)", ResMem
	}
	if len(held) == 0 {
		return true, "", ""
	}
	var used Need
	for _, h := range held {
		used = used.Plus(h)
	}
	var others []Extra
	for _, x := range extra {
		if x.Owner != headOwner || headOwner == "" {
			others = append(others, x)
			used = used.Plus(x.Need)
		}
	}
	cpuFree := f.CPU*s.CPU - used.CPU
	if head.CPU > cpuFree+1e-9 {
		return false, fmt.Sprintf("needs ≈ %.0f %% CPU, %.0f %% free", Pct(head.CPU/s.CPU), max(0, Pct(cpuFree/s.CPU))) + undeclaredWhy(others, s, ResCPU), ResCPU
	}
	// V0.admission-memory: the budget binds (ResMem) or the real headroom
	// does (ResHeadroom), less what the running jobs may still grow.
	memFree, memBind := f.Mem*float64(s.Memory)-float64(used.Mem), ResMem
	if s.Headroom != nil {
		if room := (*s.Headroom-s.Reserve(p))*float64(s.Memory) - float64(s.Growth); room < memFree {
			memFree, memBind = room, ResHeadroom
		}
	}
	if float64(head.Mem) > memFree+1 {
		grow := ""
		if memBind == ResHeadroom && s.Growth > 0 {
			grow = fmt.Sprintf(" (the running jobs may still grow %s)", GB(float64(s.Growth)))
		}
		return false, fmt.Sprintf("needs ≈ %s memory, %s free", GB(float64(head.Mem)), GB(max(0, memFree))) + grow + undeclaredWhy(others, s, ResMem), memBind
	}
	c, m := head.Shares(s)
	if (c > CheapShare || m > CheapShare) && s.Slowdown != nil && *s.Slowdown >= p.SlowdownMax {
		return false, fmt.Sprintf("CPU: new jobs run %.1f× slower", *s.Slowdown), ResCPU
	}
	return true, "", ""
}

// undeclaredWhy names the largest undeclared use in res, with "+N" for the
// others, or "" when none uses any of it.
func undeclaredWhy(others []Extra, s Signals, res string) string {
	var big *Extra
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
	use := fmt.Sprintf("%.0f %%", Pct(bigShare))
	if res == ResMem {
		use = GB(float64(big.Need.Mem))
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
func Cheap(a Need, s Signals) bool {
	c, m := a.Shares(s)
	return c <= CheapShare && m <= CheapShare
}

// LearnRuns is how many runs a class needs before its need may fall under
// the prior (decision 0029 S3).
const LearnRuns = 3

// MinShare bounds a learned need from below, per resource.
const MinShare = 0.02

// NeedOf is a class's need from its learned measures (demand in cores, peak
// in bytes): bounded to MinShare–1 of the host per resource, and under
// LearnRuns runs never below the prior (V0.163; was CostOf's one number).
func NeedOf(demand float64, peak uint64, runs int, s Signals, n int) Need {
	if s.CPU <= 0 || s.Memory == 0 {
		return Prior(s, n)
	}
	c := min(s.CPU, max(MinShare*s.CPU, demand))
	m := min(float64(s.Memory), max(MinShare*float64(s.Memory), float64(peak)))
	out := Need{c, uint64(m)}
	if runs < LearnRuns {
		out = out.Max(Prior(s, n))
	}
	return out
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
func Thrashing(s Signals, p Policy) (bool, string) {
	if s.Stall == StallCritical {
		return true, "memory pressure"
	}
	if s.Slowdown != nil && p.SlowdownMax > 0 && *s.Slowdown >= ThrashFactor*p.SlowdownMax {
		return true, fmt.Sprintf("CPU pressure: new jobs run %.1f× slower", *s.Slowdown)
	}
	return false, ""
}
