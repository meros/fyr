package engine

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

// frozenJob is a job or undeclared work the memory guard froze (decision
// 0029 S5).
type frozenJob struct {
	owner, since string // the hold: owner and its grant
	at           time.Time
	// V0.164: undeclared work frozen instead of a hold; episode is the
	// flag's since.
	undeclared bool
	episode    time.Time
	// Decision 0032 slice 2: the part of the work whose turn it is (0: the
	// work is frozen whole), since when, for how long, and the thread's
	// read-back counter when the turn began.
	active int
	turnAt time.Time
	turn   time.Duration
	faults uint64
	// parts is each frozen part's memory when it was last frozen, by pid
	// (V0.sim-fixes (4)): what comes back if the turns end in a wake.
	parts map[int]uint64
	// V0.oomd-guard (decision 0036 E2): past FreezeMax while memory stalls
	// critically, so it stays frozen; the thread was told once.
	pastMax bool
	// V0.guard-wake (decision 0036 E3, E6): the thread's memory at the
	// freeze, and its cgroup's frozen_usec then (hasUsec: the kernel has it).
	mem     uint64
	usec    uint64
	hasUsec bool
	// V0.rerun-classes: the work's heavy class (a job's, from the gate) and
	// command (undeclared work's), which the owner's rerunnable entries match.
	class, cmd string
}

// Decision 0029 S5: how long memory must be fine before a frozen job thaws,
// and the longest a job stays frozen while memory does not stall critically
// (V0.oomd-guard: on 18 and 19 Nov each of six thaws at the limit let a job
// back into a critical stall, and oomd killed under fyr.service).
const (
	ThawAfterOK = 30 * time.Second
	FreezeMax   = 10 * time.Minute
	// FreezeCeiling ends a hold past FreezeMax (V0.guard-tidy): work frozen
	// this long thaws even while the agents' memory stalls critically, unless
	// its class is rerunnable (V0.rerun-classes kills it instead).
	FreezeCeiling = 30 * time.Minute
)

// Decision 0032 slice 1: one long episode of undeclared work can be frozen
// again after a thaw (the freeze is a time slice, not a once-only verdict),
// once the work has run RefreezeAfter, and at most MaxFreezes times, so a
// job that never settles is let run in the end instead of flapping.
const (
	RefreezeAfter = 60 * time.Second
	MaxFreezes    = 8
)

// WorkMemShare is the least share of a flagged thread's memory its work's
// processes must hold to be frozen or take turns (V0.guard-tidy). On 19 Nov
// at 16:02 the guard took turns among 3 parts that each read 0.0 GB while the
// thread's 3.3 GB stayed charged to its cli/.
const WorkMemShare = 0.25

// V0.multi-freeze: while memory stays short AddFreezeAfter after the newest
// freeze, the guard freezes one more thread; when it is fine again it thaws
// them one at a time, so the thawed work does not page back in all at once.
// V0.guard-wake (decision 0036 E3, E4): the oldest freeze wakes first, at
// most one each WakeEvery, and up to the running threads less one are frozen
// at once (maxFrozen; it was 3).
const (
	AddFreezeAfter = 60 * time.Second
	WakeEvery      = 10 * time.Second
)

// TriggerFresh is how long after the PSI trigger fired the agents' stall is
// critical and the guard may freeze work that grows without waiting the
// detector's window (V0.freeze-fast-class-name): two ticks.
const TriggerFresh = slots.TriggerFresh

// frozenJobOf says the guard holds thread's job (not undeclared work) frozen
// now; under mu.
func (e *Engine) frozenJobOf(thread string) bool {
	for _, f := range e.frozen {
		if f.owner == thread && !f.undeclared {
			return true
		}
	}
	return false
}

// isFrozen says the guard holds thread frozen now; under mu.
func (e *Engine) isFrozen(thread string) bool {
	for _, f := range e.frozen {
		if f.owner == thread {
			return true
		}
	}
	return false
}

// undFreeze is what the guard froze of one thread's episode of undeclared
// work: the episode (the flag's since), how many freezes so far, and when the
// last one thawed.
type undFreeze struct {
	episode  time.Time
	n        int
	thawedAt time.Time
}

// guardLine is the guard's last log line for a key and when it was written.
type guardLine struct {
	msg string
	at  time.Time
}

// guardOnce logs a guard decision that repeats every tick (a skip, and why)
// once, and again when its text changes or a minute has passed, so the log
// shows each decision without a line per tick.
func (e *Engine) guardOnce(now time.Time, key, msg string) {
	e.mu.Lock()
	last, ok := e.guardLast[key]
	repeat := ok && last.msg == msg && now.Sub(last.at) < time.Minute
	if !repeat {
		e.guardLast[key] = guardLine{msg, now}
	}
	e.mu.Unlock()
	if !repeat {
		log.Printf("guard: %s", msg)
	}
}

// guardSignals is the signals the guard acted on, for its log lines.
func guardSignals(sig slots.Signals) string {
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

// guardKeep is how many decisions the front's history holds.
const guardKeep = 60

// guardRecord adds a decision to the history the front shows (decision 0032).
func (e *Engine) guardRecord(ev slots.GuardEvent, sig slots.Signals) {
	e.invEvent(ev, sig) // V0.load-invariants
	if sig.Headroom != nil {
		h := *sig.Headroom
		ev.Headroom = &h
	}
	ev.Stall = string(sig.Stall)
	e.mu.Lock()
	e.guardEvents = append(e.guardEvents, ev)
	if n := len(e.guardEvents); n > guardKeep {
		e.guardEvents = append([]slots.GuardEvent(nil), e.guardEvents[n-guardKeep:]...)
	}
	e.mu.Unlock()
}

// frozenUndeclared is thread's freeze of undeclared work; under mu.
func (e *Engine) frozenUndeclared(thread string) (frozenJob, bool) {
	for _, f := range e.frozen {
		if f.undeclared && f.owner == thread {
			return f, true
		}
	}
	return frozenJob{}, false
}

// GuardView is the guard's last published view (nil before the first tick).
// It waits for nothing: the pool reads it under its own lock.
func (e *Engine) GuardView() *slots.Guard { return e.guardPub.Load() }

// publishGuard builds the view at the end of a tick.
func (e *Engine) publishGuard(now time.Time) {
	e.mu.Lock()
	g := &slots.Guard{Short: e.guardShort, Max: MaxFreezes, RefreezeSec: int(RefreezeAfter / time.Second),
		Events: append([]slots.GuardEvent{}, e.guardEvents...), // never nil: the front gets [], not null
		Rerun:  e.rerunView(now)}
	if e.guardShort && !e.guardSince.IsZero() {
		t := e.guardSince
		g.ShortSince = &t
	}
	for thread, u := range e.undFroze {
		t := slots.GuardThread{Thread: thread, N: u.n, Capped: u.n >= MaxFreezes}
		if f, ok := e.frozenUndeclared(thread); ok {
			t.Frozen = true
			at := f.at
			t.FrozenAt = &at
		} else if !u.thawedAt.IsZero() && !t.Capped {
			if next := u.thawedAt.Add(RefreezeAfter); next.After(now) {
				t.NextAt = &next
			}
		}
		g.Threads = append(g.Threads, t)
	}
	e.mu.Unlock()
	g.Invariants = e.invariantView() // V0.load-invariants
	slices.SortFunc(g.Threads, func(a, b slots.GuardThread) int { return strings.Compare(a.Thread, b.Thread) })
	e.guardPub.Store(g)
}

// guardWhy says why memory is short, for a freeze's notice and log: the
// headroom first, else the critical stall, the host's or the agents'
// (V0.guard-agents-stall), else the unit's stall near oomd's limit.
func guardWhy(sig slots.Signals, p slots.Policy) string {
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
	critical, _ := sig.Critical()
	agentsCritical := sig.AgentsStall == slots.StallCritical
	nearOomd := sig.NearOomd()
	short := sig.Provider != "" && ((sig.Headroom != nil && *sig.Headroom < sig.Reserve(e.SlotPolicy)) || critical || nearOomd)
	calm := !critical && !nearOomd && sig.Stall != slots.StallElevated && sig.AgentsStall != slots.StallElevated
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
	fine := calm && (capNow > 0 || sig.Headroom == nil || *sig.Headroom >= sig.Reserve(e.SlotPolicy)+0.05)
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
	type thawing struct {
		f                         frozenJob
		holding, emptied, ceiling bool
		woke                      string // the wake rule's reason, "" for the others
	}
	var keep []frozenJob
	var thaws []thawing
	var pastMax []frozenJob // newly held past FreezeMax: told once
	for _, f := range frozen {
		holding := false
		if f.undeclared {
			// The episode goes on while the thread is still flagged with the
			// same start; admitted, ended or released, it is not.
			for _, u := range e.flags() {
				if u.Thread == f.owner && u.Since.Equal(f.episode) {
					holding = true
				}
			}
		}
		for _, h := range s.Held {
			if h.Owner == f.owner && h.Since.Format(time.RFC3339Nano) == f.since {
				holding = true
			}
		}
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
		overMax := now.Sub(f.at) >= FreezeMax
		ceiling := now.Sub(f.at) >= FreezeCeiling && e.rerunEntry(f, now) == ""
		// V0.sim-fixes (1): a hold or episode that ended while its work is
		// still there (a call's result came while the job was frozen) ends, but
		// its work does not thaw into the critical stall it was frozen for: it
		// thaws when the stall is below critical, or at the ceiling.
		if !holding && !emptied && (critical || agentsCritical) && !ceiling {
			e.guardOnce(now, "ended:"+f.owner, fmt.Sprintf("keeping thread=%s frozen: its hold or episode ended but its work is still there, and memory stalls critically (%s)", f.owner, guardSignals(sig)))
			keep = append(keep, f)
			continue
		}
		// V0.sim-fixes (3): the timed thaw waits while what of the work is out
		// of RAM would not fit above the reserve: it comes back with the thaw.
		// Work frozen on headroom saw a calm stall at 10 min because nothing
		// ran, thawed, and paged its memory back into the same shortage.
		timed := overMax && !agentsCritical
		if timed && holding && !emptied && !ceiling {
			if ok, why := e.roomToThaw(f, sig); !ok {
				e.guardOnce(now, "pastmax:"+f.owner, fmt.Sprintf("keeping thread=%s frozen past the %s limit: %s; it thaws when it fits, or at %s", f.owner, FreezeMax, why, FreezeCeiling))
				keep = append(keep, f)
				continue
			}
		}
		if !holding || emptied || timed || ceiling {
			thaws = append(thaws, thawing{f: f, holding: holding, emptied: emptied, ceiling: ceiling && agentsCritical})
			continue
		}
		if overMax {
			e.guardOnce(now, "pastmax:"+f.owner, fmt.Sprintf("keeping thread=%s frozen past the %s limit: the agents' memory stalls critically (%s); it thaws when the stall is below critical, or at %s", f.owner, FreezeMax, guardSignals(sig), FreezeCeiling))
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
	alone := len(keep) > 0 && len(thaws) == 0 && !critical && !nearOomd && len(e.runningHeavy(s, e.flags())) == 0
	if alone && now.Sub(lastWake) >= WakeEvery {
		thaws = append(thaws, thawing{f: keep[0], holding: true, woke: "no other heavy work runs"})
		keep = keep[1:]
		e.mu.Lock()
		e.guardWake = now
		e.mu.Unlock()
	}
	if len(keep) > 0 && len(thaws) == 0 && !okSince.IsZero() && now.Sub(okSince) >= ThawAfterOK && now.Sub(lastWake) >= WakeEvery {
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
		reason, why := "memory fine for 30 s", "memory"
		switch {
		case t.emptied:
			reason, why = "its processes are gone", "gone"
		case !t.holding:
			reason, why = "episode or hold ended", "ended"
		case t.woke != "":
			reason = t.woke
		case t.ceiling:
			reason, why = fmt.Sprintf("%s ceiling, the agents' memory still stalls critically", frozenWords(FreezeCeiling)), "ceiling"
		case now.Sub(f.at) >= FreezeMax:
			reason, why = "10 min limit", "limit"
			if f.pastMax {
				reason = "10 min limit, stall no longer critical"
			}
		}
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
	if !short || len(thaws) > 0 || killed > 0 {
		return // a thaw or a kill this tick: the next tick sees what it did
	}
	if !critical && !nearOomd && !stalls(sig) {
		e.guardOnce(now, "nostall", fmt.Sprintf("not freezing on the headroom alone: memory does not stall (%s); new heavy commands wait at the gate", guardSignals(sig)))
		return
	}
	// With one thread running (n is 0) the freezes below say themselves why
	// they skip it.
	// V0.guard-agents-stall: near oomd's limit the next freeze does not wait
	// AddFreezeAfter; oomd kills after its duration (20 s), sooner than that.
	wait := len(keep) > 0 && now.Sub(keep[len(keep)-1].at) < AddFreezeAfter && !nearOomd
	fl := e.flags()
	if n := e.maxFrozen(s, fl); len(keep) >= max(n, 1) || wait {
		if len(keep) >= n && n > 0 {
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
	n := len(e.frozen)
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
		victim.Owner, now.Sub(victim.Since).Round(time.Second), cmd, why, slots.GB(float64(fj.mem)), cores, guardSignals(sig), len(s.Held), len(e.Running()), n)
	e.guardRecord(slots.GuardEvent{At: now, Kind: "freeze", Work: "job", Thread: victim.Owner}, sig)
	e.slotNotice(victim.Owner, map[string]any{"paused": why})
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

// flags are the threads that run undeclared heavy work now.
func (e *Engine) flags() []slots.Undeclared {
	if e.Undeclared == nil {
		return nil
	}
	return e.Undeclared()
}

// freezeUndeclared freezes the newest flagged thread whose memory is over
// (V0.164 section 7), before any job that asked; not one already frozen
// (V0.multi-freeze: freezeGuard decides when another may be).
// Decision 0032 slice 1 changes two once-only rules of V0.164 that left a long
// episode unprotected: the work is frozen when anyone else would gain from it
// (an admitted job, another flagged thread, or any other thread that runs:
// its agent starts slowly or not at all in a thrashing cgroup), not only when
// a job or a second flag exists; and an episode is frozen again after a thaw,
// RefreezeAfter later and at most MaxFreezes times, so the freezes work as time
// slices of the long episode. It reports whether it froze one.
func (e *Engine) freezeUndeclared(sig slots.Signals, now time.Time, admitted bool, heavy map[string]time.Time) bool {
	if e.UndeclaredFreeze == nil || e.UndeclaredThaw == nil {
		return false
	}
	fl := e.flags()
	if len(fl) == 0 {
		return false
	}
	run := e.Running()
	others := func(thread string) int {
		n := 0
		for _, id := range run {
			if id != thread {
				n++
			}
		}
		return n
	}
	var pick *slots.Undeclared
	var picked undFreeze
	var skips [][2]string    // key, line: logged after the lock
	svc := map[string]bool{} // decision 0036 (D), E4: asked outside e.mu
	held := map[string]uint64{}
	turnable := map[string]bool{} // two or more parts: one keeps running
	for _, f := range fl {
		svc[f.Thread] = e.service(f.Thread)
		if f.MemOver {
			turnable[f.Thread] = len(e.workUnits(f.Thread)) >= 2
		}
		// V0.guard-tidy: what the episode's processes hold now. The kernel does
		// not move memory with a process, so a thread's flagged memory can be
		// charged where its work no longer is (cli/), and freezing the work
		// frees none of it.
		if f.MemOver && e.UndeclaredRootsMem != nil {
			if m, ok := e.UndeclaredRootsMem(f.Thread); ok {
				held[f.Thread] = m + 1 // 0: not read
			}
		}
	}
	e.mu.Lock()
	for i := range fl {
		f := &fl[i]
		if !f.MemOver || e.threads[f.Thread] == nil || e.isFrozen(f.Thread) {
			continue
		}
		if svc[f.Thread] {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: its work is a service", f.Thread)})
			continue
		}
		if m := held[f.Thread]; m > 0 && float64(m-1) < WorkMemShare*float64(f.Mem) {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: the work holds little of the thread's memory (its processes %s of %s)", f.Thread, slots.GB(float64(m-1)), slots.GB(float64(f.Mem)))})
			continue
		}
		prev := e.undFroze[f.Thread]
		if prev.episode.Equal(f.Since) {
			if prev.n >= MaxFreezes {
				skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: frozen %d times in this episode, letting it run", f.Thread, prev.n)})
				continue
			}
			if !prev.thawedAt.IsZero() && now.Sub(prev.thawedAt) < RefreezeAfter {
				continue // the slice has not run long enough yet; says nothing
			}
		}
		if !admitted && len(fl) < 2 && others(f.Thread) == 0 {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: nobody else would gain (no job, no other flag, no other thread running)", f.Thread)})
			continue
		}
		// V0.sim-fixes (3): the last heavy work that runs is never frozen
		// whole (one always runs, decision 0029 section 4, Buck2's rule); work
		// of two or more parts takes turns, so one of its parts still runs. An
		// agent with no heavy work is no reason to stop all of it: the frozen
		// memory stayed charged, and only the 10 min thaw ran the work again.
		if !turnable[f.Thread] && len(heavy) < 2 {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: its work is the only heavy work that runs; one always runs", f.Thread)})
			continue
		}
		if pick == nil || f.Since.After(pick.Since) {
			pick, picked = f, prev
		}
	}
	e.mu.Unlock()
	for _, k := range skips {
		e.guardOnce(now, "skip:"+k[0], k[1])
	}
	if pick == nil {
		return false
	}
	usec, hasUsec := e.frozenUsec(pick.Thread, true)
	first, units, turns := e.startTurns(pick.Thread)
	if !turns && !e.UndeclaredFreeze(pick.Thread) {
		e.guardOnce(now, "skip:"+pick.Thread, fmt.Sprintf("could not freeze thread=%s: no process to move or the cgroup refused", pick.Thread))
		return false
	}
	why := guardWhy(sig, e.SlotPolicy)
	n := 1
	if picked.episode.Equal(pick.Since) {
		n = picked.n + 1
	}
	faults := e.majorFaults(pick.Thread)
	fj := frozenJob{owner: pick.Thread, at: now, undeclared: true, episode: pick.Since, mem: pick.Mem, usec: usec, hasUsec: hasUsec, cmd: pick.Command}
	if turns {
		fj.active, fj.turnAt, fj.turn, fj.faults = first.PID, now, TurnMin, faults
		fj.parts = map[int]uint64{}
		for _, u := range e.workUnits(pick.Thread) {
			if u.PID != first.PID {
				fj.parts[u.PID] = u.RSS
			}
		}
	}
	e.mu.Lock()
	e.frozen = append(e.frozen, fj)
	frozenNow := len(e.frozen)
	e.undFroze[pick.Thread] = undFreeze{episode: pick.Since, n: n}
	e.mu.Unlock()
	if turns {
		log.Printf("guard: taking turns thread=%s units=%d first=%d cmd=%q mem=%s", pick.Thread, units, first.PID, first.Command, slots.GB(float64(first.RSS)))
	}
	cmd := pick.Command
	if cmd == "" {
		cmd = "unknown command"
	}
	log.Printf("guard: froze undeclared work thread=%s freeze=%d/%d episode_age=%s cmd=%q mem=%s cores=%.1f %s flagged=%d admitted=%t others_running=%d frozen_now=%d",
		pick.Thread, n, MaxFreezes, now.Sub(pick.Since).Round(time.Second), cmd, slots.GB(float64(pick.Mem)), pick.CPU, guardSignals(sig), len(fl), admitted, others(pick.Thread), frozenNow)
	e.guardRecord(slots.GuardEvent{At: now, Kind: "freeze", Work: "undeclared", Thread: pick.Thread, N: n}, sig)
	e.slotNotice(pick.Thread, map[string]any{"paused": why, "work": cmd})
	return true
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
