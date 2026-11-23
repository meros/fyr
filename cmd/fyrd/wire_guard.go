package main

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/meros/fyr/internal/admission"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/heavy"
)

// wireGuard connects the memory guard: the sampler, groups and leftovers to the engine.
//
// Needs from earlier steps: book, detector, groups, guardService, heavyCmds, jobs, mgr, n, pool, rerun, sampler, services, svcBook, usettings.
// Makes: nothing on w (it wires what it made into eng, mgr and the others).
func (w *wiring) wireGuard() {
	book := w.book
	cfg := w.cfg
	data := w.data
	detector := w.detector
	eng := w.eng
	groups := w.groups
	guardService := w.guardService
	h := w.h
	heavyCmds := w.heavyCmds
	hostName := w.hostName
	jobs := w.jobs
	mgr := w.mgr
	n := w.n
	pool := w.pool
	rerun := w.rerun
	sampler := w.sampler
	services := w.services
	svcBook := w.svcBook
	tree := w.tree
	usettings := w.usettings
	pool.Undeclared = sampler.Undeclared
	// V0.guard-mechanism: the guard's freeze, thaw, kill and signals go through
	// its Mechanism; cgroup's serves a host with the tree and one without (the process table).
	var mech guard.Mechanism = &cgroup.Mechanism{Groups: groups, Sampler: sampler, Detector: detector}
	// V0.job-limits: the tools that ignore cgroups get shares of this host.
	capOf := func() cgroup.Capacity {
		sig := sampler.Signals()
		return cgroup.Capacity{CPU: sig.CPU, Memory: sig.Memory}
	}
	mgr.SetJobHints(func() []string { return cgroup.JobEnv(capOf()) },
		func(thread, c string) string {
			return strings.TrimSpace(heavy.WorkerHint(c, cgroup.Workers(capOf())) + "\n" + services.Notice(thread, capOf()))
		})
	pool.Guard = eng.GuardView             // decision 0032
	sampler.Invariants = eng.InvariantView // V0.thread-invariants: fallback when slots are off
	if tree == nil {
		// V0.164 section 10, V0.process-tree-hosts: no cgroup tree, so each thread is its
		// process tree under the CLI, and a pause is SIGSTOP.
		trees := &cgroup.TreeReader{}
		if runtime.GOOS == "darwin" {
			// No w.ctx yet at this step: cgroup.PS bounds each run itself.
			trees.PS = func() (string, error) { return cgroup.PS(context.Background(), "-A", "-o", cgroup.TreePS) }
		}
		sampler.Trees, sampler.CLIs = trees, mgr.CLIs
		groups.Trees, groups.CLIs, groups.KV = trees, mgr.CLIs, h.st
		groups.ResumeStopped() // what the last fyrd left stopped
		eng.QuietDeps = engine.QuietDeps{ThreadCPUTime: sampler.TreeCPUTime, ThreadFrozen: groups.Frozen}
	} else {
		sampler.CLIs = mgr.CLIs // V0.freeze-old-work: the roots never take the CLI
		groups.ThawLeftFrozen() // V0.thaw-at-start: what the last fyrd left frozen
		eng.QuietDeps = engine.QuietDeps{ThreadCPUTime: tree.CPUTime, ThreadFrozen: groups.Frozen}
	}
	eng.UndeclaredDeps = engine.UndeclaredDeps{
		Undeclared: sampler.Undeclared, UndeclaredSettings: usettings.Set,
		UndeclaredThaw: mech.ThawUndeclared, // V0.episode-frozen: its calm counts again
		// V0.guard-tidy: what the episode's processes hold, against its flagged memory.
		UndeclaredRootsMem: func(thread string) (uint64, bool) {
			for _, f := range detector.Flags() {
				if f.Thread == thread {
					var m uint64
					for _, r := range sampler.Roots(f) {
						m += r.RSS
					}
					return m, true
				}
			}
			return 0, false
		},
		UndeclaredFlagNow: sampler.FlagNow, // V0.freeze-fast-class-name
		UndeclaredEmpty:   groups.UndeclaredEmpty, HeavyRemove: book.Remove, ServicePatterns: svcBook.Edit, HeavyRefused: heavyCmds.Refused, HeavySoft: heavyCmds.Soft,
		UndeclaredFreeze: mech.FreezeUndeclared,
		// Decision 0032 slice 2: the parts of the work take turns.
		UndeclaredUnits: func(thread string) []resource.WorkUnit {
			for _, f := range detector.Flags() {
				if f.Thread != thread {
					continue
				}
				var out []resource.WorkUnit
				for _, u := range groups.Units(thread, sampler.Roots(f)) {
					out = append(out, resource.WorkUnit{PID: u.PID, RSS: u.RSS, Command: cgroup.CommandOf(cgroup.Root{PID: u.PID, Cmdline: u.Cmdline}, groups.Bashes(thread))})
				}
				return out
			}
			return nil
		},
		UndeclaredRotate: func(thread string, units []resource.WorkUnit, active int) bool {
			us := make([]cgroup.Unit, 0, len(units))
			for _, u := range units {
				us = append(us, cgroup.Unit{PID: u.PID, RSS: u.RSS})
			}
			if !groups.RotateUndeclared(thread, us, active) {
				return false
			}
			detector.MarkPaused(thread)
			return true
		},
		UndeclaredFaults: groups.MajorFaultBytes,
	}
	// Decision 0029 S2–S4, V0.163, V0.164: admission by a budget of costs,
	// gated by the slowdown and memory; the policy is internal/admission.
	adm := admission.New(pool, n, sampler, jobs, cfg.SlotSlowdownMax, cfg.SlotMemoryReserve)
	adm.Open = eng.HeavyOpen // V0.admission-tidy: a quiet open call is not idle
	adm.Install()
	// V0.153: on a thrashing host the agents wait; Fyr kills no CLI for
	// being slow and restarts none into the same thrash.
	mgr.SetPressure(adm.Thrashing)
	eng.HostName = hostName           // V0.163: the heavy-job answers name this host
	eng.FolderEnv = engine.FolderArgv // V0.run-folder-env: a Run card runs in its folder's direnv
	sd := engine.SlotDeps{
		SlotActive:  jobs.Active, // #174
		SlotSignals: mech.Sample, SlotGrowth: jobs.Growth, SlotPolicy: adm.Policy,
		SlotOver:   adm.Over, // V0.163
		JobReset:   jobs.Reset,
		SlotFreeze: mech.Freeze, SlotThaw: mech.Thaw, // decision 0029 S5
		SlotEmpty: groups.JobEmpty, // V0.guard-tidy
		// V0.guard-wake: the wake rule's memory and the time frozen.
		SlotMem: sampler.ThreadMemory, FrozenUsec: groups.FrozenUsec,
		GuardService: guardService, // decision 0036 (D)
		// V0.admission-tidy: a heavier second command, and the job's baseline at the grant
		SlotClassNeed: adm.ClassNeed, SlotBaseline: sampler.Baseline,
	}
	if !pool.Off() {
		// decision 0029 S4: the budget adapts; V0.budget-restart: and outlives a deploy
		sd.SlotAIMD = &guard.AIMD{N: float64(n), Path: filepath.Join(data, "slot-budget.json"), Policy: adm.Policy} // V0.admission-tidy: the configured reserve
	}
	eng.SlotDeps = sd
	// V0.rerun-classes: kill and rerun, for the classes the owner marked.
	eng.RerunDeps = engine.RerunDeps{RerunMatch: rerun.Match, RerunSet: rerun.Set, JobKill: mech.Kill,
		RerunWall: func(thread string) (time.Duration, bool) {
			hd, ok := pool.Holding(thread)
			if !ok {
				return 0, false
			}
			l, ok := jobs.Learned(jobs.ClassOf(thread, hd.Kind))
			if !ok || len(l.Walls) == 0 {
				return 0, false
			}
			return time.Duration(l.WallP90() * float64(time.Second)), true
		},
		RerunRaise: func(thread string) {
			if hd, ok := pool.Holding(thread); ok {
				jobs.RaisePeak(jobs.ClassOf(thread, hd.Kind), 1.2)
			}
		},
	}

}
