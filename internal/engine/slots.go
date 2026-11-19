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
	e.mu.Unlock()
	e.heavyMu.Lock()
	e.heavyCalls, e.heavyBg = map[string]map[string]bool{}, map[string]bool{}
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
	e.memoryGuard(s)
	e.heavierThanPredicted(s)
	e.freezeGuard(s)
	e.publishGuard(e.Now())
	e.adaptBudget(s)
	// Decision 0029 S2: the conditions change without a release.
	e.Slots.Fill()
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
	f, why := e.SlotAIMD.Step(e.Now(), sig, len(s.Waiting) > 0, e.Slots.LastGrant(), s.Binding)
	old := e.Slots.Budget()
	if f != old || (s.Budget != nil && s.BudgetWhy != why) {
		if f != old { // V0.load-tuning: the budget's moves can be read back
			log.Printf("slots: budget cpu=%.3f->%.3f mem=%.3f->%.3f why=%q waiting=%d held=%d %s", old.CPU, f.CPU, old.Mem, f.Mem, why, len(s.Waiting), len(s.Held), guardSignals(sig))
		}
		e.Slots.SetBudget(f, why)
	}
}

// frozenJob is the job the memory guard froze (decision 0029 S5).
type frozenJob struct {
	owner, since string // the hold: owner and its grant
	at, okSince  time.Time
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
}

// Decision 0029 S5: how long memory must be fine before a frozen job thaws,
// and the longest a job stays frozen.
const (
	ThawAfterOK = 30 * time.Second
	FreezeMax   = 10 * time.Minute
)

// Decision 0032 slice 1: one long episode of undeclared work can be frozen
// again after a thaw (the freeze is a time slice, not a once-only verdict),
// once the work has run RefreezeAfter, and at most MaxFreezes times, so a
// job that never settles is let run in the end instead of flapping.
const (
	RefreezeAfter = 60 * time.Second
	MaxFreezes    = 8
)

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

// GuardView is the guard's last published view (nil before the first tick).
// It waits for nothing: the pool reads it under its own lock.
func (e *Engine) GuardView() *slots.Guard { return e.guardPub.Load() }

// publishGuard builds the view at the end of a tick.
func (e *Engine) publishGuard(now time.Time) {
	e.mu.Lock()
	g := &slots.Guard{Short: e.guardShort, Max: MaxFreezes, RefreezeSec: int(RefreezeAfter / time.Second),
		Events: append([]slots.GuardEvent{}, e.guardEvents...)} // never nil: the front gets [], not null
	if e.guardShort && !e.guardSince.IsZero() {
		t := e.guardSince
		g.ShortSince = &t
	}
	for thread, u := range e.undFroze {
		t := slots.GuardThread{Thread: thread, N: u.n, Capped: u.n >= MaxFreezes}
		if e.frozen.undeclared && e.frozen.owner == thread {
			t.Frozen = true
			at := e.frozen.at
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

// freezeVictim is the job to freeze: of the holds not frozen yet, the one whose
// memory grew most (V0.load-tuning; freezing a job that uses none frees nothing
// and stops nothing), the newest of equals. Without a reading of growth it is
// the newest, and then never the oldest hold: one job always runs.
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
		if h.Priority || done {
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
	if !found || (bestGrew == 0 && best.Owner == oldest.Owner) {
		return slots.Holder{}, false
	}
	return best, true
}

// freezeGuard freezes the slotted job that grew most while memory is short
// (freezeVictim; one at a time, each hold at most once, never the last) and
// thaws it when memory has been fine for ThawAfterOK, after FreezeMax, or
// when its slot went back. Freeze and Thaw run outside e.mu.
func (e *Engine) freezeGuard(s slots.Snapshot) {
	if e.SlotFreeze == nil || e.SlotThaw == nil || e.SlotSignals == nil {
		return
	}
	sig := e.SlotSignals()
	short := sig.Provider != "" && ((sig.Headroom != nil && *sig.Headroom < e.SlotPolicy.MemoryReserve) || sig.Stall == slots.StallCritical)
	fine := sig.Stall != slots.StallCritical && sig.Stall != slots.StallElevated && (sig.Headroom == nil || *sig.Headroom >= e.SlotPolicy.MemoryReserve+0.05)
	now := e.Now()
	e.mu.Lock()
	if e.frozeHolds == nil {
		e.frozeHolds = map[string]time.Time{}
	}
	f := e.frozen
	wasShort := e.guardShort
	e.guardShort = short
	if short && !wasShort {
		e.guardSince = now
	}
	e.mu.Unlock()
	if short != wasShort {
		state := "recovered"
		if short {
			state = "short"
		}
		e.guardRecord(slots.GuardEvent{At: now, Kind: state}, sig)
		log.Printf("guard: memory %s: %s flagged=%d held=%d running=%d frozen=%q", state, guardSignals(sig), len(e.flags()), len(s.Held), len(e.Running()), f.owner)
	}
	if f.owner != "" {
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
		if fine && f.okSince.IsZero() {
			f.okSince = now
		} else if !fine {
			f.okSince = time.Time{}
		}
		// A freeze whose cgroup holds no process any more (the work ended or
		// was killed) holds nothing; release it at once instead of letting it
		// block every other freeze for ten minutes.
		emptied := f.undeclared && holding && e.UndeclaredEmpty != nil && e.UndeclaredEmpty(f.owner)
		thaw := !holding || emptied || now.Sub(f.at) >= FreezeMax || (!f.okSince.IsZero() && now.Sub(f.okSince) >= ThawAfterOK)
		if !thaw {
			f = e.nextTurn(f, now)
		}
		e.mu.Lock()
		if thaw {
			e.frozen = frozenJob{}
			if f.undeclared {
				u := e.undFroze[f.owner]
				u.thawedAt = now
				e.undFroze[f.owner] = u
			}
		} else {
			e.frozen = f
		}
		e.mu.Unlock()
		if thaw {
			reason := "memory fine for 30 s"
			switch {
			case emptied:
				reason = "its processes are gone"
			case !holding:
				reason = "episode or hold ended"
			case now.Sub(f.at) >= FreezeMax:
				reason = "10 min limit"
			}
			kind := "job"
			work, why := "job", "memory"
			if f.undeclared {
				kind, work = "undeclared work", "undeclared"
			}
			switch {
			case emptied:
				why = "gone"
			case !holding:
				why = "ended"
			case now.Sub(f.at) >= FreezeMax:
				why = "limit"
			}
			e.guardRecord(slots.GuardEvent{At: now, Kind: "thaw", Work: work, Thread: f.owner, Reason: why}, sig)
			log.Printf("guard: thawed %s thread=%s after=%s reason=%q %s", kind, f.owner, now.Sub(f.at).Round(time.Second), reason, guardSignals(sig))
			if f.undeclared {
				e.UndeclaredThaw(f.owner)
			} else {
				e.SlotThaw(f.owner)
			}
			if holding {
				if f.undeclared {
					e.slotNotice(f.owner, map[string]any{"resumed": true, "work": true})
				} else {
					e.slotNotice(f.owner, map[string]any{"resumed": true})
				}
			}
		}
		return
	}
	if !short {
		return
	}
	if e.freezeUndeclared(sig, now, len(s.Held) > 0) {
		return
	}
	if len(s.Held) < 2 {
		return // one job always runs
	}
	victim, ok := e.freezeVictim(s.Held)
	if !ok || !e.SlotFreeze(victim.Owner) {
		return
	}
	why := "memory stalls (critical)"
	if sig.Headroom != nil && *sig.Headroom < e.SlotPolicy.MemoryReserve {
		why = fmt.Sprintf("memory: %.0f %% free", *sig.Headroom*100)
	}
	e.mu.Lock()
	e.frozen = frozenJob{owner: victim.Owner, since: victim.Since.Format(time.RFC3339Nano), at: now}
	e.frozeHolds[victim.Owner] = victim.Since
	e.mu.Unlock()
	log.Printf("guard: froze job thread=%s why=%q held=%d %s", victim.Owner, why, len(s.Held), guardSignals(sig))
	e.guardRecord(slots.GuardEvent{At: now, Kind: "freeze", Work: "job", Thread: victim.Owner}, sig)
	e.slotNotice(victim.Owner, map[string]any{"paused": why})
}

// flags are the threads that run undeclared heavy work now.
func (e *Engine) flags() []slots.Undeclared {
	if e.Undeclared == nil {
		return nil
	}
	return e.Undeclared()
}

// freezeUndeclared freezes the newest flagged thread whose memory is over
// (V0.164 section 7), before any job that asked: one at a time on the host.
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
	var skips [][2]string // key, line: logged after the lock
	e.mu.Lock()
	for i := range fl {
		f := &fl[i]
		if !f.MemOver || e.threads[f.Thread] == nil {
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
	first, units, turns := e.startTurns(pick.Thread)
	if !turns && !e.UndeclaredFreeze(pick.Thread) {
		e.guardOnce(now, "skip:"+pick.Thread, fmt.Sprintf("could not freeze thread=%s: no process to move or the cgroup refused", pick.Thread))
		return false
	}
	why := "memory stalls (critical)"
	if sig.Headroom != nil && *sig.Headroom < e.SlotPolicy.MemoryReserve {
		why = fmt.Sprintf("memory: %.0f %% free", *sig.Headroom*100)
	}
	n := 1
	if picked.episode.Equal(pick.Since) {
		n = picked.n + 1
	}
	faults := e.majorFaults(pick.Thread)
	e.mu.Lock()
	e.frozen = frozenJob{owner: pick.Thread, at: now, undeclared: true, episode: pick.Since}
	if turns {
		e.frozen.active, e.frozen.turnAt, e.frozen.turn, e.frozen.faults = first.PID, now, TurnMin, faults
	}
	e.undFroze[pick.Thread] = undFreeze{episode: pick.Since, n: n}
	e.mu.Unlock()
	if turns {
		log.Printf("guard: taking turns thread=%s units=%d first=%d cmd=%q mem=%s", pick.Thread, units, first.PID, first.Command, slots.GB(float64(first.RSS)))
	}
	cmd := pick.Command
	if cmd == "" {
		cmd = "unknown command"
	}
	log.Printf("guard: froze undeclared work thread=%s freeze=%d/%d episode_age=%s cmd=%q mem=%s cores=%.1f %s flagged=%d admitted=%t others_running=%d",
		pick.Thread, n, MaxFreezes, now.Sub(pick.Since).Round(time.Second), cmd, slots.GB(float64(pick.Mem)), pick.CPU, guardSignals(sig), len(fl), admitted, others(pick.Thread))
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
	if sig.Provider != "" && sig.Headroom != nil && *sig.Headroom < e.SlotPolicy.MemoryReserve {
		why = fmt.Sprintf("memory: %.0f %% free", *sig.Headroom*100)
	} else if sig.Stall == slots.StallCritical {
		why = "memory stalls (critical)"
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
	kind := heavy.KindOf(c.Class)
	// The admission is taken outside heavyMu (a grant calls into the engine)
	// and then checked under it: the thread's last call may just have ended.
	for range 3 {
		held := false
		if _, ok := e.Slots.Holding(c.Thread); ok {
			held = true
		} else if soft {
			held = e.Slots.TryAcquire(c.Thread, kind)
		} else {
			held, _ = e.Slots.Acquire(c.Thread, kind)
			e.syncSlotQueue()
		}
		if !held {
			break
		}
		if e.trackHeavy(c) {
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
	return true
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
