package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
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
	e.undTold, e.undRun = map[string]time.Time{}, map[string]int{}
	e.undNote, e.undFroze = map[string]time.Time{}, map[string]time.Time{}
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

// syncSlotQueue brings the threads' records in line with the pool's queue
// (V0.185): a Condition reads the record, and a thread leaves the queue by
// several paths. The tick calls it; so do the slot tools, for at once.
func (e *Engine) syncSlotQueue() {
	if e.Slots == nil {
		return
	}
	queued := map[string]bool{}
	for _, h := range e.Slots.Snapshot().Waiting {
		queued[h.Owner] = true
	}
	var moved []string
	e.mu.Lock()
	for id, t := range e.threads {
		if t.SlotQueued != queued[id] {
			moved = append(moved, id)
		}
	}
	e.mu.Unlock()
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
	e.syncSlotQueue()
	e.memoryGuard(s)
	e.heavierThanPredicted(s)
	e.tellUndeclared()
	e.freezeGuard(s)
	e.adaptBudget(s)
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
	f, why := e.SlotAIMD.Step(e.Now(), e.SlotSignals(), len(s.Waiting) > 0, e.Slots.LastGrant(), s.Binding)
	if f != e.Slots.Budget() || (s.Budget != nil && s.BudgetWhy != why) {
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
		thaw := !holding || now.Sub(f.at) >= FreezeMax || (!f.okSince.IsZero() && now.Sub(f.okSince) >= ThawAfterOK)
		e.mu.Lock()
		if thaw {
			e.frozen = frozenJob{}
		} else {
			e.frozen = f
		}
		e.mu.Unlock()
		if thaw {
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

// flags are the threads that run undeclared heavy work now.
func (e *Engine) flags() []slots.Undeclared {
	if e.Undeclared == nil {
		return nil
	}
	return e.Undeclared()
}

// freezeUndeclared freezes the newest flagged thread whose memory is over
// (V0.164 section 7), before any job that asked: one at a time on the host,
// each episode once, and never when it would help nobody (no admitted job and
// no other flagged thread). It reports whether it froze one.
func (e *Engine) freezeUndeclared(sig slots.Signals, now time.Time, admitted bool) bool {
	if e.UndeclaredFreeze == nil || e.UndeclaredThaw == nil {
		return false
	}
	fl := e.flags()
	if !admitted && len(fl) < 2 {
		return false
	}
	var pick *slots.Undeclared
	e.mu.Lock()
	for i := range fl {
		f := &fl[i]
		if !f.MemOver || e.undFroze[f.Thread].Equal(f.Since) || e.threads[f.Thread] == nil {
			continue
		}
		if pick == nil || f.Since.After(pick.Since) {
			pick = f
		}
	}
	e.mu.Unlock()
	if pick == nil || !e.UndeclaredFreeze(pick.Thread) {
		return false
	}
	why := "memory stalls (critical)"
	if sig.Headroom != nil && *sig.Headroom < e.SlotPolicy.MemoryReserve {
		why = fmt.Sprintf("memory: %.0f %% free", *sig.Headroom*100)
	}
	e.mu.Lock()
	e.frozen = frozenJob{owner: pick.Thread, at: now, undeclared: true, episode: pick.Since}
	e.undFroze[pick.Thread] = pick.Since
	e.mu.Unlock()
	cmd := pick.Command
	if cmd == "" {
		cmd = "unknown command"
	}
	e.slotNotice(pick.Thread, map[string]any{"paused": why, "work": cmd})
	return true
}

// undeclaredText is what the agent hears of a flag (V0.164 section 5), with
// the host's numbers.
func (e *Engine) undeclaredText(f slots.Undeclared) string {
	cpuPct, memPct := f.CPUPct, f.MemPct // the shares of the host, filled by the sampler
	both := !f.OverCPU && !f.OverMem
	var parts []string
	if f.OverMem || both {
		p := slots.GB(float64(f.Mem)) + " memory"
		if memPct > 0 {
			p += fmt.Sprintf(" (%.0f %% of %s)", memPct, e.hostName())
		}
		parts = append(parts, p)
	}
	if f.OverCPU || both {
		p := fmt.Sprintf("%.1f cores", f.CPU)
		if cpuPct > 0 {
			p += fmt.Sprintf(" (%.0f %%)", cpuPct)
		}
		parts = append(parts, p)
	}
	from, ex := "", "…"
	kind := "test"
	if f.Command != "" {
		from = ", from `" + f.Command + "`"
		ex = f.Command
		kind = heavy.KindOf(f.Command)
	}
	win := int(f.Window / time.Second)
	if win <= 0 {
		win = int(cgroupWindow / time.Second)
	}
	call := "Call it now for the job that runs."
	if f.Queued {
		call = "You are queued and not admitted yet: do not run the job until Fyr tells you."
	}
	cmdJSON, _ := json.Marshal(ex)
	return fmt.Sprintf("This thread runs heavy work Fyr did not admit: about %s for %d s%s. Before a heavy job call mcp__fyr__slot_acquire {\"kind\":%q,\"command\":%s}, run it once admitted, then call mcp__fyr__slot_release; a subagent's or a background run counts too. %s Fyr counts this use against the host either way, so other jobs wait; nothing is stopped, but if memory runs short Fyr may pause it.",
		strings.Join(parts, " and "), win, from, kind, cmdJSON, call)
}

// cgroupWindow is the default window of a flag when it does not say.
const cgroupWindow = 45 * time.Second

// tellUndeclared tells a flagged thread's agent once per episode and at most
// once per run, as a Next notice (V0.164 section 5): the next tool call or the
// next message that starts a run, never a wake.
func (e *Engine) tellUndeclared() {
	fl := e.flags()
	type tell struct {
		f   slots.Undeclared
		run int
	}
	var tells []tell
	e.mu.Lock()
	live := map[string]bool{}
	for _, f := range fl {
		live[f.Thread] = true
		t := e.threads[f.Thread]
		if t == nil || e.undTold[f.Thread].Equal(f.Since) {
			continue
		}
		e.undTold[f.Thread] = f.Since
		if r, ok := e.undRun[f.Thread]; ok && r == t.RunEnds {
			continue // a second episode in the same run
		}
		e.undRun[f.Thread] = t.RunEnds
		tells = append(tells, tell{f, t.RunEnds})
	}
	for th := range e.undNote {
		if !live[th] {
			delete(e.undNote, th)
		}
	}
	e.mu.Unlock()
	now := e.Now()
	for _, x := range tells {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotUndeclared, Thread: x.f.Thread,
			Args: core.SlotUndeclaredArgs{Text: e.undeclaredText(x.f), Mem: x.f.Mem, CPU: x.f.CPU}}); err != nil {
			log.Printf("slot undeclared %s: %v", x.f.Thread, err)
			continue
		}
		if e.UndeclaredTold != nil {
			e.UndeclaredTold(x.f.Thread, x.f.Since, now)
		}
	}
}

// UndeclaredSubagentNote is the text a subagent's first Bash call of a
// flagged thread carries, once per episode (V0.164 section 5): the main agent
// may make no tool call while it waits for the subagent. "" when the thread is
// not flagged or the episode already had its note.
func (e *Engine) UndeclaredSubagentNote(thread string) string {
	for _, f := range e.flags() {
		if f.Thread != thread {
			continue
		}
		e.mu.Lock()
		given := e.undNote[thread].Equal(f.Since)
		if !given {
			e.undNote[thread] = f.Since
		}
		e.mu.Unlock()
		if given {
			return ""
		}
		return e.undeclaredText(f)
	}
	return ""
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
			return fmt.Sprintf("Heavy job (%s): it is queued, not admitted yet (%d ahead; it needs %s of %s). Do not run it now; Fyr tells you in a [Fyr] message when it is admitted. Do other work meanwhile or end your turn.", class, i, strings.TrimPrefix(jobShare(s, w.Owner), "≈ "), e.hostName())
		}
	}
	if e.HeavyRefused != nil {
		e.HeavyRefused(class) // V0.164: an added pattern counts its refusals
	}
	kind := heavy.KindOf(class)
	return fmt.Sprintf("Heavy job (%s) not admitted: call mcp__fyr__slot_acquire {\"kind\":%q, \"command\":\"…\"} first; Fyr admits it when its share of %s fits. Run it once admitted, then call mcp__fyr__slot_release. A subagent's run counts too; a single test file or `go test ./pkg -run X` needs no admission.", class, kind, e.hostName())
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
	// V0.163: the names stay (prompts, hooks and habits use them); the
	// answers speak a share of the host, not slots.
	off := "This host does not limit heavy jobs; run the job."
	s.Add(mcp.Tool{Name: "slot_acquire",
		Description: "Ask to run a heavy job (a full test suite, a build, an e2e run). Fyr admits it when its share of this host (its learned cost, as a % of the CPU or memory the host gives its agents, whichever is tighter) fits the heavy-job budget across the user's threads. It answers at once: admitted (run the job, then call slot_release), or queued with the reason; then do not start the job, do other work or end your turn, and Fyr tells you in a [Fyr] message when it is admitted. Admission also ends when your run ends.",
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
				return fmt.Sprintf("Admitted: your %s job %s of %s; %s left. Run the job now, then call slot_release. Admission also ends when your run ends.", kind, jobShare(s, th), e.hostName(), freeShare(s)), nil
			}
			why := ""
			if ahead == 0 && s.Why != "" {
				why = " Waiting: " + s.Why + "." // decision 0029 S2
			}
			return fmt.Sprintf("Queued: your %s job needs %s of %s; %s free (%d ahead).%s Do not start the job; Fyr tells you in a [Fyr] message when it is admitted. Do other work meanwhile or end your turn.", kind, strings.TrimPrefix(jobShare(s, th), "≈ "), e.hostName(), freeShare(s), ahead, why), nil
		}})
	s.Add(mcp.Tool{Name: "slot_release",
		Description: "End your heavy job's admission when the job ended (its share of the host goes back), or leave the queue.",
		Schema:      obj(map[string]any{}),
		Call: func(_ context.Context, th string, _ map[string]any) (string, error) {
			if e.Slots == nil {
				return off, nil
			}
			if !e.dropSlot(th) {
				return "", errors.New("you have no admitted or queued heavy job")
			}
			e.syncSlotQueue()
			return fmt.Sprintf("Released: %s of %s free for heavy jobs.", freeShare(e.Slots.Snapshot()), e.hostName()), nil
		}})
}
