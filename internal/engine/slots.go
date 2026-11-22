package engine

import (
	"fmt"
	"log"
	"maps"
	"time"

	"github.com/meros/fyr/internal/core"
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
	delete(e.jobThawed, id)
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
		// V0.sim-fixes (1): a job the guard holds frozen does not move its
		// SlotActive, so its hold read as idle: a foreground call went stale at
		// HeavyCallStale, a background job at SlotIdle, and after a restart any
		// frozen job at SlotIdle (heavyCalls is empty). The guard then thawed it
		// as "episode or hold ended" into the critical stall it was frozen for.
		// A frozen hold is not swept, and its idle time starts at its thaw.
		t := e.threads[h.Owner]
		if t != nil && e.frozenJobOf(h.Owner) {
			delete(e.slotIdle, h.Owner)
			continue
		}
		if at, ok := e.jobThawed[h.Owner]; ok {
			if a, ok := active[h.Owner]; ok && a.Before(at) {
				active[h.Owner] = at
			}
			if since, ok := e.slotIdle[h.Owner]; ok && since.Before(at) {
				e.slotIdle[h.Owner] = at
			}
		}
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
	e.checkSlotQueueInvariant() // V0.thread-invariants
	e.heavierThanPredicted(s)
	e.freezeGuard(s)
	e.memoryGuard(s)    // V0.guard-tidy: after the freeze, which told its thread already
	e.checkInvariants() // V0.load-invariants
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
	e.guardOnce(e.Now(), "psi", "PSI trigger: the agents' memory stall crossed 200 ms in 2 s (critical); the guard runs now")
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
