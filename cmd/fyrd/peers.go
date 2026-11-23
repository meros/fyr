package main

import (
	"context"
	"errors"
	"log"
	"strconv"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/roster"
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

// wellKnown is this install's /.well-known/fyr answer (V0.123), with its
// key id (V0.124); the pairing reads it.
func wellKnown(cfg config, name, host, id string) peer.WellKnown {
	return peer.WellKnown{Fyr: 1, Name: name, Role: cfg.Role, Build: BuildID(), Owner: cfg.OwnerLogin, Host: host, ID: id}
}

// installID is this install's key id (V0.124, decision 0026): the key is
// made at the first start and kept in the data folder. fyrd does not start
// without it, so the id never changes behind the owner's back.
// finishRotation takes a waiting key (fyr rotate-key) into use once the roster
// holds the rotation to it; a waiting key the roster does not know is dropped.
func finishRotation(data string) {
	next, err := identity.LoadNext(data)
	if err != nil {
		log.Fatalf("install key: %v", err)
	}
	if next == nil {
		return
	}
	if roster.HasRotationTo(data, next.Public) {
		if err := identity.Promote(data); err != nil {
			log.Fatalf("install key: %v", err)
		}
		log.Printf("install key: rotated to %s", next.ID())
		return
	}
	identity.DropNext(data)
}

func installKey(data string) *identity.Key {
	finishRotation(data)
	k, err := identity.Load(data)
	if err != nil {
		log.Fatalf("install key: %v", err)
	}
	log.Printf("install key id %s", k.ID())
	return k
}

func installID(data string) string {
	k, err := identity.Load(data)
	if err != nil {
		log.Fatalf("install key: %v", err)
	}
	log.Printf("install key id %s", k.ID())
	return k.ID()
}

// servePublic is the host:port the browser reaches this install's Tailscale
// Serve on: the node's MagicDNS name and the first peer port (default 7443,
// the NixOS module's); "" without tailscaled.
func servePublic(cfg config) string {
	if !tailnet.Available(cfg.tsSocket()) {
		return ""
	}
	_, dns, err := tailnet.Self(context.Background(), cfg.tsSocket())
	if (err != nil && !errors.Is(err, tailnet.ErrTagged)) || dns == "" {
		return ""
	}
	port := 7443
	if len(cfg.PeerPorts) > 0 {
		port = cfg.PeerPorts[0]
	}
	return dns + ":" + strconv.Itoa(port)
}
