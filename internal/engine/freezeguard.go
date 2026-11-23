// The memory guard's tick: freezeGuard, which freezes the work that grows most and thaws what may run again. Its decisions are in guarddecide.go, its thaw rules in thaw.go, its undeclared-work freezes in undeclared.go, its log in guardlog.go.

package engine

import (
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

// freezeVictim is the job to freeze: of the holds not frozen yet, the one whose
// memory grew most (V0.load-tuning; freezing a job that uses none frees nothing
// and stops nothing), the newest of equals. V0.guard-wake (decision 0036 E4,
// Buck2's "forward progress"): never the oldest hold, which always runs, and
// never a service.
func (e *Engine) freezeVictim(held []slots.Holder) (slots.Holder, bool) {
	var best slots.Holder
	var bestGrew uint64
	found := false
	oldest := held[0]
	for _, h := range held[1:] {
		if h.Since.Before(oldest.Since) {
			oldest = h
		}
	}
	for _, h := range held {
		e.mu.Lock()
		done := e.frozeHolds[h.Owner].Equal(h.Since)
		e.mu.Unlock()
		if h.Priority || done || h.Owner == oldest.Owner || e.service(h.Owner) {
			continue
		}
		var grew uint64
		if e.SlotGrowth != nil {
			grew = e.SlotGrowth(h.Owner)
		}
		if !found || grew > bestGrew || (grew == bestGrew && h.Since.After(best.Since)) {
			best, bestGrew, found = h, grew, true
		}
	}
	return best, found
}

// freezeGuard freezes undeclared work, else the slotted job that grew most
// (freezeVictim), while memory is short: one at first, one more each
// AddFreezeAfter that memory stays short, at most maxFrozen (V0.guard-wake:
// the running threads less one), never the oldest running job or a service.
// Each freeze thaws when its work or hold ended, after FreezeMax unless the
// stall is critical (V0.oomd-guard), or by the wake rule (wakeable). Freeze
// and Thaw run outside e.mu.
func (e *Engine) freezeGuard(s slots.Snapshot) {
	if e.SlotFreeze == nil || e.SlotThaw == nil || e.SlotSignals == nil {
		return
	}
	now := e.Now()
	e.mu.Lock()
	trig := e.guardTrig
	e.mu.Unlock()
	// V0.freeze-fast-class-name: a fresh trigger is a critical stall (the
	// sampler says so too; this holds whatever SlotSignals is).
	sig := e.SlotSignals().Fresh(trig, now)
	// V0.oomd-guard (decision 0036 E2): no timed thaw into a critical stall,
	// the host's or the agents'. V0.guard-agents-stall (decision 0036 B, act
	// before oomd): either makes memory short, and so does the unit's stall
	// near oomd's limit; the host's alone used to.
	mem := memoryState(sig, e.SlotPolicy)
	critical, nearOomd, short := mem.critical, mem.nearOomd, mem.short
	e.mu.Lock()
	if e.frozeHolds == nil {
		e.frozeHolds = map[string]time.Time{}
	}
	frozen := append([]frozenJob(nil), e.frozen...)
	// V0.freeze-fast-class-name: the trigger fired within TriggerFresh.
	fast := sig.Triggered
	wasShort := e.guardShort
	e.guardShort = short
	if short && !wasShort {
		e.guardSince = now
	}
	// V0.guard-wake (decision 0036 E3): the cap is what the agents held when
	// the stall first went critical in this episode; the episode ends when
	// memory is not short and nothing is frozen.
	newCap := false
	if critical && e.guardCap == 0 && sig.AgentsMem > 0 {
		e.guardCap, newCap = sig.AgentsMem, true
	}
	if !short && len(frozen) == 0 {
		e.guardCap = 0
	}
	capNow := e.guardCap
	// With a cap the wake waits for the stall alone (below elevated); without
	// one, also for the headroom (reserve + 5 %), as before.
	fine := wakeFine(mem, sig, e.SlotPolicy, capNow)
	switch {
	case !fine:
		e.guardOK = time.Time{}
	case e.guardOK.IsZero():
		e.guardOK = now
	}
	okSince, lastWake := e.guardOK, e.guardWake
	var retry []string
	if !short && wasShort {
		for th := range e.gateHeld {
			retry = append(retry, th)
		}
		clear(e.gateHeld)
	}
	e.mu.Unlock()
	if newCap {
		log.Printf("guard: wake cap agents=%s: the agents' memory when the stall first went critical in this episode %s", slots.GB(float64(capNow)), guardSignals(sig))
	}
	if short != wasShort {
		state := "recovered"
		if short {
			state = "short"
		}
		e.guardRecord(slots.GuardEvent{At: now, Kind: state}, sig)
		log.Printf("guard: memory %s: %s flagged=%d held=%d running=%d frozen=%q", state, guardSignals(sig), len(e.flags()), len(s.Held), len(e.Running()), frozenOwners(frozen))
	}
	slices.Sort(retry)
	for _, th := range retry { // V0.guard-wake (E1): the commands the gate held
		e.slotNotice(th, map[string]any{"retry": true})
	}
	var keep []frozenJob
	var thaws []thawing
	var pastMax []frozenJob // newly held past FreezeMax: told once
	for _, f := range frozen {
		var flagged []slots.Undeclared
		if f.undeclared {
			flagged = e.flags()
		}
		holding := stillHolding(f, flagged, s.Held)
		// A freeze whose cgroup holds no process any more (the work ended or
		// was killed) holds nothing; release it at once instead of letting it
		// block every other freeze for ten minutes. V0.guard-tidy: a job's too.
		emptied := false
		if f.undeclared {
			emptied = e.UndeclaredEmpty != nil && e.UndeclaredEmpty(f.owner)
		} else {
			emptied = e.SlotEmpty != nil && e.SlotEmpty(f.owner)
		}
		// V0.guard-tidy: past FreezeMax only the agents' own critical stall
		// holds the work (the host's can come from the desktop, which the
		// frozen work does not feed), and not past FreezeCeiling unless the
		// work is rerunnable: then V0.rerun-classes's kill ends it.
		v := judgeFrozen(mem, frozenFacts{holding: holding, emptied: emptied, age: now.Sub(f.at), rerunnable: now.Sub(f.at) >= FreezeCeiling && e.rerunEntry(f, now) != ""},
			func() (bool, string) { return e.roomToThaw(f, sig) })
		switch v.why {
		case whyEnded:
			e.guardOnce(now, "ended:"+f.owner, fmt.Sprintf("keeping thread=%s frozen: its hold or episode ended but its work is still there, and memory stalls critically (%s)", f.owner, guardSignals(sig)))
		case whyNoRoom:
			e.guardOnce(now, "pastmax:"+f.owner, fmt.Sprintf("keeping thread=%s frozen past the %s limit: %s; it thaws when it fits, or at %s", f.owner, FreezeMax, v.room, FreezeCeiling))
		case whyCritical:
			e.guardOnce(now, "pastmax:"+f.owner, fmt.Sprintf("keeping thread=%s frozen past the %s limit: the agents' memory stalls critically (%s); it thaws when the stall is below critical, or at %s", f.owner, FreezeMax, guardSignals(sig), FreezeCeiling))
		}
		switch v.act {
		case actThaw:
			thaws = append(thaws, thawing{f: f, holding: holding, emptied: emptied, ceiling: v.ceiling})
			continue
		case actKeepPastMax:
			if !f.pastMax {
				f.pastMax = true
				pastMax = append(pastMax, f)
			}
		}
		keep = append(keep, f)
	}
	// V0.rerun-classes: a rerunnable freeze that could not wake within
	// RerunAfter is killed and run again.
	var killed int
	if e.RerunMatch != nil && len(keep) > 0 {
		var still []frozenJob
		for _, f := range keep {
			if now.Sub(f.at) >= RerunAfter {
				if entry := e.rerunEntry(f, now); entry != "" && e.rerunKill(f, entry, fmt.Sprintf("frozen %s and could not wake", RerunAfter), sig, now) {
					killed++
					continue
				}
			}
			still = append(still, f)
		}
		keep = still
	}
	// V0.guard-wake (decision 0036 E3, Buck2's rule): the oldest freeze wakes
	// first, at most one each WakeEvery, once the stall has been below
	// elevated for ThawAfterOK and it fits under the cap.
	// V0.sim-fixes (3): when the frozen work is all the heavy work left (the
	// rest ended), the oldest freeze wakes at once unless memory stalls
	// critically: nothing runs that it could take memory from.
	if wakesAlone(len(keep), len(thaws), mem, now.Sub(lastWake), func() int { return len(e.runningHeavy(s, e.flags())) }) {
		thaws = append(thaws, thawing{f: keep[0], holding: true, woke: "no other heavy work runs"})
		keep = keep[1:]
		e.mu.Lock()
		e.guardWake = now
		e.mu.Unlock()
	}
	if wakeDue(len(keep), len(thaws), okSince, lastWake, now) {
		f := keep[0]
		var resident uint64 // V0.wake-once: what of the agents' memory is the frozen thread's own
		if e.SlotMem != nil {
			resident = e.SlotMem(f.owner)
		}
		w := f
		if f.active != 0 {
			w.mem, resident = e.frozenParts(f)
		}
		if ok, why := wakeable(w, sig, capNow, resident); ok {
			keep = keep[1:]
			thaws = append(thaws, thawing{f: f, holding: true, woke: why})
			e.mu.Lock()
			e.guardWake = now
			e.mu.Unlock()
		} else {
			e.guardOnce(now, "wake:"+f.owner, fmt.Sprintf("not waking thread=%s yet: %s", f.owner, why))
		}
	}
	for i := range keep {
		keep[i] = e.nextTurn(keep[i], now)
	}
	e.mu.Lock()
	e.frozen = keep
	if e.jobThawed == nil {
		e.jobThawed = map[string]time.Time{}
	}
	for _, t := range thaws {
		if !t.f.undeclared {
			e.jobThawed[t.f.owner] = now
		}
		if t.f.undeclared {
			u := e.undFroze[t.f.owner]
			u.thawedAt = now
			e.undFroze[t.f.owner] = u
		}
	}
	e.mu.Unlock()
	for _, t := range thaws {
		f := t.f
		reason, why := thawWords(t, now)
		kind, work := "job", "job"
		if f.undeclared {
			kind, work = "undeclared work", "undeclared"
		}
		held := e.frozenFor(f, now) // read before the thaw: the cgroup may go with it
		e.guardRecord(slots.GuardEvent{At: now, Kind: "thaw", Work: work, Thread: f.owner, Reason: why}, sig)
		log.Printf("guard: thawed %s thread=%s after=%s frozen=%s reason=%q %s still_frozen=%d", kind, f.owner, now.Sub(f.at).Round(time.Second), held.Round(time.Second), reason, guardSignals(sig), len(keep))
		if f.undeclared {
			e.UndeclaredThaw(f.owner)
		} else {
			e.SlotThaw(f.owner)
		}
		if t.holding {
			d := map[string]any{"resumed": true, "frozen": frozenWords(held)}
			if f.undeclared {
				d["work"] = true
			}
			e.slotNotice(f.owner, d)
		}
	}
	for _, f := range pastMax {
		// V0.guard-tidy (decision 0032 "What the log gives"): the owner's
		// history shows the hold too.
		work := "job"
		if f.undeclared {
			work = "undeclared"
		}
		e.guardRecord(slots.GuardEvent{At: now, Kind: "kept", Work: work, Thread: f.owner}, sig)
		e.slotNotice(f.owner, map[string]any{"waits": true, "work": f.undeclared})
	}
	switch freezeGate(mem, stalls(sig), len(thaws)+killed) {
	case gateQuiet:
		return // a thaw or a kill this tick: the next tick sees what it did
	case gateNoStall:
		e.guardOnce(now, "nostall", fmt.Sprintf("not freezing on the headroom alone: memory does not stall (%s); new heavy commands wait at the gate", guardSignals(sig)))
		return
	}
	// With one thread running (n is 0) the freezes below say themselves why
	// they skip it.
	// V0.guard-agents-stall: near oomd's limit the next freeze does not wait
	// AddFreezeAfter; oomd kills after its duration (20 s), sooner than that.
	fl := e.flags()
	n := e.maxFrozen(s, fl)
	var lastFrozen time.Time
	if len(keep) > 0 {
		lastFrozen = keep[len(keep)-1].at
	}
	if block, say := freezeRoom(len(keep), n, lastFrozen, now, nearOomd); block {
		if say {
			e.guardOnce(now, "maxfrozen", fmt.Sprintf("not freezing more: %d frozen, at most %d (threads with heavy work less one)", len(keep), n))
		}
		return
	}
	heavy := e.runningHeavy(s, fl)
	if e.freezeUndeclared(sig, now, len(s.Held) > 0, heavy) {
		return
	}
	// V0.freeze-fast-class-name: on 20 Nov the PSI trigger fired at 07:32:12
	// and the work was frozen at 07:33:01, as the desktop froze. Within
	// TriggerFresh of the trigger the work that grew most is flagged now, not
	// a window (45 s) later, and
	// freezeUndeclared's rules (services, the share it holds, MaxFreezes,
	// someone gains) decide as for any flag.
	if fast && e.UndeclaredFlagNow != nil {
		if th := e.UndeclaredFlagNow(); th != "" {
			log.Printf("guard: flagged thread=%s without the window: the PSI trigger fired %s ago %s", th, now.Sub(trig).Round(time.Second), guardSignals(sig))
			// V0.sim-fixes (3): the early flag is heavy work too, and the
			// only heavy work that runs is still never frozen whole.
			if e.freezeUndeclared(sig, now, len(s.Held) > 0, e.runningHeavy(s, e.flags())) {
				return
			}
		}
	}
	// One job always runs: a job is frozen only while two or more run.
	var running []slots.Holder
	for _, h := range s.Held {
		e.mu.Lock()
		f := e.isFrozen(h.Owner)
		e.mu.Unlock()
		if !f {
			running = append(running, h)
		}
	}
	if len(running) < 2 {
		return
	}
	victim, ok := e.freezeVictim(running)
	if !ok {
		return
	}
	usec, hasUsec := e.frozenUsec(victim.Owner, false)
	if !e.SlotFreeze(victim.Owner) {
		return
	}
	why := guardWhy(sig, e.SlotPolicy)
	fj := frozenJob{owner: victim.Owner, since: victim.Since.Format(time.RFC3339Nano), at: now, usec: usec, hasUsec: hasUsec}
	if e.SlotMem != nil {
		fj.mem = e.SlotMem(victim.Owner)
	}
	e.heavyMu.Lock()
	fj.class = e.heavyClass[victim.Owner]
	e.heavyMu.Unlock()
	e.mu.Lock()
	e.frozeHolds[victim.Owner] = victim.Since
	e.mu.Unlock()
	// V0.rerun-classes: a short rerunnable job is killed instead: running it
	// again later costs less than holding its memory frozen.
	if entry := e.rerunEntry(fj, now); entry != "" {
		if wall, short := e.shortJob(victim.Owner); short && e.rerunKill(fj, entry, fmt.Sprintf("would be frozen; it runs %s", wall.Round(time.Second)), sig, now) {
			return
		}
	}
	e.mu.Lock()
	e.frozen = append(e.frozen, fj)
	frozenNow := len(e.frozen)
	e.mu.Unlock()
	// V0.guard-tidy: the fields of `froze undeclared work` (decision 0032
	// "What the log gives for tuning"). A hold is frozen once (frozeHolds), so
	// its freeze is 1/1; its episode is the hold, its command its class.
	cmd, cores := fj.class, 0.0
	if cmd == "" {
		cmd = "unknown command"
	}
	if victim.Need != nil {
		cores = victim.Need.CPU
	}
	log.Printf("guard: froze job thread=%s freeze=1/1 episode_age=%s cmd=%q why=%q mem=%s cores=%.1f %s held=%d running=%d frozen_now=%d",
		victim.Owner, now.Sub(victim.Since).Round(time.Second), cmd, why, slots.GB(float64(fj.mem)), cores, guardSignals(sig), len(s.Held), len(e.Running()), frozenNow)
	e.guardRecord(slots.GuardEvent{At: now, Kind: "freeze", Work: "job", Thread: victim.Owner}, sig)
	e.slotNotice(victim.Owner, map[string]any{"paused": why})
}

// maxFrozen is how many freezes the guard holds at most (V0.guard-wake,
// decision 0036 E4): the threads with heavy work (a hold or a flag) less one,
// so one always runs. V0.sim-fixes (3): it was the running threads less one,
// and an agent with no heavy work let the last heavy work be frozen for it.
func (e *Engine) maxFrozen(s slots.Snapshot, fl []slots.Undeclared) int {
	ths := map[string]bool{}
	for _, h := range s.Held {
		if !h.Priority {
			ths[h.Owner] = true
		}
	}
	for _, f := range fl {
		ths[f.Thread] = true
	}
	return max(len(ths)-1, 0)
}

// runningHeavy is the heavy work that runs now, by thread, with since when
// (V0.sim-fixes (3)): the holds and the flagged undeclared work the guard does
// not hold frozen. Work taking turns runs (one part has the turn).
func (e *Engine) runningHeavy(s slots.Snapshot, fl []slots.Undeclared) map[string]time.Time {
	out := map[string]time.Time{}
	note := func(th string, at time.Time) {
		if was, ok := out[th]; !ok || at.Before(was) {
			out[th] = at
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	stopped := map[string]bool{}
	for _, f := range e.frozen {
		if f.active == 0 {
			stopped[f.owner] = true
		}
	}
	for _, h := range s.Held {
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

// slotNotice records a slot event for the thread's agent (decision 0029).
func (e *Engine) slotNotice(owner string, d map[string]any) {
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotPause, Thread: owner, Args: d}); err != nil {
		log.Printf("slot pause %s: %v", owner, err)
	}
}

// memoryGuard tells the holder whose job grew most, once per episode, when
// memory is short (decision 0029 S2); nothing is stopped.
func (e *Engine) memoryGuard(s slots.Snapshot) {
	if e.SlotSignals == nil || e.SlotGrowth == nil {
		return
	}
	sig := e.SlotSignals()
	why := ""
	if sig.Provider != "" && sig.Headroom != nil && *sig.Headroom < sig.Reserve(e.SlotPolicy) {
		why = sig.MemoryWhy()
	} else if crit, w := sig.Critical(); crit { // V0.guard-agents-stall: the agents' stall too
		why = w
	}
	e.mu.Lock()
	was := e.memGuard
	e.memGuard = why != ""
	e.mu.Unlock()
	if why == "" || was {
		return
	}
	owner, most := "", uint64(0)
	for _, h := range s.Held {
		if g := e.SlotGrowth(h.Owner); !h.Priority && g > most {
			owner, most = h.Owner, g
		}
	}
	if owner == "" {
		return
	}
	// V0.guard-tidy: a thread the guard froze heard why in the pause notice;
	// this one says "nothing is stopped", so it would contradict it.
	e.mu.Lock()
	frozen := e.isFrozen(owner)
	e.mu.Unlock()
	if frozen {
		return
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotMemory, Thread: owner, Args: core.SlotMemoryArgs{Why: why, Grew: most}}); err != nil {
		log.Printf("slot memory %s: %v", owner, err)
	}
}
