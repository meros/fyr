package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/peerwire"
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

// start runs the peer side (internal/peerwire). srv, fr and old are nil when
// this install has none.
func (p *peerSide) start(ctx context.Context, srv *server.Server, fr *front.Front, st *auth.Store, old *oldListeners) {
	if p == nil {
		return
	}
	d := peerwire.Deps{Config: peerwire.Config{PeerListen: p.cfg.PeerListen, LANBusFile: p.cfg.LANBusFile, MDNS: p.cfg.Discovery.MDNS == nil || *p.cfg.Discovery.MDNS,
		LinkBackoff: time.Duration(p.cfg.LinkBackoffMillis) * time.Millisecond, LinkDial: time.Duration(p.cfg.LinkDialMillis) * time.Millisecond,
		Every: time.Duration(p.cfg.PeerEverySeconds) * time.Second, OwnerLogin: p.cfg.OwnerLogin, TSSocket: p.cfg.tsSocket()},
		Name: p.name, Key: p.key, Env: p.env, Dial: p.dial, Port: &p.port}
	// A nil pointer in an interface is not nil: set each only when there is one.
	if fr != nil {
		d.Front = fr
	}
	if srv != nil {
		d.Host = peerHost{srv}
	}
	if st != nil {
		d.Auth = st
	}
	if old != nil {
		d.Old = old
	}
	peerwire.Start(ctx, d)
}

// peerHost is the host's side of the peer wiring.
type peerHost struct{ s *server.Server }

func (h peerHost) Members(env *peer.Env) http.Handler { return h.s.LinkHandler(env) }
func (h peerHost) SetOnWhois(f func())                { h.s.OnWhois = f }

// peerPort is the port of the peer listener, 0 while there is none.
func (p *peerSide) peerPort() int {
	if p == nil {
		return 0
	}
	return int(p.port.Load())
}

// ownerRoutes adds the roster's calls to the owner socket (internal/peerwire).
func (p *peerSide) ownerRoutes(fr *front.Front) func(mux *http.ServeMux) {
	return peerwire.OwnerRoutes(peerwire.Owner{Env: p.env, Key: p.key, Data: p.data, WhoisLinkUses: fr.WhoisLinkUses,
		Restart: func() { syscall.Kill(os.Getpid(), syscall.SIGHUP) }})
}

func servePort(cfg config) int {
	if len(cfg.PeerPorts) > 0 {
		return cfg.PeerPorts[0]
	}
	return 7443
}
