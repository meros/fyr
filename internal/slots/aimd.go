package slots

// Decision 0029 slice S4 (#188), V0.163 per resource: the budget adapts
// slowly to the host (AIMD, as TCP and Netflix's concurrency limits do).
// Memory pressure (headroom under the reserve, or a critical stall of the
// agents themselves; V0.load-tuning: a stall of the rest of the host holds new
// jobs in Decide but says nothing about what they may use) cuts the memory budget; new jobs running slow for a minute cut the CPU budget;
// while a job waits on a calm host and nothing was admitted for a while, the
// resource that held it back grows. Slow steps and holds, because the probe,
// PSI and the jobs take time to show a change. Without a provider the
// budget stays full (the fixed count holds).

import (
	"fmt"
	"time"
)

// The controller's settings.
const (
	AIMDEvery = 30 * time.Second // at most one step this often
	AIMDHold  = 60 * time.Second // the calm or the slowdown must last this long
	AIMDUp    = 0.125            // added to a factor a step (12.5 % of the host)
	AIMDDown  = 0.75             // a factor's share kept on a cut
	AIMDCalm  = 1.2              // a slowdown under this is calm
	AIMDSlow  = 3.0              // a slowdown over this, held AIMDHold, cuts CPU
	AIMDMax   = 2.0              // a factor's ceiling
)

// AIMD is one host's budget controller; N is the host's `slots` setting:
// a factor never falls under 1/N (one prior job's share).
type AIMD struct {
	N float64

	f         Factors
	started   bool
	last      time.Time
	slowSince time.Time
	why       string
}

// Step moves the budget at now: waiting is whether a job waits, lastGrant
// the pool's last admission, binding the resource that holds the head back
// (ResCPU, ResMem or ""). It returns the factors and why.
func (a *AIMD) Step(now time.Time, s Signals, waiting bool, lastGrant time.Time, binding string) (Factors, string) {
	if !a.started {
		a.started, a.f, a.last, a.why = true, Full, now, "the full budget"
		return a.f, a.why
	}
	if s.Provider == "" {
		a.f, a.why = Full, "no measures on this host: the fixed count"
		return a.f, a.why
	}
	slow := s.Slowdown != nil && *s.Slowdown > AIMDSlow
	if !slow {
		a.slowSince = time.Time{}
	} else if a.slowSince.IsZero() {
		a.slowSince = now
	}
	if now.Sub(a.last) < AIMDEvery {
		return a.f, a.why
	}
	a.last = now
	floor := 1.0
	if a.N > 0 {
		floor = 1 / a.N
	}
	own := s.AgentsStall
	if own == "" {
		own = s.Stall
	}
	short := (s.Headroom != nil && *s.Headroom < DefaultPolicy.MemoryReserve) || own == StallCritical
	calm := s.Slowdown != nil && *s.Slowdown < AIMDCalm && !short && own != StallElevated &&
		s.Stall != StallElevated && s.Stall != StallCritical
	switch {
	case short:
		a.f.Mem = max(floor, round(a.f.Mem*AIMDDown))
		a.why = "memory is short: the memory budget shrinks"
	case slow && now.Sub(a.slowSince) >= AIMDHold:
		a.f.CPU = max(floor, round(a.f.CPU*AIMDDown))
		a.why = fmt.Sprintf("new jobs run %.1f× slower: the CPU budget shrinks", *s.Slowdown)
	case calm && waiting && (lastGrant.IsZero() || now.Sub(lastGrant) >= AIMDHold):
		if binding == ResMem {
			a.f.Mem = min(AIMDMax, a.f.Mem+AIMDUp)
			a.why = "the host is calm and a job waits for memory: the memory budget grows"
		} else {
			a.f.CPU = min(AIMDMax, a.f.CPU+AIMDUp)
			a.why = "the host is calm and a job waits for CPU: the CPU budget grows"
		}
	}
	return a.f, a.why
}

func round(v float64) float64 { return float64(int(v*1000+0.5)) / 1000 }
