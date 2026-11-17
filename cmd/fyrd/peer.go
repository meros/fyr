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
	"strconv"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/roster"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/tailnet"
)

// peerSide is an install's peer port, roster and pairing (decision 0030 3.6,
// V0.158).
type peerSide struct {
	env  *peer.Env
	port atomic.Int64
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

// startPeer opens the roster, starts the peer port when the config has one, and
// runs pairing and sync. It returns the peer side; with no peer port the roster
// still exists (the install is its own member) and nothing pairs.
func startPeer(ctx context.Context, cfg config, data, name string, srv *server.Server, fr *front.Front, st *auth.Store, key *identity.Key) *peerSide {
	ros, err := roster.Open(data, key, name, nil)
	if err != nil {
		log.Printf("peer: roster: %v; peers are off", err)
		return nil
	}
	p := &peerSide{}
	dial := &peerlink.Dialer{Key: key, Resolve: resolver(cfg.ResolveFile)}
	env := &peer.Env{Key: key, Roster: ros, Name: name, Serve: serveClient(cfg), ServePort: servePort(cfg), Dial: dial,
		Drive: peer.OpenDrive(data), Logf: log.Printf, Port: func() int { return int(p.port.Load()) }}
	if tailnet.Available(cfg.tsSocket()) && cfg.OwnerLogin != "" {
		env.TS = tsStatus{cfg.tsSocket()}
	}
	p.env = env
	selfHost := ""
	if _, dns, err := tailnet.Self(cfg.tsSocket()); (err == nil || err == tailnet.ErrTagged) && dns != "" {
		selfHost = dns
	}
	if cfg.PeerListen != "" {
		ln, err := net.Listen("tcp", cfg.PeerListen)
		if err != nil {
			log.Printf("peer: listen %s: %v; peers are off", cfg.PeerListen, err)
			return p
		}
		p.port.Store(int64(ln.Addr().(*net.TCPAddr).Port))
		var members http.Handler = http.NotFoundHandler()
		if srv != nil {
			members = srv.LinkHandler(env)
		}
		hs, err := peerlink.Serve(ln, key, env.Handler(members))
		if err != nil {
			log.Printf("peer: %v", err)
			return p
		}
		go func() { <-ctx.Done(); hs.Close() }()
		ros.SetAddress(name, selfHost, int(p.port.Load()))
		log.Printf("peer: peer port at %s (TLS 1.3, key %s)", ln.Addr(), key.ID())
	}
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
				if m.ID == key.ID() || m.Port == 0 {
					continue
				}
				host := m.Host
				if host == "" {
					host = m.Name
				}
				fr.UsePeerLink(m.ID, m.Name, front.PeerEndpoint{ID: m.ID, Addr: net.JoinHostPort(host, strconv.Itoa(m.Port)), Dial: dial})
			}
		}
		ros.OnChange(follow)
		follow()
	}
	every := time.Duration(cfg.PeerEverySeconds) * time.Second
	go env.Run(ctx, every)
	return p
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
			type row struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Self  bool   `json:"self,omitempty"`
				Port  int    `json:"port,omitempty"`
				Drive bool   `json:"drive"`
			}
			var rows []row
			for _, m := range p.env.Roster.Members() {
				rows = append(rows, row{ID: m.ID, Name: m.Name, Self: m.ID == p.env.Key.ID(), Port: m.Port, Drive: p.env.Drive.MayDrive(m.ID)})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"members": rows, "whoisLinkUses": fr.WhoisLinkUses(), "rounds": p.env.Rounds.Load()})
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
