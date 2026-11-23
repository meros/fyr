package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

const rerunKillsKey = "engine.rerunKills"

func (e *Engine) loadRerunKillsLocked(now time.Time) {
	if e.st == nil {
		return
	}
	raw, _ := e.st.GetKV(rerunKillsKey)
	if raw == "" {
		return
	}
	var m map[string][]time.Time
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for entry, ks := range m {
		if ks = recent(ks, now); len(ks) > 0 {
			if e.rerunKills == nil {
				e.rerunKills = map[string][]time.Time{}
			}
			e.rerunKills[entry] = ks
		}
	}
}

func (e *Engine) saveRerunKillsLocked(now time.Time) {
	if e.st == nil {
		return
	}
	out := map[string][]time.Time{}
	for entry, ks := range e.rerunKills {
		if ks = recent(ks, now); len(ks) > 0 {
			out[entry] = ks
		} else {
			delete(e.rerunKills, entry)
		}
	}
	if len(out) == 0 {
		e.st.SetKV(rerunKillsKey, "")
		return
	}
	if b, err := json.Marshal(out); err == nil {
		e.st.SetKV(rerunKillsKey, string(b))
	}
}

// V0.rerun-classes (decision 0036 (F)): a job of a class the owner marked
// rerunnable is killed instead of frozen when it was frozen and could not wake
// within RerunAfter, or when it would be frozen and its learned wall time is
// under RerunShort. Its thread is told to run it again, the gate holds that
// rerun until memory is fine and it fits, and its learned peak rises by
// RerunRaise. A class killed RerunCap times in RerunCapFor is frozen instead
// for the rest of that hour, and the guard's history says so to the owner.
const (
	RerunAfter  = 2 * time.Minute
	RerunShort  = 5 * time.Minute
	RerunCap    = 3
	RerunCapFor = time.Hour
)

// RerunDeps are the rerunnable classes' measures and controls. All optional:
// without RerunMatch and JobKill nothing is killed.
type RerunDeps struct {
	// RerunMatch is the owner's entry that a job's heavy class or command
	// matches, "" for none.
	RerunMatch func(class, command string) string
	// RerunWall is the learned wall time (p90) of thread's admitted job's
	// class; false while unknown.
	RerunWall func(thread string) (time.Duration, bool)
	// JobKill kills thread's job (undeclared: its undeclared work) and
	// returns how many processes it killed.
	JobKill func(thread string, undeclared bool) int
	// RerunRaise raises the learned peak of thread's admitted job's class
	// (×1.2), before its admission goes back.
	RerunRaise func(thread string)
	// RerunSet is the owner's add or remove (host.rerunClasses).
	RerunSet func(args map[string]any) error
}

// rerunEntry is the owner's entry that makes f's work rerunnable now, "" when
// it is not: no entry matches, it is a service, or its class is capped.
func (e *Engine) rerunEntry(f frozenJob, now time.Time) string {
	if e.RerunMatch == nil || e.JobKill == nil || e.service(f.owner) {
		return ""
	}
	entry := e.RerunMatch(f.class, f.cmd)
	if entry == "" {
		return ""
	}
	if until, capped := e.rerunCapped(entry, now); capped {
		e.guardOnce(now, "rerun-cap:"+entry, fmt.Sprintf("not killing class=%q: killed %d times in an hour; frozen instead until %s", entry, RerunCap, until.Format("15:04")))
		return ""
	}
	return entry
}

// rerunCapped says entry was killed RerunCap times in the last RerunCapFor,
// and until when that holds.
func (e *Engine) rerunCapped(entry string, now time.Time) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ks := recent(e.rerunKills[entry], now)
	if len(ks) < RerunCap {
		return time.Time{}, false
	}
	return ks[len(ks)-RerunCap].Add(RerunCapFor), true
}

// recent are the kills within RerunCapFor of now.
func recent(ks []time.Time, now time.Time) []time.Time {
	return slices.DeleteFunc(slices.Clone(ks), func(t time.Time) bool { return now.Sub(t) >= RerunCapFor })
}

// shortJob says thread's admitted job usually ends within RerunShort, so a
// rerun costs less than a freeze.
func (e *Engine) shortJob(thread string) (time.Duration, bool) {
	if e.RerunWall == nil {
		return 0, false
	}
	w, ok := e.RerunWall(thread)
	return w, ok && w > 0 && w < RerunShort
}

// rerunKill kills f's work for entry, says why, and arranges its rerun; false
// when nothing was killed (the freeze then stays).
func (e *Engine) rerunKill(f frozenJob, entry, why string, sig resource.Signals, now time.Time) bool {
	n := e.JobKill(f.owner, f.undeclared)
	if n == 0 {
		e.guardOnce(now, "kill:"+f.owner, fmt.Sprintf("could not kill thread=%s class=%q: no process left in its cgroup", f.owner, entry))
		return false
	}
	kind, work := "job", "job"
	if f.undeclared {
		kind, work = "undeclared work", "undeclared"
	}
	e.mu.Lock()
	if e.rerunKills == nil {
		e.rerunKills = map[string][]time.Time{}
	}
	if e.rerunWait == nil {
		e.rerunWait = map[string]string{}
	}
	ks := append(recent(e.rerunKills[entry], now), now)
	e.rerunKills[entry] = ks
	e.saveRerunKillsLocked(now)
	e.rerunWait[f.owner] = entry
	if f.undeclared {
		u := e.undFroze[f.owner]
		u.thawedAt = now
		e.undFroze[f.owner] = u
	}
	e.mu.Unlock()
	log.Printf("guard: killed %s thread=%s class=%q why=%q procs=%d mem=%s frozen_for=%s kills_this_hour=%d %s", kind, f.owner, entry, why, n, resource.GB(float64(f.mem)), now.Sub(f.at).Round(time.Second), len(ks), guardSignals(sig))
	e.guardRecord(resource.GuardEvent{At: now, Kind: "kill", Work: work, Thread: f.owner, Class: entry}, sig)
	if len(ks) == RerunCap {
		log.Printf("guard: class=%q killed %d times in an hour: frozen instead of killed until %s", entry, RerunCap, ks[0].Add(RerunCapFor).Format("15:04"))
		e.guardRecord(resource.GuardEvent{At: now, Kind: "capped", Class: entry}, sig)
	}
	if !f.undeclared {
		if e.RerunRaise != nil {
			e.RerunRaise(f.owner) // while it still holds the admission: its class is known
		}
		e.dropSlot(f.owner) // the rerun is admitted again
	}
	e.slotNotice(f.owner, map[string]any{"killed": entry, "work": f.undeclared})
	return true
}

// RerunWaiting says thread's job was killed for a rerun that has not been
// admitted yet; fyrd's gate then names an undeclared command's class by its
// entry, so the rerun is admitted like a heavy job.
func (e *Engine) RerunWaiting(thread string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.rerunWait[thread]
	return ok
}

// rerunView is the classes killed in the last hour, for the front.
func (e *Engine) rerunView(now time.Time) []resource.GuardRerun {
	var out []resource.GuardRerun
	for entry, ks := range e.rerunKills {
		ks = recent(ks, now)
		if len(ks) == 0 {
			continue
		}
		r := resource.GuardRerun{Class: entry, Kills: len(ks)}
		if len(ks) >= RerunCap {
			t := ks[len(ks)-RerunCap].Add(RerunCapFor)
			r.CappedUntil = &t
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b resource.GuardRerun) int {
		if a.Class < b.Class {
			return -1
		}
		return 1
	})
	return out
}
