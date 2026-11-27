package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

// HeavyCall is a heavy command at the gate hook (V0.209). ToolUse is its call;
// Background: the job outlives the call.
type HeavyCall struct {
	Thread, Class, ToolUse string
	Background             bool
	// Subagent: a subagent made the call; Agent and AgentType name it as the
	// hook input does. Its admission goes to its next tool call while it
	// runs, else to the thread's agent naming it (V0.subagent-admission).
	Subagent         bool
	Agent, AgentType string
}

// UseSlots gives the engine the host's pool of slots for heavy jobs
// (V0.86): the agent's slot tools, the notice when a queued slot is the
// thread's, and the release when its run ends.
func (e *Engine) UseSlots(p *slots.Pool) {
	e.mu.Lock()
	e.Slots = p
	e.slotIdle = map[string]time.Time{}
	e.gs.UndFroze = map[string]guard.UndFreeze{}
	e.guardLast = map[string]guardLine{}
	e.rerunWait = map[string]string{}
	// V0.survive-update: Load read the rerun kills from the store before
	// fyrd wires the pool, so they are kept here, not made anew (a new map
	// wiped them on every start); the holds already told come back too.
	if e.gs.RerunKills == nil {
		e.gs.RerunKills = map[string][]time.Time{}
	}
	e.loadSlotOverToldLocked()
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
	// V0.subagent-admission: a running subagent that was refused the job
	// hears it at its next tool call; the thread's agent is not woken.
	by, kept := e.routeToSubagent(owner, subWake{kind: kind})
	if kept {
		return
	}
	if err := e.applySlotGranted(owner, core.SlotGrantedArgs{Kind: kind, Agent: by.ID, AgentType: by.Type}); err != nil {
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
// guard.SlotIdle without a live run (guard.Sweep decides); the tick calls it.
func (e *Engine) sweepSlots() {
	if e.Slots == nil {
		return
	}
	s := e.Slots.Snapshot()
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
	in := guard.SweepIn{Now: e.Now(), Held: s.Held, Waiting: s.Waiting, Threads: map[string]guard.SweepThread{},
		Active: active, Calls: calls, Background: background, Frozen: e.gs.Frozen, Thawed: e.jobThawed, Idle: e.slotIdle}
	for id, t := range e.threads {
		in.Threads[id] = guard.SweepThread{Running: t.Running, Admitted: t.RunsAdmittedWork()}
	}
	drop, idle := guard.Sweep(in)
	e.slotIdle = idle
	e.mu.Unlock()
	for _, id := range drop {
		e.dropSlot(id)
	}
	e.sweepSubWakes() // V0.subagent-admission
	e.syncSlotQueue()
	e.checkSlotQueueInvariant() // V0.thread-invariants
	e.heavierThanPredicted(s)
	e.guardTick(s)      // V0.guard-tick: the freezes, then the memory notice (V0.guard-tidy: after the freeze, which told its thread already)
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
	msg := "PSI trigger: the agents' memory stall crossed 200 ms in 2 s (critical); the guard runs now"
	if e.SlotSignals != nil {
		if p := e.SlotSignals().Provider; p != "" && p != "cgroups" {
			msg = "memory stall trigger: memory stalls critically; the guard runs now"
		}
	}
	e.guardOnce(e.Now(), "psi", msg)
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
// of the budget than predicted (decision 0029 S3, guard.HeavierThanPredicted);
// nothing is stopped.
func (e *Engine) heavierThanPredicted(s resource.Snapshot) {
	if e.SlotOver == nil {
		return
	}
	e.mu.Lock()
	told := maps.Clone(e.slotOverTold)
	e.mu.Unlock()
	measure := func(owner string) guard.Over {
		class, live, pred, over := e.SlotOver(owner)
		return guard.Over{Class: class, Live: live, Predicted: pred, Over: over}
	}
	for _, n := range guard.HeavierThanPredicted(s.Held, told, measure) {
		e.mu.Lock()
		for _, h := range s.Held {
			if h.Owner == n.Thread {
				e.slotOverTold[h.Owner] = h.Since
			}
		}
		e.saveSlotOverToldLocked(s.Held)
		e.mu.Unlock()
		// V0.163: SlotOver answers in percent of the host.
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotOver, Thread: n.Thread, Args: core.SlotOverArgs{Class: n.Class, Live: n.Live, Predicted: n.Predicted}}); err != nil {
			log.Printf("slot over %s: %v", n.Thread, err)
		}
	}
}

// adaptBudget steps the budget controller and sets the pool's budget
// (decision 0029 S4, guard.Budget), outside e.mu.
func (e *Engine) adaptBudget(s resource.Snapshot) {
	if e.SlotAIMD == nil || e.SlotSignals == nil {
		return
	}
	sig := e.SlotSignals()
	m := guard.Budget(e.SlotAIMD, guard.BudgetIn{Now: e.Now(), Snapshot: s, Old: e.Slots.Budget(), LastGrant: e.Slots.LastGrant(), Signals: sig})
	if m.Log != "" { // V0.load-tuning: the budget's moves can be read back
		log.Print(m.Log)
	}
	if m.Set {
		e.Slots.SetBudget(m.F, m.Why)
	}
}

// slotOverToldKey keeps the holds already told they are heavier than
// predicted (V0.survive-update), so a restart does not tell one again.
const slotOverToldKey = "engine.slotOverTold"

func (e *Engine) loadSlotOverToldLocked() {
	e.slotOverTold = map[string]time.Time{}
	if e.st == nil {
		return
	}
	if raw, _ := e.st.GetKV(slotOverToldKey); raw != "" {
		json.Unmarshal([]byte(raw), &e.slotOverTold)
	}
}

// saveSlotOverToldLocked writes the told holds that still hold a slot: an
// entry ends with its hold, so a hold that ended is dropped here.
func (e *Engine) saveSlotOverToldLocked(held []resource.Holder) {
	if e.st == nil {
		return
	}
	live := map[string]time.Time{}
	for _, h := range held {
		live[h.Owner] = h.Since
	}
	out := map[string]time.Time{}
	for owner, since := range e.slotOverTold {
		if s, ok := live[owner]; ok && s.Equal(since) {
			out[owner] = since
		} else {
			delete(e.slotOverTold, owner)
		}
	}
	if len(out) == 0 {
		e.st.SetKV(slotOverToldKey, "")
		return
	}
	if b, err := json.Marshal(out); err == nil {
		e.st.SetKV(slotOverToldKey, string(b))
	}
}
