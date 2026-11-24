// The memory guard's tick as the engine runs it (V0.guard-tick): it gathers
// the inputs, runs guard.Tick, and applies its decisions through the host's
// controls, its own records and the notices.

package engine

import (
	"encoding/json"
	"log"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

// guardTick runs the memory guard's tick on the pool's snapshot s: the
// freezes, thaws, wakes, kills and turns, then the memory notice. guard.Tick
// decides; the engine applies, outside e.mu.
func (e *Engine) guardTick(s resource.Snapshot) {
	can := guard.Can{
		Guard:      e.SlotFreeze != nil && e.SlotThaw != nil && e.SlotSignals != nil,
		Undeclared: e.UndeclaredFreeze != nil && e.UndeclaredThaw != nil,
		FlagNow:    e.UndeclaredFlagNow != nil,
		Rerun:      e.RerunMatch != nil,
		Kill:       e.RerunMatch != nil && e.JobKill != nil,
		Memory:     e.SlotSignals != nil && e.SlotGrowth != nil,
	}
	if !can.Guard && !can.Memory {
		return
	}
	now := e.Now()
	in := guard.Inputs{Now: now, Policy: e.SlotPolicy, Held: s.Held, Can: can, View: &guardView{e: e}}
	if can.Guard {
		in.Signals = e.SlotSignals()
		in.Flags = e.flags()
		in.Running = e.Running()
	}
	e.mu.Lock()
	in.Trig = e.guardTrig
	st := e.gs
	e.mu.Unlock()
	next := guard.Run(in, st, func(d guard.Decision) guard.Answer { return e.applyGuard(d, now) })
	e.mu.Lock()
	e.gs = next
	e.mu.Unlock()
}

// applyGuard applies one of the guard's decisions and answers it.
func (e *Engine) applyGuard(d guard.Decision, now time.Time) guard.Answer {
	switch d.Kind {
	case guard.Log:
		if d.Key != "" {
			e.guardOnce(now, d.Key, d.Msg)
		} else {
			log.Print(d.Msg)
		}
	case guard.Record:
		e.guardRecord(d.Event, d.Signals)
	case guard.Notify:
		e.slotNotice(d.Thread, d.Args)
	case guard.NotifyMemory:
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotMemory, Thread: d.Thread, Args: core.SlotMemoryArgs{Why: d.Why, Grew: d.Grew}}); err != nil {
			log.Printf("slot memory %s: %v", d.Thread, err)
		}
	case guard.Freeze:
		a := guard.Answer{OK: e.SlotFreeze(d.Thread)}
		if a.OK && e.SlotMem != nil {
			a.Mem = e.SlotMem(d.Thread)
		}
		return a
	case guard.FreezeUndeclared:
		return guard.Answer{OK: e.UndeclaredFreeze(d.Thread)}
	case guard.Thaw:
		if d.Undeclared {
			e.UndeclaredThaw(d.Thread)
		} else {
			e.SlotThaw(d.Thread)
		}
	case guard.Rotate:
		return guard.Answer{OK: e.UndeclaredRotate(d.Thread, d.Units, d.PID)}
	case guard.Kill:
		return guard.Answer{N: e.JobKill(d.Thread, d.Undeclared)}
	case guard.FlagNow:
		th := e.UndeclaredFlagNow()
		return guard.Answer{Thread: th, Flags: e.flags()}
	case guard.ReadSignals:
		return guard.Answer{Signals: e.SlotSignals()}
	case guard.ReadGrowth:
		g := map[string]uint64{}
		for _, th := range d.Threads {
			g[th] = e.SlotGrowth(th)
		}
		return guard.Answer{Growth: g}
	case guard.ReadTurn:
		return guard.Answer{Units: e.workUnits(d.Thread), Faults: e.majorFaults(d.Thread)}
	case guard.Raise:
		if e.RerunRaise != nil {
			e.RerunRaise(d.Thread)
		}
	case guard.Release:
		e.dropSlot(d.Thread)
	case guard.Thawed:
		e.mu.Lock()
		if e.jobThawed == nil {
			e.jobThawed = map[string]time.Time{}
		}
		e.jobThawed[d.Thread] = d.At
		e.mu.Unlock()
	case guard.RerunWait:
		e.mu.Lock()
		if e.rerunWait == nil {
			e.rerunWait = map[string]string{}
		}
		e.rerunWait[d.Thread] = d.Entry
		e.mu.Unlock()
	case guard.KeepKills:
		e.keepRerunKills(d.Kills)
	case guard.ClearGate:
		e.mu.Lock()
		var ths []string
		for th := range e.gateHeld {
			ths = append(ths, th)
		}
		clear(e.gateHeld)
		e.mu.Unlock()
		for _, th := range ths {
			e.setGateHold(th, "") // V0.gate-hold-visible: memory recovered
		}
		return guard.Answer{Threads: ths}
	}
	return guard.Answer{}
}

// keepRerunKills writes the rerunnable classes' kills of the last hour to the
// store (V0.survive-update).
func (e *Engine) keepRerunKills(kills map[string][]time.Time) {
	if e.st == nil {
		return
	}
	if len(kills) == 0 {
		e.st.SetKV(rerunKillsKey, "")
		return
	}
	if b, err := json.Marshal(kills); err == nil {
		e.st.SetKV(rerunKillsKey, string(b))
	}
}

// guardView answers guard.View from the engine's dependencies, each question
// once per tick: every call of guard.Tick in one tick reads the same answer.
type guardView struct {
	e    *Engine
	memo map[string]any
}

// once returns the answer to key, read with read the first time.
func once[T any](v *guardView, key string, read func() T) T {
	if a, ok := v.memo[key]; ok {
		return a.(T)
	}
	if v.memo == nil {
		v.memo = map[string]any{}
	}
	a := read()
	v.memo[key] = a
	return a
}

type usecRead struct {
	u  uint64
	ok bool
}

type wallRead struct {
	d  time.Duration
	ok bool
}

func (v *guardView) Growth(th string) uint64 {
	return once(v, "growth:"+th, func() uint64 {
		if v.e.SlotGrowth == nil {
			return 0
		}
		return v.e.SlotGrowth(th)
	})
}

func (v *guardView) Mem(th string) uint64 {
	return once(v, "mem:"+th, func() uint64 {
		if v.e.SlotMem == nil {
			return 0
		}
		return v.e.SlotMem(th)
	})
}

func (v *guardView) Service(th string) bool {
	return once(v, "service:"+th, func() bool { return v.e.GuardService != nil && v.e.GuardService(th) })
}

func (v *guardView) Empty(th string, undeclared bool) bool {
	if undeclared {
		return once(v, "uempty:"+th, func() bool { return v.e.UndeclaredEmpty != nil && v.e.UndeclaredEmpty(th) })
	}
	return once(v, "empty:"+th, func() bool { return v.e.SlotEmpty != nil && v.e.SlotEmpty(th) })
}

func (v *guardView) RootsMem(th string) (uint64, bool) {
	r := once(v, "roots:"+th, func() usecRead {
		if v.e.UndeclaredRootsMem == nil {
			return usecRead{}
		}
		m, ok := v.e.UndeclaredRootsMem(th)
		return usecRead{m, ok}
	})
	return r.u, r.ok
}

func (v *guardView) Units(th string) []resource.WorkUnit {
	return once(v, "units:"+th, func() []resource.WorkUnit { return v.e.workUnits(th) })
}

func (v *guardView) Faults(th string) uint64 {
	return once(v, "faults:"+th, func() uint64 { return v.e.majorFaults(th) })
}

func (v *guardView) FrozenUsec(th string, undeclared bool) (uint64, bool) {
	key := "usec:" + th
	if undeclared {
		key = "uusec:" + th
	}
	r := once(v, key, func() usecRead {
		if v.e.FrozenUsec == nil {
			return usecRead{}
		}
		u, ok := v.e.FrozenUsec(th, undeclared)
		return usecRead{u, ok}
	})
	return r.u, r.ok
}

func (v *guardView) Wall(th string) (time.Duration, bool) {
	r := once(v, "wall:"+th, func() wallRead {
		if v.e.RerunWall == nil {
			return wallRead{}
		}
		d, ok := v.e.RerunWall(th)
		return wallRead{d, ok}
	})
	return r.d, r.ok
}

func (v *guardView) Class(th string) string {
	return once(v, "class:"+th, func() string {
		v.e.heavyMu.Lock()
		defer v.e.heavyMu.Unlock()
		return v.e.heavyClass[th]
	})
}

func (v *guardView) Rerunnable(class, command string) string {
	if v.e.RerunMatch == nil || v.e.JobKill == nil {
		return ""
	}
	return once(v, "rerun:"+class+"\x00"+command, func() string { return v.e.RerunMatch(class, command) })
}

func (v *guardView) Exists(th string) bool {
	return once(v, "exists:"+th, func() bool {
		v.e.mu.Lock()
		defer v.e.mu.Unlock()
		return v.e.threads[th] != nil
	})
}

// slotNotice records a slot event for the thread's agent (decision 0029).
func (e *Engine) slotNotice(owner string, d map[string]any) {
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotPause, Thread: owner, Args: d}); err != nil {
		log.Printf("slot pause %s: %v", owner, err)
	}
}
