package main

import (
	"log"
	"net"

	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/store"
)

// newPusher is a front's Web Push (V0.15). The VAPID subject goes to the push
// services, so it carries no personal data.
func newPusher(st *store.Store) *push.Pusher {
	p := &push.Pusher{DB: st, Subject: "mailto:push@fyr.invalid"}
	if err := p.Init(); err != nil {
		log.Printf("push: %v", err)
	}
	return p
}

// listenLoopback listens on a loopback address only (decision 0002).
func listenLoopback(addr string) net.Listener {
	host, _, err := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); err != nil || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		log.Fatalf("listen %q: v0 listens on loopback only (decision 0002)", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	return ln
}

// listenTailnet listens on a tailnet address only (V0.51): Tailscale's
// CGNAT range or its IPv6 prefix, where whois names every peer.
func listenTailnet(addr string) net.Listener {
	host, _, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !tailnetIP(ip) {
		log.Fatalf("hostListen %q: must be this machine's tailnet address (100.64.0.0/10 or fd7a:115c:a1e0::/48)", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	return ln
}

func tailnetIP(ip net.IP) bool {
	for _, c := range []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"} {
		_, n, _ := net.ParseCIDR(c)
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
