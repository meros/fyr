package engine

import (
	"log"

	"github.com/meros/fyr/internal/core"
)

// V0.subagent-admission: the admission of a heavy job, or the recovery of
// memory for one the gate held, reaches the actor whose command was refused.
// The pool and the feed know only the thread, so a subagent's refused command
// used to wake the thread's own agent with "Your test job is admitted now",
// a turn spent on a job it never asked for while the subagent went on. Now
// the gate remembers the subagent; while it runs, the line goes to its next
// tool call and the thread's agent hears nothing; once it has ended without
// running the job, the thread's agent is told, with the subagent named.

// subAgent names a subagent by its id and type, as the hook input does.
type subAgent struct{ ID, Type string }

// subWake is an admission (kind) or a recovery (retry) that waits for a
// running subagent's next tool call.
type subWake struct {
	by    subAgent
	kind  string
	retry bool
}

// noteRefused remembers who was refused c's command: the next grant or
// recovery on the thread is that actor's.
func (e *Engine) noteRefused(c HeavyCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c.Agent == "" {
		delete(e.heldBy, c.Thread)
		return
	}
	if e.heldBy == nil {
		e.heldBy = map[string]subAgent{}
	}
	e.heldBy[c.Thread] = subAgent{c.Agent, c.AgentType}
}

// noteAdmitted forgets what waits for c's actor: it ran its command again.
func (e *Engine) noteAdmitted(c HeavyCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if by, ok := e.heldBy[c.Thread]; ok && by.ID == c.Agent {
		delete(e.heldBy, c.Thread)
	}
	if w, ok := e.subWake[c.Thread]; ok && w.by.ID == c.Agent {
		delete(e.subWake, c.Thread)
	}
}

// routeToSubagent takes the subagent a grant or recovery on thread is for.
// kept: it still runs, and w waits for its next tool call; else by names the
// subagent that has ended (zero when the thread's own agent was refused).
func (e *Engine) routeToSubagent(thread string, w subWake) (by subAgent, kept bool) {
	e.mu.Lock()
	by, ok := e.heldBy[thread]
	delete(e.heldBy, thread)
	live := e.SubagentLive
	e.mu.Unlock()
	if !ok {
		return subAgent{}, false
	}
	// Asked outside mu: the manager takes its own lock.
	if live == nil || !live(thread, by.ID) {
		return by, false
	}
	w.by = by
	e.mu.Lock()
	if e.subWake == nil {
		e.subWake = map[string]subWake{}
	}
	e.subWake[thread] = w
	e.mu.Unlock()
	log.Printf("slots: held for subagent thread=%s agent=%s kind=%q retry=%t", thread, by.ID, w.kind, w.retry)
	return by, true
}

// SubagentNote is the line for a subagent's tool call: its admission or
// recovery, once; "" for none. The agent manager adds it to the call's result.
func (e *Engine) SubagentNote(thread, agent string) string {
	e.mu.Lock()
	w, ok := e.subWake[thread]
	if !ok || w.by.ID != agent {
		e.mu.Unlock()
		return ""
	}
	delete(e.subWake, thread)
	e.mu.Unlock()
	if w.retry {
		return core.NoticeText([]core.Notice{{What: core.HeldRetryText("", "")}})
	}
	return core.NoticeText([]core.Notice{{What: core.AdmittedText(w.kind, "", "")}})
}

// SubagentEnded hears that a subagent ended. An admission that waited for it
// goes to the thread's agent, naming it: nobody else will run the command.
func (e *Engine) SubagentEnded(thread, agent string) {
	e.mu.Lock()
	w, ok := e.subWake[thread]
	if ok && w.by.ID == agent {
		delete(e.subWake, thread)
	} else {
		ok = false
	}
	e.mu.Unlock()
	if ok {
		e.tellEnded(thread, w)
	}
}

// sweepSubWakes hands the waits of subagents that are gone (their process
// ended, or their stop was never heard) to their threads' agents.
func (e *Engine) sweepSubWakes() {
	e.mu.Lock()
	if len(e.subWake) == 0 {
		e.mu.Unlock()
		return
	}
	waits := make(map[string]subWake, len(e.subWake))
	for th, w := range e.subWake {
		waits[th] = w
	}
	live := e.SubagentLive
	e.mu.Unlock()
	for th, w := range waits {
		if live != nil && live(th, w.by.ID) {
			continue
		}
		e.SubagentEnded(th, w.by.ID)
	}
}

// tellEnded tells the thread's agent what a subagent that ended was waiting for.
func (e *Engine) tellEnded(thread string, w subWake) {
	if w.retry {
		e.applySlotPause(thread, map[string]any{"retry": true, "agent": w.by.ID, "agentType": w.by.Type})
		return
	}
	e.applySlotGranted(thread, core.SlotGrantedArgs{Kind: w.kind, Agent: w.by.ID, AgentType: w.by.Type})
}

func (e *Engine) applySlotGranted(owner string, a core.SlotGrantedArgs) error {
	var args any = a.Kind // the plain kind when the thread's agent was refused
	if a.Agent != "" {
		args = a
	}
	_, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotGranted, Thread: owner, Args: args})
	if err != nil {
		log.Printf("slot granted %s: %v", owner, err)
	}
	return err
}
