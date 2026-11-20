package engine

import (
	"log"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// Decision 0032 slice 2: when the undeclared work the guard freezes has several
// heavy parts, they take turns instead of all stopping: one runs, the others
// are frozen, and the turn moves after a slice long enough to pay for bringing
// the next part's memory back from swap.
const (
	// TurnMin is the shortest turn, TurnMax the longest. The parts belong to
	// one thread, so a long turn costs nobody else anything; decision 0032 has
	// the measurements (1 s never finished, and longer turns were faster).
	TurnMin = 30 * time.Second
	TurnMax = 2 * time.Minute
	// turnFactor is how many times the swap-in time a turn lasts.
	turnFactor = 3
	// TurnMinRSS: a part that holds less gets no turn while a heavier part
	// waits (V0.multi-freeze). On 19 Nov the turns of a frozen dev stack
	// went to a crash handler, a curl and a tail, 30 s each, among 44 parts.
	TurnMinRSS = 128 << 20
)

// turnTakers are the parts that get turns: those of TurnMinRSS or more, or
// all of them when none is.
func turnTakers(units []slots.WorkUnit) []slots.WorkUnit {
	var out []slots.WorkUnit
	for _, u := range units {
		if u.RSS >= TurnMinRSS {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return units
	}
	return out
}

// turnLength is a turn for work of rss bytes when the thread reads back rate
// bytes a second: turnFactor times the time to read it, within the limits. No
// measured rate means nothing was read back, so the shortest turn will do.
func turnLength(rss uint64, rate float64) time.Duration {
	if rate <= 0 {
		return TurnMin
	}
	d := time.Duration(turnFactor * float64(rss) / rate * float64(time.Second))
	return min(max(d, TurnMin), TurnMax)
}

// workUnits are the parts of thread's undeclared work, in a stable order.
func (e *Engine) workUnits(thread string) []slots.WorkUnit {
	if e.UndeclaredUnits == nil || e.UndeclaredRotate == nil {
		return nil
	}
	return e.UndeclaredUnits(thread)
}

// majorFaults is what thread has read back so far, in bytes.
func (e *Engine) majorFaults(thread string) uint64 {
	if e.UndeclaredFaults == nil {
		return 0
	}
	return e.UndeclaredFaults(thread)
}

// startTurns freezes all of thread's units but the first that takes turns; false when the work
// has fewer than two, or the cgroups refused, and is frozen whole instead.
func (e *Engine) startTurns(thread string) (slots.WorkUnit, int, bool) {
	units := e.workUnits(thread)
	if len(units) < 2 {
		return slots.WorkUnit{}, 0, false
	}
	first := turnTakers(units)[0]
	if !e.UndeclaredRotate(thread, units, first.PID) {
		return slots.WorkUnit{}, 0, false
	}
	return first, len(units), true
}

// nextTurn runs at each tick of a freeze that takes turns. Every tick it
// applies the turn again, so a part started since is frozen too; when the turn
// is over, or its part ended, the next part in pid order gets one, as long as
// the swap-in rate measured during this turn says. It runs outside e.mu.
func (e *Engine) nextTurn(f frozenJob, now time.Time) frozenJob {
	if f.active == 0 {
		return f
	}
	units := e.workUnits(f.owner)
	if len(units) == 0 {
		return f
	}
	at := -1
	for i, u := range units {
		if u.PID == f.active {
			at = i
		}
	}
	over := now.Sub(f.turnAt) >= f.turn
	if at >= 0 && !over {
		e.UndeclaredRotate(f.owner, units, f.active)
		return f
	}
	takers := turnTakers(units)
	next := takers[0]
	for _, u := range takers {
		if u.PID > f.active {
			next = u
			break
		}
	}
	faults := e.majorFaults(f.owner)
	rate := 0.0
	if s := now.Sub(f.turnAt).Seconds(); s > 0 && faults > f.faults {
		rate = float64(faults-f.faults) / s
	}
	turn := turnLength(next.RSS, rate)
	if !e.UndeclaredRotate(f.owner, units, next.PID) && len(units) > 1 {
		e.guardOnce(now, "turn:"+f.owner, "could not move the turn in thread="+f.owner+": the cgroup refused")
		return f
	}
	// V0.sim-fixes (4): each frozen part's memory at its freeze, for the wake
	// rule; a part that ended is gone from it.
	parts := map[int]uint64{}
	for _, u := range units {
		switch {
		case u.PID == next.PID:
		case u.PID == f.active:
			parts[u.PID] = u.RSS
		default:
			parts[u.PID] = max(f.parts[u.PID], u.RSS)
		}
	}
	f.parts = parts
	if next.PID != f.active {
		log.Printf("guard: turn thread=%s pid=%d cmd=%q mem=%s turn=%s units=%d read_back=%s/s", f.owner, next.PID, next.Command,
			slots.GB(float64(next.RSS)), turn.Round(time.Second), len(units), slots.GB(rate))
	}
	f.active, f.turnAt, f.turn, f.faults = next.PID, now, turn, faults
	return f
}
