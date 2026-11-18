// Package peerwire runs an install's peer side (decision 0030 3.6, Amendment
// 1, V0.158–V0.175): the mesh of links to the roster's members, the peer
// port and mDNS, the peer way in's mode (enforce removes the old host
// listeners), a front following each member over its link, and the devices
// kept in step with the roster. It was fyrd's peerSide.start until V0.183;
// fyrd builds the Deps and calls Start.
package peerwire

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/discovery"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/roster"
	"github.com/meros/fyr/internal/tailnet"
)

// Front is what the peer side asks of this install's front (*front.Front).
type Front interface {
	// AllOnPeerLink: every followed host is reached over the pinned link,
	// and why not.
	AllOnPeerLink() (bool, string)
	// DropPeers stops following the hosts reached only through an install
	// that is no longer a member.
	DropPeers(member func(id string) bool)
	// UsePeerLink follows member id over its link.
	UsePeerLink(id, name string, ep front.PeerEndpoint)
}

// Host is what the peer side asks of this install's host (a server.Server).
type Host interface {
	// Members is the host's handler for the requests a member sends over a
	// link.
	Members(env *peer.Env) http.Handler
	// SetOnWhois hears each request on the old whois listener.
	SetOnWhois(func())
}

// Auth is the front's ways in and devices (*auth.Store).
type Auth interface {
	Mode(way string) string
	Success(way, how string)
	WouldRefuse(way string)
	SetPeerGate(gate func() (bool, string), links func() int)
	OnMode(f func(way, mode string))
	Signed() []auth.Device
	OnDevice(f func(d auth.Device))
	OnRevoke(f func(ids []string))
	Revoke(id, by string) []string
}

// Old is the old ways a front reaches a host: the tailnet listener behind
// whois and a loopback host port.
type Old interface {
	// Set starts (observe) or stops (enforce) them.
	Set(enable bool)
	// SetOnUse hears each request they serve.
	SetOnUse(func())
}

// Config is the peer side's part of fyrd's config.
type Config struct {
	PeerListen  string        // the peer port's address; "": no peer port
	LANBusFile  string        // tests: the LAN is this file, not mDNS
	MDNS        bool          // announce and browse on the LAN (with a peer port)
	LinkBackoff time.Duration // the mesh's first redial wait
	LinkDial    time.Duration // the mesh's dial timeout
	Every       time.Duration // the sync round
	OwnerLogin  string        // the configured owner login
	TSSocket    string        // tailscaled's socket
}

// Deps are what Start runs on. Front, Host, Auth and Old are nil when the
// install has none (role host has no front; a front only has no host and no
// old listeners).
type Deps struct {
	Config Config
	Name   string
	Key    *identity.Key
	Env    *peer.Env
	Dial   *peerlink.Dialer
	// Port is where Start publishes the peer port, once it listens.
	Port  *atomic.Int64
	Front Front
	Host  Host
	Auth  Auth
	Old   Old
	// Self is this node's tailnet login and name (tailnet.Self when nil).
	Self func() (login, dns string, err error)
}

// Start runs the peer port when the config has one, pairing and sync. With
// no peer port the roster still exists (the install is its own member) and
// nothing pairs. It returns once everything is started; ctx ends it.
func Start(ctx context.Context, d Deps) {
	cfg, env, ros, key, name := d.Config, d.Env, d.Env.Roster, d.Key, d.Name
	dial := d.Dial
	self := d.Self
	if self == nil {
		self = func() (string, string, error) { return tailnet.Self(cfg.TSSocket) }
	}
	selfHost := ""
	tagged := true
	if login, dns, err := self(); err == nil || errors.Is(err, tailnet.ErrTagged) {
		selfHost = dns
		if err == nil {
			tagged = false
			ros.SetOwnerLogin(login) // an untagged install knows whose it is, and says so; a tagged one learns it by sync
		}
	}
	if tagged && cfg.OwnerLogin == "" {
		log.Printf("peer: this install is tagged and has no owner login yet: pair it with an invite, and it learns the owner's login from the roster")
	}
	// The mesh (decision 0030 Amendment 1): one two-way link per member, opened by either
	// side. The peer handler serves the requests a peer sends over any link.
	var members http.Handler = http.NotFoundHandler()
	if d.Host != nil {
		members = d.Host.Members(env)
	}
	handler := env.Handler(members)
	peerOf := func(m roster.Member) peerlink.Peer {
		return peerlink.Peer{ID: m.ID, Key: m.Key, Name: m.Name, Host: m.Host, Port: m.Port}
	}
	mesh := peerlink.New(peerlink.Config{Key: key, Dialer: dial, Handler: handler, Logf: log.Printf,
		MinBackoff: cfg.LinkBackoff, DialTimeout: cfg.LinkDial,
		Members: func() []peerlink.Peer {
			var out []peerlink.Peer
			for _, m := range ros.Members() {
				if m.ID != key.ID() {
					out = append(out, peerOf(m))
				}
			}
			return out
		},
		Member: func(pub ed25519.PublicKey, chain []roster.Statement) (peerlink.Peer, bool) {
			m, ok := env.MemberByKey(pub, chain)
			return peerOf(m), ok
		},
		SelfRemoved: ros.SelfRemoved,
		Candidates:  func(id string) []string { return env.LANAddrs(ctx, id) },
		OnUp:        func(string) { env.Kick() }})
	env.Mesh = mesh
	var mdns *discovery.MDNS
	switch {
	case cfg.LANBusFile != "":
		env.LAN = discovery.File(cfg.LANBusFile)
	case cfg.PeerListen != "" && cfg.MDNS:
		mdns = &discovery.MDNS{ID: key.ID(), Logf: log.Printf}
		env.LAN = mdns
	}
	ros.OnChange(mesh.Changed)
	go mesh.Run(ctx)
	if cfg.PeerListen != "" {
		ln, err := net.Listen("tcp", cfg.PeerListen)
		if err != nil {
			log.Printf("peer: listen %s: %v; peers are off", cfg.PeerListen, err)
			return
		}
		d.Port.Store(int64(ln.Addr().(*net.TCPAddr).Port))
		hs, err := peerlink.ServeMesh(ln, key, handler, mesh.Accept, ros.RotationChain()...)
		if err != nil {
			log.Printf("peer: %v", err)
			return
		}
		go func() { <-ctx.Done(); hs.Close() }()
		ros.SetAddress(name, selfHost, int(d.Port.Load()))
		log.Printf("peer: peer port at %s (TLS 1.3, key %s)", ln.Addr(), key.ID())
		if mdns != nil {
			mdns.Port = int(d.Port.Load())
			if err := mdns.Start(ctx); err != nil {
				log.Printf("peer: mDNS: %v; this install is not found on the LAN, and finds no one there", err)
			}
		}
	}
	others := func() int {
		n := 0
		for _, m := range ros.Members() {
			if m.ID != key.ID() {
				n++
			}
		}
		return n
	}
	// The peer way in's mode (decision 0030 5.1): enforce removes the old listeners, and
	// is reached by itself only when every followed host is on the pinned link.
	if st := d.Auth; st != nil {
		env.Enforce = func() bool { return st.Mode(auth.WayPeer) == auth.ModeEnforce }
		env.OnServed = func() { st.Success(auth.WayPeer, "link") }
		st.SetPeerGate(func() (bool, string) {
			if d.Front == nil {
				return true, ""
			}
			return d.Front.AllOnPeerLink()
		}, others)
		if d.Host != nil {
			d.Host.SetOnWhois(func() { st.WouldRefuse(auth.WayPeer) })
		}
		if old := d.Old; old != nil {
			old.SetOnUse(func() { st.WouldRefuse(auth.WayPeer) })
			st.OnMode(func(way, mode string) {
				if way == auth.WayPeer {
					old.Set(mode != auth.ModeEnforce)
				}
			})
		}
	}
	// A removal closes what the removed install had open, and a front stops following a
	// host that was reached only through it.
	ros.OnChange(func() {
		env.CloseRemoved()
		if d.Front != nil {
			d.Front.DropPeers(func(id string) bool { _, in := ros.Member(id); return in })
		}
		if ros.SelfRemoved() {
			log.Printf("peer: this install was removed from the roster by another member; it serves no peer and pairs with none until the owner pairs it again")
		}
	})
	// Devices: every signed-in device is a statement the other installs hold, and a
	// revoke anywhere ends the session where it was made.
	if st := d.Auth; st != nil {
		for _, dv := range st.Signed() {
			ros.DeviceAdd(dv.ID, dv.Name, dv.How)
		}
		st.OnDevice(func(dv auth.Device) { ros.DeviceAdd(dv.ID, dv.Name, dv.How) })
		st.OnRevoke(func(ids []string) {
			for _, id := range ids {
				ros.DeviceRevoke(id)
			}
		})
		ros.OnChange(func() {
			for _, dv := range st.Signed() {
				if ros.DeviceRevoked(dv.ID) {
					st.Revoke(dv.ID, "roster")
				}
			}
		})
	}
	// A front prefers the peer link to every member that has a peer port.
	if fr := d.Front; fr != nil {
		follow := func() {
			for _, m := range ros.Members() {
				if m.ID == key.ID() {
					continue
				}
				// membership decides who is followed; an address only decides who is dialled
				fr.UsePeerLink(m.ID, m.Name, front.PeerEndpoint{ID: m.ID, Addr: Addr(m), Dial: dial, Mesh: mesh})
			}
		}
		ros.OnChange(follow)
		follow()
	}
	go env.Run(ctx, cfg.Every)
}

// Addr is where a member's peer port is dialled: its host (else its name) and
// port; "" without a port.
func Addr(m roster.Member) string {
	if m.Port == 0 {
		return ""
	}
	host := m.Host
	if host == "" {
		host = m.Name
	}
	return net.JoinHostPort(host, strconv.Itoa(m.Port))
}
