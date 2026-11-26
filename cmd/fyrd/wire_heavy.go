package main

import (
	"log"
	"strconv"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/slots"
)

// wireHeavy makes the heavy-work machinery: the slot pool, the jobs, the services and the sampler.
//
// Needs from earlier steps: mgr.
// Makes: book, detector, groups, guardService, heavyCmds, jobs, n, pool, rerun, sampler, services, stops, svcBook, usettings.
// Order: after wireAgents (w.mgr); wireGuard reads everything it makes.
func (w *wiring) wireHeavy() {
	cfg := w.cfg
	eng := w.eng
	h := w.h
	mgr := w.mgr
	tree := w.tree
	// V0.86: at most cfg.Slots heavy jobs at once; the deploy's tests first.
	// slots: 0 is off, absent or negative is the default; the pool survives a
	// restart in the store.
	n := slots.Default
	if cfg.Slots != nil {
		n = *cfg.Slots
	}
	pool := slots.Open(n, h.st)
	eng.UseSlots(pool)
	tree.SetRank(eng.ProjectRank) // V0.project-priority: a high project's threads get more CPU
	// Decision 0029 S1 (#185): measure each slot hold and the host's
	// signals; nothing is admitted by them yet.
	heavyCmds, err := heavy.New(heavy.Defaults)
	if cfg.HeavyCommands != nil {
		if m, e := heavy.New(cfg.HeavyCommands); e != nil {
			log.Printf("heavyCommands: %v; the defaults hold", e)
		} else {
			heavyCmds = m
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	jobs := &cgroup.Jobs{KV: h.st, Project: eng.ThreadProject}
	// V0.209: the gate hook admits a heavy command by itself.
	groups := &cgroup.JobGroups{Tree: tree, N: n}              // decision 0029 S5: each holder's job in a cgroup of its own; V0.admission-memory: N sizes a class under three runs
	groups.Size, groups.Raise = jobs.SizeOf, jobs.RaiseOverrun // V0.job-limits: a job sizes itself
	groups.Limit = guard.JobLimit                              // V0.guard-finish: the guard's policy decides its limits
	// V0.guard-finish: without the cgroup tree a thread's job is its process
	// tree, and ProcStops stops it; wireGuard gives it the trees.
	var stops *cgroup.ProcStops
	if tree == nil {
		stops = &cgroup.ProcStops{KV: h.st}
	}
	groups.Limited = func(thread, class, why, high string, budget uint64, runs int) { // V0.load-invariants: limit-under-prior
		if h, err := strconv.ParseUint(high, 10, 64); err == nil && n > 0 {
			eng.JobLimitSet(thread, class, why, h, resource.Prior(resource.Signals{Memory: budget}, n).Mem, cgroup.LimitSettled(why, runs))
		}
	}
	// V0.164, V0.209: the learned and added patterns; the detector of
	// work that nobody asked for, its settings and the record of its episodes.
	book := &heavy.Book{KV: h.st, Matcher: heavyCmds, Base: cfg.HeavyAdded}
	book.Load()
	usettings := &cgroup.UndeclaredSettings{KV: h.st, Base: cgroup.UndeclaredConfig{
		Off: cfg.Undeclared == "off", CPU: cfg.UndeclaredCPU, Memory: cfg.UndeclaredMemory, After: time.Duration(cfg.UndeclaredAfter) * time.Second}}
	usettings.Load()
	detector := &cgroup.Detector{}
	episodes := &cgroup.Episodes{KV: h.st}
	// V0.services-not-jobs: the service patterns, and the threads' running services.
	svcBook := &heavy.Services{KV: h.st, Base: cfg.ServicePatterns}
	svcBook.Load()
	services := &cgroup.Services{Match: svcBook.Match, Docker: &cgroup.Docker{}, KV: h.st,
		Cwd: func(th string) string { t, _ := eng.Thread(th); return t.Cwd }}
	// V0.rerun-classes: the classes the owner marked rerunnable on this host.
	rerun := &cgroup.RerunClasses{KV: h.st}
	rerun.Load()
	calls := &heavy.Calls{} // V0.freeze-controller: what each agent ran, to name its unadmitted work
	mgr.SetOnBash(func(c agent.BashCall) string {
		now := time.Now()
		groups.Bash(c.Thread, c.Command)
		services.Bash(c.Thread, c.Command)
		class, text := heavyCmds.Match(c.Command)
		// V0.freeze-controller: a thread that runs heavy work Fyr did not
		// admit and runs the same command again (the retry of a timeout) has a
		// class at once, without three finished episodes: the first storm
		// would be over before one ended.
		if class == "" && eng.HasUndeclared(c.Thread) {
			if r := calls.Repeat(c.Thread, c.Command, now); r != "" {
				class, text = r, r
			}
		}
		calls.Note(c.Thread, c.Command, now)
		if class == "" && eng.RerunWaiting(c.Thread) {
			class = rerun.Match("", c.Command) // a killed undeclared job's rerun is admitted too
		}
		jobs.Saw(c.Thread, class)
		groups.Seen(c.Thread, text)
		stops.Seen(c.Thread, text)
		if cfg.HeavyGate == "off" {
			return ""
		}
		return eng.HeavyGate(engine.HeavyCall{Thread: c.Thread, Class: class, ToolUse: c.ToolUse, Background: c.Background || heavy.Detached(c.Command), Subagent: c.Subagent, Agent: c.Agent, AgentType: c.AgentType})
	}, func(thread, toolUse string) { go eng.HeavyDone(thread, toolUse) }) // never on the stream's goroutine: it may grant
	// V0.subagent-admission: a subagent's admission reaches it, not its parent.
	eng.SubagentLive = mgr.SubagentLive
	mgr.SetSubagentHooks(eng.SubagentNote, eng.SubagentEnded)
	sampler := &cgroup.Sampler{Tree: tree, Slots: pool, Probe: &cgroup.Slowdown{}, Jobs: jobs, Groups: groups, // V0.80, V0.86
		Detect: detector, DetectConfig: usettings.Get, Settings: usettings, Book: book, Project: eng.ThreadProject, // V0.164
		Services: services, ServiceBook: svcBook, // V0.services-not-jobs
		Rerun: rerun} // V0.rerun-classes
	if stops != nil {
		sampler.Control = stops
	}
	// Decision 0036 (D), E4: the guard never freezes or kills work that holds
	// one of its thread's services (a dev stack, `docker compose up`).
	guardService := func(th string) bool {
		for _, sv := range services.Of(th) {
			if groups.Holds(th, sv.PID) {
				return true
			}
		}
		return false
	}
	// V0.guard-tidy: a rotation of a thread no longer flagged does nothing.
	groups.Live = func(thread string) bool {
		for _, f := range detector.Flags() {
			if f.Thread == thread {
				return true
			}
		}
		return false
	}
	if stops != nil {
		stops.Live = groups.Live
	}
	// V0.164: an ended episode is kept, teaches its command, and ends its frozen group.
	detector.OnEnd = func(ep cgroup.Episode) {
		ep.Project = eng.ThreadProject(ep.Thread)
		episodes.Add(ep)
		// V0.freeze-controller: the command the agent ran names the work (the
		// gate matches that text); the process command line is the fallback.
		cmd := calls.Cause(ep.Thread, ep.Start)
		if cmd == "" {
			cmd = ep.Command
		}
		if cmd != "" {
			book.Saw(heavy.Seen{Thread: ep.Thread, Command: cmd, At: ep.End, Memory: ep.PeakMem, CPU: ep.MeanCores, Seconds: int(ep.End.Sub(ep.Start).Seconds())})
		}
		groups.EndUndeclared(ep.Thread)
		stops.EndUndeclared(ep.Thread)
	}
	w.book = book
	w.detector = detector
	w.groups = groups
	w.stops = stops
	w.guardService = guardService
	w.heavyCmds = heavyCmds
	w.jobs = jobs
	w.n = n
	w.pool = pool
	w.rerun = rerun
	w.sampler = sampler
	w.services = services
	w.svcBook = svcBook
	w.usettings = usettings
}
