package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/lockwatch"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/setup"
	"github.com/meros/fyr/internal/stream"
	"github.com/meros/fyr/internal/tailnet"
)

// wireDeploy makes the deployer, the GitHub runners and the setup checker.
//
// Needs from earlier steps: pool.
// Makes: dep, gh, ghBudget, ghMeter, pollGh, setupChecker.
// Order: main.go sets w.ctx and w.stop (the signal context) before it.
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
	// V0.survive-update: a launcher that does not say its pid predates
	// probation-ok, and so does no launcher at all.
	if launchenv.Current().LauncherPid == 0 {
		dep.OldLauncher = deploy.OldLauncherProbation
	}
	if !pool.Off() {
		dep.Slot = func(ctx context.Context) (func(), error) { return pool.Wait(ctx, cgroup.DeployDir, resource.Test) }
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
	// V0.update-now-fresh: it reads `deployable` at once (not the hourly
	// read) and skips the quiet period; the watcher reads it again too, so
	// the host's update state follows.
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
		watcher.Kick()
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
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Host:           hostName,
		Home:           home(),
		Data:           data,
		ServeTarget:    serveTarget,
		ForwardTargets: forwardTargets(cfg, data),
		TLSOn:          cfg.TLS.Cert != "" && cfg.TLS.Key != "",
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
			_, dns, _ := tailnet.Self(context.Background(), sock)
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

// wireBackground starts what runs for the life of the host: the poller, the sampler, the watchers.
//
// Needs from earlier steps: dep, gh, ghBudget, ghMeter, mgr, pollGh, sampler.
// Makes: ghView, prDetails.
// Order: after wireDeploy (w.dep and the GitHub runners).
func (w *wiring) wireBackground() {
	cfg := w.cfg
	ctx := w.ctx
	// V0.116: each project from before gets one setup run, a minute after start and 30 s apart.
	// Here and not in wireProjects: w.ctx is set only after wireGuard.
	go func() {
		select {
		case <-time.After(setupExistingAfter):
		case <-ctx.Done():
			return
		}
		if n := w.eng.SetupExisting(ctx, setupExistingEvery); n > 0 {
			log.Printf("project setup: started %d runs for projects from before", n)
		}
	}()
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
	go eng.ProbeLoop(ctx)      // V0.project-playbook: the stage probes after a merge
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
	if tree == nil {
		sampler.OnCritical = func() {
			select {
			case guardWake <- struct{}{}:
			default:
			}
		}
		log.Printf("guard: no cgroup tree; critical memory stalls wake the guard on each %v sample (fallback every %v)", cgroup.ProbeEvery, engineTick)
	} else if err := cgroup.WatchAgentsPressure(ctx, tree, func() {
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
