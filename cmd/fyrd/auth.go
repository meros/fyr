package main

import (
	"log"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/tailnet"
)

// openAuth loads a front's browser sessions (V0.130).
func openAuth(st *store.Store, stepUp time.Duration) *auth.Store {
	a, err := auth.Open(st, nil)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	a.SetStepUp(stepUp)
	return a
}

// serveAuthSockets serves a front's two Unix sockets (V0.130, decision
// 0028): the owner socket, where fyr open gets a one-time link into
// http://fyr.localhost:PORT, and the socket Tailscale Serve proxies to, the
// only place the tailnet login header counts. A socket that cannot be made
// is logged, and the front keeps serving. public is the host:port the
// browser reaches Serve on (servePublic).
func serveAuthSockets(fr *front.Front, data, listen, public string, serveUsers []int, serve func(net.Listener, http.Handler)) {
	_, port, _ := net.SplitHostPort(listen)
	if ln, err := auth.ListenUnix(auth.SocketPath(data, auth.OwnerSocket)); err != nil {
		log.Printf("auth: owner socket: %v; fyr open will not work", err)
	} else {
		serve(auth.OwnerListener{Listener: ln}, fr.OwnerHandler("http://fyr.localhost:"+port))
		log.Printf("auth: owner socket at %s", ln.Addr())
	}
	if ln, err := auth.ListenUnix(auth.SocketPath(data, auth.ServeSocket)); err != nil {
		log.Printf("auth: serve socket: %v; Tailscale Serve must proxy to the loopback port", err)
	} else {
		// Decision 0030 3.2: tailscaled reaches this socket as root. Another
		// user, fyrd's own included, could write the login header, so it is
		// closed (serveSocketUsers lists the users a test or a setup adds).
		serve(auth.PeerListener{Listener: ln, Name: "serve socket", Allow: func(uid int) bool { return uid == 0 || slices.Contains(serveUsers, uid) }},
			server.ServeSocket(public, fr.Handler()))
		log.Printf("auth: Tailscale Serve socket at %s", ln.Addr())
	}
}

// tailnetNodes is the whois the front asks which node a Serve request came
// from (decision 0030 3.4); nil without tailscaled, which enrols by login as
// V0.130 did.
func tailnetNodes(cfg config) front.NodeAuth {
	if !tailnet.Available(cfg.tsSocket()) {
		return nil
	}
	return tailnet.Peers{Socket: cfg.tsSocket()}
}
