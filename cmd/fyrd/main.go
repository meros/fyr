// fyrd is the Fyr daemon (SPEC 0.5). It runs as a host (runs threads), a
// front (serves the client from a list of hosts) or both, the default
// (decision 0016, V0.51).
//
//	fyrd [serve] [-role both|host|front] [-listen 127.0.0.1:7777] [-hosts name=url,…] [-host-listen ADDR] [-data DIR] [-web DIR]
//	fyrd import-t3 [-list]
//
// This file only wires the parts together; the rules live in their packages.
package main

import (
	"context"
	"errors"
	"flag"
	"github.com/meros/fyr/internal/lockwatch"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/peers"
	"github.com/meros/fyr/internal/pipe"
	"github.com/meros/fyr/internal/preview"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/runner"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/tailnet"
	"github.com/meros/fyr/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("fyrd ")
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	if cmd == "loadprobe" {
		// Decision 0029 S1: the slowdown probe, a child in agents/probe/.
		cgroup.LoadProbe(os.Stdout)
		return
	}
	if cmd == "version" {
		// V0.81: the deployer reads a new build's id before a client-only switch.
		os.Stdout.WriteString(BuildID() + "\n")
		return
	}
	fl := flag.NewFlagSet(cmd, flag.ExitOnError)
	data := fl.String("data", filepath.Join(home(), ".local", "share", "fyr"), "data directory")
	cfgPath := fl.String("config", filepath.Join(home(), ".config", "fyr", "config.json"), "config file")
	listen := fl.String("listen", "", "listen address (loopback only), default from config or 127.0.0.1:7777")
	webDir := fl.String("web", "", "serve the client from this directory instead of the embedded build (development)")
	ownerLogin := fl.String("owner-login", "", "Tailscale login allowed over the tailnet (default from config ownerLogin)")
	t3Dir := fl.String("t3", filepath.Join(home(), ".t3", "userdata"), "T3 Code user data directory")
	list := fl.Bool("list", false, "import-t3: only list the candidates")
	role := fl.String("role", "", "host, front or both (default from config role, else both)")
	hostsFlag := fl.String("hosts", "", "front: other hosts as name=url,… (default from config hosts)")
	peersFlag := fl.String("peers", "", "other Fyr page origins to probe, comma separated (default from config peers; V0.123)")
	runnerFlag := fl.String("runner", "", "use this runner socket even when it is not this install's launcher's (#167; development)")
	hostListen := fl.String("host-listen", "", "host: also serve fronts on this tailnet address, or tailnet:PORT (default from config hostListen)")
	fl.Parse(args)

	// #167: the launcher's variables name the live install. Take them out
	// of the environment before anything starts, so no agent or test
	// inherits them, and keep only what belongs to this data folder and a
	// parent that is its launcher.
	if le, dropped := launchenv.Init(*data, *runnerFlag); len(dropped) > 0 {
		log.Printf("launcher: not this install's, left alone: %s", strings.Join(dropped, "; "))
	} else if le.Launcher {
		log.Printf("launcher: pid %d, runner %q", os.Getppid(), le.Runner)
	}
	os.MkdirAll(*data, 0o700)
	// A systemd unit has a minimal PATH; agents need the user's tools.
	if u := os.Getenv("USER"); u != "" || home() != "" {
		if u == "" {
			u = filepath.Base(home())
		}
		os.Setenv("PATH", agent.UserPath(os.Getenv("PATH"), home(), u, agent.DirExists))
	}
	cfg := loadConfig(*cfgPath, filepath.Join(*t3Dir, "settings.json"))
	cfg.override(*listen, *ownerLogin, *role, *hostsFlag, *hostListen)
	if *peersFlag != "" {
		cfg.Peers = strings.Split(*peersFlag, ",")
	}
	var webFS fs.FS = web.Dist()
	if *webDir != "" {
		webFS = os.DirFS(*webDir)
	}
	if cmd == "selftest" {
		selftest(*data, webFS)
		return
	}
	hostName, _ := os.Hostname()
	if cfg.Name != "" {
		hostName = cfg.Name
	}
	if cfg.Role == "front" {
		if cmd != "serve" {
			log.Fatalf("%s needs the host role", cmd)
		}
		serveFront(cfg, webFS, hostName, *data)
		return
	}
	// V0.80: the daemon moves to server/ before it starts anything; every
	// agent then starts in agents/<thread>/. Off (and logged) without the
	// module's cgroups or a delegated cgroup.
	var tree *cgroup.Tree
	if cmd == "serve" {
		budget := cgroup.Budget{}
		if cfg.Cgroups != nil {
			budget = *cfg.Cgroups
		}
		tree = cgroup.Setup(cgroup.Options{Budget: budget, Logf: log.Printf})
	}
	// V0.90: the launcher's runner holds the agent processes. Its list
	// comes before the restart repair, which leaves their runs live.
	var rn *kept
	if cmd == "serve" {
		rn = keptProcs(launchenv.Current().Runner)
	}
	h := openHost(cfg, *data, filepath.Join(*t3Dir, "statev2.sqlite"), rn.threads())
	defer h.st.Close()
	switch cmd {
	case "import-t3":
		importT3(h.t3, *list)
	case "serve":
		h.serve(cfg, webFS, hostName, *data, tree, rn)
	default:
		log.Fatalf("unknown command %q", cmd)
	}
}

// serve runs the host until a signal: its agents, effects and pollers, the
// host protocol, and in role both the front on the same port (V0.51).
func (h *host) serve(cfg config, webFS fs.FS, hostName, data string, tree *cgroup.Tree, rn *kept) {
	eng, reg := h.eng, h.reg
	ln := listenLoopback(cfg.Listen)
	att := &attach.Store{Dir: filepath.Join(data, "attachments")}
	eng.AttachmentExists = func(id string) bool { _, err := att.Get(id); return err == nil }
	eng.Attach = att
	// V0.19: screenshots for the show tool, with one Chrome started on demand.
	if bin := preview.FindChrome(); bin != "" {
		shooter := &preview.Chrome{Bin: bin, Dir: filepath.Join(data, "chrome")}
		eng.Shooter = shooter
		defer shooter.Close()
	} else {
		log.Printf("show: no Chrome on PATH; screenshots are off")
	}
	// V0.44: provider quota, read with the logins on this host.
	quotaList := func() []quota.Profile {
		if reg.Empty() {
			return quotaProfiles(cfg.Profiles)
		}
		return reg.QuotaProfiles()
	}
	quotas := quota.New(nil, home())
	quotas.Client = &http.Client{Timeout: 10 * time.Second}
	quotas.Logf = log.Printf
	// V0.122: the data and the rate-limit wait survive a restart, so frequent deploys never mean frequent reads.
	quotas.StatePath = filepath.Join(data, "quota-state.json")
	quotas.SetProfiles(quotaList())
	reg.OnChange = func() { quotas.SetProfiles(quotaList()) }
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
	})
	eng.Agents = mgr
	eng.Titler = mgr.Title                                              // V0.108
	eng.SystemAgentResolve = func(job string) (string, string, error) { // V0.115: Settings shows what each job runs
		a, err := mgr.SystemAgent(job)
		return a.Profile.ID, a.Model, err
	}
	eng.SystemAgentProviders = func() []engine.SystemProvider {
		var out []engine.SystemProvider
		for _, p := range h.profiles() {
			out = append(out, engine.SystemProvider{ID: p.ID, Name: p.Name, Models: p.Models, Haiku: agent.HaikuModel(p), Unavailable: p.Unavailable})
		}
		return out
	}
	// V0.116: project setup. The icons live with the host's data; a new
	// project with a folder gets a setup run, and each project from before
	// gets one once, a minute after start and 30 s apart. The run's provider
	// and model are the owner's System agents setting (SystemAgentResolve).
	eng.Icons = &projicon.Store{Dir: filepath.Join(data, "project-icons")}
	eng.AutoSetup = true
	go func() {
		time.Sleep(time.Minute)
		if n := eng.SetupExisting(30 * time.Second); n > 0 {
			log.Printf("project setup: started %d runs for projects from before", n)
		}
	}()
	if rn != nil {
		mgr.Adopt(rn.procs) // V0.90: before anything sends to a thread
	}
	eng.ProfileExists = func(id string) bool { _, ok := h.profile(id); return ok }
	// V0.86: at most cfg.Slots heavy jobs at once; the deploy's tests first.
	// slots: 0 is off, absent or negative is the default; the pool survives a
	// restart in the store.
	n := slots.Default
	if cfg.Slots != nil {
		n = *cfg.Slots
	}
	pool := slots.Open(n, h.st)
	eng.UseSlots(pool)
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
	// #174: the gate hook refuses a heavy command without a held slot.
	groups := &cgroup.JobGroups{Tree: tree} // decision 0029 S5: each holder's job in a cgroup of its own
	mgr.SetOnBash(func(thread, c string) string {
		class, text := heavyCmds.Match(c)
		jobs.Saw(thread, class)
		groups.Seen(thread, text)
		if cfg.HeavyGate == "off" {
			return ""
		}
		return eng.HeavyGate(thread, class)
	})
	eng.SlotActive = jobs.Active
	sampler := &cgroup.Sampler{Tree: tree, Slots: pool, Probe: &cgroup.Slowdown{}, Jobs: jobs, Groups: groups} // V0.80, V0.86
	// Decision 0029 S2 (#186): admission by a budget of costs (N), gated by
	// the slowdown and memory; one job always runs; no provider (macOS)
	// keeps the fixed count. Costs are 1.0, an idle hold 0.1, until S3
	// learns them.
	policy := slots.DefaultPolicy
	if cfg.SlotSlowdownMax > 0 {
		policy.SlowdownMax = cfg.SlotSlowdownMax
	}
	if cfg.SlotMemoryReserve > 0 {
		policy.MemoryReserve = cfg.SlotMemoryReserve
	}
	// V0.153: on a thrashing host the agents wait; Fyr kills no CLI for
	// being slow and restarts none into the same thrash.
	mgr.SetPressure(func() (bool, string) { return slots.Thrashing(sampler.Signals(), policy) })
	// Decision 0029 S3 (#187), V0.163: a job needs CPU and memory, apart:
	// its class's learned demand (cores) and peak (bytes), the prior (1/N of
	// the host) until learned; a holder at least what it uses now.
	predict := func(h slots.Holder) (need slots.Need, wall float64) {
		sig := sampler.Signals()
		l, ok := jobs.Learned(jobs.ClassOf(h.Owner, h.Kind))
		if !ok {
			return slots.Prior(sig, n), 0
		}
		return slots.NeedOf(l.Demand, l.Peak, l.Runs, sig, n), l.WallP90()
	}
	live := func(h slots.Holder) (slots.Need, bool) {
		cores, grew, ok := jobs.LiveUse(h.Owner)
		return slots.Need{CPU: cores, Mem: grew}, ok
	}
	pool.Host = sampler.Signals
	pool.Cost = func(h slots.Holder) slots.Need {
		sig := sampler.Signals()
		if h.Priority {
			return slots.Prior(sig, n)
		}
		l, isLive := live(h)
		if at, ok := jobs.Active(h.Owner); ok && time.Since(at) >= slots.IdleAfter {
			// An idle hold: a little CPU, and the memory it really holds.
			return slots.Need{CPU: slots.IdleShare * sig.CPU, Mem: l.Mem}
		}
		need, _ := predict(h)
		if isLive {
			need = need.Max(l)
		}
		return need
	}
	heldNeeds := func(held []slots.Holder) []slots.Need {
		out := make([]slots.Need, len(held))
		for i, h := range held {
			out[i] = pool.Cost(h)
		}
		return out
	}
	pool.Admit = func(head slots.Holder, held []slots.Holder) (bool, string, string) {
		need, _ := predict(head)
		return slots.Decide(policy, sampler.Signals(), pool.Budget(), n, heldNeeds(held), need)
	}
	// EASY backfill: c may pass the head when it fits now and is cheap, or
	// ends (its class's p90) before the earliest running job is expected to.
	pool.Backfill = func(head, c slots.Holder, held []slots.Holder) bool {
		need, wall := predict(c)
		sig := sampler.Signals()
		if ok, _, _ := slots.Decide(policy, sig, pool.Budget(), n, heldNeeds(held), need); !ok {
			return false
		}
		ends := make([]time.Time, len(held))
		for i, h := range held {
			if _, w := predict(h); w > 0 {
				ends[i] = h.Since.Add(time.Duration(w * float64(time.Second)))
			}
		}
		return slots.EASY(time.Now(), slots.Cheap(need, sig), wall, ends)
	}
	eng.SlotSignals, eng.SlotGrowth, eng.SlotPolicy = sampler.Signals, jobs.Growth, policy
	eng.HostName = hostName // V0.163: the heavy-job answers name this host
	eng.SlotJob = func(thread, c string) {
		class, text := heavyCmds.Match(c)
		jobs.Saw(thread, class)
		groups.Seen(thread, text)
	}
	eng.SlotFreeze, eng.SlotThaw = groups.Freeze, groups.Thaw // decision 0029 S5
	if !pool.Off() {
		eng.SlotAIMD = &slots.AIMD{N: float64(n)} // decision 0029 S4: the budget adapts
	}
	eng.JobReset = jobs.Reset
	// V0.163: heavier than predicted, in either resource, by more than an
	// eighth of the host; told in percent of the host.
	eng.SlotOver = func(owner string) (string, float64, float64, bool) {
		h, ok := pool.Holding(owner)
		if !ok {
			return "", 0, 0, false
		}
		sig := sampler.Signals()
		p, _ := predict(h)
		l, ok := live(h)
		pc, pm := p.Shares(sig)
		lc, lm := l.Shares(sig)
		over := ok && (lc > pc+slots.CheapShare || lm > pm+slots.CheapShare)
		return jobs.ClassOf(owner, h.Kind), slots.Pct(max(lc, lm)), slots.Pct(max(pc, pm)), over
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// SIGHUP stops with exit 75: the launcher switches to a next build that
	// scripts/update.sh linked (with its probation and rollback), or restarts
	// the current one.
	var hup atomic.Bool
	hupc := make(chan os.Signal, 1)
	signal.Notify(hupc, syscall.SIGHUP)
	go func() { <-hupc; hup.Store(true); stop() }()
	// V0.90: with the runner the agents outlive the switch: no drain.
	dep := &deploy.Deployer{Data: data, DB: h.st, Host: eng, Quit: stop, Live: BuildID(), Cgroup: tree.Deploy(), Survive: rn != nil}
	dep.OnProgress = func(p deploy.Progress) { eng.SetDeploy(p, p.Phase) } // V0.39
	if !pool.Off() {
		dep.Slot = func(ctx context.Context) (func(), error) { return pool.Wait(ctx, cgroup.DeployDir, slots.Test) }
	}
	// V0.82: a self-deploy makes the landed changes its build holds live.
	dep.OnShipped = func(s deploy.Shipped) {
		n := eng.SelfDeployed(agent.NormalizeRemote(s.Remote), s.Build, s.Contains)
		log.Printf("deploy %s (%s): %d changes live", s.Build, s.Commit, n)
	}
	eng.DeployNow = dep.DeployNow // V0.83: the owner's Restart now
	// Decision 0027: auto-update installs CI's signed releases only.
	dep.Releases = &deploy.Releases{Repo: cfg.UpdateRepo, Gh: github.Gh}
	// V0.111, #159: an ask without a checkout installs the newest release by
	// itself; a local build of a checkout always waits for the owner's press.
	eng.DeployGate = func(ctx context.Context, dir string) (string, string) {
		if dir != "" {
			return "", "a local build of " + dir + " needs your press (it is not a CI release)"
		}
		sha, err := dep.Releases.Deployable(ctx)
		if err != nil {
			return "", err.Error()
		}
		return sha, ""
	}
	eng.DeployBusy = dep.Busy
	dep.OnIdle = eng.AutoDeploySweep
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
	go watcher.Run(ctx)
	// V0.126: Update now / Back to release (owner only): the newest release,
	// at once, ending a local build's pause.
	eng.UpdateNow = func() error {
		if dep.Busy() {
			return errors.New("An update runs already")
		}
		dep.Unpause()
		go func() {
			req := core.EffectReq{ID: engine.NewID("fx_"), Action: core.FyrDeploy, Args: map[string]any{"trigger": "owner"}}
			if _, err := dep.Deploy(context.Background(), "", req, ""); err != nil && !errors.Is(err, core.ErrReportLater) {
				log.Printf("update now: %v", err)
			}
		}()
		return nil
	}
	prDetails := &github.Details{Gh: github.Gh} // V0.43: the PR tab's details, cached 30 s
	eng.Effects = engine.Outside{Gh: github.Gh, PR: prDetails, Deploy: dep.Deploy}
	poller := &github.Poller{Gh: github.Gh, Src: engine.PRWatch{E: eng}}
	eng.OnPR = poller.Kick
	go poller.Run(ctx)
	go checkModels(ctx, h.reg) // V0.120: each provider's models, once a day
	go sampler.Run(ctx)
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
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				eng.Tick()
			}
		}
	}()

	// V0.54 (decision 0016): a host sends no push; the front that serves
	// the devices does, for the threads of all its hosts.
	// V0.51: the host never serves the client; a front does.
	key := installKey(data)
	id := key.ID()
	srv := &server.Server{ID: id, Engine: eng, MCP: mcpSrv, BuildID: BuildID(), ProfileList: h.profiles, Providers: providerAPI{reg}, HandoverPlan: mgr.HandoverPlan, OwnerLogin: cfg.OwnerLogin, Attach: att, FS: h.folders, PR: prDetails,
		Quota: func(ctx context.Context) any { return quotas.Get(ctx) },
		Load:  sampler,
		// V0.126 (#151): the update record, the pause and the bad release.
		Updates: func() any {
			bad, why := dep.Bad()
			return map[string]any{"updates": dep.Updates(), "paused": dep.PausedBy(), "bad": map[string]string{"commit": bad, "why": why}, "live": BuildID()}
		},
		UpdateLog: dep.UpdateLog,
		Focus: func(device, thread string, on bool) { // V0.85
			if on {
				tree.Focus(device, thread, time.Now())
			} else {
				tree.Unfocus(device, thread, time.Now())
			}
		},
		T3Candidates: func() (any, error) { return h.t3.Candidates() }}
	if tree != nil {
		go func() { // V0.85: a focus whose beats stopped drops back
			t := time.NewTicker(5 * time.Second)
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
	var servers []*http.Server
	frontDone := make(chan struct{})
	serve := func(l net.Listener, hd http.Handler) {
		hs := &http.Server{Handler: hd, ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, hs)
		go func() {
			if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		}()
	}
	// V0.54: "tailnet:PORT" is this machine's tailnet address.
	if port, ok := strings.CutPrefix(cfg.HostListen, "tailnet:"); ok {
		ip, err := tailnet.IPv4(cfg.tsSocket())
		if err != nil {
			log.Fatalf("hostListen %s: %v", cfg.HostListen, err)
		}
		cfg.HostListen = net.JoinHostPort(ip, port)
	}
	hostHandler := srv.Handler()
	// #148: the launcher trusts a new build once /healthz on this listener
	// is green: the store reads, the client has its page, and the runner's
	// processes were adopted when there is a runner.
	health := []deploy.Check{storeHealthy(h.st), webHealthy(webFS), func() error {
		if launchenv.Current().Runner != "" && rn == nil {
			return errors.New("the runner did not answer; its agent processes were not adopted")
		}
		return nil
	}}
	if cfg.Role == "host" {
		serve(ln, deploy.Health(hostHandler, health...))
		startPeer(ctx, cfg, data, hostName, srv, nil, nil, key) // a host-only install pairs and serves its peer port too
	} else {
		// both (V0.51): the front reaches this host over a loopback link,
		// through the host protocol only; agents keep /mcp on the main port.
		// V0.129 (3), decision 0028: in-process, so no loopback port is
		// open for a local process to drive the host protocol through.
		link := pipe.New()
		serve(link, srv.PipeHandler()) // decision 0030 3.2: the pipe needs no browser guard; its requests carry Fyr-Actor
		hosts := append([]front.HostConfig{{Name: hostName, URL: front.LocalURL, Dial: link.Dial}}, cfg.Hosts...)
		// The front's device data (push keys and subscriptions) stays in
		// this machine's database, where the host kept it before V0.54.
		// V0.123 (decision 0025): other installs find this one by its
		// well-known answer, which names where its host protocol is.
		wk := wellKnown(cfg, hostName, hostURL(cfg.HostListen), id)
		var fr *front.Front
		var ps *peerSide
		authStore := openAuth(h.st)
		fr = front.New(front.Options{ID: id, Name: hostName, BuildID: BuildID(), OwnerLogin: cfg.OwnerLogin, Web: webFS,
			Push: newPusher(h.st), CacheDir: filepath.Join(data, "front"), Local: hostName, Store: h.st, Auth: authStore, Tailnet: tailnetNodes(cfg),
			OwnerRoutes: func(mux *http.ServeMux) {
				if ps != nil {
					ps.ownerRoutes(fr)(mux)
				}
			},
			WellKnown: func() peers.WellKnown { w := wk; w.Peer = ps.peerPort(); return w }}, hosts)
		go func() { fr.Run(ctx); close(frontDone) }()
		ps = startPeer(ctx, cfg, data, hostName, srv, fr, authStore, key) // decision 0030 3.6
		go newDiscovery(cfg, wk, ln.Addr().String(), fr).Run(ctx, peers.Every)
		// V0.81: a client-only deploy swaps the client this front serves.
		dep.SwitchClient = func(build, dir string) error { return fr.SetClient(build, os.DirFS(dir)) }
		mux := http.NewServeMux()
		mux.Handle("/mcp", hostHandler)
		mux.Handle("/", fr.Handler())
		serve(ln, deploy.Health(mux, health...))
		serveAuthSockets(fr, data, ln.Addr().String(), servePublic(cfg), cfg.ServeSocketUsers, serve) // V0.130
	}
	switch {
	case cfg.HostListen == "":
	case loopbackAddr(cfg.HostListen):
		// V0.123: a second install on this machine (development, tests)
		// follows this host on loopback, behind the loopback guard (V0.14).
		serve(listenLoopback(cfg.HostListen), hostHandler)
		log.Printf("serving fronts on loopback at %s", cfg.HostListen)
	default:
		serve(listenTailnet(cfg.HostListen), srv.PeerHandler(tailnet.Peers{Socket: cfg.tsSocket()}))
		log.Printf("serving fronts on the tailnet at %s", cfg.HostListen)
	}
	log.Printf("build %s (%s) listening on http://%s (%d threads, %d providers)", BuildID(), cfg.Role, ln.Addr(), len(eng.Snapshot()), len(h.profiles()))
	deploy.MarkReady(ln.Addr().String())
	dep.ReportPending()
	eng.AutoDeploySweep() // V0.111: the deploy asks that waited across the restart

	<-ctx.Done()
	log.Printf("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, hs := range servers {
		hs.Shutdown(sctx)
	}
	if cfg.Role != "host" {
		<-frontDone // the front's cache is written
	}
	mgr.StopAll()
	if hup.Load() {
		os.Exit(deploy.ExitDeploy)
	}
	os.Exit(dep.ExitCode())
}

// serveFront runs fyrd as a front only (V0.51): the client and the browser
// API, from the configured hosts; no threads, no store.
func serveFront(cfg config, webFS fs.FS, name, data string) {
	if len(cfg.Hosts) == 0 {
		log.Fatal("a front needs hosts (config hosts or -hosts name=url,…)")
	}
	// V0.54: a front keeps device data only (push keys and subscriptions).
	st, err := store.Open(filepath.Join(data, "fyr.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	ln := listenLoopback(cfg.Listen)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	key := installKey(data)
	id := key.ID()
	wk := wellKnown(cfg, name, "", id)
	var fr *front.Front
	var ps *peerSide
	authStore := openAuth(st)
	fr = front.New(front.Options{ID: id, Name: name, BuildID: BuildID(), OwnerLogin: cfg.OwnerLogin, Web: webFS,
		Push: newPusher(st), CacheDir: filepath.Join(data, "front"), Store: st, Auth: authStore, Tailnet: tailnetNodes(cfg),
		OwnerRoutes: func(mux *http.ServeMux) {
			if ps != nil {
				ps.ownerRoutes(fr)(mux)
			}
		},
		WellKnown: func() peers.WellKnown { w := wk; w.Peer = ps.peerPort(); return w }}, cfg.Hosts)
	done := make(chan struct{})
	go func() { fr.Run(ctx); close(done) }()
	go newDiscovery(cfg, wk, ln.Addr().String(), fr).Run(ctx, peers.Every) // V0.123
	ps = startPeer(ctx, cfg, data, name, nil, fr, authStore, key)
	hs := &http.Server{Handler: deploy.Health(fr.Handler(), storeHealthy(st), webHealthy(webFS)), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	var extra []*http.Server
	serveAuthSockets(fr, data, ln.Addr().String(), servePublic(cfg), cfg.ServeSocketUsers, func(l net.Listener, h http.Handler) { // V0.130
		s := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
		extra = append(extra, s)
		go s.Serve(l)
	})
	defer func() {
		for _, s := range extra {
			s.Close()
		}
	}()
	log.Printf("build %s (front) listening on http://%s for %d hosts", BuildID(), ln.Addr(), len(cfg.Hosts))
	deploy.MarkReady(ln.Addr().String())
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(sctx)
	<-done // the cache is written
}

// kept is the launcher's runner and the processes it held at the start
// (V0.90).
type kept struct {
	rc    *runner.Client
	procs []runner.Proc
}

// keptProcs asks the runner at sock for its processes. Without a runner, or
// with one this build cannot speak to, it returns nil and the agents run as
// fyrd's children (a deploy then drains, V0.13).
func keptProcs(sock string) *kept {
	if sock == "" {
		return nil
	}
	rc := &runner.Client{Path: sock}
	procs, err := rc.ListWait(10 * time.Second) // V0.90 (3): never miss a kept process
	if err != nil {
		log.Printf("runner: %v; agents run as fyrd's children", err)
		return nil
	}
	log.Printf("runner: %s holds %d agent processes", sock, len(procs))
	return &kept{rc: rc, procs: procs}
}

func (k *kept) threads() map[string]bool {
	if k == nil {
		return nil
	}
	return agent.Kept(k.procs)
}

func (k *kept) client() *runner.Client {
	if k == nil {
		return nil
	}
	return k.rc
}

// checkModels asks each provider that can run for its models once a day
// (V0.120): a minute after the start, then every hour for the ones due.
func checkModels(ctx context.Context, reg *provider.Registry) {
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		reg.CheckDaily(time.Now())
		t.Reset(time.Hour)
	}
}
