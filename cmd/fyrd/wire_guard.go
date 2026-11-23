package main

import (
	"context"
	"path/filepath"
	"runtime"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/heavy"
)

// wireGuard connects the memory guard: the sampler, groups and leftovers to the engine.
//
// Needs from earlier steps: book, detector, groups, guardService, heavyCmds, jobs, mgr, n, pool, rerun, sampler, services, stops, svcBook, usettings.
// Makes: nothing on w (it wires what it made into eng, mgr and the others).
func (w *wiring) wireGuard() {
	book := w.book
	cfg := w.cfg
	data := w.data
	detector := w.detector
	eng := w.eng
	groups := w.groups
	guardService := w.guardService
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
	stops := w.stops
	tree := w.tree
	usettings := w.usettings
	pool.Undeclared = sampler.Undeclared
	// V0.job-limits: the tools that ignore cgroups get shares of this host.
	hints := cgroup.JobHints{Sampler: sampler, Services: services, Worker: heavy.WorkerHint}
	mgr.SetJobHints(hints.Env, hints.Hint)
	pool.Guard = eng.GuardView             // decision 0032
	sampler.Invariants = eng.InvariantView // V0.thread-invariants: fallback when slots are off
	// V0.guard-mechanism, V0.guard-finish: the guard's freeze, thaw, kill and
	// signals go through its Mechanism: the cgroup tree's, or the process
	// table's on a host without it. ctl is the same host's job control.
	var mech guard.Mechanism
	var parts guard.Parts // the same host's undeclared work and its turns
	var ctl cgroup.Control
	if tree == nil {
		// V0.164 section 10, V0.process-tree-hosts: no cgroup tree, so each thread is its
		// process tree under the CLI, and a pause is SIGSTOP.
		trees := &cgroup.TreeReader{}
		if runtime.GOOS == "darwin" {
			// No w.ctx yet at this step: cgroup.PS bounds each run itself.
			trees.PS = func() (string, error) { return cgroup.PS(context.Background(), "-A", "-o", cgroup.TreePS) }
		}
		sampler.Trees, sampler.CLIs = trees, mgr.CLIs
		stops.Trees, stops.CLIs = trees, mgr.CLIs
		stops.ResumeStopped() // what the last fyrd left stopped
		pm := &cgroup.ProcMechanism{Stops: stops, Sampler: sampler, Detector: detector}
		mech, parts, ctl = pm, pm, stops
		eng.QuietDeps = engine.QuietDeps{ThreadCPUTime: sampler.TreeCPUTime, ThreadFrozen: stops.Frozen}
	} else {
		sampler.CLIs = mgr.CLIs // V0.freeze-old-work: the roots never take the CLI
		groups.ThawLeftFrozen() // V0.thaw-at-start: what the last fyrd left frozen
		cm := &cgroup.Mechanism{Groups: groups, Sampler: sampler, Detector: detector}
		mech, parts, ctl = cm, cm, groups
		eng.QuietDeps = engine.QuietDeps{ThreadCPUTime: tree.CPUTime, ThreadFrozen: groups.Frozen}
	}
	eng.UndeclaredDeps = engine.UndeclaredDeps{
		Undeclared: sampler.Undeclared, UndeclaredSettings: usettings.Set,
		UndeclaredThaw: mech.ThawUndeclared, // V0.episode-frozen: its calm counts again
		// V0.guard-tidy: what the episode's processes hold, against its flagged memory.
		UndeclaredRootsMem: parts.RootsMem,
		UndeclaredFlagNow:  sampler.FlagNow, // V0.freeze-fast-class-name
		UndeclaredEmpty:    ctl.UndeclaredEmpty, HeavyRemove: book.Remove, ServicePatterns: svcBook.Edit, HeavyRefused: heavyCmds.Refused, HeavySoft: heavyCmds.Soft,
		UndeclaredFreeze: mech.FreezeUndeclared,
		// Decision 0032 slice 2: the parts of the work take turns.
		UndeclaredUnits: parts.Units, UndeclaredRotate: parts.Rotate,
		UndeclaredFaults: parts.Faults,
	}
	// Decision 0029 S2–S4, V0.163, V0.164: admission by a budget of costs,
	// gated by the slowdown and memory; the policy is internal/guard.
	adm := guard.NewAdmission(pool, n, sampler, jobs, cfg.SlotSlowdownMax, cfg.SlotMemoryReserve)
	adm.Open = eng.HeavyOpen // V0.admission-tidy: a quiet open call is not idle
	pool.Install(adm)
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
		SlotEmpty: ctl.JobEmpty, // V0.guard-tidy
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
		RerunWall: adm.RerunWall, RerunRaise: adm.RerunRaise}
}
