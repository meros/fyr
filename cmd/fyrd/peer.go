package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/qr"
	"github.com/meros/fyr/internal/roster"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/tailnet"
)

// peerSide is an install's peer port, roster and pairing (decision 0030 3.6,
// V0.158).
type peerSide struct {
	env  *peer.Env
	port atomic.Int64
	cfg  config
	name string
	data string
	key  *identity.Key
	dial *peerlink.Dialer
}

// tsStatus is tailscaled's status for the pairing.
type tsStatus struct{ socket string }

func (t tsStatus) Status(ctx context.Context) (tailnet.StatusNode, []tailnet.StatusNode, error) {
	return tailnet.Status(ctx, t.socket)
}

// resolver reads cfg.ResolveFile at each dial.
func resolver(path string) func(string) string {
	if path == "" {
		return nil
	}
	return func(hostport string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		var m map[string]string
		if json.Unmarshal(b, &m) != nil {
			return ""
		}
		if a := m[hostport]; a != "" {
			return a
		}
		// a name alone maps every port of that name to the address, port kept
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			if a := m[host]; a != "" {
				return net.JoinHostPort(a, port)
			}
		}
		return ""
	}
}

// serveClient probes a node's Serve endpoint over ordinary WebPKI.
func serveClient(cfg config) *http.Client {
	roots, _ := x509.SystemCertPool()
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if cfg.RootCAs != "" {
		if pem, err := os.ReadFile(cfg.RootCAs); err == nil {
			roots.AppendCertsFromPEM(pem)
		} else {
			log.Printf("peer: rootCAs: %v", err)
		}
	}
	res := resolver(cfg.ResolveFile)
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if res != nil {
				if a := res(addr); a != "" {
					addr = a
				}
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		}}}
}

// newPeerSide opens the roster and makes the peer side's environment. It starts
// nothing: the front needs the environment (Settings → Hosts) before it exists,
// and the listener needs the front (to follow a member over its peer link).
func newPeerSide(cfg config, data, name string, key *identity.Key) *peerSide {
	ros, err := roster.Open(data, key, name, nil)
	if err != nil {
		log.Printf("peer: roster: %v; peers are off", err)
		return nil
	}
	p := &peerSide{cfg: cfg, name: name, data: data, key: key}
	p.dial = &peerlink.Dialer{Key: key, Resolve: resolver(cfg.ResolveFile)}
	p.env = &peer.Env{Key: key, Roster: ros, Name: name, Serve: serveClient(cfg), ServePort: servePort(cfg), Dial: p.dial,
		Drive: peer.OpenDrive(data), Invites: &peer.Invites{}, Logf: log.Printf, Port: func() int { return int(p.port.Load()) }}
	p.dial.Opts = p.env.DialOptions // a rotated peer is accepted by its chain; this install shows its own
	if tailnet.Available(cfg.tsSocket()) && cfg.OwnerLogin != "" {
		p.env.TS = tsStatus{cfg.tsSocket()}
	}
	return p
}

// envOrNil is the environment, nil-safe.
func (p *peerSide) envOrNil() *peer.Env {
	if p == nil {
		return nil
	}
	return p.env
}

// ownerLogin is the owner's login as the roster carries it, for an install that
// has none of its own (a tagged one).
func (p *peerSide) ownerLogin() string {
	if p == nil {
		return ""
	}
	return p.env.Roster.OwnerLogin()
}

// start runs the peer port when the config has one, pairing and sync. With no peer
// port the roster still exists (the install is its own member) and nothing pairs.
func (p *peerSide) start(ctx context.Context, data string, srv *server.Server, fr *front.Front, st *auth.Store, old *oldListeners) {
	if p == nil {
		return
	}
	cfg, env, ros, key, name := p.cfg, p.env, p.env.Roster, p.key, p.name
	dial := p.dial
	selfHost := ""
	tagged := true
	if login, dns, err := tailnet.Self(cfg.tsSocket()); err == nil || errors.Is(err, tailnet.ErrTagged) {
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
	if srv != nil {
		members = srv.LinkHandler(env)
	}
	handler := env.Handler(members)
	peerOf := func(m roster.Member) peerlink.Peer {
		return peerlink.Peer{ID: m.ID, Key: m.Key, Name: m.Name, Host: m.Host, Port: m.Port}
	}
	mesh := peerlink.New(peerlink.Config{Key: key, Dialer: dial, Handler: handler, Logf: log.Printf,
		MinBackoff: time.Duration(cfg.LinkBackoffMillis) * time.Millisecond, DialTimeout: time.Duration(cfg.LinkDialMillis) * time.Millisecond,
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
		OnUp:        func(string) { env.Kick() }})
	env.Mesh = mesh
	ros.OnChange(mesh.Changed)
	go mesh.Run(ctx)
	if cfg.PeerListen != "" {
		ln, err := net.Listen("tcp", cfg.PeerListen)
		if err != nil {
			log.Printf("peer: listen %s: %v; peers are off", cfg.PeerListen, err)
			return
		}
		p.port.Store(int64(ln.Addr().(*net.TCPAddr).Port))
		hs, err := peerlink.ServeMesh(ln, key, handler, mesh.Accept, ros.RotationChain()...)
		if err != nil {
			log.Printf("peer: %v", err)
			return
		}
		go func() { <-ctx.Done(); hs.Close() }()
		ros.SetAddress(name, selfHost, int(p.port.Load()))
		log.Printf("peer: peer port at %s (TLS 1.3, key %s)", ln.Addr(), key.ID())
	}
	// The peer way in's mode (decision 0030 5.1): enforce removes the old listeners, and
	// is reached by itself only when every followed host is on the pinned link.
	if st != nil {
		env.Enforce = func() bool { return st.Mode(auth.WayPeer) == auth.ModeEnforce }
		env.OnServed = func() { st.Success(auth.WayPeer, "link") }
		st.SetPeerGate(func() (bool, string) {
			if fr == nil {
				return true, ""
			}
			return fr.AllOnPeerLink()
		}, func() int {
			n := 0
			for _, m := range ros.Members() {
				if m.ID != key.ID() {
					n++
				}
			}
			return n
		})
		if srv != nil {
			srv.OnWhois = func() { st.WouldRefuse(auth.WayPeer) }
		}
		if old != nil {
			old.onUse = func() { st.WouldRefuse(auth.WayPeer) }
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
		if fr != nil {
			fr.DropPeers(func(id string) bool { _, in := ros.Member(id); return in })
		}
		if ros.SelfRemoved() {
			log.Printf("peer: this install was removed from the roster by another member; it serves no peer and pairs with none until the owner pairs it again")
		}
	})
	// Devices: every signed-in device is a statement the other installs hold, and a
	// revoke anywhere ends the session where it was made.
	if st != nil {
		for _, d := range st.Signed() {
			ros.DeviceAdd(d.ID, d.Name, d.How)
		}
		st.OnDevice(func(d auth.Device) { ros.DeviceAdd(d.ID, d.Name, d.How) })
		st.OnRevoke(func(ids []string) {
			for _, id := range ids {
				ros.DeviceRevoke(id)
			}
		})
		ros.OnChange(func() {
			for _, d := range st.Signed() {
				if ros.DeviceRevoked(d.ID) {
					st.Revoke(d.ID, "roster")
				}
			}
		})
	}
	// A front prefers the peer link to every member that has a peer port.
	if fr != nil {
		follow := func() {
			for _, m := range ros.Members() {
				if m.ID == key.ID() {
					continue
				}
				// membership decides who is followed; an address only decides who is dialled
				a := ""
				if m.Port != 0 {
					host := m.Host
					if host == "" {
						host = m.Name
					}
					a = net.JoinHostPort(host, strconv.Itoa(m.Port))
				}
				fr.UsePeerLink(m.ID, m.Name, front.PeerEndpoint{ID: m.ID, Addr: a, Dial: dial, Mesh: mesh})
			}
		}
		ros.OnChange(follow)
		follow()
	}
	every := time.Duration(cfg.PeerEverySeconds) * time.Second
	go env.Run(ctx, every)
}

// peerPort is the port of the peer listener, 0 while there is none.
func (p *peerSide) peerPort() int {
	if p == nil {
		return 0
	}
	return int(p.port.Load())
}

// ownerRoutes adds the roster's calls to the owner socket: fyr host list and
// fyr host drive.
func (p *peerSide) ownerRoutes(fr *front.Front) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /host/list", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"members": p.env.Members(), "candidates": p.env.Candidates(r.Context()),
				"whoisLinkUses": fr.WhoisLinkUses(), "rounds": p.env.Rounds.Load(), "ownerLogin": p.env.Roster.OwnerLogin()})
		})
		// The owner's invite (decision 0030 3.6 condition 3): open one here, or join
		// with one from another install. The owner socket is never gated.
		mux.HandleFunc("POST /pair/invite", func(w http.ResponseWriter, r *http.Request) {
			code, until := p.env.NewInvite()
			out := map[string]any{"code": code, "until": until}
			if m, err := qr.Encode(code); err == nil {
				out["qr"] = m.Text()
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(out)
		})
		mux.HandleFunc("POST /pair/join", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ Code, Addr string }
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
			res, err := p.env.Join(r.Context(), b.Code, b.Addr)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": res.Name, "id": res.ID, "addr": res.Addr})
		})
		mux.HandleFunc("POST /host/remove", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ ID string }
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
			w.Header().Set("Content-Type", "application/json")
			if err := p.env.Remove(b.ID); err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		})
		// fyr rotate-key: a new key waits beside the old, the roster says so (signed by the
		// old key), and fyrd restarts on the new one. A key that may be stolen is removed
		// and paired again instead.
		mux.HandleFunc("POST /host/rotate", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			nk, err := identity.New()
			if err == nil {
				err = identity.WriteNext(p.data, nk)
			}
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			p.env.Roster.Rotate(nk.Public)
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "from": p.key.ID(), "to": nk.ID()})
			go func() { // the launcher restarts fyrd on exit 75, as for a release
				time.Sleep(300 * time.Millisecond)
				syscall.Kill(os.Getpid(), syscall.SIGHUP)
			}()
		})
		mux.HandleFunc("POST /host/drive", func(w http.ResponseWriter, r *http.Request) {
			var b struct {
				ID string
				On bool
			}
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
			if _, ok := p.env.Roster.Member(b.ID); !ok || b.ID == p.env.Key.ID() {
				http.Error(w, `{"error":"no such install in the roster"}`, http.StatusNotFound)
				return
			}
			p.env.Drive.Set(b.ID, b.On)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		})
	}
}

func servePort(cfg config) int {
	if len(cfg.PeerPorts) > 0 {
		return cfg.PeerPorts[0]
	}
	return 7443
}
