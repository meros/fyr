// Package authtest is the harness of decision 0030 (Part 4.1): a fake
// tailscaled with a Tailscale Serve emulator, an HTTPS front with a test CA, a
// real-process fyrd starter, an in-memory mDNS bus and a fixture of the live
// install's state. Nothing here is imported by the product; tests and the
// browser harness use it.
package authtest

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// User is a tailnet user.
type User struct {
	ID    int64
	Login string
}

// Node is a machine on the fake tailnet. A tagged node (Tags set) has no
// user of its own: tailscaled names the tag's pseudo user for it, as the
// real one does. SharedIn is a node another tailnet shared with this one.
type Node struct {
	Name     string // MagicDNS name without the trailing dot: laptop.tail-test.ts.net
	IP       string // 100.64.x.y
	UserID   int64
	Tags     []string
	SharedIn bool
	Online   bool
	Expired  bool // the node key has expired
}

// Tailscaled is a fake tailscaled: the LocalAPI (status and whois) on a Unix
// socket. Self is the node fyrd runs on.
type Tailscaled struct {
	Socket string

	mu    sync.Mutex
	self  Node
	users map[int64]User
	nodes map[string]Node // by name, the peers
	ln    net.Listener
	srv   *http.Server
}

// NewTailscaled starts the LocalAPI in dir (a short path: a Unix socket's
// limit is about 100 bytes). self is this machine's node.
func NewTailscaled(dir string, self Node, users ...User) (*Tailscaled, error) {
	t := &Tailscaled{Socket: filepath.Join(dir, "tailscaled.sock"), self: self, users: map[int64]User{}, nodes: map[string]Node{}}
	for _, u := range users {
		t.users[u.ID] = u
	}
	ln, err := net.Listen("unix", t.Socket)
	if err != nil {
		return nil, err
	}
	t.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/localapi/v0/status", t.status)
	mux.HandleFunc("/localapi/v0/whois", t.whois)
	t.srv = &http.Server{Handler: requireLocalAPI(mux)}
	go t.srv.Serve(ln)
	return t, nil
}

// requireLocalAPI: the real LocalAPI refuses a request without
// Sec-Tailscale: localapi, so the fake does too.
func requireLocalAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Tailscale") != "localapi" {
			http.Error(w, "invalid localapi request", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (t *Tailscaled) Close() {
	t.srv.Close()
	os.Remove(t.Socket)
}

// AddUser adds a tailnet user.
func (t *Tailscaled) AddUser(u User) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.users[u.ID] = u
}

// AddNode adds (or replaces) a peer.
func (t *Tailscaled) AddNode(n Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[n.Name] = n
}

// RemoveNode drops a peer.
func (t *Tailscaled) RemoveNode(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.nodes, name)
}

// Self is this machine's node.
func (t *Tailscaled) Self() Node {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.self
}

// Node finds a node (the peers and self) by name.
func (t *Tailscaled) Node(name string) (Node, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.self.Name == name {
		return t.self, true
	}
	n, ok := t.nodes[name]
	return n, ok
}

// Login of a user id, "" when unknown.
func (t *Tailscaled) Login(id int64) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.users[id].Login
}

// byIP finds the node with that address; the connection's port is ignored.
func (t *Tailscaled) byIP(addr string) (Node, bool) {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		addr = h
	}
	if t.self.IP == addr {
		return t.self, true
	}
	for _, n := range t.nodes {
		if n.IP == addr {
			return n, true
		}
	}
	return Node{}, false
}

// The shapes below are the fields of the real LocalAPI's JSON that fyrd
// reads (internal/tailnet), with the real names.
type statusNode struct {
	ID           string
	DNSName      string
	UserID       int64
	Tags         []string `json:",omitempty"`
	TailscaleIPs []string
	Online       bool
	ShareeNode   bool `json:",omitempty"`
	Expired      bool `json:",omitempty"`
}

func (t *Tailscaled) status(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	mk := func(n Node) statusNode {
		return statusNode{ID: "n" + n.IP, DNSName: n.Name + ".", UserID: n.UserID, Tags: n.Tags, TailscaleIPs: []string{n.IP}, Online: n.Online, ShareeNode: n.SharedIn, Expired: n.Expired}
	}
	self := mk(t.self)
	self.Online = true
	peers := map[string]statusNode{}
	names := make([]string, 0, len(t.nodes))
	for name := range t.nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		peers["nodekey:"+name] = mk(t.nodes[name])
	}
	users := map[string]map[string]any{}
	for id, u := range t.users {
		users[strconv.FormatInt(id, 10)] = map[string]any{"ID": id, "LoginName": u.Login}
	}
	writeJSON(w, map[string]any{"BackendState": "Running", "Self": self, "TailscaleIPs": []string{t.self.IP}, "Peer": peers, "User": users})
}

func (t *Tailscaled) whois(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, ok := t.byIP(r.URL.Query().Get("addr"))
	if !ok {
		http.Error(w, "no match for "+r.URL.Query().Get("addr"), http.StatusNotFound)
		return
	}
	node := map[string]any{"Name": n.Name + ".", "StableID": "n" + n.IP, "Addresses": []string{n.IP + "/32"}}
	if n.SharedIn {
		node["Sharer"] = 99
	}
	var prof map[string]any
	if len(n.Tags) > 0 {
		// The real LocalAPI names a tagged node's pseudo user.
		node["Tags"] = n.Tags
		prof = map[string]any{"ID": 0, "LoginName": "tagged-devices"}
	} else {
		prof = map[string]any{"ID": n.UserID, "LoginName": t.users[n.UserID].Login}
	}
	writeJSON(w, map[string]any{"Node": node, "UserProfile": prof})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Serve is the Tailscale Serve emulator for one client node: a TCP listener
// that proxies to a fyrd's serve.sock exactly as tailscaled does with a
// `serve --set-path / unix:/…` handler (#225). It strips every incoming
// Tailscale-* header, sets Tailscale-User-Login, -Name and -Profile-Pic from
// the client's node (none for a tagged node), sets X-Forwarded-For to the
// node's address, and sends Host: localhost: the browser's Host does not
// pass on a Unix-socket handler.
type Serve struct {
	URL    string // http://127.0.0.1:PORT, standing for https://<node>.ts.net
	Client Node
	srv    *http.Server
}

// serveProxy is the reverse proxy of a Serve for a browser (or an install's
// probe) on client: what tailscaled does on its way to a Unix socket (#225).
func (t *Tailscaled) serveProxy(client Node, sock string, selfURL string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: &http.Transport{DialContext: unixDialer(sock), DisableKeepAlives: true},
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "localhost"
			pr.Out.Host = "localhost"
			for k := range pr.Out.Header {
				if strings.HasPrefix(strings.ToLower(k), "tailscale-") {
					delete(pr.Out.Header, k)
				}
			}
			if len(client.Tags) == 0 {
				if login := t.Login(client.UserID); login != "" {
					pr.Out.Header.Set("Tailscale-User-Login", login)
					pr.Out.Header.Set("Tailscale-User-Name", strings.SplitN(login, "@", 2)[0])
					pr.Out.Header.Set("Tailscale-User-Profile-Pic", "https://example.invalid/pic.png")
				}
			}
			// The browser is at the public name (https://<node>.ts.net:7443); this
			// emulator stands in for it on a loopback port, so what the browser
			// would have said about itself is said as the public name.
			public := "https://" + t.Self().Name + ":7443"
			if selfURL != "" {
				if o := pr.In.Header.Get("Origin"); o == selfURL {
					pr.Out.Header.Set("Origin", public)
				}
				if ref := pr.In.Header.Get("Referer"); strings.HasPrefix(ref, selfURL) {
					pr.Out.Header.Set("Referer", public+strings.TrimPrefix(ref, selfURL))
				}
			}
			pr.Out.Header.Set("X-Forwarded-For", client.IP)
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
		},
		FlushInterval: -1, // a live stream passes at once
	}
}

// ServeFor starts an emulator for a browser on client, reaching sock.
func (t *Tailscaled) ServeFor(client Node, sock string) (*Serve, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Serve{URL: "http://" + ln.Addr().String(), Client: client}
	s.srv = &http.Server{Handler: t.serveProxy(client, sock, s.URL)}
	go s.srv.Serve(ln)
	return s, nil
}

// ServeTLS is a node's Serve endpoint as another node's program sees it: HTTPS
// with a certificate from ca for the node's MagicDNS name (the WebPKI probe of
// condition 1), proxying to sock with the caller's identity.
func (t *Tailscaled) ServeTLS(caller Node, sock string, ca *CA) (*TLSFront, error) {
	return ca.Serve(t.serveProxy(caller, sock, ""), t.Self().Name)
}

func (s *Serve) Close() { s.srv.Close() }

// Public is the host:port the browser used, for a Serve on port 7443.
func (s *Serve) Public(t *Tailscaled) string { return t.Self().Name + ":7443" }

func (t *Tailscaled) String() string { return fmt.Sprintf("tailscaled(%s)", t.Socket) }
