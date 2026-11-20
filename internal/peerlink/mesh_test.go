package peerlink

// SPEC V0.174 (decision 0030 Amendment 1): the mesh. Either side opens the link;
// one link per pair; removal and key replacement close it; the reason a link is down
// is what fyrd can see.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/roster"
)

type node struct {
	key     *identity.Key
	mesh    *Mesh
	addr    string
	mu      sync.Mutex
	members map[string]Peer // who this node's roster holds (others)
	blocked func(addr string) string
}

func (n *node) setMembers(ps ...Peer) {
	n.mu.Lock()
	n.members = map[string]Peer{}
	for _, p := range ps {
		n.members[p.ID] = p
	}
	n.mu.Unlock()
	n.mesh.Changed()
}

func (n *node) peerOf(m *node, known bool) Peer {
	p := Peer{ID: m.key.ID(), Key: m.key.Public, Name: "n"}
	if known {
		p.Host, p.Port = hostPort(m.addr)
	}
	return p
}

func hostPort(a string) (string, int) {
	h, p, _ := net.SplitHostPort(a)
	var port int
	for _, c := range p {
		port = port*10 + int(c-'0')
	}
	return h, port
}

func newNode(t *testing.T) *node {
	t.Helper()
	n := &node{key: key(t), members: map[string]Peer{}}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	n.addr = ln.Addr().String()
	pl, err := Listen(ln, n.key)
	if err != nil {
		t.Fatal(err)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pub, _ := Caller(r)
		if r.URL.Path == "/stream" {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.Write([]byte(n.key.ID() + " served " + identity.IDOf(pub)))
	})
	d := &Dialer{Key: n.key, Resolve: func(a string) string {
		if n.blocked != nil {
			if r := n.blocked(a); r != "" {
				return r
			}
		}
		return ""
	}}
	n.mesh = New(Config{Key: n.key, Dialer: d, Handler: h, MinBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond, DialTimeout: 400 * time.Millisecond,
		Members: func() []Peer {
			n.mu.Lock()
			defer n.mu.Unlock()
			var out []Peer
			for _, p := range n.members {
				out = append(out, p)
			}
			return out
		},
		Member: func(k ed25519.PublicKey, _ []roster.Statement) (Peer, bool) {
			n.mu.Lock()
			defer n.mu.Unlock()
			p, ok := n.members[identity.IDOf(k)]
			return p, ok
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	go n.mesh.Run(ctx)
	go func() {
		for {
			select {
			case lc := <-Links(pl):
				n.mesh.Accept(lc)
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { cancel(); pl.Close() })
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func call(t *testing.T, from, to *node, path string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://x"+path, nil)
	resp, err := from.mesh.Transport(to.key.ID(), 0).RoundTrip(req)
	if err != nil {
		t.Fatalf("%s -> %s: %v", from.key.ID(), to.key.ID(), err)
	}
	defer resp.Body.Close()
	b := make([]byte, 128)
	n, _ := resp.Body.Read(b)
	return string(b[:n])
}

// A cannot be reached by B (B has no address for A), but A can reach B: the one
// link A opens carries B's requests to A as well.
func TestEitherSideOpensTheLink(t *testing.T) {
	a, b := newNode(t), newNode(t)
	a.setMembers(a.peerOf(b, true))  // a knows where b is
	b.setMembers(b.peerOf(a, false)) // b knows a only by key
	waitFor(t, "both links up", func() bool { return a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID()) })
	if got := call(t, a, b, "/x"); got != b.key.ID()+" served "+a.key.ID() {
		t.Errorf("a -> b: %q", got)
	}
	if got := call(t, b, a, "/x"); got != a.key.ID()+" served "+b.key.ID() {
		t.Errorf("b -> a: %q", got)
	}
	if s := a.mesh.Status(b.key.ID()); s.State != "up" || s.Opened != "here" {
		t.Errorf("a's view: %+v", s)
	}
	if s := b.mesh.Status(a.key.ID()); s.State != "up" || s.Opened != "them" {
		t.Errorf("b's view: %+v", s)
	}
}

// Both can dial and both do, at once: one link stays, both sides use the same one,
// and a request still works. Which one stays depends on timing: a dial that is
// refused or slow leaves the other side's link alone, and a connector does not
// dial while a link is up. When both come up, the smaller id's wins
// (TestTheSmallerDialerWins).
func TestSimultaneousDialsLeaveOneLink(t *testing.T) {
	for i := 0; i < 5; i++ {
		a, b := newNode(t), newNode(t)
		a.setMembers(a.peerOf(b, true))
		b.setMembers(b.peerOf(a, true))
		waitFor(t, "one link that both sides use", func() bool {
			la, lb := a.mesh.linkFor(b.key.ID()), b.mesh.linkFor(a.key.ID())
			return a.mesh.Status(b.key.ID()).State == "up" && b.mesh.Status(a.key.ID()).State == "up" &&
				la != nil && lb != nil && la.Dialer == lb.Dialer && !la.Draining() && !lb.Draining()
		})
		if got := call(t, a, b, "/x"); !strings.Contains(got, "served") {
			t.Errorf("a -> b: %q", got)
		}
		if got := call(t, b, a, "/x"); !strings.Contains(got, "served") {
			t.Errorf("b -> a: %q", got)
		}
	}
}

// Two links up for one peer: the one opened by the install with the smaller id
// stays, in either order, and the other drains and closes.
func TestTheSmallerDialerWins(t *testing.T) {
	self, peer := key(t), key(t)
	fake := func(dialer string) *Link {
		c, _ := net.Pipe()
		return &Link{PeerID: peer.ID(), PeerKey: peer.Public, Dialer: dialer, Since: time.Now(), conn: c,
			done: make(chan struct{}), ready: make(chan struct{}), cancel: func() {}}
	}
	for _, smallFirst := range []bool{true, false} {
		m := New(Config{Key: self, Members: func() []Peer { return []Peer{{ID: peer.ID(), Key: peer.Public}} }})
		small, big := fake("a"), fake("b")
		if smallFirst {
			m.register(small)
			m.register(big)
		} else {
			m.register(big)
			m.register(small)
		}
		if m.linkFor(peer.ID()) != small || small.Draining() || !big.Draining() {
			t.Fatalf("small first %v: the smaller dialer's link does not win", smallFirst)
		}
		select {
		case <-big.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("small first %v: the losing link was not closed", smallFirst)
		}
	}
}

// A flap from either end: one link again, the connector redials.
func TestAFlapLeavesOneLink(t *testing.T) {
	a, b := newNode(t), newNode(t)
	a.setMembers(a.peerOf(b, true))
	b.setMembers(b.peerOf(a, true))
	waitFor(t, "up", func() bool { return a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID()) })
	first := a.mesh.linkFor(b.key.ID())
	first.Close() // the link drops
	waitFor(t, "a new link", func() bool {
		l := a.mesh.linkFor(b.key.ID())
		return l != nil && l != first && a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID())
	})
	if got := call(t, a, b, "/x"); !strings.Contains(got, "served") {
		t.Errorf("after the flap: %q", got)
	}
}

// A removed member's link closes at once, an open stream with it, in both directions.
func TestRemovalClosesTheLinkAndItsStreams(t *testing.T) {
	a, b := newNode(t), newNode(t)
	a.setMembers(a.peerOf(b, true))
	b.setMembers(b.peerOf(a, true))
	waitFor(t, "up", func() bool { return a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID()) })
	for _, dir := range []struct{ from, to *node }{{a, b}, {b, a}} {
		req, _ := http.NewRequest("GET", "http://x/stream", nil)
		resp, err := dir.from.mesh.Transport(dir.to.key.ID(), time.Second).RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			b := make([]byte, 1)
			resp.Body.Read(b)
			close(done)
		}()
		a.setMembers() // a removes b
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("an open stream survived the removal")
		}
		waitFor(t, "a to show it removed", func() bool {
			s := a.mesh.Status(b.key.ID())
			return s.State == "down" && s.Err != nil && s.Err.Kind == ErrRemoved
		})
		a.setMembers(a.peerOf(b, true)) // back for the other direction
		waitFor(t, "up again", func() bool { return a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID()) })
	}
}

// B does not hold A as a member yet: A is told so, not "rejected", and the link comes
// up once B does.
func TestNotAMemberThereYet(t *testing.T) {
	a, b := newNode(t), newNode(t)
	a.setMembers(a.peerOf(b, true))
	waitFor(t, "a to learn why", func() bool {
		s := a.mesh.Status(b.key.ID())
		return s.State == "down" && s.Err != nil && s.Err.Kind == ErrNotAMember
	})
	if a.mesh.Up(b.key.ID()) {
		t.Fatal("a link to a member that does not know us")
	}
	b.setMembers(b.peerOf(a, false))
	waitFor(t, "the link once b holds a", func() bool { return a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID()) })
}

// The fake ACL: a's dials to b go nowhere (a silent drop). It shows as a timeout, and
// b's dial to a still works, so the pair has a link.
func TestAnACLThatBlocksOneDirection(t *testing.T) {
	a, b := newNode(t), newNode(t)
	// a blackhole: accepts, never answers
	bh, _ := net.Listen("tcp", "127.0.0.1:0")
	defer bh.Close()
	go func() {
		for {
			c, err := bh.Accept()
			if err != nil {
				return
			}
			_ = c // held open, silent
		}
	}()
	a.blocked = func(addr string) string {
		if addr == b.addr {
			return bh.Addr().String()
		}
		return ""
	}
	a.setMembers(a.peerOf(b, true))
	b.setMembers(b.peerOf(a, true))
	waitFor(t, "a link from b's side", func() bool { return a.mesh.Up(b.key.ID()) && b.mesh.Up(a.key.ID()) })
	if s := a.mesh.Status(b.key.ID()); s.Opened != "them" {
		t.Errorf("a's view: %+v (the link is opened by b)", s)
	}
	if got := call(t, a, b, "/x"); !strings.Contains(got, "served") {
		t.Errorf("a -> b over b's link: %q", got)
	}
	// both blocked: nothing comes up, and the reason is a timeout
	c, d := newNode(t), newNode(t)
	block := func(n *node, other *node) {
		n.blocked = func(addr string) string {
			if addr == other.addr {
				return bh.Addr().String()
			}
			return ""
		}
	}
	block(c, d)
	block(d, c)
	c.setMembers(c.peerOf(d, true))
	d.setMembers(d.peerOf(c, true))
	waitFor(t, "a timeout reason on both", func() bool {
		sc, sd := c.mesh.Status(d.key.ID()), d.mesh.Status(c.key.ID())
		return sc.Err != nil && sc.Err.Kind == ErrTimeout && sd.Err != nil && sd.Err.Kind == ErrTimeout
	})
	if c.mesh.Up(d.key.ID()) || d.mesh.Up(c.key.ID()) {
		t.Error("a link came up through a blocked network")
	}
}

// A peer that speaks only fyr-peer/1 is shown as older, with no link.
func TestAnOlderPeerHasNoLink(t *testing.T) {
	a := newNode(t)
	k := key(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv, err := Serve(ln, k, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	// Serve offers /2 and closes it: from the dialer's side that is a link that never
	// comes up. An install that offers only /1 is the older one; make one.
	old := oldListener(t, k)
	h, p := hostPort(old)
	a.setMembers(Peer{ID: k.ID(), Key: k.Public, Name: "old", Host: h, Port: p})
	waitFor(t, "the older-version reason", func() bool {
		s := a.mesh.Status(k.ID())
		return s.Err != nil && s.Err.Kind == ErrOlderVersion
	})
}

// V0.peerlink-replace-drain: a late link from the smaller id replaces one both
// sides already use. Each side switches when it registers the new link, so for a
// while one side has moved and the other still sends on the old link. The old link
// closes only when both are done with it: a request the other side has open on it,
// or starts before it switches, is answered. The order is set by hand: who
// registers when, and a handler that holds a request open.
func TestAReplacedLinkDrainsBothWays(t *testing.T) {
	small, big := key(t), key(t)
	if big.ID() < small.ID() {
		small, big = big, small
	}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	handler := func(self *identity.Key) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/held" {
				entered <- struct{}{}
				<-release
			}
			w.Write([]byte(self.ID() + " served"))
		})
	}
	meshOf := func(self, peer *identity.Key) *Mesh {
		return New(Config{Key: self, Members: func() []Peer { return []Peer{{ID: peer.ID(), Key: peer.Public}} }})
	}
	ms, mb := meshOf(small, big), meshOf(big, small)
	send := func(m *Mesh, to *identity.Key, path string) (string, error) {
		req, _ := http.NewRequest("GET", "http://x"+path, nil)
		resp, err := m.Transport(to.ID(), 0).RoundTrip(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}

	// the first link, opened by the bigger id: both sides use it
	old1b, old1s := linkBetween(t, big, small, handler(big), handler(small))
	mb.register(old1b)
	ms.register(old1s)

	// big has a request open on it, held in small's handler
	held := make(chan error, 1)
	go func() {
		got, err := send(mb, small, "/held")
		if err == nil && got != small.ID()+" served" {
			err = errors.New("answer " + got)
		}
		held <- err
	}()
	<-entered

	// the smaller id's link comes up; small registers it first
	new2s, new2b := linkBetween(t, small, big, handler(small), handler(big))
	ms.register(new2s)
	if ms.linkFor(big.ID()) != new2s || !old1s.Draining() {
		t.Fatal("small did not move to the smaller id's link")
	}
	select {
	case <-old1s.Done():
		t.Fatal("small closed the replaced link while big still had a request open on it")
	case <-time.After(time.Second):
	}
	// big has not moved: a request it starts now still goes on the old link
	if got, err := send(mb, small, "/x"); err != nil || got != small.ID()+" served" {
		t.Fatalf("big's new request on the old link: %q %v", got, err)
	}
	// small's requests go on the new one
	if got, err := send(ms, big, "/x"); err != nil || got != big.ID()+" served" {
		t.Fatalf("small's request: %q %v", got, err)
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatalf("big's held request: %v", err)
	}

	// big moves too: both sides are done with the old link and it closes, well
	// before the 5 s limit
	mb.register(new2b)
	for _, l := range []*Link{old1s, old1b} {
		select {
		case <-l.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("the replaced link stayed open after both sides moved")
		}
	}
	if new2s.Draining() || new2b.Draining() {
		t.Fatal("the new link drains")
	}
	if got, err := send(mb, small, "/x"); err != nil || got != small.ID()+" served" {
		t.Fatalf("big -> small on the new link: %q %v", got, err)
	}
	if got, err := send(ms, big, "/x"); err != nil || got != big.ID()+" served" {
		t.Fatalf("small -> big on the new link: %q %v", got, err)
	}
}

// V0.peerlink-replace-drain: a peer whose hello did not say it announces (an older
// install) is not waited for; the replaced link closes once no request is open on
// it either way, the peer's held one too, and long before the 5 s limit (the
// tunnel is not a request).
func TestAReplacedLinkToAnOlderPeer(t *testing.T) {
	self, peer := key(t), key(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/held" {
			entered <- struct{}{}
			<-release
		}
		w.Write([]byte("ok"))
	})
	m := New(Config{Key: self, Members: func() []Peer { return []Peer{{ID: peer.ID(), Key: peer.Public}} }})
	old, theirs := linkBetween(t, self, peer, h, h) // opened here
	old.setPeerDrains(false)
	m.register(old)
	held := make(chan error, 1)
	go func() {
		resp, err := theirs.RoundTrip(mustReq("/held"))
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		held <- err
	}()
	<-entered
	newer, _ := linkBetween(t, self, peer, h, h)
	newer.setPeerDrains(false)
	m.register(newer) // the same dialer: the newer replaces the older
	if !old.Draining() {
		t.Fatal("the older link does not drain")
	}
	select {
	case <-old.Done():
		t.Fatal("closed while it served the peer's request")
	case <-time.After(500 * time.Millisecond):
	}
	start := time.Now()
	close(release)
	if err := <-held; err != nil {
		t.Fatalf("the held request: %v", err)
	}
	select {
	case <-old.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the replaced link stayed open with nothing on it")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("closed after %v", d)
	}
}
