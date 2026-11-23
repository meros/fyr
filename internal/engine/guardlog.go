// The guard's log and its published view: guardOnce, guardRecord, publishGuard, guardWhy.

package engine

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

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
func guardSignals(sig resource.Signals) string {
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

// guardRecord adds a decision to the history the front shows (decision 0032).
func (e *Engine) guardRecord(ev resource.GuardEvent, sig resource.Signals) {
	e.invEvent(ev, sig) // V0.load-invariants
	if sig.Headroom != nil {
		h := *sig.Headroom
		ev.Headroom = &h
	}
	ev.Stall = string(sig.Stall)
	e.mu.Lock()
	e.guardEvents = append(e.guardEvents, ev)
	if n := len(e.guardEvents); n > guardKeep {
		e.guardEvents = append([]resource.GuardEvent(nil), e.guardEvents[n-guardKeep:]...)
	}
	e.mu.Unlock()
}

// GuardView is the guard's last published view (nil before the first tick).
// It waits for nothing: the pool reads it under its own lock.
func (e *Engine) GuardView() *resource.Guard { return e.guardPub.Load() }

// publishGuard builds the view at the end of a tick.
func (e *Engine) publishGuard(now time.Time) {
	e.mu.Lock()
	g := &resource.Guard{Short: e.guardShort, Max: MaxFreezes, RefreezeSec: int(RefreezeAfter / time.Second),
		Events: append([]resource.GuardEvent{}, e.guardEvents...), // never nil: the front gets [], not null
		Rerun:  e.rerunView(now)}
	if e.guardShort && !e.guardSince.IsZero() {
		t := e.guardSince
		g.ShortSince = &t
	}
	for thread, u := range e.undFroze {
		t := resource.GuardThread{Thread: thread, N: u.n, Capped: u.n >= MaxFreezes}
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
	slices.SortFunc(g.Threads, func(a, b resource.GuardThread) int { return strings.Compare(a.Thread, b.Thread) })
	e.guardPub.Store(g)
}

// guardWhy says why memory is short, for a freeze's notice and log: the
// headroom first, else the critical stall, the host's or the agents'
// (V0.guard-agents-stall), else the unit's stall near oomd's limit.
func guardWhy(sig resource.Signals, p resource.Policy) string {
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
