package engine

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/slots"
)

// SlotIdle is how long an admission may carry no work before it goes back
// (V0.86 (3), V0.209): no live run, or a live run with no heavy command going
// and the thread's CPU at its idle level. Going back costs nothing: the next
// heavy command is admitted again at the gate.
const SlotIdle = 2 * time.Minute

// HeavyCallStale is how long a heavy command may look dead (no result, no CPU)
// before its admission stops counting on it: a result can get lost.
const HeavyCallStale = 15 * time.Minute

// HeavyCall is a heavy command at the gate hook (V0.209). ToolUse is its call;
// Background: the job outlives the call.
type HeavyCall struct {
	Thread, Class, ToolUse string
	Background             bool
	// Subagent: a subagent made the call. The admission notice goes to the
	// thread's own agent, so the refusal tells a subagent not to wait for it.
	Subagent bool
}

// UseSlots gives the engine the host's pool of slots for heavy jobs
// (V0.86): the agent's slot tools, the notice when a queued slot is the
// thread's, and the release when its run ends.
func (e *Engine) UseSlots(p *slots.Pool) {
	e.mu.Lock()
	e.Slots = p
	e.slotIdle = map[string]time.Time{}
	e.slotOverTold = map[string]time.Time{}
	e.undFroze = map[string]undFreeze{}
	e.guardLast = map[string]guardLine{}
	e.rerunKills, e.rerunWait = map[string][]time.Time{}, map[string]string{}
	e.mu.Unlock()
	e.heavyMu.Lock()
	e.heavyCalls, e.heavyBg, e.heavyClass = map[string]map[string]bool{}, map[string]bool{}, map[string]string{}
	e.heavyGranted = map[string]grantedClass{}
	e.publishOpenLocked()
	e.heavyMu.Unlock()
	p.OnGrant = e.slotGranted
	p.Rank = e.ProjectRank // V0.project-priority
	p.Fill()               // a pool loaded from the store may have free slots for its queue
}

// slotGranted tells a queued thread's agent that the slot is its own.
func (e *Engine) slotGranted(owner, kind string) {
	e.mu.Lock()
	t := e.threads[owner]
	if t != nil && !t.Running {
		e.slotIdle[owner] = e.Now()
	}
	e.mu.Unlock()
	if t == nil {
		return
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotGranted, Thread: owner, Args: kind}); err != nil {
		e.Slots.Release(owner)
	}
}

// slotRunEnded gives back the slot a thread held when its run ended; a
// place in the queue stays. V0.201: not while a background task may carry the
// job. An agent that starts `make check` in the background and ends its turn
// to wait lost its admission here, and the job it had asked for was then
// flagged as undeclared; sweepSlots gives the slot back when the task is done.
func (e *Engine) slotRunEnded(id string) {
	if e.Slots == nil {
		return
	}
	e.heavyMu.Lock()
	delete(e.heavyCalls, id) // a run that ended leaves no call to wait for
	e.publishOpenLocked()
	e.heavyMu.Unlock()
	e.mu.Lock()
	t := e.threads[id]
	keep := t != nil && t.RunsAdmittedWork()
	e.mu.Unlock()
	if keep {
		return
	}
	if _, ok := e.Slots.Holding(id); ok {
		e.dropSlot(id)
	}
}

func (e *Engine) dropSlot(id string) bool {
	e.heavyMu.Lock()
	delete(e.heavyCalls, id)
	delete(e.heavyBg, id)
	delete(e.heavyGranted, id)
	e.publishOpenLocked()
	e.heavyMu.Unlock()
	e.mu.Lock()
	delete(e.slotIdle, id)
	e.mu.Unlock()
	return e.Slots.Release(id)
}

// syncSlotQueue brings the threads' records in line with the pool's queue
// (V0.185): a Condition reads the record, and a thread leaves the queue by
// several paths. The tick calls it; so do the slot tools, for at once.
func (e *Engine) syncSlotQueue() {
	if e.Slots == nil {
		return
	}
	snap := e.Slots.Snapshot()
	queued := map[string]bool{}
	for _, h := range snap.Waiting {
		queued[h.Owner] = true
	}
	var moved []string
	var waits []string
	e.mu.Lock()
	// V0.load-tuning: how long a job waited, for tuning; the grant resets the
	// holder's Since, so the queue's start is kept here.
	if e.queuedAt == nil {
		e.queuedAt = map[string]time.Time{}
	}
	for _, h := range snap.Waiting {
		if _, ok := e.queuedAt[h.Owner]; !ok {
			e.queuedAt[h.Owner] = h.Since
		}
	}
	for id, at := range e.queuedAt {
		if queued[id] {
			continue
		}
		delete(e.queuedAt, id)
		line := fmt.Sprintf("slots: left the queue thread=%s waited=%s", id, e.Now().Sub(at).Round(time.Second))
		for _, h := range snap.Held {
			if h.Owner == id {
				line = fmt.Sprintf("slots: admitted thread=%s kind=%s waited=%s", id, h.Kind, h.Since.Sub(at).Round(time.Second))
			}
		}
		waits = append(waits, line)
	}
	for id, t := range e.threads {
		if t.SlotQueued != queued[id] {
			moved = append(moved, id)
		}
	}
	e.mu.Unlock()
	for _, w := range waits {
		log.Print(w)
	}
	for _, id := range moved {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotQueued, Thread: id, Args: queued[id]}); err != nil {
			log.Printf("slot queue %s: %v", id, err)
		}
	}
}

// sweepSlots gives back the slots of threads that are gone or held one for
// SlotIdle without a live run; the tick calls it.
func (e *Engine) sweepSlots() {
	if e.Slots == nil {
		return
	}
	s := e.Slots.Snapshot()
	var drop []string
	// V0.147: the tracker is asked before e.mu is taken. Asking it under
	// e.mu deadlocked fyrd on 17 Nov: the tracker's tick held its own lock
	// and asked the engine for a thread's project.
	active := map[string]time.Time{}
	for _, h := range s.Held {
		if e.SlotActive != nil && !h.Priority {
			at, ok := e.SlotActive(h.Owner)
			if !ok || at.Before(h.Since) {
				at = h.Since
			}
			active[h.Owner] = at
		}
	}
	calls, background := e.heavyWork()
	e.mu.Lock()
	now := e.Now()
	for _, h := range s.Held {
		if h.Priority {
			continue // the self-deploy releases its own
		}
		t := e.threads[h.Owner]
		switch {
		case t == nil:
			drop = append(drop, h.Owner)
		case t.Running:
			delete(e.slotIdle, h.Owner)
			// V0.209: an admission nothing uses goes back by itself.
			if at, ok := active[h.Owner]; ok {
				idle := now.Sub(at)
				switch {
				case calls[h.Owner] > 0 && idle < HeavyCallStale:
				case background[h.Owner] && t.RunsAdmittedWork():
				case idle >= SlotIdle:
					drop = append(drop, h.Owner)
				}
			}
		case t.RunsAdmittedWork(): // V0.201: the idle time starts when it is done
			delete(e.slotIdle, h.Owner)
		default:
			since, ok := e.slotIdle[h.Owner]
			if !ok {
				e.slotIdle[h.Owner] = now
			} else if now.Sub(since) >= SlotIdle {
				drop = append(drop, h.Owner)
			}
		}
	}
	for _, h := range s.Waiting {
		if !h.Priority && e.threads[h.Owner] == nil {
			drop = append(drop, h.Owner)
		}
	}
	e.mu.Unlock()
	for _, id := range drop {
		e.dropSlot(id)
	}
	e.syncSlotQueue()
	e.heavierThanPredicted(s)
	e.freezeGuard(s)
	e.memoryGuard(s) // V0.guard-tidy: after the freeze, which told its thread already
	e.publishGuard(e.Now())
	e.adaptBudget(s)
	// Decision 0029 S2: the conditions change without a release.
	e.Slots.Fill()
}

// GuardNow runs the memory guard now, between ticks (V0.guard-wake,
// decision 0036 E5): fyrd calls it when the PSI trigger on the agents'
// memory stall fires. It must run on the goroutine that calls Tick.
func (e *Engine) GuardNow() {
	if e.Slots == nil {
		return
	}
	e.mu.Lock()
	e.guardTrig = e.Now()
	e.mu.Unlock()
	e.guardOnce(e.Now(), "psi", "PSI trigger: the agents' memory stall crossed 150 ms in 2 s; the guard runs now")
	e.sweepSlots()
}

// heavyWork is what carries each admission now: the running foreground calls
// per thread, and the threads with a job that outlives its call.
func (e *Engine) heavyWork() (map[string]int, map[string]bool) {
	e.heavyMu.Lock()
	defer e.heavyMu.Unlock()
	calls := make(map[string]int, len(e.heavyCalls))
	for th, c := range e.heavyCalls {
		calls[th] = len(c)
	}
	return calls, maps.Clone(e.heavyBg)
}

// heavierThanPredicted tells a holder once per hold that its job uses more
// of the budget than predicted (decision 0029 S3); nothing is stopped.
func (e *Engine) heavierThanPredicted(s slots.Snapshot) {
	if e.SlotOver == nil {
		return
	}
	for _, h := range s.Held {
		if h.Priority {
			continue
		}
		e.mu.Lock()
		since, done := e.slotOverTold[h.Owner]
		e.mu.Unlock()
		if done && since.Equal(h.Since) {
			continue
		}
		class, live, pred, over := e.SlotOver(h.Owner)
		if !over {
			continue
		}
		e.mu.Lock()
		e.slotOverTold[h.Owner] = h.Since
		e.mu.Unlock()
		// V0.163: SlotOver answers in percent of the host.
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotOver, Thread: h.Owner, Args: core.SlotOverArgs{Class: class, Live: live, Predicted: pred}}); err != nil {
			log.Printf("slot over %s: %v", h.Owner, err)
		}
	}
}

// adaptBudget steps the budget controller and sets the pool's budget
// (decision 0029 S4), outside e.mu.
func (e *Engine) adaptBudget(s slots.Snapshot) {
	if e.SlotAIMD == nil || e.SlotSignals == nil {
		return
	}
	sig := e.SlotSignals()
	f, why := e.SlotAIMD.Step(e.Now(), sig, len(s.Waiting) > 0, e.Slots.LastGrant(), s.Bound())
	old := e.Slots.Budget()
	if f != old || (s.Budget != nil && s.BudgetWhy != why) {
		if f != old { // V0.load-tuning: the budget's moves can be read back
			log.Printf("slots: budget cpu=%.3f->%.3f mem=%.3f->%.3f why=%q waiting=%d held=%d %s", old.CPU, f.CPU, old.Mem, f.Mem, why, len(s.Waiting), len(s.Held), guardSignals(sig))
		}
		e.Slots.SetBudget(f, why)
	}
}

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

// TriggerFresh is how long after the PSI trigger fired the guard may freeze
// work that grows without waiting the detector's window, when the agents'
// memory stalls critically or the unit is near oomd's limit
// (V0.freeze-fast-class-name): two ticks.
const TriggerFresh = 20 * time.Second

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
	sig := e.SlotSignals()
	// V0.oomd-guard (decision 0036 E2): no timed thaw into a critical stall,
	// the host's or the agents'. V0.guard-agents-stall (decision 0036 B, act
	// before oomd): either makes memory short, and so does the unit's stall
	// near oomd's limit; the host's alone used to.
	critical, _ := sig.Critical()
	agentsCritical := sig.AgentsStall == slots.StallCritical
	nearOomd := sig.NearOomd()
	short := sig.Provider != "" && ((sig.Headroom != nil && *sig.Headroom < sig.Reserve(e.SlotPolicy)) || critical || nearOomd)
	calm := !critical && !nearOomd && sig.Stall != slots.StallElevated && sig.AgentsStall != slots.StallElevated
	now := e.Now()
	e.mu.Lock()
	if e.frozeHolds == nil {
		e.frozeHolds = map[string]time.Time{}
	}
	frozen := append([]frozenJob(nil), e.frozen...)
	// V0.freeze-fast-class-name: the trigger fired lately and the agents'
	// stall is critical, or the unit is near oomd's limit.
	fast := !e.guardTrig.IsZero() && now.Sub(e.guardTrig) < TriggerFresh && (agentsCritical || nearOomd)
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
		if holding && f.undeclared {
			emptied = e.UndeclaredEmpty != nil && e.UndeclaredEmpty(f.owner)
		} else if holding {
			emptied = e.SlotEmpty != nil && e.SlotEmpty(f.owner)
		}
		// V0.guard-tidy: past FreezeMax only the agents' own critical stall
		// holds the work (the host's can come from the desktop, which the
		// frozen work does not feed), and not past FreezeCeiling unless the
		// work is rerunnable: then V0.rerun-classes's kill ends it.
		overMax := now.Sub(f.at) >= FreezeMax
		ceiling := now.Sub(f.at) >= FreezeCeiling && e.rerunEntry(f, now) == ""
		if !holding || emptied || (overMax && !agentsCritical) || ceiling {
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
	if len(keep) > 0 && len(thaws) == 0 && !okSince.IsZero() && now.Sub(okSince) >= ThawAfterOK && now.Sub(lastWake) >= WakeEvery {
		f := keep[0]
		var resident uint64 // V0.wake-once: what of the agents' memory is the frozen thread's own
		if e.SlotMem != nil {
			resident = e.SlotMem(f.owner)
		}
		if ok, why := wakeable(f, sig, capNow, resident); ok {
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
	for _, t := range thaws {
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
	// With one thread running (n is 0) the freezes below say themselves why
	// they skip it.
	// V0.guard-agents-stall: near oomd's limit the next freeze does not wait
	// AddFreezeAfter; oomd kills after its duration (20 s), sooner than that.
	wait := len(keep) > 0 && now.Sub(keep[len(keep)-1].at) < AddFreezeAfter && !nearOomd
	if n := e.maxFrozen(); len(keep) >= max(n, 1) || wait {
		if len(keep) >= n && n > 0 {
			e.guardOnce(now, "maxfrozen", fmt.Sprintf("not freezing more: %d frozen, at most %d (running threads less one)", len(keep), n))
		}
		return
	}
	if e.freezeUndeclared(sig, now, len(s.Held) > 0) {
		return
	}
	// V0.freeze-fast-class-name: on 20 Nov the PSI trigger fired at 07:32:12
	// and the work was frozen at 07:33:01, as the desktop froze. When the
	// trigger fired and the agents stall critically (or near oomd's limit),
	// the work that grew most is flagged now, not a window (45 s) later, and
	// freezeUndeclared's rules (services, the share it holds, MaxFreezes,
	// someone gains) decide as for any flag.
	if fast && e.UndeclaredFlagNow != nil {
		if th := e.UndeclaredFlagNow(); th != "" {
			log.Printf("guard: flagged thread=%s without the window: the PSI trigger fired and %s", th, fastWhy(sig))
			if e.freezeUndeclared(sig, now, len(s.Held) > 0) {
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

// maxFrozen is how many freezes the guard holds at most (V0.guard-wake,
// decision 0036 E4): the running threads less one, so one always runs.
func (e *Engine) maxFrozen() int {
	return max(len(e.Running())-1, 0)
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

// fastWhy says why the guard did not wait the window (V0.freeze-fast-class-name).
func fastWhy(sig slots.Signals) string {
	if w := sig.OomdWhy(); w != "" {
		return w
	}
	return "the agents' memory stalls (critical)"
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
func (e *Engine) freezeUndeclared(sig slots.Signals, now time.Time, admitted bool) bool {
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
	for _, f := range fl {
		svc[f.Thread] = e.service(f.Thread)
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

// HeavyGate answers the gate hook for a Bash command (V0.209): it admits a
// heavy command by itself, so neither the agent nor the owner manages anything.
// "" lets the command run; else the host has no room, the thread now waits in
// the queue, and the reason says so. A learned command never waits: its pattern
// is a best guess, so it takes a free share or simply runs.
func (e *Engine) HeavyGate(c HeavyCall) string {
	if c.Class == "" || e.Slots == nil || e.Slots.Off() {
		return ""
	}
	soft := e.HeavySoft != nil && e.HeavySoft(c.Class)
	e.heavyMu.Lock()
	e.heavyClass[c.Thread] = c.Class
	e.heavyMu.Unlock()
	// V0.rerun-classes: a job Fyr killed to free memory is admitted again,
	// never just run, so its rerun waits until memory is fine and it fits.
	e.mu.Lock()
	entry, rerun := e.rerunWait[c.Thread]
	e.mu.Unlock()
	if rerun {
		soft = false
	}
	if r := e.gateHold(c, soft); r != "" {
		return r
	}
	kind := heavy.KindOf(c.Class)
	// The admission is taken outside heavyMu (a grant calls into the engine)
	// and then checked under it: the thread's last call may just have ended.
	for range 3 {
		held, fresh := false, false
		if h, ok := e.Slots.Holding(c.Thread); ok {
			held = true
			// V0.admission-tidy: a heavier command than the hold was admitted
			// for is admitted again for the difference (a learned one never
			// waits); a cheaper one runs.
			if !soft && !h.Priority && e.heavier(c, h) {
				held, _ = e.Slots.Grow(c.Thread, kind)
				e.syncSlotQueue()
				if held {
					e.heavyMu.Lock()
					e.heavyGranted[c.Thread] = grantedClass{c.Class, h.Since}
					e.heavyMu.Unlock()
				}
			}
		} else if soft {
			held = e.Slots.TryAcquire(c.Thread, kind)
			fresh = held
		} else {
			held, _ = e.Slots.Acquire(c.Thread, kind)
			fresh = held
			e.syncSlotQueue()
		}
		if !held {
			break
		}
		if fresh {
			e.noteGrant(c)
		}
		if e.trackHeavy(c) {
			if rerun {
				e.mu.Lock()
				delete(e.rerunWait, c.Thread)
				e.mu.Unlock()
				log.Printf("guard: rerun admitted thread=%s class=%q", c.Thread, entry)
			}
			return ""
		}
	}
	if soft {
		return ""
	}
	s := e.Slots.Snapshot()
	for i, w := range s.Waiting {
		if w.Owner == c.Thread {
			if e.HeavyRefused != nil {
				e.HeavyRefused(c.Class) // an added pattern counts its refusals
			}
			why := ""
			if i == 0 && s.Why != "" {
				why = " Waiting: " + s.Why + "."
			}
			head := fmt.Sprintf("Heavy job (%s): %s has no room for it yet, so it is queued (%d ahead; it needs %s).%s Do not work around this.", c.Class, e.hostName(), i, strings.TrimPrefix(jobShare(s, w.Owner), "≈ "), why)
			if c.Subagent {
				// A subagent never gets the [Fyr] admission and ends its run
				// by handing back: on 2026-11-19 one sent the same queued
				// check four times.
				return head + " You are a subagent: the admission message goes to the main session, not to you. Do not run this command again; say in your report that it is queued and did not run, and hand back when the rest is done."
			}
			return head + " Fyr tells you in a [Fyr] message when it is admitted; run the command again then. Do other work meanwhile or end your turn."
		}
	}
	return "" // neither held nor queued: the pool changed under us, let it run
}

// grantedClass is the class a hold (its Since) was admitted for at the gate.
type grantedClass struct {
	class string
	since time.Time
}

// heavier says c's class needs more of the host, in either resource, than the
// class h was admitted for (V0.admission-tidy). A hold the gate did not grant
// (a queued grant, slot_acquire) is taken as granted for c's class.
func (e *Engine) heavier(c HeavyCall, h slots.Holder) bool {
	e.heavyMu.Lock()
	if e.heavyGranted == nil {
		e.heavyGranted = map[string]grantedClass{}
	}
	g, ok := e.heavyGranted[c.Thread]
	if !ok || !g.since.Equal(h.Since) || e.SlotClassNeed == nil {
		e.heavyGranted[c.Thread] = grantedClass{c.Class, h.Since}
		e.heavyMu.Unlock()
		return false
	}
	e.heavyMu.Unlock()
	if g.class == c.Class {
		return false
	}
	was, now := e.SlotClassNeed(c.Thread, g.class), e.SlotClassNeed(c.Thread, c.Class)
	return now.CPU > was.CPU+1e-9 || now.Mem > was.Mem
}

// noteGrant notes the class a new hold was admitted for and takes the job's
// baseline before the command runs (V0.admission-tidy).
func (e *Engine) noteGrant(c HeavyCall) {
	if h, ok := e.Slots.Holding(c.Thread); ok {
		e.heavyMu.Lock()
		if e.heavyGranted == nil {
			e.heavyGranted = map[string]grantedClass{}
		}
		e.heavyGranted[c.Thread] = grantedClass{c.Class, h.Since}
		e.heavyMu.Unlock()
	}
	if e.SlotBaseline != nil {
		e.SlotBaseline(c.Thread)
	}
}

// gateHold holds a newly matched heavy command at the gate while the memory
// guard says memory is short (V0.guard-wake, decision 0036 E1): a command
// that has not started has no timer running, so holding it costs less than
// freezing a job that runs. The pool already queues a thread that holds no
// admission (memory short holds every job); this holds the rest: a thread
// that already holds one, and a learned command, which never queues. The
// thread is told when memory has recovered. "" lets the gate go on.
func (e *Engine) gateHold(c HeavyCall, soft bool) string {
	e.mu.Lock()
	short := e.guardShort
	e.mu.Unlock()
	if !short {
		return ""
	}
	h, holds := e.Slots.Holding(c.Thread)
	if (holds && h.Priority) || (!holds && !soft) {
		return ""
	}
	e.mu.Lock()
	if e.gateHeld == nil {
		e.gateHeld = map[string]bool{}
	}
	e.gateHeld[c.Thread] = true
	e.mu.Unlock()
	e.guardOnce(e.Now(), "gate:"+c.Thread, fmt.Sprintf("held at the gate thread=%s class=%q admitted=%t learned=%t: memory is short; a new heavy command waits before a running one is frozen", c.Thread, c.Class, holds, soft))
	head := fmt.Sprintf("Heavy job (%s): memory is short on %s, so Fyr holds new heavy commands until it recovers instead of pausing ones that run. It did not run. Do not work around this.", c.Class, e.hostName())
	if c.Subagent {
		return head + " You are a subagent: the message that memory has recovered goes to the main session, not to you. Do not run this command again; say in your report that it was held and did not run, and hand back when the rest is done."
	}
	return head + " Fyr tells you in a [Fyr] message when memory has recovered; run the command again then. Do other work meanwhile or end your turn."
}

// trackHeavy notes what carries the thread's admission; false when the
// admission is gone by now.
func (e *Engine) trackHeavy(c HeavyCall) bool {
	e.heavyMu.Lock()
	defer e.heavyMu.Unlock()
	if _, ok := e.Slots.Holding(c.Thread); !ok {
		return false
	}
	if c.Background || c.ToolUse == "" {
		e.heavyBg[c.Thread] = true // only its CPU can say when it ends
		return true
	}
	if e.heavyCalls[c.Thread] == nil {
		e.heavyCalls[c.Thread] = map[string]bool{}
	}
	e.heavyCalls[c.Thread][c.ToolUse] = true
	e.publishOpenLocked()
	return true
}

// publishOpenLocked publishes the threads with a heavy call open; call it
// with heavyMu held after heavyCalls changed.
func (e *Engine) publishOpenLocked() {
	open := make(map[string]bool, len(e.heavyCalls))
	for th, c := range e.heavyCalls {
		if len(c) > 0 {
			open[th] = true
		}
	}
	e.heavyOpen.Store(&open)
}

// HeavyOpen says thread has a heavy foreground call open. It takes no lock:
// admission asks it under the pool's lock (V0.admission-tidy).
func (e *Engine) HeavyOpen(thread string) bool {
	m := e.heavyOpen.Load()
	return m != nil && (*m)[thread]
}

// HeavyDone hears that a tool call ended (V0.209). When it was the last heavy
// command carrying the thread's admission, the share goes back at once.
func (e *Engine) HeavyDone(thread, toolUse string) {
	if e.Slots == nil {
		return
	}
	e.heavyMu.Lock()
	calls := e.heavyCalls[thread]
	if !calls[toolUse] {
		e.heavyMu.Unlock()
		return
	}
	delete(calls, toolUse)
	gave := false
	if len(calls) == 0 {
		delete(e.heavyCalls, thread)
		e.publishOpenLocked()
		if !e.heavyBg[thread] {
			if h, ok := e.Slots.Holding(thread); ok && !h.Priority {
				gave = e.Slots.Take(thread) // under heavyMu, so no new call slips in
			}
		}
	}
	e.heavyMu.Unlock()
	if !gave {
		return
	}
	e.mu.Lock()
	delete(e.slotIdle, thread)
	e.mu.Unlock()
	e.Slots.Fill()
	e.syncSlotQueue()
}

// hostName is how the heavy-job answers name this host (V0.163).
func (e *Engine) hostName() string {
	if e.HostName != "" {
		return e.HostName
	}
	return "this host"
}

// jobShare is a job's share of the host in words (V0.163): CPU and memory
// apart, or one share on a host that counts jobs.
func jobShare(s slots.Snapshot, owner string) string {
	for _, h := range append(s.Held, s.Waiting...) {
		if h.Owner == owner {
			if h.Need != nil {
				return fmt.Sprintf("≈ %.0f %% CPU, %.0f %% memory", h.CPUPct, h.MemPct)
			}
			return fmt.Sprintf("≈ %.0f %%", h.Pct)
		}
	}
	return "its share"
}

// freeShare is what the heavy-job budget has left, in words.
func freeShare(s slots.Snapshot) string {
	if s.CPUFreePct != nil && s.MemFreePct != nil {
		return fmt.Sprintf("%.0f %% CPU and %.0f %% memory", *s.CPUFreePct, *s.MemFreePct)
	}
	if s.UsedPct != nil {
		return fmt.Sprintf("%.0f %%", max(0, 100-*s.UsedPct))
	}
	return "room"
}

func article(kind string) string {
	if kind != "" && slices.Contains([]byte("aeiou"), kind[0]) {
		return "an " + kind
	}
	return "a " + kind
}

// slotTools are slot_acquire and slot_release (V0.86 (2)): both answer at
// once; a queued thread hears later, in a [Fyr] notice.
func (e *Engine) slotTools(s *mcp.Server) {
	// V0.209: Fyr admits and releases by itself at the gate; these two remain
	// for running agents that were told about them, and to name a job early.
	off := "This host does not limit heavy jobs; run the job."
	s.Add(mcp.Tool{Name: "slot_acquire",
		Description: "Not needed: Fyr admits a heavy command by itself when you run it. Only for work Fyr cannot recognise as heavy: asks for a share of this host now, and answers admitted or queued.",
		Schema: obj(map[string]any{"kind": map[string]any{"type": "string", "enum": slots.Kinds()},
			"command": map[string]any{"type": "string", "description": "The command you will run, e.g. \"make test\"; Fyr then knows what the job costs."}}, "kind"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			kind := sArg(a, "kind")
			if !slices.Contains(slots.Kinds(), kind) {
				return "", fmt.Errorf("kind must be one of %v", slots.Kinds())
			}
			if c := sArg(a, "command"); c != "" && e.SlotJob != nil {
				e.SlotJob(th, c) // decision 0029 S3: the job's class before it runs
			}
			if e.Slots == nil {
				return off, nil
			}
			if e.Slots.Off() {
				return off, nil
			}
			held, ahead := e.Slots.Acquire(th, kind)
			e.syncSlotQueue()
			s := e.Slots.Snapshot()
			if held {
				return fmt.Sprintf("Admitted: your %s job %s of %s; %s left. Run the job now; the admission ends by itself when the work stops.", kind, jobShare(s, th), e.hostName(), freeShare(s)), nil
			}
			why := ""
			if ahead == 0 && s.Why != "" {
				why = " Waiting: " + s.Why + "." // decision 0029 S2
			}
			return fmt.Sprintf("Queued: your %s job needs %s of %s; %s free (%d ahead).%s Do not start the job; Fyr tells you in a [Fyr] message when it is admitted. Do other work meanwhile or end your turn.", kind, strings.TrimPrefix(jobShare(s, th), "≈ "), e.hostName(), freeShare(s), ahead, why), nil
		}})
	s.Add(mcp.Tool{Name: "slot_release",
		Description: "Not needed: an admission ends by itself. Gives it back early, or leaves the queue.",
		Schema:      obj(map[string]any{}),
		Call: func(_ context.Context, th string, _ map[string]any) (string, error) {
			if e.Slots == nil {
				return off, nil
			}
			if !e.dropSlot(th) { // V0.209: it usually ended by itself already
				return "Nothing to release: Fyr already ended this admission.", nil
			}
			e.syncSlotQueue()
			return fmt.Sprintf("Released: %s of %s free for heavy jobs.", freeShare(e.Slots.Snapshot()), e.hostName()), nil
		}})
}
