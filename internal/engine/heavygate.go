package engine

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/heavy"
)

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
	e.setGateHold(c.Thread, "") // V0.gate-hold-visible: not held now
	kind := heavy.KindOf(c.Class)
	// The admission is taken outside heavyMu (a grant calls into the engine)
	// and then checked under it: the thread's last call may just have ended.
	waitsToGrow := false // V0.freeze-controller: a concurrent run that found no room waits, learned or not
	for range 3 {
		held, fresh := false, false
		if h, ok := e.Slots.Holding(c.Thread); ok {
			held = true
			// V0.admission-tidy: a heavier command than the hold was admitted
			// for is admitted again for the difference (a learned one never
			// waits); a cheaper one runs.
			// V0.freeze-controller: another heavy command beside one that runs
			// asks for room for both, as a heavier one does. A learned
			// command asks too: it never waits when the thread holds nothing,
			// but a second one at once, with the first still running, is
			// what filled the host on 26 Nov.
			more := e.HeavyOpenCalls(c.Thread) >= 1 && !e.callOpen(c) && !e.cheaperThanHold(c, h)
			if !h.Priority && (more || (!soft && e.heavier(c, h))) {
				held, _ = e.Slots.GrowFor(c.Thread, kind, e.HeavyOpenCalls(c.Thread)+1)
				waitsToGrow = waitsToGrow || (!held && more)
				e.syncSlotQueue()
				e.checkSlotQueueInvariant() // V0.thread-invariants
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
			e.checkSlotQueueInvariant() // V0.thread-invariants
		}
		if !held {
			break
		}
		if fresh {
			e.noteGrant(c)
		}
		if e.trackHeavy(c) {
			e.noteAdmitted(c) // V0.subagent-admission
			if rerun {
				e.mu.Lock()
				delete(e.rerunWait, c.Thread)
				e.mu.Unlock()
				log.Printf("guard: rerun admitted thread=%s class=%q", c.Thread, entry)
			}
			return ""
		}
	}
	if soft && !waitsToGrow {
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
			e.noteRefused(c) // V0.subagent-admission
			if c.Subagent {
				// A subagent cannot wait for a message: on 2026-11-19 one
				// sent the same queued check four times. V0.subagent-admission:
				// the admission reaches it at its next tool call while it runs.
				return head + subagentWait("it is admitted", "queued")
			}
			return head + " Fyr tells you in a [Fyr] message when it is admitted; run the command again then. Do other work meanwhile or end your turn."
		}
	}
	return "" // neither held nor queued: the pool changed under us, let it run
}

// cheaperThanHold says c is another command whose class needs no more than the
// class h was admitted for, in both resources: it runs free beside it
// (V0.admission-tidy). The same class is not cheaper: a second run of it at
// once is another need (V0.freeze-controller).
func (e *Engine) cheaperThanHold(c HeavyCall, h resource.Holder) bool {
	e.heavyMu.Lock()
	g, ok := e.heavyGranted[c.Thread]
	e.heavyMu.Unlock()
	if !ok || !g.since.Equal(h.Since) || g.class == c.Class || e.SlotClassNeed == nil {
		return false
	}
	was, now := e.SlotClassNeed(c.Thread, g.class), e.SlotClassNeed(c.Thread, c.Class)
	return now.CPU <= was.CPU+1e-9 && now.Mem <= was.Mem
}

// callOpen says c's tool call is already counted open (the gate is asked again
// for a command it held).
func (e *Engine) callOpen(c HeavyCall) bool {
	if c.ToolUse == "" {
		return false
	}
	e.heavyMu.Lock()
	defer e.heavyMu.Unlock()
	return e.heavyCalls[c.Thread][c.ToolUse]
}

// grantedClass is the class a hold (its Since) was admitted for at the gate.
type grantedClass struct {
	class string
	since time.Time
}

// heavier says c's class needs more of the host, in either resource, than the
// class h was admitted for (V0.admission-tidy). A hold the gate did not grant
// (a queued grant, slot_acquire) is taken as granted for c's class.
func (e *Engine) heavier(c HeavyCall, h resource.Holder) bool {
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
	short := e.gs.Short
	e.mu.Unlock()
	if !short {
		return ""
	}
	h, holds := e.Slots.Holding(c.Thread)
	if !guard.GateHolds(short, holds, h.Priority, soft) {
		return ""
	}
	e.mu.Lock()
	if e.gateHeld == nil {
		e.gateHeld = map[string]bool{}
	}
	e.gateHeld[c.Thread] = true
	e.mu.Unlock()
	e.guardOnce(e.Now(), "gate:"+c.Thread, fmt.Sprintf("held at the gate thread=%s class=%q admitted=%t learned=%t: memory is short; a new heavy command waits before a running one is frozen", c.Thread, c.Class, holds, soft))
	e.setGateHold(c.Thread, fmt.Sprintf("memory is short on %s; the %s command runs when it recovers", e.hostName(), c.Class))
	head := fmt.Sprintf("Heavy job (%s): memory is short on %s, so Fyr holds new heavy commands until it recovers instead of pausing ones that run. It did not run. Do not work around this.", c.Class, e.hostName())
	e.noteRefused(c) // V0.subagent-admission
	if c.Subagent {
		return head + subagentWait("memory has recovered", "held")
	}
	return head + " Fyr tells you in a [Fyr] message when memory has recovered; run the command again then. Do other work meanwhile or end your turn."
}

// subagentWait is the end of a subagent's refusal (V0.subagent-admission):
// when the command may run, Fyr says so in the result of the subagent's next
// tool call, and if it ends first, the main session is told, naming it.
func subagentWait(when, state string) string {
	return " You are a subagent: no message can wake you. If " + when + " while you still run, Fyr says so in the result of your next tool call; run the command again then. Do not run it again before that, and do not wait or poll for it: do the rest of your work, and if that ends first, say in your report that it is " + state + " and did not run, and hand back. Fyr then tells the main session, naming you."
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
	n := make(map[string]int, len(e.heavyCalls))
	for th, c := range e.heavyCalls {
		if len(c) > 0 {
			open[th] = true
			n[th] = len(c)
		}
	}
	e.heavyOpen.Store(&open)
	e.heavyOpenN.Store(&n)
}

// HeavyOpen says thread has a heavy foreground call open. It takes no lock:
// admission asks it under the pool's lock (V0.admission-tidy).
func (e *Engine) HeavyOpen(thread string) bool {
	m := e.heavyOpen.Load()
	return m != nil && (*m)[thread]
}

// HeavyOpenCalls is how many heavy foreground calls thread has open. It takes
// no lock, like HeavyOpen: admission asks it under the pool's lock.
func (e *Engine) HeavyOpenCalls(thread string) int {
	m := e.heavyOpenN.Load()
	if m == nil {
		return 0
	}
	return (*m)[thread]
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
	e.publishOpenLocked() // V0.freeze-controller: the count changes with every call that ends, not only the last
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
	e.checkSlotQueueInvariant() // V0.thread-invariants
}

// setGateHold records on the thread that the gate holds its heavy command
// back ("" ends it), so the list shows it as waiting and why
// (V0.gate-hold-visible). The pool's queue does not hold such a thread.
func (e *Engine) setGateHold(thread, why string) {
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindGateHold, Thread: thread, Args: core.GateHoldArgs{Why: why}}); err != nil {
		log.Printf("gate hold %s: %v", thread, err)
	}
}
