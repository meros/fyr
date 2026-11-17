package main

import (
	"log"
	"net"
	"net/http"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// openAuth loads a front's browser sessions (V0.130).
func openAuth(st *store.Store) *auth.Store {
	a, err := auth.Open(st, nil)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	return a
}

// serveAuthSockets serves a front's two Unix sockets (V0.130, decision
// 0028): the owner socket, where fyr open gets a one-time link into
// http://fyr.localhost:PORT, and the socket Tailscale Serve proxies to, the
// only place the tailnet login header counts. A socket that cannot be made
// is logged, and the front keeps serving. public is the host:port the
// browser reaches Serve on (servePublic).
func serveAuthSockets(fr *front.Front, data, listen, public string, serve func(net.Listener, http.Handler)) {
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
		serve(ln, server.ServeSocket(public, fr.Handler()))
		log.Printf("auth: Tailscale Serve socket at %s", ln.Addr())
	}
}
