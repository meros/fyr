// Package peer is how installs find each other and agree who may talk
// (decision 0030 3.6, V0.158): the peer port's handler, the pairing of two
// installs of the same tailnet user by their Serve endpoints (condition 1), and
// the roster sync that gives every member the same statements. Discovery
// proposes; the roster decides; the link proves the key.
package peer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/discovery"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/peers"
	"github.com/meros/fyr/internal/roster"
	"github.com/meros/fyr/internal/tailnet"
)

// Tailnet is tailscaled's status.
type Tailnet interface {
	Status(ctx context.Context) (self tailnet.StatusNode, peers []tailnet.StatusNode, err error)
}

// Env is what an install's peer side needs.
type Env struct {
	Key    *identity.Key
	Roster *roster.Roster
	Name   string
	// TS is tailscaled; nil: no tailnet, so no automatic pairing.
	TS Tailnet
	// Serve probes a node's Serve endpoint over ordinary WebPKI.
	Serve     *http.Client
	ServePort int
	// Port is this install's peer port, 0 while there is none.
	Port func() int
	Dial *peerlink.Dialer
	// Drive says which installs may drive this host (3.7).
	Drive *Drive
	// Invites is the owner's invite this install has open (condition 3).
	Invites *Invites
	Logf    func(string, ...any)
	// Rounds counts the pairing rounds that ran (a test waits for some).
	Rounds atomic.Int64
	// Enforce says the peer way in is in enforce mode; Served counts a request over
	// the link (a success of that way). Both optional.
	Enforce  func() bool
	OnServed func()

	// LAN is where mDNS answers come from (decision 0030 3.6); nil: none. An answer is
	// an address and a claim, never trust.
	LAN discovery.Source

	// Mesh is the two-way link manager (decision 0030 Amendment 1); nil keeps the
	// one-way per-request dial of slice 3.
	Mesh *peerlink.Mesh

	kickc   chan struct{}
	kickMu  sync.Mutex
	trackMu sync.Mutex
	tracked map[string]map[int]context.CancelFunc
	trackN  int
}

func (e *Env) init() {
	if e.Invites == nil {
		e.Invites = &Invites{}
	}
	e.kickMu.Lock()
	if e.kickc == nil {
		e.kickc = make(chan struct{}, 1)
	}
	e.kickMu.Unlock()
}

// Kick asks the sync loop to run soon (a link came up, the roster changed).
func (e *Env) Kick() {
	e.init()
	select {
	case e.kickc <- struct{}{}:
	default:
	}
}

func (e *Env) logf(f string, a ...any) {
	if e.Logf != nil {
		e.Logf(f, a...)
	} else {
		log.Printf(f, a...)
	}
}

// Answer is the peer port's /.well-known/fyr, open to any caller: who this
// install is, nothing else.
type Answer struct {
	Fyr int    `json:"fyr"`
	ID  string `json:"id"`
}

// Found is what a node's Serve says about its install.
type Found struct {
	ID   string
	Port int
	Name string // the install's name
}

// Why a node is not paired by condition 1.
var (
	ErrNotSameUser = errors.New("not the same tailnet user")
	ErrTagged      = errors.New("a tagged node: a tag says who may reach a node, not whose it is")
	ErrShared      = errors.New("a node shared in from another tailnet")
	ErrExpired     = errors.New("the node key has expired")
	ErrNoServe     = errors.New("the node does not answer on its Serve endpoint")
)

// Condition1 is the check of 3.6: this node is untagged with user U; the node
// is in tailscaled's status with the same user id, no tags, not shared in and
// not expired; and GET https://<its MagicDNS name>:<Serve port>/.well-known/fyr
// over ordinary WebPKI returns its install id and its peer port. Serve is set
// by root or the node's operator, so no other program on that node, and no
// phone app, can answer for it. It returns the id and the peer port.
func (e *Env) Condition1(ctx context.Context, self, node tailnet.StatusNode) (f Found, err error) {
	switch {
	case !self.Untagged() || self.UserID == 0:
		return Found{}, ErrTagged
	case !node.Untagged():
		return Found{}, ErrTagged
	case node.Shared:
		return Found{}, ErrShared
	case node.UserID != self.UserID:
		return Found{}, ErrNotSameUser
	case node.Expired:
		return Found{}, ErrExpired
	}
	u := "https://" + net.JoinHostPort(node.Name, strconv.Itoa(e.ServePort)) + "/.well-known/fyr"
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := e.Serve.Do(req)
	if err != nil {
		return Found{}, fmt.Errorf("%w: %v", ErrNoServe, err)
	}
	defer resp.Body.Close()
	var w struct {
		peers.WellKnown
		Peer int `json:"peer"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&w) != nil || w.Fyr != 1 || w.ID == "" {
		return Found{}, ErrNoServe
	}
	if w.Peer <= 0 || w.Peer > 65535 {
		return Found{}, errors.New("the node has no peer port")
	}
	return Found{ID: w.ID, Port: w.Peer, Name: w.Name}, nil
}

// Handler is the peer port's HTTP. Open to any client with a key: who this
// install is, and the pairing of the same tailnet user. Everything else needs a
// key that is a live member of the roster; that is `members`, the host
// protocol with the principal read from Fyr-Actor.
func (e *Env) Handler(members http.Handler) http.Handler {
	e.init()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/fyr", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Answer{Fyr: 1, ID: e.Key.ID()})
	})
	mux.HandleFunc("POST /pair/tailnet", e.pairTailnet)
	mux.HandleFunc("POST /pair/invite", e.joinInvite)
	mux.HandleFunc("GET /roster", e.member(func(w http.ResponseWriter, r *http.Request, _ roster.Member) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(e.Roster.Statements())
	}))
	mux.HandleFunc("POST /roster", e.member(func(w http.ResponseWriter, r *http.Request, _ roster.Member) {
		var list []roster.Statement
		if json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&list) != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		e.Roster.Merge(list)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(e.Roster.Statements())
	}))
	mux.Handle("/", members)
	return mux
}

func (e *Env) member(f func(http.ResponseWriter, *http.Request, roster.Member)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := e.Caller(r)
		if !ok {
			http.Error(w, "this install is not in the roster", http.StatusForbidden)
			return
		}
		f(w, r, m)
	}
}

// Caller is the roster member whose key the connection proved. A key that is not
// a member yet may be a member's new key: its certificate then carries the
// rotations, signed by the key the roster knows, and they are merged first.
func (e *Env) Caller(r *http.Request) (roster.Member, bool) {
	pub, ok := peerlink.Caller(r)
	if !ok {
		return roster.Member{}, false
	}
	return e.MemberByKey(pub, peerlink.CallerRotation(r))
}

// MemberByKey is the member whose key this is. A key that is not a member's may be a
// member's new key: the chain its certificate carried is merged first, but only
// when it leads from a key the roster holds to this key.
func (e *Env) MemberByKey(pub ed25519.PublicKey, chain []roster.Statement) (roster.Member, bool) {
	if m, in := e.Roster.KeyMember(pub); in {
		return m, true
	}
	if len(chain) == 0 {
		return roster.Member{}, false
	}
	old, ok := e.Roster.Member(chain[0].Subject)
	if !ok {
		return roster.Member{}, false
	}
	if end, ok := roster.ChainFrom(chain, old.Key); !ok || !end.Equal(pub) {
		return roster.Member{}, false
	}
	e.Roster.Merge(chain)
	return e.Roster.KeyMember(pub)
}

// DialOptions are what a dial shows and accepts about rotated keys: this
// install's own rotations in its certificate, and a peer's chain accepted when it
// leads from the key the roster holds for the id that was meant.
func (e *Env) DialOptions() peerlink.Options {
	return peerlink.Options{
		Rotation: e.Roster.RotationChain(),
		OldKey: func(id string) ed25519.PublicKey {
			if m, ok := e.Roster.Member(id); ok {
				return m.Key
			}
			return nil
		},
		OnRotation: func(chain []roster.Statement) { e.Roster.Merge(chain) },
	}
}

// Remove ends an install for ever (decision 0030 3.6): the roster says so, every
// open request from it is closed, and the next sync tells the other members.
func (e *Env) Remove(id string) error {
	if err := e.Roster.Remove(id); err != nil {
		return err
	}
	e.CloseRemoved()
	e.logf("peer: removed install %s", id)
	return nil
}

// CloseRemoved cancels the open requests of installs that are no longer members
// (a stream is one long request), within one change of the roster.
func (e *Env) CloseRemoved() {
	e.trackMu.Lock()
	var cancel []context.CancelFunc
	for id, m := range e.tracked {
		if _, in := e.Roster.Member(id); !in {
			for _, c := range m {
				cancel = append(cancel, c)
			}
			delete(e.tracked, id)
		}
	}
	e.trackMu.Unlock()
	for _, c := range cancel {
		c()
	}
}

type pairReq struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}
type pairResp struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// pairTailnet is the other side of condition 1: the caller says which node it
// is; this install runs the same check on that node, and its Serve endpoint
// must name the key the connection just proved.
func (e *Env) pairTailnet(w http.ResponseWriter, r *http.Request) {
	pub, ok := peerlink.Caller(r)
	if !ok || e.TS == nil || e.Roster.SelfRemoved() {
		http.Error(w, "no", http.StatusForbidden)
		return
	}
	var req pairReq
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req) != nil || req.Name == "" || req.Port <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if _, in := e.Roster.KeyMember(pub); in { // already paired: say where to find this install
		json.NewEncoder(w).Encode(e.self())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	self, nodes, err := e.TS.Status(ctx)
	if err != nil {
		http.Error(w, "tailscaled does not answer", http.StatusServiceUnavailable)
		return
	}
	var node *tailnet.StatusNode
	for i := range nodes {
		if strings.EqualFold(nodes[i].Name, req.Name) {
			node = &nodes[i]
		}
	}
	if node == nil {
		http.Error(w, "that node is not on this tailnet", http.StatusForbidden)
		return
	}
	found, err := e.Condition1(ctx, self, *node)
	id := found.ID
	if err != nil {
		e.logf("peer: %s asked to pair and failed condition 1: %v", req.Name, err)
		http.Error(w, "not paired: "+err.Error(), http.StatusForbidden)
		return
	}
	if id != identity.IDOf(pub) { // the key on this connection is not the install at that node's Serve
		e.logf("peer: %s asked to pair with a key that is not the one its Serve names", req.Name)
		http.Error(w, "not paired: the key is not the one that node's Serve names", http.StatusForbidden)
		return
	}
	e.Roster.Add(pub, nameOr(found.Name, node.Name), node.Name, req.Port)
	e.logf("peer: paired with %s (%s) by the same tailnet user", node.Name, id)
	json.NewEncoder(w).Encode(e.self())
}

func (e *Env) self() pairResp {
	p := 0
	if e.Port != nil {
		p = e.Port()
	}
	return pairResp{Name: e.Name, Port: p}
}

// addr is where a member's peer port is reached.
func addr(m roster.Member) string {
	h := m.Host
	if h == "" {
		h = m.Name
	}
	return net.JoinHostPort(h, strconv.Itoa(m.Port))
}

// Client is an HTTP client to a member over the link, pinned to its id.
func (e *Env) Client(m roster.Member) *http.Client { return e.Dial.Client(m.ID, addr(m)) }

// PairRound runs condition 1 against every node of the same tailnet user that
// is not in the roster yet. Nodes that fail it are left alone and tried again
// next round (a Serve that is not up yet).
func (e *Env) PairRound(ctx context.Context) {
	defer e.Rounds.Add(1)
	if e.Roster.SelfRemoved() {
		return // a member removed this install: it pairs with no one until the owner pairs it again
	}
	if e.TS == nil || e.Port == nil || e.Port() == 0 {
		return
	}
	self, nodes, err := e.TS.Status(ctx)
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, m := range e.Roster.Members() {
		known[strings.ToLower(m.Host)] = true
		known[strings.ToLower(m.Name)] = true
	}
	var wg sync.WaitGroup
	for _, n := range nodes {
		if known[strings.ToLower(n.Name)] || !n.Online || !n.Untagged() || n.Shared || n.UserID != self.UserID {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.pairWith(ctx, self, n); err != nil {
				e.logf("peer: %s: %v", n.Name, err)
			}
		}()
	}
	wg.Wait()
}

func (e *Env) pairWith(ctx context.Context, self, node tailnet.StatusNode) error {
	found, err := e.Condition1(ctx, self, node)
	if err != nil {
		return err
	}
	id, port := found.ID, found.Port
	if id == e.Key.ID() {
		return nil // this node
	}
	m := roster.Member{ID: id, Name: nameOr(found.Name, node.Name), Host: node.Name, Port: port}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := e.Dial.DialTLS(ctx, id, addr(m)) // the handshake proves the key hashes to id
	if err != nil {
		return err
	}
	pub, err := peerlink.ConnKey(conn)
	if err != nil {
		conn.Close()
		return err
	}
	body, _ := json.Marshal(pairReq{Name: self.Name, Port: e.Port()})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://"+addr(m)+"/pair/tailnet", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: peerlink.Once(conn)}).Do(req)
	if err != nil {
		conn.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("it did not pair: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	e.Roster.Add(pub, m.Name, node.Name, port)
	e.logf("peer: paired with %s (%s) by the same tailnet user", node.Name, id)
	return nil
}

// Sync exchanges statements with every member that has a peer port: ours to
// them, theirs to us. A member that is not reachable is left for the next round.
func (e *Env) Sync(ctx context.Context) {
	var wg sync.WaitGroup
	for _, m := range e.Roster.Members() {
		if m.ID == e.Key.ID() || (m.Port == 0 && (e.Mesh == nil || !e.Mesh.Up(m.ID))) {
			continue // nothing to dial and no link up: it may open one itself
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.syncWith(ctx, m); err != nil {
				e.logf("peer: sync with %s: %v", nameOf(m), err)
			}
		}()
	}
	wg.Wait()
}

func nameOf(m roster.Member) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

func (e *Env) syncWith(ctx context.Context, m roster.Member) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, _ := json.Marshal(e.Roster.Statements())
	var rt http.RoundTripper
	url := "https://" + addr(m) + "/roster"
	if e.Mesh != nil && e.Mesh.Up(m.ID) { // over the link, whichever side opened it
		rt, url = e.Mesh.Transport(m.ID, 0), "http://peer.link/roster"
	} else {
		tr := e.Dial.Transport(m.ID, addr(m))
		defer tr.CloseIdleConnections()
		rt = tr
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: rt}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	var theirs []roster.Statement
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&theirs); err != nil {
		return err
	}
	e.Roster.Merge(theirs)
	return nil
}

// Run pairs and syncs: at once, on every roster change, and every interval,
// until ctx ends.
func (e *Env) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 60 * time.Second
	}
	e.init()
	kick := e.kickc
	e.Roster.OnChange(e.Kick)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		e.PairRound(ctx)
		e.Sync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-kick:
			time.Sleep(50 * time.Millisecond) // let a burst of changes settle
		}
	}
}

// Drive is the host's own switch (3.7): which installs may drive it. Every
// member reads; a member drives unless the owner switched it off here. It is
// local to the host and never part of the roster, so no member can grant itself.
type Drive struct {
	mu   sync.Mutex
	path string
	off  map[string]bool
}

// OpenDrive reads the switch in the data folder.
func OpenDrive(dir string) *Drive {
	d := &Drive{path: dir + "/drive.json", off: map[string]bool{}}
	if b, err := readFile(d.path); err == nil {
		var ids []string
		if json.Unmarshal(b, &ids) == nil {
			for _, id := range ids {
				d.off[id] = true
			}
		}
	}
	return d
}

// MayDrive: the install may give this host commands.
func (d *Drive) MayDrive(id string) bool {
	if d == nil {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.off[id]
}

// Set switches an install on or off.
func (d *Drive) Set(id string, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		delete(d.off, id)
	} else {
		d.off[id] = true
	}
	var ids []string
	for k := range d.off {
		ids = append(ids, k)
	}
	b, _ := json.Marshal(ids)
	writeFile(d.path, b)
}

// Off lists the installs switched off.
func (d *Drive) Off() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for k := range d.off {
		ids = append(ids, k)
	}
	return ids
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }
func writeFile(p string, b []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Member, DeviceRevoked and MayDrive make an Env the gate of the host's peer
// handler (server.PeerGate).
func (e *Env) Member(r *http.Request) (string, bool) {
	m, ok := e.Caller(r)
	return m.ID, ok
}
func (e *Env) DeviceRevoked(id string) bool { return e.Roster.DeviceRevoked(id) }

// DeviceKnown: a member signed the device in.
func (e *Env) DeviceKnown(id string) bool {
	for _, d := range e.Roster.Devices() {
		if d.Subject == id {
			return true
		}
	}
	return false
}

// Enforcing: the peer way in is in enforce mode.
func (e *Env) Enforcing() bool { return e.Enforce != nil && e.Enforce() }

// Served counts a request over the link.
func (e *Env) Served() {
	if e.OnServed != nil {
		e.OnServed()
	}
}

// Track registers an open request of an install so that removing it closes it.
func (e *Env) Track(install string, cancel context.CancelFunc) func() {
	e.trackMu.Lock()
	defer e.trackMu.Unlock()
	if e.tracked == nil {
		e.tracked = map[string]map[int]context.CancelFunc{}
	}
	if e.tracked[install] == nil {
		e.tracked[install] = map[int]context.CancelFunc{}
	}
	e.trackN++
	n := e.trackN
	e.tracked[install][n] = cancel
	return func() {
		e.trackMu.Lock()
		delete(e.tracked[install], n)
		e.trackMu.Unlock()
	}
}
func (e *Env) MayDrive(install string) bool { return e.Drive.MayDrive(install) }

func nameOr(n, d string) string {
	if n != "" {
		return n
	}
	return d
}
