// fyrd is the Fyr daemon (SPEC 0.5). It runs as a host (runs threads), a
// front (serves the client from a list of hosts) or both, the default
// (decision 0016, V0.51).
//
//	fyrd [serve] [-role both|host|front] [-listen 127.0.0.1:7777] [-hosts name=url,…] [-host-listen ADDR] [-data DIR] [-web DIR]
//
// This file only wires the parts together; the rules live in their packages.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
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
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/preview"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/runner"
	"github.com/meros/fyr/internal/setup"
	"github.com/meros/fyr/internal/store"
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
	if cmd == "serve" && len(args) > 0 && (args[0] == "--version" || args[0] == "-version") {
		// V0.versioned-releases: the version, or the build on main.
		os.Stdout.WriteString(versionLine() + "\n")
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
	role := fl.String("role", "", "host, front or both (default from config role, else both)")
	hostsFlag := fl.String("hosts", "", "front: other hosts as name=url,… (default from config hosts)")
	// V0.retire-discovery: the tailnet discovery that probed these is gone; the flag is
	// still accepted, so a unit that passes it starts.
	peersFlag := fl.String("peers", "", "ignored (V0.123's discovery was removed; installs pair instead)")
	runnerFlag := fl.String("runner", "", "use this runner socket even when it is not this install's launcher's (#167; development)")
	hostListen := fl.String("host-listen", "", "host: also serve fronts on this tailnet address, or tailnet:PORT (default from config hostListen)")
	wellKnownListen := fl.String("well-known-listen", "", "answer only /.well-known/fyr on this loopback address, for Tailscale Serve over TCP (default from config wellKnownListen)")
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
	cfg := loadConfig(*cfgPath)
	// A systemd unit has a minimal PATH; agents need the user's tools: the
	// config's agentPath, the login shell's PATH (V0.linux-without-nix) and
	// the profile folders that exist (decision 0007).
	if u := os.Getenv("USER"); u != "" || home() != "" {
		if u == "" {
			u = filepath.Base(home())
		}
		login, err := agent.LoginPath(agent.LoginShell(u), loginShellWait)
		if err != nil {
			log.Printf("agent PATH: no login shell PATH (%v); the unit's PATH and agentPath only", err)
		}
		os.Setenv("PATH", agent.AgentPath(agent.PathInput{Config: cfg.AgentPath, Unit: os.Getenv("PATH"), Login: login,
			Home: home(), User: u, Exists: agent.DirExists}))
	}
	// Decision 0037: tools installed by the setup wizard (e.g. gh into <data>/tools).
	setup.EnsureToolsOnPath(filepath.Join(*data, "tools"))
	cfg.override(*listen, *ownerLogin, *role, *hostsFlag, *hostListen)
	if *peersFlag != "" || len(cfg.Peers) > 0 {
		log.Printf("peers: ignored; the old discovery is gone, installs pair instead (fyr pair; V0.retire-discovery)")
	}
	if *wellKnownListen != "" {
		cfg.WellKnownListen = *wellKnownListen
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
		tree = cgroup.Setup(cgroup.Options{Budget: budget, Logf: log.Printf, OomdLimit: cgroup.UnitOomdLimit})
	}
	// V0.90: the launcher's runner holds the agent processes. Its list
	// comes before the restart repair, which leaves their runs live.
	var rn *kept
	if cmd == "serve" {
		rn = keptProcs(launchenv.Current().Runner)
	}
	h := openHost(cfg, *data, rn.threads())
	defer h.st.Close()
	switch cmd {
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
	eng.Attach = att
	// V0.19: screenshots for the show tool, with one Chrome started on demand.
	if bin := preview.FindChrome(); bin != "" {
		shooter := &preview.Chrome{Bin: bin, Dir: filepath.Join(data, "chrome")}
		eng.Shooter = shooter
		defer shooter.Close()
	} else {
		log.Printf("show: no Chrome on PATH; screenshots are off")
	}
	w := &wiring{cfg: cfg, h: h, webFS: webFS, hostName: hostName, data: data, tree: tree, rn: rn, eng: eng, reg: reg, ln: ln, att: att}
	w.wireQuota()
	w.wireAgents()
	w.wireProjects()
	w.wireHeavy()
	w.wireGuard()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go cfg.watch.run(ctx, cfg.configEvery()) // V0.180: say when the config changed under fyrd
	// SIGHUP stops with exit 75: the launcher switches to a next build that
	// scripts/update.sh linked (with its probation and rollback), or restarts
	// the current one.
	var hup atomic.Bool
	hupc := make(chan os.Signal, 1)
	signal.Notify(hupc, syscall.SIGHUP)
	go func() { <-hupc; hup.Store(true); stop() }()
	w.ctx, w.stop = ctx, stop
	w.wireDeploy()
	w.wireBackground()
	w.wireListeners()
	dep := w.dep
	fe := w.fe
	stops := w.stops
	mcpSrv := w.mcpSrv
	mgr := w.mgr
	old := w.old
	srvs := w.srvs

	<-ctx.Done()
	log.Printf("shutting down")
	stops.ResumeStopped() // nothing with the cgroup tree
	mcpSrv.Close() // V0.tool-reload: each CLI connects again to the next fyrd
	sctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	srvs.shutdown(sctx)
	old.Close(sctx)
	if fe != nil {
		<-fe.done // the front's cache is written
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
	go cfg.watch.run(ctx, cfg.configEvery()) // V0.180: say when the config changed under fyrd
	fe := startFront(ctx, frontParts{cfg: cfg, webFS: webFS, name: name, data: data, key: installKey(data), st: st, auth: openAuth(st, cfg.stepUp()),
		hosts: cfg.Hosts}, nil, nil)
	primary := &servers{}
	primary.serve(ln, deploy.Health(fe.fr.Handler(), storeHealthy(st), webHealthy(webFS), cfg.watch.health))
	extra := &servers{onErr: func(error) {}}
	serveFrontSockets(cfg, fe.fr, data, ln.Addr().String(), extra) // V0.130
	defer extra.close()
	log.Printf("build %s (front) listening on http://%s for %d hosts", BuildID(), ln.Addr(), len(cfg.Hosts))
	deploy.MarkReady(ln.Addr().String())
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	primary.shutdown(sctx)
	<-fe.done // the cache is written
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
	procs, err := rc.ListWait(runnerListWait) // V0.90 (3): never miss a kept process
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

// releaseKey is the key releases verify with, for Settings → Updates
// (V0.release-without-gh): its fingerprint, whether it is built in, and why
// it is not a key.
func releaseKey(r *deploy.Releases) map[string]any {
	fp, builtIn, err := r.KeyInfo()
	m := map[string]any{"fingerprint": fp, "builtIn": builtIn}
	if err != nil {
		m["error"] = "updateKey is not an ed25519 public key"
	}
	return m
}
