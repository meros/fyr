// Freezing work no command declared: the flags and freezeUndeclared.

package engine

import (
	"fmt"
	"log"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// undFreeze is what the guard froze of one thread's episode of undeclared
// work: the episode (the flag's since), how many freezes so far, and when the
// last one thawed.
type undFreeze struct {
	episode  time.Time
	n        int
	thawedAt time.Time
}

// frozenUndeclared is thread's freeze of undeclared work; under mu.
func (e *Engine) frozenUndeclared(thread string) (frozenJob, bool) {
	for _, f := range e.frozen {
		if f.undeclared && f.owner == thread {
			return f, true
		}
	}
	return frozenJob{}, false
}

// flags are the threads that run undeclared heavy work now.
func (e *Engine) flags() []slots.Undeclared {
	if e.Undeclared == nil {
		return nil
	}
	return e.Undeclared()
}

// freezeUndeclared freezes the newest flagged thread whose memory is over
// (V0.164 section 7), before any job that asked; not one already frozen
// (V0.multi-freeze: freezeGuard decides when another may be).
// Decision 0032 slice 1 changes two once-only rules of V0.164 that left a long
// episode unprotected: the work is frozen when anyone else would gain from it
// (an admitted job, another flagged thread, or any other thread that runs:
// its agent starts slowly or not at all in a thrashing cgroup), not only when
// a job or a second flag exists; and an episode is frozen again after a thaw,
// RefreezeAfter later and at most MaxFreezes times, so the freezes work as time
// slices of the long episode. It reports whether it froze one.
func (e *Engine) freezeUndeclared(sig slots.Signals, now time.Time, admitted bool, heavy map[string]time.Time) bool {
	if e.UndeclaredFreeze == nil || e.UndeclaredThaw == nil {
		return false
	}
	fl := e.flags()
	if len(fl) == 0 {
		return false
	}
	run := e.Running()
	others := func(thread string) int {
		n := 0
		for _, id := range run {
			if id != thread {
				n++
			}
		}
		return n
	}
	var pick *slots.Undeclared
	var picked undFreeze
	var skips [][2]string    // key, line: logged after the lock
	svc := map[string]bool{} // decision 0036 (D), E4: asked outside e.mu
	held := map[string]uint64{}
	turnable := map[string]bool{} // two or more parts: one keeps running
	for _, f := range fl {
		svc[f.Thread] = e.service(f.Thread)
		if f.MemOver {
			turnable[f.Thread] = len(e.workUnits(f.Thread)) >= 2
		}
		// V0.guard-tidy: what the episode's processes hold now. The kernel does
		// not move memory with a process, so a thread's flagged memory can be
		// charged where its work no longer is (cli/), and freezing the work
		// frees none of it.
		if f.MemOver && e.UndeclaredRootsMem != nil {
			if m, ok := e.UndeclaredRootsMem(f.Thread); ok {
				held[f.Thread] = m + 1 // 0: not read
			}
		}
	}
	e.mu.Lock()
	for i := range fl {
		f := &fl[i]
		if !f.MemOver || e.threads[f.Thread] == nil || e.isFrozen(f.Thread) {
			continue
		}
		if svc[f.Thread] {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: its work is a service", f.Thread)})
			continue
		}
		if m := held[f.Thread]; m > 0 && float64(m-1) < WorkMemShare*float64(f.Mem) {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: the work holds little of the thread's memory (its processes %s of %s)", f.Thread, slots.GB(float64(m-1)), slots.GB(float64(f.Mem)))})
			continue
		}
		prev := e.undFroze[f.Thread]
		if prev.episode.Equal(f.Since) {
			if prev.n >= MaxFreezes {
				skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: frozen %d times in this episode, letting it run", f.Thread, prev.n)})
				continue
			}
			if !prev.thawedAt.IsZero() && now.Sub(prev.thawedAt) < RefreezeAfter {
				continue // the slice has not run long enough yet; says nothing
			}
		}
		if !admitted && len(fl) < 2 && others(f.Thread) == 0 {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: nobody else would gain (no job, no other flag, no other thread running)", f.Thread)})
			continue
		}
		// V0.sim-fixes (3): the last heavy work that runs is never frozen
		// whole (one always runs, decision 0029 section 4, Buck2's rule); work
		// of two or more parts takes turns, so one of its parts still runs. An
		// agent with no heavy work is no reason to stop all of it: the frozen
		// memory stayed charged, and only the 10 min thaw ran the work again.
		if !turnable[f.Thread] && len(heavy) < 2 {
			skips = append(skips, [2]string{f.Thread, fmt.Sprintf("not freezing thread=%s: its work is the only heavy work that runs; one always runs", f.Thread)})
			continue
		}
		if pick == nil || f.Since.After(pick.Since) {
			pick, picked = f, prev
		}
	}
	e.mu.Unlock()
	for _, k := range skips {
		e.guardOnce(now, "skip:"+k[0], k[1])
	}
	if pick == nil {
		return false
	}
	usec, hasUsec := e.frozenUsec(pick.Thread, true)
	first, units, turns := e.startTurns(pick.Thread)
	if !turns && !e.UndeclaredFreeze(pick.Thread) {
		e.guardOnce(now, "skip:"+pick.Thread, fmt.Sprintf("could not freeze thread=%s: no process to move or the cgroup refused", pick.Thread))
		return false
	}
	why := guardWhy(sig, e.SlotPolicy)
	n := 1
	if picked.episode.Equal(pick.Since) {
		n = picked.n + 1
	}
	faults := e.majorFaults(pick.Thread)
	fj := frozenJob{owner: pick.Thread, at: now, undeclared: true, episode: pick.Since, mem: pick.Mem, usec: usec, hasUsec: hasUsec, cmd: pick.Command}
	if turns {
		fj.active, fj.turnAt, fj.turn, fj.faults = first.PID, now, TurnMin, faults
		fj.parts = map[int]uint64{}
		for _, u := range e.workUnits(pick.Thread) {
			if u.PID != first.PID {
				fj.parts[u.PID] = u.RSS
			}
		}
	}
	e.mu.Lock()
	e.frozen = append(e.frozen, fj)
	frozenNow := len(e.frozen)
	e.undFroze[pick.Thread] = undFreeze{episode: pick.Since, n: n}
	e.mu.Unlock()
	if turns {
		log.Printf("guard: taking turns thread=%s units=%d first=%d cmd=%q mem=%s", pick.Thread, units, first.PID, first.Command, slots.GB(float64(first.RSS)))
	}
	cmd := pick.Command
	if cmd == "" {
		cmd = "unknown command"
	}
	log.Printf("guard: froze undeclared work thread=%s freeze=%d/%d episode_age=%s cmd=%q mem=%s cores=%.1f %s flagged=%d admitted=%t others_running=%d frozen_now=%d",
		pick.Thread, n, MaxFreezes, now.Sub(pick.Since).Round(time.Second), cmd, slots.GB(float64(pick.Mem)), pick.CPU, guardSignals(sig), len(fl), admitted, others(pick.Thread), frozenNow)
	e.guardRecord(slots.GuardEvent{At: now, Kind: "freeze", Work: "undeclared", Thread: pick.Thread, N: n}, sig)
	e.slotNotice(pick.Thread, map[string]any{"paused": why, "work": cmd})
	return true
}
