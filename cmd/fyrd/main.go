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
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/peers"
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
	hostListen := fl.String("host-listen", "", "host: also serve fronts on this tailnet address, or tailnet:PORT (default from config hostListen)")
	fl.Parse(args)

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
		rn = keptProcs(os.Getenv("FYR_RUNNER"))
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
		LogDir: filepath.Join(data, "logs"), Attachments: att.Get, OnRateLimit: quotas.Observe, OnCommands: eng.SetCommands, HandoverDir: filepath.Join(data, "handover"),
		Cgroups: tree, Runner: rn.client(),
		// V0.115: the system agents run on this host, on the owner's setting.
		HostName: hostName, ProfileExists: func(id string) bool { _, ok := h.profile(id); return ok }, SystemAgents: eng.SystemAgents,
	})
	eng.Agents = mgr
	eng.Titler = mgr.Title // V0.108
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
	eng.DeployNow = dep.DeployNow               // V0.83: the owner's Restart now
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
	id := installID(data)
	srv := &server.Server{ID: id, Engine: eng, MCP: mcpSrv, BuildID: BuildID(), ProfileList: h.profiles, Providers: providerAPI{reg}, HandoverPlan: mgr.HandoverPlan, OwnerLogin: cfg.OwnerLogin, Attach: att, FS: h.folders, PR: prDetails,
		Quota: func(ctx context.Context) any { return quotas.Get(ctx) },
		Load:  &cgroup.Sampler{Tree: tree, Slots: pool}, // V0.80, V0.86
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
		ip, err := tailnet.IPv4(tailnet.DefaultSocket)
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
		if os.Getenv("FYR_RUNNER") != "" && rn == nil {
			return errors.New("the runner did not answer; its agent processes were not adopted")
		}
		return nil
	}}
	if cfg.Role == "host" {
		serve(ln, deploy.Health(hostHandler, health...))
	} else {
		// both (V0.51): the front reaches this host over a loopback link,
		// through the host protocol only; agents keep /mcp on the main port.
		link := listenLoopback("127.0.0.1:0")
		serve(link, hostHandler)
		hosts := append([]front.HostConfig{{Name: hostName, URL: "http://" + link.Addr().String()}}, cfg.Hosts...)
		// The front's device data (push keys and subscriptions) stays in
		// this machine's database, where the host kept it before V0.54.
		// V0.123 (decision 0025): other installs find this one by its
		// well-known answer, which names where its host protocol is.
		wk := wellKnown(cfg, hostName, hostURL(cfg.HostListen), id)
		fr := front.New(front.Options{ID: id, Name: hostName, BuildID: BuildID(), OwnerLogin: cfg.OwnerLogin, Web: webFS,
			Push: newPusher(h.st), CacheDir: filepath.Join(data, "front"), Local: hostName, Store: h.st,
			WellKnown: func() peers.WellKnown { return wk }}, hosts)
		go func() { fr.Run(ctx); close(frontDone) }()
		go newDiscovery(cfg, wk, ln.Addr().String(), fr).Run(ctx, peers.Every)
		// V0.81: a client-only deploy swaps the client this front serves.
		dep.SwitchClient = func(build, dir string) error { return fr.SetClient(build, os.DirFS(dir)) }
		mux := http.NewServeMux()
		mux.Handle("/mcp", hostHandler)
		mux.Handle("/", fr.Handler())
		serve(ln, deploy.Health(mux, health...))
	}
	switch {
	case cfg.HostListen == "":
	case loopbackAddr(cfg.HostListen):
		// V0.123: a second install on this machine (development, tests)
		// follows this host on loopback, behind the loopback guard (V0.14).
		serve(listenLoopback(cfg.HostListen), hostHandler)
		log.Printf("serving fronts on loopback at %s", cfg.HostListen)
	default:
		serve(listenTailnet(cfg.HostListen), srv.PeerHandler(tailnet.Peers{Socket: tailnet.DefaultSocket}))
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
	id := installID(data)
	wk := wellKnown(cfg, name, "", id)
	fr := front.New(front.Options{ID: id, Name: name, BuildID: BuildID(), OwnerLogin: cfg.OwnerLogin, Web: webFS,
		Push: newPusher(st), CacheDir: filepath.Join(data, "front"), Store: st, WellKnown: func() peers.WellKnown { return wk }}, cfg.Hosts)
	done := make(chan struct{})
	go func() { fr.Run(ctx); close(done) }()
	go newDiscovery(cfg, wk, ln.Addr().String(), fr).Run(ctx, peers.Every) // V0.123
	hs := &http.Server{Handler: deploy.Health(fr.Handler(), storeHealthy(st), webHealthy(webFS)), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
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
