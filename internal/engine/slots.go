package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/slots"
)

// SlotIdle is how long a thread may hold a slot without a live run (its
// grant's wake did not start one) before the slot goes back (V0.86 (3)).
const SlotIdle = 2 * time.Minute

// UseSlots gives the engine the host's pool of slots for heavy jobs
// (V0.86): the agent's slot tools, the notice when a queued slot is the
// thread's, and the release when its run ends.
func (e *Engine) UseSlots(p *slots.Pool) {
	e.mu.Lock()
	e.Slots = p
	e.slotIdle = map[string]time.Time{}
	e.mu.Unlock()
	p.OnGrant = e.slotGranted
	p.Fill() // a pool loaded from the store may have free slots for its queue
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
// place in the queue stays.
func (e *Engine) slotRunEnded(id string) {
	if e.Slots == nil {
		return
	}
	if _, ok := e.Slots.Holding(id); ok {
		e.dropSlot(id)
	}
}

func (e *Engine) dropSlot(id string) bool {
	e.mu.Lock()
	delete(e.slotIdle, id)
	e.mu.Unlock()
	return e.Slots.Release(id)
}

// sweepSlots gives back the slots of threads that are gone or held one for
// SlotIdle without a live run; the tick calls it.
func (e *Engine) sweepSlots() {
	if e.Slots == nil {
		return
	}
	s := e.Slots.Snapshot()
	var drop []string
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
	off := "This host has no slots for heavy jobs; run the job."
	s.Add(mcp.Tool{Name: "slot_acquire",
		Description: "Take a slot before a heavy job (a full test suite, a build, an e2e run): at most a few run at once across the user's threads. It answers at once: either you hold the slot (run the job, then call slot_release), or you are queued with your position; then do not start the job, do other work or end your turn, and Fyr tells you in a [Fyr] message when the slot is yours. The slot also goes back when your run ends.",
		Schema:      obj(map[string]any{"kind": map[string]any{"type": "string", "enum": slots.Kinds()}}, "kind"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			kind := sArg(a, "kind")
			if !slices.Contains(slots.Kinds(), kind) {
				return "", fmt.Errorf("kind must be one of %v", slots.Kinds())
			}
			if e.Slots == nil {
				return off, nil
			}
			if e.Slots.Off() {
				return "Slots are off on this host: no limit, run the job.", nil
			}
			held, ahead := e.Slots.Acquire(th, kind)
			if held {
				h, _ := e.Slots.Holding(th)
				return "You hold " + article(h.Kind) + " slot. Run the job now, then call slot_release. It also goes back when your run ends.", nil
			}
			return fmt.Sprintf("Queued for %s slot: %d ahead. Do not start the job; Fyr tells you in a [Fyr] message when the slot is yours. Do other work meanwhile or end your turn.", article(kind), ahead), nil
		}})
	s.Add(mcp.Tool{Name: "slot_release",
		Description: "Give back your slot for a heavy job when the job ended, or leave the queue for one.",
		Schema:      obj(map[string]any{}),
		Call: func(_ context.Context, th string, _ map[string]any) (string, error) {
			if e.Slots == nil {
				return off, nil
			}
			if !e.dropSlot(th) {
				return "", errors.New("you hold no slot and wait for none")
			}
			return "Released.", nil
		}})
}
