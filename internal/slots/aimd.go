package slots

// Decision 0029 slice S4 (#188): the budget adapts slowly to the host
// (AIMD, as TCP and Netflix's concurrency limits do): up by a step while
// jobs wait on a calm host and nothing was admitted for a while, down by a
// quarter when new jobs run slow for a minute or memory is short. Slow
// steps and holds, because the probe, PSI and the jobs themselves take time
// to show a change. Every threshold is a slowdown or a share, so the same
// rule holds on any host; without a provider the budget stays N.

import (
	"fmt"
	"time"
)

// The controller's settings.
const (
	AIMDEvery = 30 * time.Second // at most one step this often
	AIMDHold  = 60 * time.Second // the calm or the slowdown must last this long
	AIMDUp    = 0.25             // added a step
	AIMDDown  = 0.75             // the budget's share kept on a cut
	AIMDCalm  = 1.2              // a slowdown under this is calm
	AIMDSlow  = 3.0              // a slowdown over this, held AIMDHold, cuts
)

// AIMD is one host's budget controller; N is the configured slot count.
type AIMD struct {
	N float64

	b         float64
	last      time.Time // the last step
	slowSince time.Time
	why       string
}

// Step moves the budget at now from the host's signals: waiting is whether
// a job waits; lastGrant is the pool's last admission. It returns the
// budget and why it is what it is.
func (a *AIMD) Step(now time.Time, s Signals, waiting bool, lastGrant time.Time) (float64, string) {
	if a.b == 0 {
		a.b, a.last, a.why = a.N, now, fmt.Sprintf("N = %g", a.N)
		return a.b, a.why
	}
	if s.Provider == "" {
		a.b, a.why = a.N, "no measures on this host: the fixed count"
		return a.b, a.why
	}
	slow := s.Slowdown != nil && *s.Slowdown > AIMDSlow
	if !slow {
		a.slowSince = time.Time{}
	} else if a.slowSince.IsZero() {
		a.slowSince = now
	}
	if now.Sub(a.last) < AIMDEvery {
		return a.b, a.why
	}
	a.last = now
	short := (s.Headroom != nil && *s.Headroom < DefaultPolicy.MemoryReserve) || s.Stall == StallCritical
	calm := s.Slowdown != nil && *s.Slowdown < AIMDCalm && !short && s.Stall != StallElevated
	switch {
	case short:
		a.b = max(1, round(a.b*AIMDDown))
		a.why = "memory is short"
	case slow && now.Sub(a.slowSince) >= AIMDHold:
		a.b = max(1, round(a.b*AIMDDown))
		a.why = fmt.Sprintf("new jobs run %.1f× slower", *s.Slowdown)
	case calm && waiting && (lastGrant.IsZero() || now.Sub(lastGrant) >= AIMDHold):
		a.b = min(2*a.N, a.b+AIMDUp)
		a.why = "the host is calm and jobs wait"
	}
	return a.b, a.why
}

func round(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
