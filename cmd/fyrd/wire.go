package main

import (
	"context"
	"errors"
	"github.com/meros/fyr/internal/lockwatch"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/admission"
	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/pipe"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/setup"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/stream"
	"github.com/meros/fyr/internal/tailnet"
)

// wiring holds what host.serve builds, so each wire* step reads what the steps before it made.
type wiring struct {
	cfg          config
	h            *host
	webFS        fs.FS
	hostName     string
	data         string
	tree         *cgroup.Tree
	rn           *kept
	adm          *admission.Admission
	att          *attach.Store
	authStore    *auth.Store
	book         *heavy.Book
	capOf        func() cgroup.Capacity
	createPR     func(ctx context.Context, req github.CreatePRReq) (string, error)
	ctx          context.Context
	dep          *deploy.Deployer
	deployGate   func(ctx context.Context, dir string) (string, string)
	detector     *cgroup.Detector
	eng          *engine.Engine
	episodes     *cgroup.Episodes
	fe           *runningFront
	gh           github.Runner
	ghBudget     *github.Budget
	ghMeter      *github.Meter
	ghView       func() github.MeterView
	groups       *cgroup.JobGroups
	guardService func(th string) bool
	health       []deploy.Check
	heavyCmds    *heavy.Matcher
	hostHandler  http.Handler
	id           string
	jobs         *cgroup.Jobs
	key          *identity.Key
	ln           net.Listener
	mcpSrv       *mcp.Server
	metered      github.Runner
	mgr          *agent.Manager
	n            int
	old          *oldListeners
	peerEnvPtr   atomic.Pointer[peer.Env]
	pollGh       github.Runner
	poller       *github.Poller
	pool         *slots.Pool
	prDetails    *github.Details
	quotaList    func() []quota.Profile
	quotas       *quota.Service
	reg          *provider.Registry
	rerun        *cgroup.RerunClasses
	sampler      *cgroup.Sampler
	services     *cgroup.Services
	setupChecker *setup.Checker
	srv          *server.Server
	srvs         *servers
	stop         context.CancelFunc
	svcBook      *heavy.Services
	updateNow    func() error
	usettings    *cgroup.UndeclaredSettings
	watcher      *deploy.Watcher
}

func (w *wiring) wireQuota() {
	cfg := w.cfg
	data := w.data
	eng := w.eng
	reg := w.reg
	// V0.44: provider quota, read with the logins on this host.
	quotaList := func() []quota.Profile {
		if reg.Empty() {
			return quotaProfiles(cfg.Profiles)
		}
		return reg.QuotaProfiles()
	}
	quotas := quota.New(nil, home())
	quotas.Client = &http.Client{Timeout: quotaHTTPTimeout}
	quotas.Logf = log.Printf
	// V0.122: the data and the rate-limit wait survive a restart, so frequent deploys never mean frequent reads.
	quotas.StatePath = filepath.Join(data, "quota-state.json")
	quotas.OnUpdate = eng.SyncQuotaResets
	quotas.SetProfiles(quotaList())
	reg.OnChange = func() { quotas.SetProfiles(quotaList()) }
	w.quotas = quotas
}

func (w *wiring) wireAgents() {
	att := w.att
	cfg := w.cfg
	data := w.data
	eng := w.eng
	h := w.h
	hostName := w.hostName
	ln := w.ln
	quotas := w.quotas
	rn := w.rn
	tree := w.tree
	var mgr *agent.Manager
	mcpSrv := mcp.New(func(tok string) (string, bool) { return mgr.ThreadForToken(tok) })
	eng.Tools(mcpSrv)
	mgr = agent.New(eng, agent.Config{
		Profile: func(id string) provider.Profile {
			if p, ok := h.profile(id); ok {
				return p
			}
			return h.profiles()[0]
		},
		IdleStop: time.Duration(cfg.IdleStopMinutes) * time.Minute, MCPURL: "http://" + ln.Addr().String() + "/mcp",
		LogDir: filepath.Join(data, "logs"), RunDir: filepath.Join(data, "run"), Attachments: att.Get, OnRateLimit: quotas.Observe, OnCommands: eng.SetCommands, HandoverDir: filepath.Join(data, "handover"),
		Cgroups: tree, Runner: rn.client(),
		// V0.115: the system agents run on this host, on the owner's setting.
		HostName: hostName, ProfileExists: func(id string) bool { _, ok := h.profile(id); return ok }, SystemAgents: eng.SystemAgents,
		// V0.tool-approval-judge: project/thread/owner context for auto-judging tool approvals.
		ApprovalContext: eng.ApprovalContext,
		// V0.tool-reload: a process from an older build that has not read
		// this build's tools ends at its next idle point.
		Tools: mcpSrv.Hash, ToolsCurrent: mcpSrv.Current, Build: BuildID(),
	})
	eng.AgentDeps = engine.AgentDeps{
		Agents: mgr,
		AgentInspect: func() map[string]engine.AgentState { // V0.thread-invariants
			raw := mgr.Inspect()
			out := make(map[string]engine.AgentState, len(raw))
			for id, s := range raw {
				out[id] = engine.AgentState{Live: s.Live, Starting: s.Starting, TurnLive: s.TurnLive, Asks: s.Asks, Outbox: s.Outbox, BG: s.BG, LastOut: s.LastOut, OpenTools: s.OpenTools, Judging: s.Judging}
			}
			return out
		},
		Titler:           mgr.Title,         // V0.108
		TextJudge:        mgr.JudgeText,     // V0.210
		Searcher:         mgr.SearchThreads, // V0.ai-search
		Providers:        h.reg,             // #48: provider commands go down the engine's path
		AttachmentExists: func(id string) bool { _, err := att.Get(id); return err == nil },
		ProfileExists:    func(id string) bool { _, ok := h.profile(id); return ok },
		SystemAgentResolve: func(job string) (string, string, error) { // V0.115: Settings shows what each job runs
			a, err := mgr.SystemAgent(job)
			return a.Profile.ID, a.Model, err
		},
		SystemAgentProviders: func() []engine.SystemProvider {
			var out []engine.SystemProvider
			for _, p := range h.profiles() {
				out = append(out, engine.SystemProvider{ID: p.ID, Name: p.Name, Models: p.Models, Haiku: agent.HaikuModel(p), Unavailable: p.Unavailable})
			}
			return out
		},
		QuotaReset:    quotas.ResetOf,
		QuotaSnapshot: quotas.Snapshot,
	}
	w.mcpSrv = mcpSrv
	w.mgr = mgr
}

func (w *wiring) wireProjects() {
	data := w.data
	eng := w.eng
	mgr := w.mgr
	rn := w.rn
	// V0.116: project setup. The icons live with the host's data; a new
	// project with a folder gets a setup run, and each project from before
	// gets one once, a minute after start and 30 s apart. The run's provider
	// and model are the owner's System agents setting (SystemAgentResolve).
	eng.Icons = &projicon.Store{Dir: filepath.Join(data, "project-icons")}
	eng.AutoSetup = true
	go func() {
		time.Sleep(setupExistingAfter)
		if n := eng.SetupExisting(setupExistingEvery); n > 0 {
			log.Printf("project setup: started %d runs for projects from before", n)
		}
	}()
	if rn != nil {
		mgr.Adopt(rn.procs) // V0.90: before anything sends to a thread
	}
}

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
	groups := &cgroup.JobGroups{Tree: tree, N: n}                                     // decision 0029 S5: each holder's job in a cgroup of its own; V0.admission-memory: N sizes a class under three runs
	groups.Size, groups.Raise = jobs.SizeOf, jobs.RaiseOverrun                        // V0.job-limits: a job sizes itself
	groups.Limited = func(thread, class, why, high string, budget uint64, runs int) { // V0.load-invariants: limit-under-prior
		if h, err := strconv.ParseUint(high, 10, 64); err == nil && n > 0 {
			eng.JobLimitSet(thread, class, why, h, slots.Prior(slots.Signals{Memory: budget}, n).Mem, cgroup.LimitSettled(why, runs))
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
	mgr.SetOnBash(func(c agent.BashCall) string {
		groups.Bash(c.Thread, c.Command)
		services.Bash(c.Thread, c.Command)
		class, text := heavyCmds.Match(c.Command)
		if class == "" && eng.RerunWaiting(c.Thread) {
			class = rerun.Match("", c.Command) // a killed undeclared job's rerun is admitted too
		}
		jobs.Saw(c.Thread, class)
		groups.Seen(c.Thread, text)
		if cfg.HeavyGate == "off" {
			return ""
		}
		return eng.HeavyGate(engine.HeavyCall{Thread: c.Thread, Class: class, ToolUse: c.ToolUse, Background: c.Background || heavy.Detached(c.Command), Subagent: c.Subagent})
	}, func(thread, toolUse string) { go eng.HeavyDone(thread, toolUse) }) // never on the stream's goroutine: it may grant
	sampler := &cgroup.Sampler{Tree: tree, Slots: pool, Probe: &cgroup.Slowdown{}, Jobs: jobs, Groups: groups, // V0.80, V0.86
		Detect: detector, DetectConfig: usettings.Get, Settings: usettings, Book: book, Project: eng.ThreadProject, // V0.164
		Services: services, ServiceBook: svcBook, // V0.services-not-jobs
		Rerun: rerun} // V0.rerun-classes
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
	// V0.164: an ended episode is kept, teaches its command, and ends its frozen group.
	detector.OnEnd = func(ep cgroup.Episode) {
		ep.Project = eng.ThreadProject(ep.Thread)
		episodes.Add(ep)
		if ep.Command != "" {
			book.Saw(heavy.Seen{Thread: ep.Thread, Command: ep.Command, At: ep.End, Memory: ep.PeakMem, CPU: ep.MeanCores, Seconds: int(ep.End.Sub(ep.Start).Seconds())})
		}
		groups.EndUndeclared(ep.Thread)
	}
	w.book = book
	w.detector = detector
	w.groups = groups
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
			trees.PS = func() (string, error) {
				out, err := exec.Command("ps", "-A", "-o", cgroup.TreePS).Output()
				return string(out), err
			}
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
		UndeclaredThaw: func(thread string) {
			groups.ThawUndeclared(thread)
			detector.MarkThawed(thread) // V0.episode-frozen: its calm counts again
		},
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
		UndeclaredFreeze: func(thread string) bool {
			for _, f := range detector.Flags() {
				if f.Thread == thread && groups.FreezeUndeclared(thread, sampler.Roots(f)) {
					detector.MarkPaused(thread)
					return true
				}
			}
			return false
		},
		// Decision 0032 slice 2: the parts of the work take turns.
		UndeclaredUnits: func(thread string) []slots.WorkUnit {
			for _, f := range detector.Flags() {
				if f.Thread != thread {
					continue
				}
				var out []slots.WorkUnit
				for _, u := range groups.Units(thread, sampler.Roots(f)) {
					out = append(out, slots.WorkUnit{PID: u.PID, RSS: u.RSS, Command: cgroup.CommandOf(cgroup.Root{PID: u.PID, Cmdline: u.Cmdline}, groups.Bashes(thread))})
				}
				return out
			}
			return nil
		},
		UndeclaredRotate: func(thread string, units []slots.WorkUnit, active int) bool {
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
		SlotSignals: sampler.Signals, SlotGrowth: jobs.Growth, SlotPolicy: adm.Policy,
		SlotOver:   adm.Over, // V0.163
		JobReset:   jobs.Reset,
		SlotFreeze: groups.Freeze, SlotThaw: groups.Thaw, // decision 0029 S5
		SlotEmpty: groups.JobEmpty, // V0.guard-tidy
		// V0.guard-wake: the wake rule's memory and the time frozen.
		SlotMem: sampler.ThreadMemory, FrozenUsec: groups.FrozenUsec,
		GuardService: guardService, // decision 0036 (D)
		// V0.admission-tidy: a heavier second command, and the job's baseline at the grant
		SlotClassNeed: adm.ClassNeed, SlotBaseline: sampler.Baseline,
	}
	if !pool.Off() {
		// decision 0029 S4: the budget adapts; V0.budget-restart: and outlives a deploy
		sd.SlotAIMD = &slots.AIMD{N: float64(n), Path: filepath.Join(data, "slot-budget.json"), Policy: adm.Policy} // V0.admission-tidy: the configured reserve
	}
	eng.SlotDeps = sd
	// V0.rerun-classes: kill and rerun, for the classes the owner marked.
	eng.RerunDeps = engine.RerunDeps{RerunMatch: rerun.Match, RerunSet: rerun.Set, JobKill: groups.KillJob,
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

func (w *wiring) wireDeploy() {
	cfg := w.cfg
	ctx := w.ctx
	data := w.data
	eng := w.eng
	h := w.h
	hostName := w.hostName
	pool := w.pool
	reg := w.reg
	rn := w.rn
	stop := w.stop
	tree := w.tree
	// V0.90: with the runner the agents outlive the switch: no drain.
	dep := &deploy.Deployer{Data: data, DB: h.st, Host: eng, Quit: stop, Live: BuildID(), Cgroup: tree.Deploy(), Wrap: tree.DeployWrap(), Survive: rn != nil}
	dep.OnProgress = func(p deploy.Progress) { eng.SetDeploy(p, p.Phase) } // V0.39
	if !pool.Off() {
		dep.Slot = func(ctx context.Context) (func(), error) { return pool.Wait(ctx, cgroup.DeployDir, slots.Test) }
	}
	// V0.82: a self-deploy makes the landed changes its build holds live.
	dep.OnShipped = func(s deploy.Shipped) {
		n := eng.SelfDeployed(agent.NormalizeRemote(s.Remote), s.Build, s.Contains)
		log.Printf("deploy %s (%s): %d changes live", s.Build, s.Commit, n)
	}
	// Decision 0027: auto-update installs CI's signed releases only.
	// V0.173: every gh call of fyrd is counted, and the account's limit
	// read every 10 minutes (free), for daemon.log and Settings.
	// V0.178: one budget: polls give way while the account is low or
	// rate limited; the updater, merges, re-runs and the PR tab run.
	// V0.persist-runtime-state: GitHub caches, budget and poller state survive restarts.
	github.UseCache(h.st)
	ghBudget := &github.Budget{KV: h.st}
	ghMeter := &github.Meter{OnRate: ghBudget.SetRate}
	metered := ghMeter.Wrap(github.Gh)
	gh, pollGh := ghBudget.Runner(metered, github.Act), ghBudget.Runner(metered, github.Poll)
	go ghMeter.Run(ctx, github.Gh)
	// V0.release-without-gh: releases are read over HTTPS, no gh; a token
	// (github.env, else gh's login when gh is there) is optional.
	dep.Releases = &deploy.Releases{Repo: cfg.UpdateRepo, Key: cfg.UpdateKey, Channel: cfg.UpdateChannel, Token: deploy.TokenFromEnv(), KV: h.st, HTTP: &http.Client{Timeout: releaseHTTPTimeout}}
	dep.LiveVersion = version
	// V0.versioned-releases: the channel; a bad one follows main, said once.
	if _, err := deploy.ParseChannel(cfg.UpdateChannel); err != nil {
		log.Printf("%v: following main", err)
	}
	log.Printf("update channel %s; this is %s", dep.Releases.ChannelName(), versionLine())
	if fp, builtIn, err := dep.Releases.KeyInfo(); err != nil {
		log.Printf("updateKey in the config is not an ed25519 public key (%v): no release will verify", err)
	} else {
		log.Printf("releases from %s, verified with %s%s", dep.Releases.Name(), fp, map[bool]string{true: " (built in)", false: " (updateKey)"}[builtIn])
	}
	// V0.111, #159: an ask without a checkout installs the newest release by
	// itself; a local build of a checkout always waits for the owner's press.
	deployGate := func(ctx context.Context, dir string) (string, string) {
		if dir != "" {
			return "", "a local build of " + dir + " needs your press (it is not a CI release)"
		}
		sha, err := dep.Releases.Deployable(ctx)
		if err != nil {
			return "", err.Error()
		}
		return sha, ""
	}
	dep.OnIdle = eng.AutoDeploySweep
	// V0.host-updates: the fronts follow this host's update state live.
	dep.OnChange = func() {
		if eng.Hub != nil {
			eng.Hub.Send(stream.FrameUpdate, dep.Summary())
		}
	}
	// #153: follow `deployable`, batched by a quiet period.
	watcher := &deploy.Watcher{D: dep, On: func() bool { return eng.AutoDeploy().On }, Start: func(sha string) {
		go func() {
			req := core.EffectReq{ID: engine.NewID("fx_"), Action: core.FyrDeploy, Args: map[string]any{"commit": sha, "trigger": "deployable"}}
			if _, err := dep.Deploy(context.Background(), "", req, ""); err != nil && !errors.Is(err, core.ErrReportLater) {
				log.Printf("update %s: %v", sha, err)
			}
		}()
	}}
	if cfg.UpdateQuietMinutes > 0 {
		watcher.Quiet = func() time.Duration { return time.Duration(cfg.UpdateQuietMinutes) * time.Minute }
	}
	watcher.Every = time.Duration(cfg.UpdateEveryMinutes) * time.Minute
	// V0.126: Update now / Back to release (owner only): the newest release,
	// at once, ending a local build's pause.
	// V0.host-updates: the press is also the retry of a release held back
	// as bad here; if it fails again it is marked again.
	updateNow := func() error {
		if dep.Busy() {
			return errors.New("An update runs already")
		}
		dep.Unpause()
		dep.ClearBad()
		go func() {
			req := core.EffectReq{ID: engine.NewID("fx_"), Action: core.FyrDeploy, Args: map[string]any{"trigger": "owner"}}
			if _, err := dep.Deploy(context.Background(), "", req, ""); err != nil && !errors.Is(err, core.ErrReportLater) {
				log.Printf("update now: %v", err)
			}
		}()
		return nil
	}
	serveTarget := "unix:" + auth.SocketPath(data, auth.ServeSocket)
	if runtime.GOOS == "darwin" {
		if cfg.WellKnownListen != "" {
			serveTarget = "http://" + cfg.WellKnownListen
		} else {
			serveTarget = ""
		}
	}
	setupChecker := &setup.Checker{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Host:        hostName,
		Home:        home(),
		Data:        data,
		ServeTarget: serveTarget,
		TLSOn:       cfg.TLS.Cert != "" && cfg.TLS.Key != "",
		Providers: func() []setup.ProviderInfo {
			views := reg.List()
			out := make([]setup.ProviderInfo, len(views))
			for i, v := range views {
				out[i] = setup.ProviderInfo{ID: v.ID, Name: v.Name, Status: v.Status}
			}
			return out
		},
		Projects: func() int { return len(eng.Projects()) },
		Tailscale: func() setup.TailscaleInfo {
			sock := cfg.tsSocket()
			if !tailnet.Available(sock) {
				return setup.TailscaleInfo{}
			}
			_, dns, _ := tailnet.Self(sock)
			return setup.TailscaleInfo{Available: true, DNSName: dns}
		},
		Peers: func(ctx context.Context) setup.PeerInfo {
			pe := w.peerEnvPtr.Load()
			if pe == nil {
				return setup.PeerInfo{}
			}
			members := pe.Members()
			paired := 0
			for _, m := range members {
				if !m.Self {
					paired++
				}
			}
			return setup.PeerInfo{Paired: paired, Candidates: len(pe.Candidates(ctx))}
		},
	}
	eng.DeployDeps = engine.DeployDeps{DeployGate: deployGate, DeployBusy: dep.Busy, DeployNow: dep.DeployNow /* V0.83: the owner's Restart now */, UpdateNow: updateNow,
		SetupFix: func(action string, args map[string]any) (any, error) {
			return setupChecker.Fix(context.Background(), action, args)
		}}
	go watcher.Run(ctx)
	w.dep = dep
	w.gh = gh
	w.ghBudget = ghBudget
	w.ghMeter = ghMeter
	w.pollGh = pollGh
	w.setupChecker = setupChecker
}

func (w *wiring) wireBackground() {
	cfg := w.cfg
	ctx := w.ctx
	data := w.data
	dep := w.dep
	eng := w.eng
	gh := w.gh
	ghBudget := w.ghBudget
	ghMeter := w.ghMeter
	h := w.h
	mgr := w.mgr
	pollGh := w.pollGh
	sampler := w.sampler
	tree := w.tree
	ghView := func() github.MeterView { v := ghMeter.View(); v.Paused = ghBudget.Paused(); return v }
	prDetails := &github.Details{Gh: gh} // V0.43: the PR tab's details, cached 30 s
	poller := &github.Poller{Gh: pollGh, Src: engine.PRWatch{E: eng}, KV: h.st}
	createPR := func(ctx context.Context, req github.CreatePRReq) (string, error) {
		r := ghBudget.Runner(ghMeter.Wrap(github.GhIn(req.Dir)), github.Act)
		return github.CreatePR(ctx, r, req)
	}
	eng.OutsideDeps = engine.OutsideDeps{Effects: engine.Outside{Gh: gh, PR: prDetails, Deploy: dep.Deploy}, OnPR: poller.Kick, RefreshPR: poller.Refresh, CreatePR: createPR}
	go poller.Run(ctx)
	go checkModels(ctx, h.reg) // V0.120: each provider's models, once a day
	go sampler.Run(ctx)
	if cfg.Leftovers != "off" {
		runLeftovers(ctx, tree, sampler, eng, mgr, h, cfg.Worktrees)
	}
	// V0.147: the stall watchdog. A watched lock held or a request running
	// past lockwatch.StallAfter writes every goroutine's stack to
	// <data>/stalls and tells the owner; the engine's lock held past
	// lockwatch.ExitAfter ends fyrd (exit 70) for the launcher to restart
	// it; the runner keeps the agents (V0.90). SIGQUIT writes the same dump.
	wd := &lockwatch.Watchdog{Dir: filepath.Join(data, "stalls"), OnStall: eng.SetStall}
	if s, ok := lockwatch.Latest(wd.Dir, time.Now().Add(-engine.StallShown)); ok {
		eng.SetStall(s) // a stall before this start, such as the exit that led to it
	}
	go wd.Run(ctx.Done())
	quitc := make(chan os.Signal, 1)
	signal.Notify(quitc, syscall.SIGQUIT)
	go func() {
		<-quitc
		wd.Dump("SIGQUIT", nil, time.Now())
		os.Stderr.Write(lockwatch.Stacks()) // daemon.log keeps the dump too, as before
		os.Exit(2)
	}()
	if self, err := os.Executable(); err == nil {
		go sampler.Probe.RunProbe(ctx, cgroup.ProbeCommand(tree, self), sampler.CPUPressureNow)
	}
	// Decision 0020: the durable effects the last run left run once or land.
	if err := eng.ResumeEffects(); err != nil {
		log.Printf("resume effects: %v", err)
	}
	// V0.guard-wake (decision 0036 E5): a PSI trigger on the agents' memory
	// stall runs the guard at once; the 10 s tick stays as the fallback.
	// V0.psi-trigger-slice: a refused open (the file not yet the user's, or
	// the slice not there yet) is tried again each minute.
	guardWake := make(chan struct{}, 1)
	psiScope := tree.Layout() == cgroup.LayoutScope // V0.load-invariants: psi-trigger-missing
	eng.PSITrigger(psiScope, false)
	if err := cgroup.WatchAgentsPressure(ctx, tree, func() {
		select {
		case guardWake <- struct{}{}:
		default:
		}
	}, func() {
		eng.PSITrigger(psiScope, true)
		log.Printf("guard: PSI trigger %q on the agents' memory.pressure, set on a later try", cgroup.PressureTrigger)
	}); err != nil {
		if cgroup.PressureRetrying(err) {
			log.Printf("guard: no PSI trigger yet (%v); the guard reads every 10 s and tries the trigger again every %v", err, cgroup.PressureRetry)
		} else {
			log.Printf("guard: no PSI trigger (%v); the guard reads every 10 s", err)
		}
	} else {
		eng.PSITrigger(psiScope, true)
		log.Printf("guard: PSI trigger %q on the agents' memory.pressure", cgroup.PressureTrigger)
	}
	go func() {
		t := time.NewTicker(engineTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				eng.Tick()
			case <-guardWake:
				sampler.Triggered() // V0.freeze-fast-class-name: a critical stall for TriggerFresh
				sampler.Tick()      // fresh signals for the guard
				eng.GuardNow()
			}
		}
	}()

	w.ghView = ghView
	w.prDetails = prDetails
}

func (w *wiring) wireListeners() {
	att := w.att
	cfg := w.cfg
	ctx := w.ctx
	data := w.data
	dep := w.dep
	eng := w.eng
	ghView := w.ghView
	h := w.h
	hostName := w.hostName
	ln := w.ln
	mcpSrv := w.mcpSrv
	mgr := w.mgr
	prDetails := w.prDetails
	quotas := w.quotas
	reg := w.reg
	rn := w.rn
	sampler := w.sampler
	setupChecker := w.setupChecker
	tree := w.tree
	webFS := w.webFS
	// V0.54 (decision 0016): a host sends no push; the front that serves
	// the devices does, for the threads of all its hosts.
	// V0.51: the host never serves the client; a front does.
	key := installKey(data)
	id := key.ID()
	// V0.208: this host's mark gets one setup run, as a project does.
	eng.InstallID = id
	go func() {
		time.Sleep(setupHostAfter)
		if eng.SetupHostOnce() {
			log.Printf("host setup: started a run for this host's mark")
		}
	}()
	srv := &server.Server{ID: id, Engine: eng, MCP: mcpSrv, BuildID: BuildID(), ProfileList: h.profiles, Providers: reg, HandoverPlan: mgr.HandoverPlan, HandoverExport: mgr.HandoverExport, HandoverImport: mgr.HandoverIn, OwnerLogin: cfg.OwnerLogin, Attach: att, FS: h.folders, PR: prDetails,
		Quota: quotas.Get,
		Load:  sampler,
		// V0.126 (#151): the update record, the pause and the bad release.
		Updates: func() any {
			bad, why := dep.Bad()
			return map[string]any{"updates": dep.Updates(), "paused": dep.PausedBy(), "bad": map[string]string{"commit": bad, "why": why}, "live": BuildID(), "github": ghView(), "repo": dep.Releases.Name(), "key": releaseKey(dep.Releases), "version": version, "channel": dep.Releases.ChannelName()}
		},
		UpdateLog:   dep.UpdateLog,
		UpdateState: dep.Summary,        // V0.host-updates
		Setup:       setupChecker.Check, // decision 0037
		Focus: func(device, thread string, on bool) { // V0.85
			if on {
				tree.Focus(device, thread, time.Now())
			} else {
				tree.Unfocus(device, thread, time.Now())
			}
		}}
	if tree != nil {
		go func() { // V0.85: a focus whose beats stopped drops back
			t := time.NewTicker(focusExpireEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-t.C:
					tree.Expire(now)
				}
			}
		}()
	}
	srvs := &servers{}
	var fe *runningFront
	// V0.54: "tailnet:PORT" is this machine's tailnet address.
	if port, ok := strings.CutPrefix(cfg.HostListen, "tailnet:"); ok {
		ip, err := tailnet.IPv4(cfg.tsSocket())
		switch {
		case err == nil:
			cfg.HostListen = net.JoinHostPort(ip, port)
		case runtime.GOOS == "darwin":
			// A Mac is often off the tailnet, and launchd would restart a
			// fatal fyrd in a loop with no Fyr at all: serve without the
			// host listener and restart once the tailnet is up.
			log.Printf("hostListen %s: %v; not serving other installs until the tailnet is up", cfg.HostListen, err)
			cfg.HostListen = ""
			go restartWhenTailnet(ctx, cfg.tsSocket())
		default:
			log.Fatalf("hostListen %s: %v", cfg.HostListen, err) // systemd restarts until the tailnet is up
		}
	}
	hostHandler := srv.Handler()
	// #148: the launcher trusts a new build once /healthz on this listener
	// is green: the store reads, the client has its page, and the runner's
	// processes were adopted when there is a runner.
	health := []deploy.Check{storeHealthy(h.st), webHealthy(webFS), cfg.watch.health, func() error {
		if launchenv.Current().Runner != "" && rn == nil {
			return errors.New("the runner did not answer; its agent processes were not adopted")
		}
		return nil
	}}
	authStore := openAuth(h.st, cfg.stepUp()) // the browser sessions of a front, and the stored mode of each way in (decision 0030 5.1)
	old := &oldListeners{}
	if cfg.Role == "host" {
		ps := newPeerSide(cfg, data, hostName, key)
		srv.OwnerFn = ps.ownerLogin
		srvs.serve(ln, deploy.Health(hostHandler, health...))
		w.peerEnvPtr.Store(ps.envOrNil())
		ps.start(ctx, srv, nil, authStore, old) // a host-only install pairs and serves its peer port too
	} else {
		// both (V0.51): the front reaches this host over a loopback link,
		// through the host protocol only; agents keep /mcp on the main port.
		// V0.129 (3), decision 0028: in-process, so no loopback port is
		// open for a local process to drive the host protocol through.
		link := pipe.New()
		srvs.serve(link, srv.PipeHandler()) // decision 0030 3.2: the pipe needs no browser guard; its requests carry Fyr-Actor
		hosts := append([]front.HostConfig{{Name: hostName, URL: front.LocalURL, Dial: link.Dial}}, cfg.Hosts...)
		fe = startFront(ctx, frontParts{cfg: cfg, webFS: webFS, name: hostName, data: data, key: key, st: h.st, auth: authStore,
			hosts: hosts, local: hostName, hostURL: hostURL(cfg.HostListen)}, srv, old)
		srv.OwnerFn = fe.ownerLogin
		w.peerEnvPtr.Store(fe.peer)
		fr := fe.fr
		// V0.81: a client-only deploy swaps the client this front serves.
		dep.SwitchClient = func(build, dir string) error { return fr.SetClient(build, os.DirFS(dir)) }
		mux := http.NewServeMux()
		mux.Handle("/mcp", hostHandler)
		mux.Handle("/", fr.Handler())
		srvs.serve(ln, deploy.Health(mux, health...))
		serveFrontSockets(cfg, fr, data, ln.Addr().String(), srvs)
		if cfg.WellKnownListen != "" {
			// The macOS Serve's way to this answer: it names the peer port too, or condition 1 cannot pair this install.
			srvs.serve(listenLoopback(cfg.WellKnownListen), peer.OnlyWellKnown(cfg.OwnerLogin, fe.wellKnown))
			log.Printf("answering /.well-known/fyr for Tailscale Serve at %s", cfg.WellKnownListen)
		}
	}
	// The old ways a front reaches a host: the tailnet listener behind whois, and a loopback
	// host port. The peer way in's enforce mode removes them; observe brings them back.
	old.open = func() *servers {
		out := &servers{onErr: func(err error) { log.Printf("host listener: %v", err) }}
		switch {
		case cfg.HostListen == "":
		case loopbackAddr(cfg.HostListen):
			// V0.123: a second install on this machine (development, tests)
			// follows this host on loopback, behind the loopback guard (V0.14).
			out.serve(listenLoopback(cfg.HostListen), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if old.onUse != nil {
					old.onUse()
				}
				hostHandler.ServeHTTP(w, r)
			}))
			log.Printf("serving fronts on loopback at %s", cfg.HostListen)
		default:
			out.serve(listenTailnet(cfg.HostListen), srv.PeerHandler(tailnet.Peers{Socket: cfg.tsSocket()}))
			log.Printf("serving fronts on the tailnet at %s", cfg.HostListen)
		}
		return out
	}
	if authStore.Mode(auth.WayPeer) == auth.ModeEnforce {
		log.Printf("peer is in enforce: the old host listeners are not started (fyr auth observe brings them back)")
	} else {
		old.Set(true)
	}
	log.Printf("build %s (%s) listening on http://%s (%d threads, %d providers)", BuildID(), cfg.Role, ln.Addr(), len(eng.Snapshot()), len(h.profiles()))
	deploy.MarkReady(ln.Addr().String())
	dep.ReportPending(ctx)
	eng.AutoDeploySweep() // V0.111: the deploy asks that waited across the restart

	w.fe = fe
	w.old = old
	w.srvs = srvs
}
