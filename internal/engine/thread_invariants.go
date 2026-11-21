package engine

// V0.thread-invariants: runtime, transition, temporal, and process-consistency
// invariants for threads, checked on every Engine.apply, after Engine.Load,
// on Engine.Tick, and after slot-queue sync. Pure state and transition
// invariants live in internal/core/thread_invariants.go; temporal and
// cross-component invariants live here. Violations share V0.load-invariants'
// log line, kv persistence, Needs-you card, Dismiss, Fix it, and Settings list.

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
)

// Engine/runtime thread invariant IDs (stable: logs, cards, fix threads).
const (
	InvThreadRestartLiveWork    = "thread-restart-left-live-work"
	InvThreadStartingStuck      = "thread-starting-stuck"
	InvThreadHandoverStuck      = "thread-handover-stuck"
	InvThreadMachineWaitStuck   = "thread-machine-wait-stuck"
	InvThreadSettledStillRuns   = "thread-settled-still-running"
	InvThreadOutboxStuck        = "thread-outbox-stuck"
	InvThreadWakingFeedStalled  = "thread-waking-feed-stalled"
	InvThreadRuleOverdue        = "thread-rule-overdue"
	InvThreadActionRunStuck     = "thread-action-run-stuck"
	InvThreadAttentionDrift     = "thread-attention-drift"
	InvThreadSlotQueueDrift     = "thread-slot-queue-drift"
	InvThreadPhantomRunning     = "thread-phantom-running"
	InvThreadGhostRunning       = "thread-ghost-running"
	InvThreadPhantomTasks       = "thread-phantom-tasks"
	InvThreadPhantomDecision    = "thread-phantom-decision"
	InvThreadPhantomOutbox      = "thread-phantom-outbox"
)

// Thresholds for temporal and process-consistency thread invariants.
const (
	// InvStartingStuck: Starting without a machine wait longer than this.
	InvStartingStuck = 5 * time.Minute
	// InvHandoverStuck: Handover in progress longer than this.
	InvHandoverStuck = 15 * time.Minute
	// InvMachineWaitStuck: MachineWait longer than this (past PressureMaxWait).
	InvMachineWaitStuck = 20 * time.Minute
	// InvSettledRunningGrace: a settled thread still running past this after Settle.At.
	InvSettledRunningGrace = 2 * time.Minute
	// InvOutboxStuck: unread messages sitting in an idle thread's outbox longer than this.
	InvOutboxStuck = 2 * time.Minute
	// InvFeedStuck: a wakeable idle thread with a waking notice within budget longer than this.
	InvFeedStuck = 2 * time.Minute
	// InvActionRunStuck: a pressed action run still marked running past this (maxScriptSec is 1h).
	InvActionRunStuck = 2 * time.Hour
	// InvProcessDriftGrace: engine vs agent process mismatch lasting longer than this.
	InvProcessDriftGrace = 30 * time.Second
	// invThreadRingKeep: how many recent transitions to retain per thread for card evidence.
	invThreadRingKeep = 30
)

// engineThreadInvTexts describes each engine/runtime thread invariant in one sentence.
var engineThreadInvTexts = map[string]string{
	InvThreadRestartLiveWork:   "After host restart recovery, a thread still had live process state from before the restart.",
	InvThreadStartingStuck:     "A thread stayed in Starting without a machine wait for more than 5 minutes.",
	InvThreadHandoverStuck:     "A handover stayed in progress for more than 15 minutes.",
	InvThreadMachineWaitStuck:  "A thread waited for the machine for more than 20 minutes (past PressureMaxWait).",
	InvThreadSettledStillRuns:  "A settled thread stayed running for more than 2 minutes after settling.",
	InvThreadOutboxStuck:       "A thread stayed idle with unread messages in its outbox for more than 2 minutes.",
	InvThreadWakingFeedStalled: "A wakeable idle thread had a waking notice within budget that did not start a turn for more than 2 minutes.",
	InvThreadRuleOverdue:       "A thread's armed rule held without firing after the clock pass.",
	InvThreadActionRunStuck:    "A pressed action run stayed marked running for more than 2 hours.",
	InvThreadAttentionDrift:    "The host's last broadcast attention state disagreed with the thread's computed attention.",
	InvThreadSlotQueueDrift:    "A thread's SlotQueued flag disagreed with the host's heavy-job queue after sync.",
	InvThreadPhantomRunning:    "A thread is marked Running, but the agent manager has no live turn for it for more than 30 seconds.",
	InvThreadGhostRunning:      "The agent manager has a live turn for a thread that the engine marks as not Running for more than 30 seconds.",
	InvThreadPhantomTasks:      "A thread has running background tasks, but no live agent process exists for more than 30 seconds.",
	InvThreadPhantomDecision:   "A thread has an open process-bound question or approval that the agent manager is not waiting on for more than 30 seconds.",
	InvThreadPhantomOutbox:     "A thread has unread messages in its outbox, but no agent process is live or starting for more than 30 seconds.",
}

// engineThreadInvOrder is the display order of the engine/runtime thread invariants.
var engineThreadInvOrder = []string{
	InvThreadRestartLiveWork,
	InvThreadStartingStuck,
	InvThreadHandoverStuck,
	InvThreadMachineWaitStuck,
	InvThreadSettledStillRuns,
	InvThreadOutboxStuck,
	InvThreadWakingFeedStalled,
	InvThreadRuleOverdue,
	InvThreadActionRunStuck,
	InvThreadAttentionDrift,
	InvThreadSlotQueueDrift,
	InvThreadPhantomRunning,
	InvThreadGhostRunning,
	InvThreadPhantomTasks,
	InvThreadPhantomDecision,
	InvThreadPhantomOutbox,
}

// threadInvOrder is every thread invariant in display order (core pure + engine runtime).
var threadInvOrder = append(append([]string(nil), core.CoreThreadInvariantOrder...), engineThreadInvOrder...)

// threadInvText returns the one-sentence description of a thread invariant, or "".
func threadInvText(id string) string {
	if s, ok := core.CoreThreadInvariantTexts[id]; ok {
		return s
	}
	return engineThreadInvTexts[id]
}

// recordThreadTransition appends a compact trace line for thread id into its
// in-memory ring buffer so a broken thread invariant card shows what led up to it.
func (e *Engine) recordThreadTransition(threadID string, at time.Time, line string) {
	if threadID == "" {
		return
	}
	c := &e.inv
	c.mu.Lock()
	if c.threadRing == nil {
		c.threadRing = map[string][]string{}
	}
	ls := append(c.threadRing[threadID], at.Format("15:04:05")+" "+line)
	if len(ls) > invThreadRingKeep {
		ls = append([]string(nil), ls[len(ls)-invThreadRingKeep:]...)
	}
	c.threadRing[threadID] = ls
	c.mu.Unlock()
}

// threadLines returns a copy of threadID's recent transition lines.
func (e *Engine) threadLines(threadID string) []string {
	if threadID == "" {
		return nil
	}
	c := &e.inv
	c.mu.Lock()
	defer c.mu.Unlock()
	if ls := c.threadRing[threadID]; len(ls) > 0 {
		return append([]string(nil), ls...)
	}
	return nil
}

// checkThreadApplied checks pure state, transition, and broadcast-sync
// invariants after one Engine.apply commit. Call it without e.mu held.
func (e *Engine) checkThreadApplied(s *staged, autoMerge map[string]bool, lastAfter core.State, hasLast bool) {
	if s == nil || s.t == nil {
		return
	}
	on := autoMergeFunc(autoMerge)
	beforeAtt := core.ComputeWith(s.cur, s.prevView, s.now, on)
	afterAtt := core.ComputeWith(s.t, s.v, s.now, on)

	var evNames []string
	for _, ev := range s.res.Events {
		evNames = append(evNames, ev.Type)
	}
	evSummary := ""
	if len(evNames) > 0 {
		evSummary = " events=" + strings.Join(evNames, ",")
	}
	e.recordThreadTransition(s.t.ID, s.now, fmt.Sprintf("%s (%s) %s→%s running=%t tasks=%d dec=%d out=%d settled=%t archived=%t%s",
		s.in.Kind, s.in.Source, beforeAtt.State, afterAtt.State,
		s.t.Running, s.t.RunningTasks(), len(s.t.Decisions), len(s.t.Outbox),
		s.t.Settle.At != nil, s.t.ArchivedAt != nil, evSummary))

	var vs []violation
	for _, v := range core.CheckTransition(s.cur, s.prevView, s.t, s.v, s.in, s.res, s.now, on) {
		vs = append(vs, violation{id: v.ID, evidence: v.Evidence, thread: s.t.ID})
	}
	for _, v := range core.CheckThread(s.t, s.v, s.now, on) {
		vs = append(vs, violation{id: v.ID, evidence: fmt.Sprintf("after=%s %s", s.in.Kind, v.Evidence), thread: s.t.ID})
	}
	if hasLast && lastAfter != afterAtt.State {
		vs = append(vs, violation{
			id:       InvThreadAttentionDrift,
			evidence: fmt.Sprintf("thread=%s after=%s last=%s computed=%s feedOnly=%t", s.t.ID, s.in.Kind, lastAfter, afterAtt.State, s.feedOnly),
			thread:   s.t.ID,
		})
	}
	e.reportThreadViolations(vs, s.now)
}

// checkThreadLoad checks post-restart recovery invariants right after
// Engine.Load finishes its host.restarted pass. Call it without e.mu held.
func (e *Engine) checkThreadLoad() {
	e.mu.Lock()
	now := e.Now()
	autoMerge := maps.Clone(e.autoMerge)
	kept := maps.Clone(e.Kept)
	type item struct {
		t *core.Thread
		v *core.View
	}
	items := make([]item, 0, len(e.threads))
	for id, t := range e.threads {
		items = append(items, item{core.CloneThread(t), cloneViewCopy(e.views[id], id)})
	}
	e.mu.Unlock()

	on := autoMergeFunc(autoMerge)
	var vs []violation
	for _, it := range items {
		t := it.t
		var probs []string
		if t.Starting != nil {
			probs = append(probs, "starting!=nil")
		}
		if t.MachineWait != nil {
			probs = append(probs, "machineWait!=nil")
		}
		if t.Handover != nil {
			probs = append(probs, "handover!=nil")
		}
		if !kept[t.ID] {
			if t.Running {
				probs = append(probs, "running=true")
			}
			if n := t.RunningTasks(); n > 0 {
				probs = append(probs, fmt.Sprintf("runningTasks=%d", n))
			}
			if len(t.Outbox) > 0 {
				probs = append(probs, fmt.Sprintf("outbox=%d", len(t.Outbox)))
			}
			for _, d := range t.Decisions {
				if d.NeedsProcess() {
					probs = append(probs, fmt.Sprintf("process-decision=%s", d.ID))
					break
				}
			}
		}
		if len(probs) > 0 {
			vs = append(vs, violation{
				id:       InvThreadRestartLiveWork,
				evidence: fmt.Sprintf("thread=%s kept=%t %s", t.ID, kept[t.ID], strings.Join(probs, "; ")),
				thread:   t.ID,
			})
		}
		for _, v := range core.CheckThread(t, it.v, now, on) {
			vs = append(vs, violation{id: v.ID, evidence: "at=load " + v.Evidence, thread: t.ID})
		}
	}
	e.reportThreadViolations(vs, now)
}

// checkSlotQueueInvariant checks that every thread's SlotQueued flag agrees
// with the slot pool's waiting list right after syncSlotQueue. Call without e.mu.
func (e *Engine) checkSlotQueueInvariant() {
	if e.Slots == nil {
		return
	}
	waiting := map[string]bool{}
	for _, h := range e.Slots.Snapshot().Waiting {
		waiting[h.Owner] = true
	}
	e.mu.Lock()
	now := e.Now()
	var vs []violation
	for id, t := range e.threads {
		want := waiting[id]
		if t.SlotQueued != want {
			vs = append(vs, violation{
				id:       InvThreadSlotQueueDrift,
				evidence: fmt.Sprintf("thread=%s thread.slotQueued=%t pool.waiting=%t", id, t.SlotQueued, want),
				thread:   id,
			})
		}
	}
	e.mu.Unlock()
	e.reportThreadViolations(vs, now)
}

// checkThreadTick checks temporal, overdue-rule, broadcast-drift, and
// process-consistency invariants on Engine.Tick. Call it without e.mu held.
func (e *Engine) checkThreadTick(tickNow time.Time) {
	// Read AgentInspect outside e.mu (V0.147 / V0.182 lock rule).
	var agentSnap map[string]AgentState
	hasAgentSnap := e.AgentInspect != nil
	if hasAgentSnap {
		agentSnap = e.AgentInspect()
	}
	hasAgents := e.Agents != nil

	e.mu.Lock()
	now := e.Now()
	autoMerge := maps.Clone(e.autoMerge)
	type snap struct {
		t    *core.Thread
		v    *core.View
		last core.State
		hasL bool
	}
	items := make([]snap, 0, len(e.threads))
	for id, t := range e.threads {
		l, ok := e.last[id]
		items = append(items, snap{
			t:    core.CloneThread(t),
			v:    cloneViewCopy(e.views[id], id),
			last: l,
			hasL: ok,
		})
	}
	e.mu.Unlock()

	on := autoMergeFunc(autoMerge)
	activeGrace := map[string]bool{}
	var vs []violation

	// graceCheck tracks conditions without an inherent timestamp on the thread
	// (idle outbox, agent-vs-engine drift) so a mismatch must persist past grace.
	c := &e.inv
	c.mu.Lock()
	if c.once == nil {
		c.once = map[string]time.Time{}
	}
	if c.driftSince == nil {
		c.driftSince = map[string]time.Time{}
	}
	for k, at := range c.once {
		if now.Sub(at) > InvCardEvery {
			delete(c.once, k)
		}
	}
	first := func(key string) bool {
		if _, ok := c.once[key]; ok {
			return false
		}
		c.once[key] = now
		return true
	}
	graceFired := func(key string, grace time.Duration) (time.Duration, bool) {
		activeGrace[key] = true
		since, ok := c.driftSince[key]
		if !ok {
			c.driftSince[key] = now
			return 0, false
		}
		d := now.Sub(since)
		if d > grace && first(key+"@"+since.Format(time.RFC3339Nano)) {
			return d, true
		}
		return d, false
	}

	knownThreads := map[string]bool{}
	for _, it := range items {
		t, v := it.t, it.v
		knownThreads[t.ID] = true

		// 1. Attention drift: after Tick's clock and broadcast passes, e.last
		// must match ComputeWith at tickNow (skip threads updated concurrently after tickNow).
		if it.hasL && !t.UpdatedAt.After(tickNow) {
			if got := core.ComputeWith(t, v, tickNow, on).State; got != it.last && first("attdrift:"+t.ID+":"+string(it.last)+"->"+string(got)) {
				vs = append(vs, violation{
					id:       InvThreadAttentionDrift,
					evidence: fmt.Sprintf("thread=%s last=%s computed=%s", t.ID, it.last, got),
					thread:   t.ID,
				})
			}
		}

		// 2. Overdue armed rule: after Tick's clock pass, no armed rule without
		// a future RetryAt should evaluate true with LastValue==false.
		if !t.UpdatedAt.After(tickNow) {
			if overdue := core.OverdueRule(t, v, tickNow); overdue != "" && first("overdue:"+t.ID+":"+overdue) {
				vs = append(vs, violation{
					id:       InvThreadRuleOverdue,
					evidence: fmt.Sprintf("thread=%s %s", t.ID, overdue),
					thread:   t.ID,
				})
			}
		}

		// 3. Temporal stuck states.
		if hasAgents && t.Starting != nil && t.MachineWait == nil {
			if d := now.Sub(*t.Starting); d > InvStartingStuck && first("starting:"+t.ID+"@"+t.Starting.Format(time.RFC3339Nano)) {
				vs = append(vs, violation{
					id:       InvThreadStartingStuck,
					evidence: fmt.Sprintf("thread=%s startingFor=%s", t.ID, d.Round(time.Second)),
					thread:   t.ID,
				})
			}
		}
		if hasAgents && t.Handover != nil {
			if d := now.Sub(t.Handover.At); d > InvHandoverStuck && first("handover:"+t.ID+"@"+t.Handover.At.Format(time.RFC3339Nano)) {
				vs = append(vs, violation{
					id:       InvThreadHandoverStuck,
					evidence: fmt.Sprintf("thread=%s to=%s for=%s", t.ID, t.Handover.To, d.Round(time.Second)),
					thread:   t.ID,
				})
			}
		}
		if t.MachineWait != nil {
			if d := now.Sub(t.MachineWait.Since); d > InvMachineWaitStuck && first("mwait:"+t.ID+"@"+t.MachineWait.Since.Format(time.RFC3339Nano)) {
				vs = append(vs, violation{
					id:       InvThreadMachineWaitStuck,
					evidence: fmt.Sprintf("thread=%s why=%q for=%s", t.ID, t.MachineWait.Why, d.Round(time.Second)),
					thread:   t.ID,
				})
			}
		}
		if hasAgents && t.Settle.At != nil && t.Running {
			if d := now.Sub(*t.Settle.At); d > InvSettledRunningGrace && first("settle-run:"+t.ID+"@"+t.Settle.At.Format(time.RFC3339Nano)) {
				vs = append(vs, violation{
					id:       InvThreadSettledStillRuns,
					evidence: fmt.Sprintf("thread=%s running %s after settle", t.ID, d.Round(time.Second)),
					thread:   t.ID,
				})
			}
		}
		if hasAgents && len(t.Outbox) > 0 && !t.Running && t.Starting == nil && t.MachineWait == nil && t.Unresponsive == nil {
			if d, ok := graceFired("outbox-stuck:"+t.ID, InvOutboxStuck); ok {
				vs = append(vs, violation{
					id:       InvThreadOutboxStuck,
					evidence: fmt.Sprintf("thread=%s outbox=%d idleFor=%s", t.ID, len(t.Outbox), d.Round(time.Second)),
					thread:   t.ID,
				})
			}
		}
		if hasAgents && !t.Running && t.Starting == nil && t.MachineWait == nil {
			if oldest := core.OldestWakingNotice(t, now); !oldest.IsZero() {
				startingNow := hasAgentSnap && agentSnap[t.ID].Starting
				if !startingNow {
					if d := now.Sub(oldest); d > InvFeedStuck && first("feed-stuck:"+t.ID+"@"+oldest.Format(time.RFC3339Nano)) {
						vs = append(vs, violation{
							id:       InvThreadWakingFeedStalled,
							evidence: fmt.Sprintf("thread=%s queue=%d oldestWake=%s", t.ID, len(t.Feed.Queue), d.Round(time.Second)),
							thread:   t.ID,
						})
					}
				}
			}
		}
		if stuck := stuckActionRun(t, now); stuck != "" && first("action-run:"+t.ID+":"+stuck) {
			vs = append(vs, violation{
				id:       InvThreadActionRunStuck,
				evidence: fmt.Sprintf("thread=%s %s", t.ID, stuck),
				thread:   t.ID,
			})
		}

		// 4. Process <-> Engine consistency (when AgentInspect is wired).
		if hasAgentSnap {
			st := agentSnap[t.ID]
			if t.Running && !st.Starting && (!st.Live || !st.TurnLive) {
				if d, ok := graceFired("phantom-run:"+t.ID, InvProcessDriftGrace); ok {
					vs = append(vs, violation{
						id:       InvThreadPhantomRunning,
						evidence: fmt.Sprintf("thread=%s engine.running=true agent.live=%t agent.turnLive=%t for=%s", t.ID, st.Live, st.TurnLive, d.Round(time.Second)),
						thread:   t.ID,
					})
				}
			}
			if !t.Running && st.Live && st.TurnLive {
				if d, ok := graceFired("ghost-run:"+t.ID, InvProcessDriftGrace); ok {
					vs = append(vs, violation{
						id:       InvThreadGhostRunning,
						evidence: fmt.Sprintf("thread=%s engine.running=false agent.turnLive=true for=%s", t.ID, d.Round(time.Second)),
						thread:   t.ID,
					})
				}
			}
			if n := t.RunningTasks(); n > 0 && !st.Live && !st.Starting {
				if d, ok := graceFired("phantom-tasks:"+t.ID, InvProcessDriftGrace); ok {
					vs = append(vs, violation{
						id:       InvThreadPhantomTasks,
						evidence: fmt.Sprintf("thread=%s runningTasks=%d agent.live=false for=%s", t.ID, n, d.Round(time.Second)),
						thread:   t.ID,
					})
				}
			}
			for _, dec := range t.Decisions {
				if dec.NeedsProcess() && (!st.Live || !slices.Contains(st.Asks, dec.ID)) {
					if d, ok := graceFired("phantom-dec:"+t.ID+":"+dec.ID, InvProcessDriftGrace); ok {
						vs = append(vs, violation{
							id:       InvThreadPhantomDecision,
							evidence: fmt.Sprintf("thread=%s decision=%s kind=%s agent.live=%t agent.asks=%d for=%s", t.ID, dec.ID, dec.Kind, st.Live, len(st.Asks), d.Round(time.Second)),
							thread:   t.ID,
						})
					}
				}
			}
			if len(t.Outbox) > 0 && t.MachineWait == nil && !st.Live && !st.Starting {
				if d, ok := graceFired("phantom-outbox:"+t.ID, InvProcessDriftGrace); ok {
					vs = append(vs, violation{
						id:       InvThreadPhantomOutbox,
						evidence: fmt.Sprintf("thread=%s outbox=%d agent.live=false for=%s", t.ID, len(t.Outbox), d.Round(time.Second)),
						thread:   t.ID,
					})
				}
			}
		}
	}

	// Clear any drift timer whose condition resolved (or whose thread went away).
	for k := range c.driftSince {
		if !activeGrace[k] {
			delete(c.driftSince, k)
		}
	}
	c.mu.Unlock()

	for _, v := range vs {
		e.violate(v, now)
	}
}

// reportThreadViolations deduplicates identical episode violations within
// InvCardEvery and records each new violation.
func (e *Engine) reportThreadViolations(vs []violation, now time.Time) {
	if len(vs) == 0 {
		return
	}
	var emit []violation
	c := &e.inv
	c.mu.Lock()
	if c.once == nil {
		c.once = map[string]time.Time{}
	}
	for k, at := range c.once {
		if now.Sub(at) > InvCardEvery {
			delete(c.once, k)
		}
	}
	for _, v := range vs {
		key := "th:" + v.id + ":" + v.thread + ":" + v.evidence
		if _, seen := c.once[key]; seen {
			continue
		}
		c.once[key] = now
		emit = append(emit, v)
	}
	c.mu.Unlock()
	for _, v := range emit {
		e.violate(v, now)
	}
}

func stuckActionRun(t *core.Thread, now time.Time) string {
	check := func(where string, runs []core.ActionRun) string {
		for _, r := range runs {
			if r.Running && now.Sub(r.At) > InvActionRunStuck {
				return fmt.Sprintf("action=%s on=%s runningFor=%s", r.Action, where, now.Sub(r.At).Round(time.Minute))
			}
		}
		return ""
	}
	if s := check("thread", t.Runs); s != "" {
		return s
	}
	if t.Live != nil {
		if s := check("view", t.Live.Runs); s != "" {
			return s
		}
	}
	for i := range t.Changes {
		if s := check("change:"+t.Changes[i].ID, t.Changes[i].Runs); s != "" {
			return s
		}
	}
	return ""
}

func autoMergeFunc(m map[string]bool) func(string) bool {
	if len(m) == 0 {
		return nil
	}
	return func(repo string) bool { return m[core.RepoKey(repo)] }
}

func cloneViewCopy(v *core.View, threadID string) *core.View {
	if v == nil {
		return &core.View{User: core.Owner, Entity: threadID}
	}
	cp := *v
	cp.Rules = slices.Clone(v.Rules)
	return &cp
}
