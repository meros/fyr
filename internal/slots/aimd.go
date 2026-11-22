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
	"encoding/json"
	"fmt"
	"github.com/meros/fyr/internal/fsutil"
	"log"
	"os"
	"strings"
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
	// V0.budget-restart: a kept budget younger than this is taken back on
	// start; it is written on each move and at least this often.
	AIMDKeep  = 30 * time.Minute
	AIMDWrite = 5 * time.Minute
)

// AIMD is one host's budget controller; N is the host's `slots` setting:
// a factor never falls under 1/N (one prior job's share).
type AIMD struct {
	N float64
	// Path keeps the budget across a restart of fyrd (V0.budget-restart);
	// empty keeps it in memory only.
	Path string
	// Policy is the host's thresholds, as admission reads them (V0.admission-tidy);
	// zero is DefaultPolicy.
	Policy Policy

	f         Factors
	started   bool
	last      time.Time
	slowSince time.Time
	why       string
	written   time.Time
	wrote     Factors // the budget Path holds
}

// keptPrefix starts the reason of a budget read back after a restart.
const keptPrefix = "kept across a restart: "

// trimKept drops every keptPrefix a reason already carries: a file an older
// fyrd wrote may hold several.
func trimKept(why string) string {
	for strings.HasPrefix(why, keptPrefix) {
		why = strings.TrimPrefix(why, keptPrefix)
	}
	return why
}

// kept is the budget as Path holds it.
type kept struct {
	Factors
	Why string    `json:"why"`
	At  time.Time `json:"at"`
}

// Step moves the budget at now: waiting is whether a job waits, lastGrant
// the pool's last admission, binding the resource that holds the head back
// (ResCPU, ResMem, ResHeadroom or ""; Snapshot.Bound). It returns the factors and why.
func (a *AIMD) Step(now time.Time, s Signals, waiting bool, lastGrant time.Time, binding string) (Factors, string) {
	if !a.started {
		a.started, a.f, a.last, a.why = true, Full, now, "the full budget"
		if k, ok := a.load(now); ok {
			// V0.restart-why: what an earlier restart kept says so once.
			a.f, a.why, a.written, a.wrote = k.Factors, keptPrefix+trimKept(k.Why), k.At, k.Factors
		}
		return a.f, a.why
	}
	defer a.save(now)
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
	pol := a.Policy
	if pol == (Policy{}) {
		pol = DefaultPolicy
	}
	short := (s.Headroom != nil && *s.Headroom < s.Reserve(pol)) || own == StallCritical
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
		switch binding {
		case ResHeadroom:
			// V0.admission-memory: memory outside the budget holds the head
			// back; a larger budget would not admit it, only admit more once
			// that memory is freed (up to AIMDMax of the host).
		case ResMem:
			a.f.Mem = min(AIMDMax, a.f.Mem+AIMDUp)
			a.why = "the host is calm and a job waits for memory: the memory budget grows"
		default:
			a.f.CPU = min(AIMDMax, a.f.CPU+AIMDUp)
			a.why = "the host is calm and a job waits for CPU: the CPU budget grows"
		}
	}
	return a.f, a.why
}

func round(v float64) float64 { return float64(int(v*1000+0.5)) / 1000 }

// V0.budget-restart: before, every start of fyrd (each deploy) began at the
// full budget, admitted two jobs into a host the budget had learned was full,
// and the memory guard froze one within a minute (19 Nov: 09:53, 11:04,
// 12:34). The budget is now kept in Path and taken back when it is younger
// than AIMDKeep, bounded to 1/N–AIMDMax.
func (a *AIMD) load(now time.Time) (kept, bool) {
	if a.Path == "" {
		return kept{}, false
	}
	b, err := os.ReadFile(a.Path)
	if err != nil {
		return kept{}, false
	}
	var k kept
	if json.Unmarshal(b, &k) != nil || k.CPU <= 0 || k.Mem <= 0 {
		log.Printf("slots: ignoring an unreadable %s", a.Path)
		return kept{}, false
	}
	if age := now.Sub(k.At); age < 0 || age > AIMDKeep {
		return kept{}, false
	}
	floor := 1.0
	if a.N > 0 {
		floor = 1 / a.N
	}
	k.CPU = min(AIMDMax, max(floor, k.CPU))
	k.Mem = min(AIMDMax, max(floor, k.Mem))
	return k, true
}

// save writes the budget to Path when it moved or the last write is
// AIMDWrite old, so a kept budget's age says when it was last true.
func (a *AIMD) save(now time.Time) {
	if a.Path == "" {
		return
	}
	if a.wrote == a.f && now.Sub(a.written) < AIMDWrite {
		return
	}
	b, err := json.Marshal(kept{Factors: a.f, Why: a.why, At: now})
	if err != nil {
		return
	}
	if err := fsutil.WriteAtomic(a.Path, b, 0o600); err != nil {
		log.Printf("slots: cannot keep the budget: %v", err)
		return
	}
	a.written, a.wrote = now, a.f
}
