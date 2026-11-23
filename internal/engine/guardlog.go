// The guard's log and its published view: guardOnce, guardRecord, publishGuard.

package engine

import (
	"log"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/guard"
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
	g := &resource.Guard{Short: e.gs.Short, Max: guard.MaxFreezes, RefreezeSec: int(guard.RefreezeAfter / time.Second),
		Events: append([]resource.GuardEvent{}, e.guardEvents...), // never nil: the front gets [], not null
		Rerun:  e.rerunView(now)}
	if e.gs.Short && !e.gs.ShortSince.IsZero() {
		t := e.gs.ShortSince
		g.ShortSince = &t
	}
	for thread, u := range e.gs.UndFroze {
		t := resource.GuardThread{Thread: thread, N: u.N, Capped: u.N >= guard.MaxFreezes}
		if f, ok := guard.FrozenUndeclared(e.gs.Frozen, thread); ok {
			t.Frozen = true
			at := f.At
			t.FrozenAt = &at
		} else if !u.ThawedAt.IsZero() && !t.Capped {
			if next := u.ThawedAt.Add(guard.RefreezeAfter); next.After(now) {
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
