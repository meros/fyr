package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// servers are HTTP servers fyrd started, kept for one shutdown. onErr hears a
// server that stops on its own; nil ends fyrd (log.Fatal).
type servers struct {
	mu    sync.Mutex
	list  []*http.Server
	onErr func(error)
}

// serve serves h on l in the background.
func (s *servers) serve(l net.Listener, h http.Handler) {
	hs := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.list = append(s.list, hs)
	s.mu.Unlock()
	go func() {
		if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.onErr == nil {
				log.Fatal(err)
			}
			s.onErr(err)
		}
	}()
}

// shutdown stops them gracefully, until ctx ends.
func (s *servers) shutdown(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, hs := range s.list {
		hs.Shutdown(ctx)
	}
}

// close stops them at once.
func (s *servers) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, hs := range s.list {
		hs.Close()
	}
}

// frontParts are what a front takes from its install (V0.51): role both
// gives it this host over the pipe first, role front only the configured
// hosts.
type frontParts struct {
	cfg   config
	webFS fs.FS
	name  string
	data  string
	key   *identity.Key
	st    *store.Store
	auth  *auth.Store
	hosts []front.HostConfig
	// local names this host among hosts (role both); "" for a front only.
	local string
	// hostURL is where other installs reach this host (V0.123); "" for a
	// front only.
	hostURL string
}

// runningFront is a front that runs: wellKnown is its well-known answer, with
// the peer port as it is now; done closes once its cache is written at the end.
type runningFront struct {
	fr        *front.Front
	wellKnown func() peer.WellKnown
	done      chan struct{}
}

// startFront makes the front with its peer side (decision 0030 3.6) and runs
// both. srv and old are the host's (role both); nil for a front only. It
// serves no listener.
func startFront(ctx context.Context, p frontParts, srv *server.Server, old *oldListeners) *runningFront {
	id := p.key.ID()
	// V0.123 (decision 0025): the well-known answer, which the pairing reads;
	// it names where the old host listener is, for an older install's discovery.
	wk := wellKnown(p.cfg, p.name, p.hostURL, id)
	ps := newPeerSide(p.cfg, p.data, p.name, p.key)
	wellKnownNow := func() peer.WellKnown { w := wk; w.Peer = ps.peerPort(); return w }
	var legacy func() int64 // a front only has no host, so it counts none
	if srv != nil {
		legacy = srv.LegacyFronts
	}
	var fr *front.Front
	// The front's device data (push keys and subscriptions) stays in this
	// machine's database, where the host kept it before V0.54.
	fr = front.New(front.Options{ID: id, Name: p.name, BuildID: BuildID(), LegacyFronts: legacy, OwnerLogin: p.cfg.OwnerLogin, OwnerFn: ps.ownerLogin, Peer: ps.envOrNil(), ConfigNote: p.cfg.watch.Note, OnWhoisFallback: func() { p.auth.WouldRefuse(auth.WayPeer) }, Web: p.webFS,
		Push: newPusher(p.st), CacheDir: filepath.Join(p.data, "front"), Store: p.st, Auth: p.auth, Tailnet: tailnetNodes(p.cfg),
		OwnerRoutes: func(mux *http.ServeMux) {
			if ps != nil {
				ps.ownerRoutes(fr)(mux)
			}
		},
		WellKnown: wellKnownNow}, p.hosts)
	rf := &runningFront{fr: fr, wellKnown: wellKnownNow, done: make(chan struct{})}
	go func() { fr.Run(ctx); close(rf.done) }()
	ps.start(ctx, srv, fr, p.auth, old)
	return rf
}

// serveFrontSockets serves a front's ways in beside its main listener: the
// owner and Serve sockets (V0.130) and the LAN over HTTPS (decision 0030
// 3.5).
func serveFrontSockets(cfg config, fr *front.Front, data, listen string, s *servers) {
	serveAuthSockets(fr, data, listen, servePublic(cfg), cfg.ServeSocketUsers, s.serve)
	serveLAN(cfg, fr, s.serve)
}
