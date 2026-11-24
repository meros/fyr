package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strconv"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/httpguard"
	"github.com/meros/fyr/internal/tailnet"

	"context"
	"errors"
)

// V0.forwarded-ports: each forward port of a front is reached the three ways
// the front itself is: on loopback, through Tailscale Serve's own socket, and
// on the owner's LAN listener. A port that cannot be opened is logged and
// left out; the front serves on.

// forwardSocket is the socket Tailscale Serve proxies forward port p to.
func forwardSocket(data string, p int) string {
	return auth.SocketPath(data, "serve-"+strconv.Itoa(p)+".sock")
}

// forwardTargets is what the setup's Tailscale Serve fix publishes: each
// forward port to its socket. None on macOS, where Serve cannot proxy to a
// socket, and none on an install that is no front.
func forwardTargets(cfg config, data string) map[int]string {
	if runtime.GOOS == "darwin" || cfg.Role == "host" {
		return nil
	}
	out := map[int]string{}
	for _, p := range cfg.forwardPorts() {
		out[p] = "unix:" + forwardSocket(data, p)
	}
	return out
}

func serveForwards(cfg config, fr *front.Front, data, listen string, serve func(net.Listener, http.Handler)) {
	ports := cfg.forwardPorts()
	if len(ports) == 0 {
		return
	}
	host, _, _ := net.SplitHostPort(listen)
	dns := ""
	if tailnet.Available(cfg.tsSocket()) {
		if _, d, err := tailnet.Self(context.Background(), cfg.tsSocket()); err == nil || errors.Is(err, tailnet.ErrTagged) {
			dns = d
		}
	}
	var pair *tls.Certificate
	if t := cfg.TLS; t.Cert != "" && t.Key != "" && t.Listen != "" {
		if p, err := tls.LoadX509KeyPair(t.Cert, t.Key); err == nil {
			pair = &p
		}
	}
	var open []int
	for _, p := range ports {
		h := fr.ForwardHandler(p)
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err != nil {
			log.Printf("forward port %d: %v; not forwarding on it", p, err)
			continue
		}
		serve(ln, h)
		open = append(open, p)
		if sock, err := auth.ListenUnix(forwardSocket(data, p)); err != nil {
			log.Printf("forward port %d: serve socket: %v", p, err)
		} else {
			public := ""
			if dns != "" {
				public = dns + ":" + strconv.Itoa(p)
			}
			// As serve.sock (decision 0030 3.2): only tailscaled, as root, and the listed users.
			serve(auth.PeerListener{Listener: sock, Name: "forward socket", Allow: func(uid int) bool { return uid == 0 || slices.Contains(cfg.ServeSocketUsers, uid) }},
				httpguard.ServeSocket(public, h))
		}
		if pair != nil {
			lanHost, _, _ := net.SplitHostPort(cfg.TLS.Listen)
			// HTTP/1.1 only: a WebSocket's upgrade needs it, and a browser falls back to it by itself.
			lan, err := tls.Listen("tcp", net.JoinHostPort(lanHost, strconv.Itoa(p)), &tls.Config{Certificates: []tls.Certificate{*pair}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
			if err != nil {
				log.Printf("forward port %d: tls: %v", p, err)
			} else {
				serve(lan, httpguard.ViaLAN(h))
			}
		}
	}
	if len(open) > 0 {
		log.Printf("forwarding threads' local servers on ports %v", open)
	}
}
