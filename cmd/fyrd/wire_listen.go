package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/pipe"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/tailnet"
)

// wireListeners makes the servers and the listeners and starts serving.
//
// Needs from earlier steps: dep, ghView, mcpSrv, mgr, prDetails, quotas, sampler, setupChecker.
// Makes: fe, old, srvs.
// Order: last: it reads w.ghView and w.prDetails from wireBackground and w.dep from wireDeploy.
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
		select {
		case <-time.After(setupHostAfter):
		case <-ctx.Done():
			return
		}
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
