package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/roster"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/tailnet"
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

// loopbackAddr: addr is host:port on a loopback address.
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	return err == nil && (host == "localhost" || (ip != nil && ip.IsLoopback()))
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

// storeHealthy: the database answers a read (#148).
func storeHealthy(st *store.Store) deploy.Check {
	return func() error {
		if _, err := st.GetKV("autoDeploy"); err != nil {
			return fmt.Errorf("the database does not answer: %v", err)
		}
		return nil
	}
}

// webHealthy: the client this build serves has its page (#148).
func webHealthy(webFS fs.FS) deploy.Check {
	return func() error {
		if _, err := fs.Stat(webFS, "index.html"); err != nil {
			return errors.New("the web client has no index.html")
		}
		return nil
	}
}

// selftest is a new build's smoke test (#150): run on a snapshot of the
// database before a switch, it opens the data with this build's schema,
// reads it, and checks the client it embeds. It starts no agent, effect,
// poller or listener.
func selftest(data string, webFS fs.FS) {
	fail := func(f string, a ...any) {
		fmt.Fprintf(os.Stderr, "selftest: "+f+"\n", a...)
		os.Exit(1)
	}
	st, err := store.Open(filepath.Join(data, "fyr.db"))
	if err != nil {
		fail("opening the database: %v", err)
	}
	defer st.Close()
	if err := storeHealthy(st)(); err != nil {
		fail("%v", err)
	}
	if _, err := st.Changes(); err != nil {
		fail("reading the changes: %v", err)
	}
	// Decision 0030 3.9: a build that cannot read the install key, the roster or the
	// device table is not switched to.
	key, err := identity.Load(data)
	if err != nil {
		fail("reading the install key: %v", err)
	}
	ros, err := roster.Open(data, key, "selftest", nil)
	if err != nil {
		fail("reading the roster: %v", err)
	}
	if !ros.SelfRemoved() {
		if m := ros.Members(); len(m) == 0 || m[0].ID != key.ID() {
			fail("the roster does not hold this install as a member")
		}
	}
	as, err := auth.Open(st, nil)
	if err != nil {
		fail("reading the devices: %v", err)
	}
	_ = as.List()
	if err := webHealthy(webFS)(); err != nil {
		fail("%v", err)
	}
	fmt.Printf("selftest: build %s passed\n", BuildID())
}

// restartWhenTailnet restarts fyrd once this machine has a tailnet address,
// so the hostListen it started without comes up (a Mac off the tailnet at
// start). SIGHUP ends it as a deploy does (ExitDeploy): the launcher restarts
// current and the runner keeps the agents.
func restartWhenTailnet(ctx context.Context, socket string) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := tailnet.IPv4(socket); err == nil {
				log.Printf("tailnet is up; restarting to serve other installs on it")
				syscall.Kill(os.Getpid(), syscall.SIGHUP)
				return
			}
		}
	}
}

// oldListeners are the two ways a front reached a host before the peer link: the
// tailnet listener behind tailscaled's whois, and a loopback host port. The peer
// way in's enforce mode removes them (decision 0030 3.6, 5.1); observe restores
// them, so `fyr auth observe` is always the way back.
type oldListeners struct {
	mu    sync.Mutex
	open  func() *servers // starts them
	srvs  *servers
	onUse func()
}

// Set starts or stops the old listeners.
func (o *oldListeners) Set(enable bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if enable == (o.srvs != nil && len(o.srvs.list) > 0) || o.open == nil {
		return
	}
	if enable {
		o.srvs = o.open()
		log.Printf("peer: the old host listeners are back (peer is in observe)")
		return
	}
	o.srvs.close()
	o.srvs = nil
	log.Printf("peer: the old host listeners are removed (peer is in enforce)")
}

// SetOnUse hears each request the old listeners serve.
func (o *oldListeners) SetOnUse(f func()) { o.onUse = f }

// Close stops them at shutdown.
func (o *oldListeners) Close(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.srvs != nil {
		o.srvs.shutdown(ctx)
	}
}
