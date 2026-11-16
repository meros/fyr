package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/peers"
	"github.com/meros/fyr/internal/tailnet"
)

// hostURL is where this install serves the host protocol to fronts on other
// machines (its resolved hostListen), or "" when it serves none.
func hostURL(hostListen string) string {
	if hostListen == "" {
		return ""
	}
	return "http://" + hostListen
}

// wellKnown is this install's /.well-known/fyr answer (V0.123).
func wellKnown(cfg config, name, host string) peers.WellKnown {
	return peers.WellKnown{Fyr: 1, Name: name, Role: cfg.Role, Build: BuildID(), Owner: cfg.OwnerLogin, Host: host}
}

// newDiscovery finds the owner's other Fyr hosts (V0.123, decision 0025):
// the static peers of the config, and, when tailscaled runs here, the nodes
// of the owner's own Tailscale user that answer /.well-known/fyr. The front
// follows each found host that serves the host protocol, as it follows a
// configured one: the hosts keep the connections, the client its one uplink.
func newDiscovery(cfg config, self peers.WellKnown, listen string, fr *front.Front) *peers.Discovery {
	opt := peers.Options{Owner: cfg.OwnerLogin, Self: "http://" + listen, Static: cfg.Peers, Ports: cfg.PeerPorts,
		Logf: log.Printf, WellKnown: self,
		OnChange: func(ps []peers.Peer) {
			for _, p := range ps {
				if p.Online && p.Host != "" && p.Name != self.Name {
					fr.AddHost(front.HostConfig{Name: p.Name, URL: p.Host, Remote: true, Found: true})
				}
			}
		}}
	if _, err := os.Stat(tailnet.DefaultSocket); err == nil && cfg.OwnerLogin != "" {
		opt.Nodes = func(ctx context.Context) ([]string, error) { return tailnet.Candidates(ctx, tailnet.DefaultSocket) }
		if _, dns, err := tailnet.Self(tailnet.DefaultSocket); err == nil && dns != "" {
			port := 7443
			if len(cfg.PeerPorts) > 0 {
				port = cfg.PeerPorts[0]
			}
			opt.Self = "https://" + dns + ":" + strconv.Itoa(port)
		}
	}
	return peers.New(opt)
}
