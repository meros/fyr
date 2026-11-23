package guard

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// SignalsLine is the signals the guard acted on, for its log lines.
func SignalsLine(sig resource.Signals) string {
	h, s := "n/a", "n/a"
	if sig.Headroom != nil {
		h = fmt.Sprintf("%.0f%%", *sig.Headroom*100)
	}
	if sig.Slowdown != nil {
		s = fmt.Sprintf("%.1fx", *sig.Slowdown)
	}
	out := fmt.Sprintf("headroom=%s stall=%s slowdown=%s", h, sig.Stall, s)
	if sig.AgentsStall != "" {
		out += " agents_stall=" + string(sig.AgentsStall)
	}
	if sig.Triggered { // V0.freeze-fast-class-name: critical by the trigger, not avg10
		out += " psi_trigger=fresh"
	}
	if sig.UnitStall != nil && sig.OomdLimit > 0 { // V0.guard-agents-stall
		out += fmt.Sprintf(" unit_full=%.0f%% oomd_limit=%.0f%%", *sig.UnitStall, sig.OomdLimit)
	}
	if sig.SwapFull {
		out += " swap_full=true"
	}
	return out
}

// Why says why memory is short, for a freeze's notice and log: the
// headroom first, else the critical stall, the host's or the agents'
// (V0.guard-agents-stall), else the unit's stall near oomd's limit.
func Why(sig resource.Signals, p resource.Policy) string {
	if sig.Headroom != nil && *sig.Headroom < sig.Reserve(p) {
		return sig.MemoryWhy()
	}
	if crit, why := sig.Critical(); crit {
		return why
	}
	if why := sig.OomdWhy(); why != "" {
		return why
	}
	return "memory stalls (critical)"
}

// Stalls says memory stalls at all (V0.sim-fixes (3)): the host's or the
// agents' stall is elevated or worse. A freeze needs short
// headroom and at least an elevated stall; admission still holds new jobs on
// the headroom alone.
func Stalls(sig resource.Signals) bool {
	return sig.Stall == resource.StallElevated || sig.Stall == resource.StallCritical ||
		sig.AgentsStall == resource.StallElevated || sig.AgentsStall == resource.StallCritical
}

// wakeable says whether frozen job f may wake now by Buck2's rule (V0.guard-wake,
// decision 0036 E3): the agents' memory plus f's memory at its freeze is under
// the cap; without a cap (the stall never went critical) the headroom rule
// already held. The text says why, for the log.
//
// V0.wake-once: resident is the frozen thread's memory now. Its pages stay
// charged to the agents while they are in RAM, so they come out of the
// agents' memory before the job's memory at the freeze is added: counted
// twice, a job still in RAM could never fit.
func wakeable(f Frozen, sig resource.Signals, cap, resident uint64) (bool, string) {
	if cap == 0 {
		return true, "memory fine for 30 s"
	}
	if sig.AgentsMem == 0 {
		return true, "stall below elevated for 30 s (agents' memory unknown)"
	}
	others := sig.AgentsMem - min(resident, sig.AgentsMem)
	need := others + f.Mem
	words := fmt.Sprintf("agents=%s + its %s %s cap=%s", resource.GB(float64(others)), resource.GB(float64(f.Mem)), "%s", resource.GB(float64(cap)))
	if need < cap {
		return true, "fits: " + fmt.Sprintf(words, "<") + ", stall below elevated for 30 s"
	}
	return false, fmt.Sprintf(words, ">=")
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

// FrozenOwners names the frozen threads for the log, comma-separated.
func FrozenOwners(fs []Frozen) string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Owner)
	}
	return strings.Join(out, ",")
}

// turnTakers are the parts that get turns: those of TurnMinRSS or more, or
// all of them when none is.
func turnTakers(units []resource.WorkUnit) []resource.WorkUnit {
	var out []resource.WorkUnit
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

// MaxFrozen is how many freezes the guard holds at most (V0.guard-wake,
// decision 0036 E4): the threads with heavy work (a hold or a flag) less one,
// so one always runs. V0.sim-fixes (3): it was the running threads less one,
// and an agent with no heavy work let the last heavy work be frozen for it.
func MaxFrozen(held []resource.Holder, fl []resource.Undeclared) int {
	ths := map[string]bool{}
	for _, h := range held {
		if !h.Priority {
			ths[h.Owner] = true
		}
	}
	for _, f := range fl {
		ths[f.Thread] = true
	}
	return max(len(ths)-1, 0)
}

// RunningHeavy is the heavy work that runs now, by thread, with since when
// (V0.sim-fixes (3)): the holds and the flagged undeclared work fs does not
// hold frozen. Work taking turns runs (one part has the turn).
func RunningHeavy(held []resource.Holder, fl []resource.Undeclared, fs []Frozen) map[string]time.Time {
	out := map[string]time.Time{}
	note := func(th string, at time.Time) {
		if was, ok := out[th]; !ok || at.Before(was) {
			out[th] = at
		}
	}
	stopped := map[string]bool{}
	for _, f := range fs {
		if f.Active == 0 {
			stopped[f.Owner] = true
		}
	}
	for _, h := range held {
		if !h.Priority && !stopped[h.Owner] {
			note(h.Owner, h.Since)
		}
	}
	for _, f := range fl {
		if !stopped[f.Thread] {
			note(f.Thread, f.Since)
		}
	}
	return out
}

// recent are the kills within RerunCapFor of now.
func recent(ks []time.Time, now time.Time) []time.Time {
	return slices.DeleteFunc(slices.Clone(ks), func(t time.Time) bool { return now.Sub(t) >= RerunCapFor })
}

// Recent are the kills of ks within RerunCapFor of now.
func Recent(ks []time.Time, now time.Time) []time.Time { return recent(ks, now) }
