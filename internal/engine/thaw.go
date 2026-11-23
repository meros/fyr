// What may thaw, and how a frozen job is described: wakeable, roomToThaw, frozenParts, frozenFor and the words of the log.

package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// wakeable says whether frozen job f may wake now by Buck2's rule (V0.guard-wake,
// decision 0036 E3): the agents' memory plus f's memory at its freeze is under
// the cap; without a cap (the stall never went critical) the headroom rule
// already held. The text says why, for the log.
//
// V0.wake-once: resident is the frozen thread's memory now. Its pages stay
// charged to the agents while they are in RAM, so they come out of the
// agents' memory before the job's memory at the freeze is added: counted
// twice, a job still in RAM could never fit.
func wakeable(f frozenJob, sig slots.Signals, cap, resident uint64) (bool, string) {
	if cap == 0 {
		return true, "memory fine for 30 s"
	}
	if sig.AgentsMem == 0 {
		return true, "stall below elevated for 30 s (agents' memory unknown)"
	}
	others := sig.AgentsMem - min(resident, sig.AgentsMem)
	need := others + f.mem
	words := fmt.Sprintf("agents=%s + its %s %s cap=%s", slots.GB(float64(others)), slots.GB(float64(f.mem)), "%s", slots.GB(float64(cap)))
	if need < cap {
		return true, "fits: " + fmt.Sprintf(words, "<") + ", stall below elevated for 30 s"
	}
	return false, fmt.Sprintf(words, ">=")
}

// stalls says memory stalls at all (V0.sim-fixes (3)): the host's or the
// agents' stall is elevated or worse. A freeze needs short
// headroom and at least an elevated stall; admission still holds new jobs on
// the headroom alone.
func stalls(sig slots.Signals) bool {
	return sig.Stall == slots.StallElevated || sig.Stall == slots.StallCritical ||
		sig.AgentsStall == slots.StallElevated || sig.AgentsStall == slots.StallCritical
}

// frozenParts is, for work taking turns, the frozen parts' memory at their
// freeze and their memory now (V0.sim-fixes (4)). The thread's memory at the
// freeze (f.mem) counted the part whose turn it is, which runs and is in the
// agents' memory already, and the parts that ended since: with one part left
// the work could never wake ("agents=0.8 GB + its 11.2 GB >= cap=12.0 GB").
func (e *Engine) frozenParts(f frozenJob) (atFreeze, now uint64) {
	for _, u := range e.workUnits(f.owner) {
		if u.PID == f.active {
			continue
		}
		at, ok := f.parts[u.PID]
		if !ok {
			at = u.RSS // started since: frozen at the last turn's apply
		}
		atFreeze += max(at, u.RSS)
		now += u.RSS
	}
	return atFreeze, now
}

// roomToThaw says whether f's work fits back above the reserve: what of it is
// out of RAM now (its memory at the freeze less the thread's memory now) comes
// back with the thaw (V0.sim-fixes (3)). Without a headroom, a budget or f's
// memory it fits.
func (e *Engine) roomToThaw(f frozenJob, sig slots.Signals) (bool, string) {
	if sig.Headroom == nil || sig.Memory == 0 || f.mem == 0 {
		return true, ""
	}
	var resident uint64
	if e.SlotMem != nil {
		resident = e.SlotMem(f.owner)
	}
	back := float64(f.mem - min(resident, f.mem))
	room := (*sig.Headroom - sig.Reserve(e.SlotPolicy)) * float64(sig.Memory)
	if back <= room {
		return true, ""
	}
	return false, fmt.Sprintf("its %s out of RAM would come back, %s free above the reserve", slots.GB(back), slots.GB(max(0, room)))
}

// service says thread's heavy work is a service the guard never freezes
// (decision 0036 (D); GuardService is nil until the service class lands).
func (e *Engine) service(thread string) bool {
	return e.GuardService != nil && e.GuardService(thread)
}

// frozenUsec is FrozenUsec, false without it.
func (e *Engine) frozenUsec(thread string, undeclared bool) (uint64, bool) {
	if e.FrozenUsec == nil {
		return 0, false
	}
	return e.FrozenUsec(thread, undeclared)
}

// frozenFor is how long f was frozen (V0.guard-wake, decision 0036 E6): the
// cgroup's frozen_usec since the freeze where the kernel has it, else the
// wall time since the freeze.
func (e *Engine) frozenFor(f frozenJob, now time.Time) time.Duration {
	wall := now.Sub(f.at)
	if !f.hasUsec || f.active != 0 { // taking turns: the parts thaw in turn
		return wall
	}
	u, ok := e.frozenUsec(f.owner, f.undeclared)
	if !ok || u <= f.usec {
		return wall
	}
	return time.Duration(u-f.usec) * time.Microsecond
}

// frozenWords is a time frozen as the notice says it: "45 s", "4 min 12 s".
func frozenWords(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d.Seconds()))
	}
	m, s := int(d/time.Minute), int((d%time.Minute)/time.Second)
	if s == 0 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d min %d s", m, s)
}

// frozenOwners names the frozen threads for the log, comma-separated.
func frozenOwners(fs []frozenJob) string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.owner)
	}
	return strings.Join(out, ",")
}
