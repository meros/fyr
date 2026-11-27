package guard

// V0.oomd-frozen-work: past oomd's limit the guard ends work before oomd ends
// a thread. On 27 Nov the agents' swap was full from 11:15 (10.1 of 10.1 GB),
// so freezing freed no memory (frozen pages had nowhere to go), and oomd
// killed six thread scopes in 13 minutes, each with its agent. It picks by
// pgscan since its last look, which was a few pages for every scope then: at
// 11:29:12 it took a scope whose work the guard had frozen 3 s before, by 2
// pages. Fyr cannot steer that pick: oomd reads user.oomd_avoid only on
// cgroups root owns, and a thread's scope is delegated to its owner.
//
// So once the agents' stall has stayed at or over oomd's limit OomdEndAfter,
// the guard kills one thread's heavy work, never its agent: the frozen work
// that holds most (it makes no progress, and ending it frees its swap too),
// else the running heavy work that holds most, never the oldest (one always
// runs) or a service. The thread is told, and its agent can run it again.

import (
	"fmt"
	"slices"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// overOomd says the agents' stall is at or over oomd's limit.
func overOomd(s resource.Signals) bool {
	return s.OomdLimit > 0 && s.UnitStall != nil && *s.UnitStall >= s.OomdLimit
}

// endBeforeOomd ends one thread's heavy work once the stall has stayed over
// oomd's limit OomdEndAfter; it reports whether it ended any.
func (t *tick) endBeforeOomd(sig resource.Signals) bool {
	now, st, v := t.in.Now, &t.st, t.in.View
	if !overOomd(sig) {
		st.OverOomd = time.Time{}
		return false
	}
	if st.OverOomd.IsZero() {
		st.OverOomd = now
	}
	if !t.in.Can.End || now.Sub(st.OverOomd) < OomdEndAfter {
		return false
	}
	owner, und, cmd, frozen, best := "", false, "", -1, uint64(0)
	for i, f := range st.Frozen {
		if m := v.Mem(f.Owner); owner == "" || m > best {
			owner, und, cmd, frozen, best = f.Owner, f.Undeclared, f.Cmd, i, m
			if !f.Undeclared {
				cmd = f.Class
			}
		}
	}
	if owner == "" {
		heavy := RunningHeavy(t.in.Held, t.flags, st.Frozen)
		ths := make([]string, 0, len(heavy))
		for th := range heavy {
			ths = append(ths, th)
		}
		slices.Sort(ths)
		oldest := ""
		for _, th := range ths {
			if oldest == "" || heavy[th].Before(heavy[oldest]) {
				oldest = th
			}
		}
		for _, th := range ths {
			if th == oldest || v.Service(th) {
				continue
			}
			if m := v.Mem(th); owner == "" || m > best {
				owner, best = th, m
				held := slices.ContainsFunc(t.in.Held, func(h resource.Holder) bool { return h.Owner == th && !h.Priority })
				und, cmd = !held, v.Class(th)
				if und {
					cmd = ""
					for _, f := range t.flags {
						if f.Thread == th {
							cmd = f.Command
						}
					}
				}
			}
		}
	}
	if owner == "" {
		t.once("end:none", fmt.Sprintf("guard: over oomd's limit for %s and no heavy work to end but the oldest (%s)", now.Sub(st.OverOomd).Round(time.Second), SignalsLine(sig)))
		return false
	}
	// Running undeclared work is still in cli/ beside the agent: the freeze
	// moves its processes into undeclared/, whose cgroup.kill then takes them
	// and not the agent.
	if und && frozen < 0 && !t.ask(Decision{Kind: FreezeUndeclared, Thread: owner}).OK {
		t.once("end:"+owner, fmt.Sprintf("guard: could not end the work of thread=%s before oomd: no process to move or the cgroup refused", owner))
		return false
	}
	n := t.ask(Decision{Kind: Kill, Thread: owner, Undeclared: und}).N
	if n == 0 {
		t.once("end:"+owner, fmt.Sprintf("guard: could not end the work of thread=%s before oomd: no process left in its cgroup", owner))
		return false
	}
	kind, work := "job", "job"
	if und {
		kind, work = "undeclared work", "undeclared"
	}
	if cmd == "" {
		cmd = "unknown command"
	}
	if frozen >= 0 {
		st.Frozen = slices.Delete(st.Frozen, frozen, frozen+1)
		t.emit(Decision{Kind: Thaw, Thread: owner, Undeclared: und}) // the killed processes leave a frozen cgroup
	}
	if und {
		u := st.UndFroze[owner]
		u.ThawedAt = now
		st.UndFroze[owner] = u
	} else {
		t.emit(Decision{Kind: Release, Thread: owner}) // its rerun is admitted again
	}
	over := now.Sub(st.OverOomd).Round(time.Second)
	st.OverOomd = time.Time{} // the next one waits OomdEndAfter again, for this kill to free memory
	t.log("guard: ended %s thread=%s before oomd: over its limit for %s cmd=%q frozen=%t procs=%d mem=%s %s", kind, owner, over, cmd, frozen >= 0, n, resource.GB(float64(best)), SignalsLine(sig))
	t.record(resource.GuardEvent{At: now, Kind: "ended", Work: work, Thread: owner}, sig)
	t.notify(owner, map[string]any{"ended": cmd, "work": und})
	return true
}
