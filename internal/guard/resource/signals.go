// Package resource is the resource guard's vocabulary (V0.guard-mechanism):
// the host's signals, a job's need, the pool's holders and the guard's
// published view. The mechanism (internal/cgroup, the process table) and the
// policy (slots, admission, the engine's guard) both speak it, so the
// mechanism no longer imports the policy. It holds no decision: the rules that read these types
// stay with the policy; only how a class's need is counted (NeedOf, Prior,
// LearnRuns) lives here, because admission and the job limit must count it
// alike (decision 0029 section 2).
package resource

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
	// Triggered: the PSI trigger on the agents' memory.pressure fired
	// within TriggerFresh, so AgentsStall reads critical whatever avg10 says
	// (V0.freeze-fast-class-name).
	Triggered bool
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
	// UnitStall is the full memory stall (avg10, percent) on the unit
	// systemd-oomd watches, nil when unread; OomdLimit is oomd's limit on it
	// in percent, 0 for none or unknown (V0.oomd-guard). V0.guard-agents-stall:
	// near the limit the guard does not wait to freeze more.
	UnitStall *float64
	OomdLimit float64
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

// Critical says the host's memory stall or the agents' own is critical, and
// why. V0.guard-agents-stall: the agents' stall is what systemd-oomd compares
// against its limit (decision 0032: the agents at 53 % while the desktop read
// 0.1 %), so it counts on its own; the host's stays a trigger too
// (V0.load-tuning).
func (s Signals) Critical() (bool, string) {
	switch {
	case s.Stall == StallCritical:
		return true, "memory stalls (critical)"
	case s.AgentsStall == StallCritical:
		return true, "the agents' memory stalls (critical)"
	}
	return false, ""
}

// TriggerFresh is how long a firing of the PSI trigger (10 % full stall in
// 2 s, the critical level) counts as the agents' critical stall, whatever
// avg10 says: avg10 lags it by tens of seconds (V0.freeze-fast-class-name; on
// 20 Nov the trigger fired at 07:32:12 and avg10 read critical at 07:33:01).
const TriggerFresh = 20 * time.Second

// Fresh applies a trigger that fired at at: within TriggerFresh of now the
// agents' stall is critical. A zero at is no trigger.
func (s Signals) Fresh(at, now time.Time) Signals {
	if !at.IsZero() && !now.Before(at) && now.Sub(at) < TriggerFresh {
		s.Triggered, s.AgentsStall = true, StallCritical
	}
	return s
}

// OomdNearMargin is how close (percentage points) the unit's stall may come
// to oomd's limit before the guard stops waiting between freezes
// (V0.guard-agents-stall); the same as cgroup.OomdNearMargin, at which it logs.
const OomdNearMargin = 10.0

// NearOomd says the unit's stall is within OomdNearMargin of oomd's limit;
// false while either is unknown.
func (s Signals) NearOomd() bool {
	return s.OomdLimit > 0 && s.UnitStall != nil && *s.UnitStall >= s.OomdLimit-OomdNearMargin
}

// OomdWhy says the unit's stall is near oomd's limit, "" when it is not.
func (s Signals) OomdWhy() string {
	if !s.NearOomd() {
		return ""
	}
	return fmt.Sprintf("memory stalls near oomd's limit (%.0f %% of %.0f %%)", *s.UnitStall, s.OomdLimit)
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
