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
	"syscall"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/preview"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/server"
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
	var webFS fs.FS = web.Dist()
	if *webDir != "" {
		webFS = os.DirFS(*webDir)
	}
	hostName, _ := os.Hostname()
	if cfg.Role == "front" {
		if cmd != "serve" {
			log.Fatalf("%s needs the host role", cmd)
		}
		serveFront(cfg, webFS, hostName, *data)
		return
	}
	h := openHost(cfg, *data, filepath.Join(*t3Dir, "statev2.sqlite"))
	defer h.st.Close()
	switch cmd {
	case "import-t3":
		importT3(h.t3, *list)
	case "serve":
		h.serve(cfg, webFS, hostName, *data)
	default:
		log.Fatalf("unknown command %q", cmd)
	}
}

// serve runs the host until a signal: its agents, effects and pollers, the
// host protocol, and in role both the front on the same port (V0.51).
func (h *host) serve(cfg config, webFS fs.FS, hostName, data string) {
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
	quotas := quota.New(quotaList(), home())
	reg.OnChange = func() { quotas.SetProfiles(quotaList()) }
	quotas.Client = &http.Client{Timeout: 10 * time.Second}
	quotas.Logf = log.Printf
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
		LogDir: filepath.Join(data, "logs"), Attachments: att.Get, OnRateLimit: quotas.Observe, HandoverDir: filepath.Join(data, "handover"),
	})
	eng.Agents = mgr
	eng.ProfileExists = func(id string) bool { _, ok := h.profile(id); return ok }

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	dep := &deploy.Deployer{Data: data, DB: h.st, Host: eng, Quit: stop, Live: BuildID()}
	dep.OnProgress = func(p deploy.Progress) { eng.SetDeploy(p, p.Phase) } // V0.39
	prDetails := &github.Details{Gh: github.Gh}                            // V0.43: the PR tab's details, cached 30 s
	eng.Effects = engine.Outside{Gh: github.Gh, PR: prDetails, Deploy: dep.Deploy}
	poller := &github.Poller{Gh: github.Gh, Src: engine.PRWatch{E: eng}}
	eng.OnPR = poller.Kick
	go poller.Run(ctx)
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
	srv := &server.Server{Engine: eng, MCP: mcpSrv, BuildID: BuildID(), ProfileList: h.profiles, Providers: providerAPI{reg}, HandoverPlan: mgr.HandoverPlan, OwnerLogin: cfg.OwnerLogin, Attach: att, FS: h.folders, PR: prDetails,
		Quota:        func(ctx context.Context) any { return quotas.Get(ctx) },
		T3Candidates: func() (any, error) { return h.t3.Candidates() }}
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
	hostHandler := srv.Handler()
	if cfg.Role == "host" {
		serve(ln, hostHandler)
	} else {
		// both (V0.51): the front reaches this host over a loopback link,
		// through the host protocol only; agents keep /mcp on the main port.
		link := listenLoopback("127.0.0.1:0")
		serve(link, hostHandler)
		hosts := append([]front.HostConfig{{Name: hostName, URL: "http://" + link.Addr().String()}}, cfg.Hosts...)
		// The front's device data (push keys and subscriptions) stays in
		// this machine's database, where the host kept it before V0.54.
		fr := front.New(front.Options{Name: hostName, BuildID: BuildID(), OwnerLogin: cfg.OwnerLogin, Web: webFS,
			Push: newPusher(h.st), CacheDir: filepath.Join(data, "front"), Local: hostName}, hosts)
		go func() { fr.Run(ctx); close(frontDone) }()
		mux := http.NewServeMux()
		mux.Handle("/mcp", hostHandler)
		mux.Handle("/", fr.Handler())
		serve(ln, mux)
	}
	if cfg.HostListen != "" {
		// V0.54: "tailnet:PORT" is this machine's tailnet address.
		if port, ok := strings.CutPrefix(cfg.HostListen, "tailnet:"); ok {
			ip, err := tailnet.IPv4(tailnet.DefaultSocket)
			if err != nil {
				log.Fatalf("hostListen %s: %v", cfg.HostListen, err)
			}
			cfg.HostListen = net.JoinHostPort(ip, port)
		}
		serve(listenTailnet(cfg.HostListen), srv.PeerHandler(tailnet.Peers{Socket: tailnet.DefaultSocket}))
		log.Printf("serving fronts on the tailnet at %s", cfg.HostListen)
	}
	log.Printf("build %s (%s) listening on http://%s (%d threads, %d providers)", BuildID(), cfg.Role, ln.Addr(), len(eng.Snapshot()), len(h.profiles()))
	deploy.MarkReady()
	dep.ReportPending()

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
	fr := front.New(front.Options{Name: name, BuildID: BuildID(), OwnerLogin: cfg.OwnerLogin, Web: webFS,
		Push: newPusher(st), CacheDir: filepath.Join(data, "front")}, cfg.Hosts)
	done := make(chan struct{})
	go func() { fr.Run(ctx); close(done) }()
	hs := &http.Server{Handler: fr.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	log.Printf("build %s (front) listening on http://%s for %d hosts", BuildID(), ln.Addr(), len(cfg.Hosts))
	deploy.MarkReady()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(sctx)
	<-done // the cache is written
}
