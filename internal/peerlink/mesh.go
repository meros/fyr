package peerlink

// The mesh (decision 0030 Amendment 1): a connector for every member of the
// roster, so a link between two members comes up whichever side can open it. Both
// sides dial; when two connections are up the one opened by the install with the
// smaller id wins and the other drains. A member that cannot be reached (an ACL, a
// NAT) joins by dialling out. Membership decides who is linked; an address only
// decides who is dialled.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/roster"
)

// Peer is a member the mesh links to.
type Peer struct {
	ID   string
	Key  ed25519.PublicKey
	Name string
	Host string // where to dial it: a name or an address, "" when unknown
	Port int
}

func (p Peer) addr() string {
	h := p.Host
	if h == "" {
		h = p.Name
	}
	if h == "" || p.Port == 0 {
		return ""
	}
	return net.JoinHostPort(h, strconv.Itoa(p.Port))
}

// Why a link is not up: what fyrd can observe, not what the network did (a
// Tailscale ACL drops silently, so it shows as a timeout).
const (
	ErrTimeout      = "timeout"
	ErrRefused      = "refused"
	ErrKeyMismatch  = "key-mismatch"
	ErrNotAMember   = "not-a-member"
	ErrRemoved      = "removed"
	ErrOlderVersion = "older-version"
	ErrNoAddress    = "no-address"
	ErrNoRoute      = "no-route"
	ErrClosed       = "closed"
)

// LinkErr is the last reason a link is down.
type LinkErr struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func (e *LinkErr) Error() string { return e.Kind + ": " + e.Text }

// Status is what Settings → Hosts shows of a link.
type Status struct {
	// State: up, connecting or down.
	State string `json:"state"`
	// Opened: "here" when this install dialed it, "them" when the peer did.
	Opened string    `json:"opened,omitempty"`
	Since  time.Time `json:"since"`
	Err    *LinkErr  `json:"error,omitempty"`
}

// Config is what a mesh needs from its install.
type Config struct {
	Key    *identity.Key
	Dialer *Dialer
	// Members lists the members other than this install, with their addresses.
	Members func() []Peer
	// Member finds the member whose key this is, or false. rotation is the chain the
	// peer's certificate carried: a key that is a member's rotated one is accepted
	// when the chain leads from a key the roster holds (and is merged).
	Member func(key ed25519.PublicKey, rotation []roster.Statement) (Peer, bool)
	// Handler serves the requests a peer sends over a link.
	Handler http.Handler
	// SelfRemoved: this install was removed from the roster; it dials nobody.
	SelfRemoved func() bool
	Logf        func(string, ...any)
	// Candidates are the addresses a source other than the roster (mDNS on a LAN) gives
	// for a member's id. An address is a guess: the dial is pinned to the id, so the
	// install that answers there is that member or the dial fails.
	Candidates func(id string) []string
	// OnUp hears a link coming up (the roster syncs over it at once).
	OnUp func(peerID string)
	// MinBackoff and MaxBackoff bound the wait between dials to one member;
	// DialTimeout bounds one dial (8 s by default).
	MinBackoff, MaxBackoff, DialTimeout time.Duration
}

// Mesh keeps the links of one install.
type Mesh struct {
	cfg Config

	mu      sync.Mutex
	good    map[string]string  // the address a peer was last reached at, by peer id
	links   map[string]*Link   // the link in use, by peer id
	losers  map[*Link]struct{} // links that lost to another and drain (register)
	status  map[string]*Status // by peer id
	conns   map[string]context.CancelFunc
	waiters map[string][]chan struct{}
	kick    chan struct{}
	ctx     context.Context
}

// New makes a mesh; Run starts its connectors.
func New(cfg Config) *Mesh {
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 5 * time.Minute
	}
	return &Mesh{cfg: cfg, links: map[string]*Link{}, losers: map[*Link]struct{}{}, status: map[string]*Status{}, conns: map[string]context.CancelFunc{},
		waiters: map[string][]chan struct{}{}, kick: make(chan struct{}, 1)}
}

func (m *Mesh) logf(f string, a ...any) {
	if m.cfg.Logf != nil {
		m.cfg.Logf(f, a...)
	}
}

// Run starts a connector per member and keeps them in step with the roster until
// ctx ends.
func (m *Mesh) Run(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	m.Reconcile()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			for _, l := range m.links {
				l.Close()
			}
			m.mu.Unlock()
			return
		case <-t.C:
			m.Reconcile()
		case <-m.kick:
			m.Reconcile()
		}
	}
}

// Reconcile is called when the roster changed: it ends the links of installs that
// are no longer members or whose key was replaced, and starts a connector for each
// new member.
func (m *Mesh) Reconcile() {
	m.mu.Lock()
	ctx := m.ctx
	if ctx == nil {
		m.mu.Unlock()
		return
	}
	removedSelf := m.cfg.SelfRemoved != nil && m.cfg.SelfRemoved()
	members := map[string]Peer{}
	if !removedSelf {
		for _, p := range m.cfg.Members() {
			members[p.ID] = p
		}
	}
	var closing []*Link
	for id, l := range m.links {
		if p, ok := members[id]; !ok || !p.Key.Equal(l.PeerKey) {
			closing = append(closing, l)
			delete(m.links, id)
			kind, text := ErrRemoved, "this install is no longer in the roster"
			if ok {
				kind, text = ErrKeyMismatch, "the install's key was replaced"
			}
			m.status[id] = &Status{State: "down", Since: time.Now(), Err: &LinkErr{Kind: kind, Text: text}}
		}
	}
	// A link that lost and still drains its requests goes too: a removal closes every
	// stream with the peer at once, not after the drain.
	for l := range m.losers {
		if p, ok := members[l.PeerID]; !ok || !p.Key.Equal(l.PeerKey) {
			closing = append(closing, l)
			delete(m.losers, l)
		}
	}
	for id, cancel := range m.conns {
		if _, ok := members[id]; !ok {
			cancel()
			delete(m.conns, id)
			if _, has := m.status[id]; has && m.status[id].State != "down" {
				m.status[id] = &Status{State: "down", Since: time.Now(), Err: &LinkErr{Kind: ErrRemoved, Text: "this install is no longer in the roster"}}
			}
		}
	}
	for id, p := range members {
		if _, ok := m.conns[id]; !ok {
			cctx, cancel := context.WithCancel(ctx)
			m.conns[id] = cancel
			go m.connector(cctx, id, p.Key)
		}
	}
	m.mu.Unlock()
	for _, l := range closing {
		l.Close()
	}
}

// Changed asks the mesh to reconcile soon (the roster changed).
func (m *Mesh) Changed() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// peer is the current roster entry of a member.
func (m *Mesh) peer(id string) (Peer, bool) {
	for _, p := range m.cfg.Members() {
		if p.ID == id {
			return p, true
		}
	}
	return Peer{}, false
}

func (m *Mesh) setStatus(id string, s Status) {
	m.mu.Lock()
	m.status[id] = &s
	m.mu.Unlock()
}

// connector keeps trying to open a link to one member while none is up.
func (m *Mesh) connector(ctx context.Context, id string, key ed25519.PublicKey) {
	backoff := m.cfg.MinBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		cur := m.links[id]
		m.mu.Unlock()
		if cur != nil && !cur.Draining() {
			select { // up: wait for it to end (or this connector to be cancelled)
			case <-cur.Done():
			case <-ctx.Done():
				return
			}
			backoff = m.cfg.MinBackoff
			continue
		}
		p, ok := m.peer(id)
		if !ok {
			return
		}
		addrs := m.addrs(p)
		if len(addrs) == 0 {
			m.setIfNotUp(ctx, id, Status{State: "down", Since: time.Now(), Err: &LinkErr{Kind: ErrNoAddress, Text: "no address for it yet; it may open the link itself"}})
			if !m.sleep(ctx, 5*time.Second) {
				return
			}
			continue
		}
		m.setIfNotUp(ctx, id, Status{State: "connecting", Since: time.Now()})
		l, err := m.dialAny(ctx, p, addrs)
		if err != nil {
			var le *LinkErr
			if !errors.As(err, &le) {
				le = classify(err)
			}
			m.setIfNotUp(ctx, id, Status{State: "down", Since: time.Now(), Err: le})
			wait := jitter(backoff)
			if le.Kind == ErrOlderVersion {
				wait = time.Minute
			}
			backoff = min(backoff*2, m.cfg.MaxBackoff)
			if !m.sleep(ctx, wait) {
				return
			}
			continue
		}
		backoff = m.cfg.MinBackoff
		m.register(l)
	}
}

// addrs is where to try a member: the address last found good, the roster's, then what
// a source says for its id.
func (m *Mesh) addrs(p Peer) []string {
	var out []string
	add := func(a string) {
		if a != "" && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	m.mu.Lock()
	add(m.good[p.ID])
	m.mu.Unlock()
	add(p.addr())
	if m.cfg.Candidates != nil {
		for _, a := range m.cfg.Candidates(p.ID) {
			add(a)
		}
	}
	return out
}

// dialAny tries each address in turn; the first error is the one reported (the
// roster's address is the one the owner knows).
func (m *Mesh) dialAny(ctx context.Context, p Peer, addrs []string) (*Link, error) {
	var first error
	for _, a := range addrs {
		l, err := m.dial(ctx, p, a)
		if err == nil {
			m.mu.Lock()
			if m.good == nil {
				m.good = map[string]string{}
			}
			if a != p.addr() && m.good[p.ID] != a {
				m.logf("peer: %s found at %s", p.Name, a)
			}
			m.good[p.ID] = a
			m.mu.Unlock()
			return l, nil
		}
		if first == nil {
			first = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, first
}

// setIfNotUp is a connector's status. A connector that was cancelled writes none:
// Reconcile cancels it under m.mu and writes why (removed), and a dial that ends
// in that cancel must not put "context canceled" over it.
func (m *Mesh) setIfNotUp(ctx context.Context, id string, s Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if l := m.links[id]; l != nil && !l.Draining() {
		return // a link from the other side came up meanwhile
	}
	m.status[id] = &s
}

func (m *Mesh) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int63n(int64(d)))
}

// classify names why a dial failed, from what fyrd can see.
func classify(err error) *LinkErr {
	switch {
	case errors.Is(err, ErrWrongInstall):
		return &LinkErr{Kind: ErrKeyMismatch, Text: "the install that answered is not the one in the roster"}
	case errors.Is(err, syscall.ECONNREFUSED):
		return &LinkErr{Kind: ErrRefused, Text: "its peer port refused the connection"}
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return &LinkErr{Kind: ErrTimeout, Text: "no answer in time (a firewall or an ACL drops silently)"}
	}
	return &LinkErr{Kind: ErrNoRoute, Text: err.Error()}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// dial opens a link to a member: TLS pinned to its id, the hello, the tunnel.
func (m *Mesh) dial(ctx context.Context, p Peer, addr string) (*Link, error) {
	to := m.cfg.DialTimeout
	if to <= 0 {
		to = 8 * time.Second
	}
	dctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	conn, proto, err := m.cfg.Dialer.DialLink(dctx, p.ID, addr)
	if err != nil {
		return nil, err
	}
	if proto != ALPN2 {
		conn.Close()
		return nil, &LinkErr{Kind: ErrOlderVersion, Text: "it speaks only fyr-peer/1: followed one way until it is updated"}
	}
	if err := writeHello(conn, hello{V: 2}); err != nil {
		conn.Close()
		return nil, err
	}
	h, err := readHello(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !h.OK {
		conn.Close()
		kind := h.Reason
		if kind == "" {
			kind = ErrNoRoute
		}
		return nil, &LinkErr{Kind: kind, Text: refusedText(kind)}
	}
	key, err := ConnKey(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	l, err := NewDialerLink(conn, m.cfg.Key.ID(), p.ID, key, m.cfg.Handler)
	if err != nil {
		return nil, err
	}
	select {
	case <-l.Ready():
		return l, nil
	case <-l.Done():
		return nil, errors.New("the link closed before it was ready")
	case <-dctx.Done():
		l.Close()
		return nil, dctx.Err()
	}
}

func refusedText(kind string) string {
	if kind == ErrNotAMember {
		return "it does not have this install as a member yet (the roster has not reached it)"
	}
	return "the peer refused the link: " + kind
}

// Accept takes a connection a peer opened to this install's peer port and
// negotiated fyr-peer/2. Only a member gets a link; anyone else is told why and
// closed.
func (m *Mesh) Accept(lc *LinkConn) {
	go func() {
		c := lc.Conn
		h, err := readHello(c)
		if err != nil || h.V != 2 {
			c.Close()
			return
		}
		p, ok := Peer{}, false
		if m.cfg.Member != nil {
			p, ok = m.cfg.Member(lc.Key, lc.Rotation)
		}
		if !ok || (m.cfg.SelfRemoved != nil && m.cfg.SelfRemoved()) {
			writeHello(c, hello{V: 2, Reason: ErrNotAMember})
			c.Close()
			return
		}
		if err := writeHello(c, hello{V: 2, OK: true}); err != nil {
			c.Close()
			return
		}
		l := NewAcceptorLink(c, m.cfg.Key.ID(), p.ID, lc.Key, m.cfg.Handler)
		select {
		case <-l.Ready():
			m.register(l)
		case <-l.Done():
		case <-time.After(10 * time.Second):
			l.Close()
		}
	}()
}

// register puts a link that is up into service. A link is registered first and
// membership re-checked after, so one that comes up while a removal merges is
// closed too. When two links are up for one peer, the one opened by the install
// with the smaller id wins (a newer one from the same dialer replaces an older);
// the loser takes no new requests and is closed when its own have drained, or
// after 5 s.
func (m *Mesh) register(l *Link) {
	m.mu.Lock()
	old := m.links[l.PeerID]
	keep := l
	if old != nil && !old.Draining() {
		switch {
		case old.Dialer == l.Dialer:
			keep = l // a newer link from the same dialer replaces the older
		case l.Dialer < old.Dialer:
			keep = l
		default:
			keep = old
		}
	}
	loser := l
	if keep == l {
		loser = old
	}
	if loser != nil {
		loser.mu.Lock()
		loser.drain = true
		loser.mu.Unlock()
		m.losers[loser] = struct{}{}
	}
	m.links[l.PeerID] = keep
	if keep == l {
		m.status[l.PeerID] = &Status{State: "up", Opened: openedBy(l, m.cfg.Key.ID()), Since: l.Since}
	}
	ws := m.waiters[l.PeerID]
	delete(m.waiters, l.PeerID)
	m.mu.Unlock()
	for _, w := range ws {
		close(w)
	}
	if loser != nil {
		go func() {
			drainAndClose(loser)
			m.mu.Lock()
			delete(m.losers, loser)
			m.mu.Unlock()
		}()
	}
	if keep == l && m.cfg.OnUp != nil {
		go m.cfg.OnUp(l.PeerID)
	}
	// the membership check after: a removal that merged while the link came up
	if p, ok := m.peer(l.PeerID); !ok || !p.Key.Equal(l.PeerKey) {
		m.Reconcile()
		l.Close()
		return
	}
	go func() {
		<-l.Done()
		m.mu.Lock()
		if m.links[l.PeerID] == l {
			delete(m.links, l.PeerID)
			if m.status[l.PeerID] == nil || m.status[l.PeerID].State == "up" {
				m.status[l.PeerID] = &Status{State: "down", Since: time.Now(), Err: &LinkErr{Kind: ErrClosed, Text: "the link closed"}}
			}
		}
		m.mu.Unlock()
		m.Changed()
	}()
}

func openedBy(l *Link, self string) string {
	if l.Dialer == self {
		return "here"
	}
	return "them"
}

func drainAndClose(l *Link) {
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) && l.InFlight() > 0 {
		time.Sleep(50 * time.Millisecond)
	}
	l.Close()
}

// Up reports whether a link to the peer is up.
func (m *Mesh) Up(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.links[id]
	return l != nil && !l.Draining()
}

// Status is the link's state, "down" with no reason for a peer never seen.
func (m *Mesh) Status(id string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.status[id]; s != nil {
		return *s
	}
	return Status{State: "down"}
}

// Close ends the link to a peer now (a removal, a rotation).
func (m *Mesh) Close(id string) {
	m.mu.Lock()
	l := m.links[id]
	delete(m.links, id)
	m.mu.Unlock()
	if l != nil {
		l.Close()
	}
}

// Transport sends requests to the peer over its link. With wait > 0 a request
// waits that long for a link to come up (a link opened from the other side); with
// 0 it fails at once with the link's last reason, so a caller with another route
// (the whois link) can use it.
func (m *Mesh) Transport(id string, wait time.Duration) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		l, err := m.pick(r.Context(), id, wait)
		if err != nil {
			return nil, err
		}
		return l.RoundTrip(r)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// ErrNoLink wraps the reason a request could not be sent.
type ErrNoLink struct{ Err *LinkErr }

func (e *ErrNoLink) Error() string { return "no link to the host: " + e.Err.Error() }
func (e *ErrNoLink) Unwrap() error { return e.Err }

func (m *Mesh) pick(ctx context.Context, id string, wait time.Duration) (*Link, error) {
	m.mu.Lock()
	l := m.links[id]
	if l != nil && !l.Draining() {
		m.mu.Unlock()
		return l, nil
	}
	var w chan struct{}
	if wait > 0 {
		w = make(chan struct{})
		m.waiters[id] = append(m.waiters[id], w)
	}
	st := m.status[id]
	m.mu.Unlock()
	if w != nil {
		select {
		case <-w:
			m.mu.Lock()
			l = m.links[id]
			m.mu.Unlock()
			if l != nil && !l.Draining() {
				return l, nil
			}
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	le := &LinkErr{Kind: ErrNoRoute, Text: "no link yet"}
	if st != nil && st.Err != nil {
		le = st.Err
	}
	return nil, &ErrNoLink{Err: le}
}

// linkFor is the link in use for a peer, nil with none (tests).
func (m *Mesh) linkFor(id string) *Link {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.links[id]
}
