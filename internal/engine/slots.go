package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/slots"
)

// SlotIdle is how long a thread may hold a slot without a live run (its
// grant's wake did not start one) before the slot goes back (V0.86 (3)).
const SlotIdle = 2 * time.Minute

// SlotIdleNudge is how long a held slot may run no heavy job before its
// agent is reminded once to release it (#174).
const SlotIdleNudge = 5 * time.Minute

// UseSlots gives the engine the host's pool of slots for heavy jobs
// (V0.86): the agent's slot tools, the notice when a queued slot is the
// thread's, and the release when its run ends.
func (e *Engine) UseSlots(p *slots.Pool) {
	e.mu.Lock()
	e.Slots = p
	e.slotIdle = map[string]time.Time{}
	e.slotNudged = map[string]time.Time{}
	e.slotOverTold = map[string]time.Time{}
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
	type nudge struct {
		owner, kind string
		min         int
	}
	var nudges []nudge
	// V0.147: the tracker is asked before e.mu is taken. Asking it under
	// e.mu deadlocked fyrd on 17 Nov: the tracker's tick held its own lock
	// and asked the engine for a thread's project.
	held := map[string]time.Time{}
	active := map[string]time.Time{}
	for _, h := range s.Held {
		held[h.Owner] = h.Since
		if e.SlotActive != nil && !h.Priority {
			at, ok := e.SlotActive(h.Owner)
			if !ok || at.Before(h.Since) {
				at = h.Since
			}
			active[h.Owner] = at
		}
	}
	e.mu.Lock()
	now := e.Now()
	for owner, since := range e.slotNudged {
		if h, ok := held[owner]; !ok || !h.Equal(since) {
			delete(e.slotNudged, owner)
		}
	}
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
			// #174: a held slot that ran no heavy job for SlotIdleNudge: one
			// reminder per hold to release it.
			if at, ok := active[h.Owner]; ok {
				if since, done := e.slotNudged[h.Owner]; !done || !since.Equal(h.Since) {
					if idle := now.Sub(at); idle >= SlotIdleNudge {
						e.slotNudged[h.Owner] = h.Since
						nudges = append(nudges, nudge{h.Owner, h.Kind, int(idle / time.Minute)})
					}
				}
			}
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
	e.memoryGuard(s)
	e.heavierThanPredicted(s)
	e.freezeGuard(s)
	// Decision 0029 S2: the conditions change without a release.
	e.Slots.Fill()
	for _, n := range nudges {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotIdle, Thread: n.owner, Args: core.SlotIdleArgs{Kind: n.kind, Minutes: n.min}}); err != nil {
			log.Printf("slot idle %s: %v", n.owner, err)
		}
	}
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
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotOver, Thread: h.Owner, Args: core.SlotOverArgs{Class: class, Live: live, Predicted: pred}}); err != nil {
			log.Printf("slot over %s: %v", h.Owner, err)
		}
	}
}

// frozenJob is the job the memory guard froze (decision 0029 S5).
type frozenJob struct {
	owner, since string // the hold: owner and its grant
	at, okSince  time.Time
}

// Decision 0029 S5: how long memory must be fine before a frozen job thaws,
// and the longest a job stays frozen.
const (
	ThawAfterOK = 30 * time.Second
	FreezeMax   = 10 * time.Minute
)

// freezeGuard freezes the newest slotted job while memory is short (never
// the oldest running one, and one at a time, each hold at most once) and
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
	e.mu.Unlock()
	if f.owner != "" {
		holding := false
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
		thaw := !holding || now.Sub(f.at) >= FreezeMax || (!f.okSince.IsZero() && now.Sub(f.okSince) >= ThawAfterOK)
		e.mu.Lock()
		if thaw {
			e.frozen = frozenJob{}
		} else {
			e.frozen = f
		}
		e.mu.Unlock()
		if thaw {
			e.SlotThaw(f.owner)
			if holding {
				e.slotNotice(f.owner, map[string]any{"resumed": true})
			}
		}
		return
	}
	if !short {
		return
	}
	var running []slots.Holder
	for _, h := range s.Held {
		if !h.Priority {
			running = append(running, h)
		}
	}
	if len(s.Held) < 2 || len(running) == 0 {
		return // one job always runs
	}
	newest := running[0]
	for _, h := range running[1:] {
		if h.Since.After(newest.Since) {
			newest = h
		}
	}
	oldest := s.Held[0]
	for _, h := range s.Held[1:] {
		if h.Since.Before(oldest.Since) {
			oldest = h
		}
	}
	e.mu.Lock()
	done := e.frozeHolds[newest.Owner].Equal(newest.Since)
	e.mu.Unlock()
	if newest.Owner == oldest.Owner || done {
		return
	}
	if !e.SlotFreeze(newest.Owner) {
		return
	}
	why := "memory stalls (critical)"
	if sig.Headroom != nil && *sig.Headroom < e.SlotPolicy.MemoryReserve {
		why = fmt.Sprintf("memory: %.0f %% free", *sig.Headroom*100)
	}
	e.mu.Lock()
	e.frozen = frozenJob{owner: newest.Owner, since: newest.Since.Format(time.RFC3339Nano), at: now}
	e.frozeHolds[newest.Owner] = newest.Since
	e.mu.Unlock()
	e.slotNotice(newest.Owner, map[string]any{"paused": why})
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

// HeavyGate answers the gate hook for a heavy command of class (#174): ""
// lets it run (no class, slots off, or the thread holds a slot); else the
// reason it is refused, with the call that fixes it.
func (e *Engine) HeavyGate(thread, class string) string {
	if class == "" || e.Slots == nil || e.Slots.Off() {
		return ""
	}
	if _, ok := e.Slots.Holding(thread); ok {
		return ""
	}
	s := e.Slots.Snapshot()
	for i, w := range s.Waiting {
		if w.Owner == thread {
			return fmt.Sprintf("Heavy job (%s): you are queued for %s slot (%d ahead) and hold none yet. Do not run it now; Fyr tells you in a [Fyr] message when the slot is yours. Do other work meanwhile or end your turn.", class, article(w.Kind), i)
		}
	}
	kind := heavy.KindOf(class)
	return fmt.Sprintf("Heavy job (%s) without a slot: call mcp__fyr__slot_acquire {\"kind\":%q} first, run it once you hold the slot, then call mcp__fyr__slot_release. A subagent's run counts too; a single test file or `go test ./pkg -run X` needs no slot.", class, kind)
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
				return "Slots are off on this host: no limit, run the job.", nil
			}
			held, ahead := e.Slots.Acquire(th, kind)
			if held {
				h, _ := e.Slots.Holding(th)
				return "You hold " + article(h.Kind) + " slot. Run the job now, then call slot_release. It also goes back when your run ends.", nil
			}
			why := ""
			if s := e.Slots.Snapshot(); ahead == 0 && s.Why != "" {
				why = " Waiting: " + s.Why + "." // decision 0029 S2
			}
			return fmt.Sprintf("Queued for %s slot: %d ahead.%s Do not start the job; Fyr tells you in a [Fyr] message when the slot is yours. Do other work meanwhile or end your turn.", article(kind), ahead, why), nil
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
