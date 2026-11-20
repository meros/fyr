package authtest_test

// Decision 0030 Amendment 1 (slice 5b, V0.174): one two-way link per pair of
// installs, opened by either side. A fake ACL blocks one direction (or both) of the
// peer port; trust is by key and does not care who dialed.

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/authtest"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
)

func waitLink(t *testing.T, n *authtest.MeshNode, cookie, host string, cond func(authtest.LinkOf) bool) authtest.LinkOf {
	t.Helper()
	end := time.Now().Add(30 * time.Second)
	var last authtest.LinkOf
	for time.Now().Before(end) {
		if l, ok := authtest.HostLink(n.Fyrd, cookie, host); ok {
			last = l
			if cond(l) {
				return l
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s's front never showed %s's link as wanted; last %+v\n%s", n.Name, host, last, n.Fyrd.Log())
	return last
}

// The owner's problem: the tailnet ACL lets a reach b and not b reach a. Their fronts
// both still see the other, over the one link the side that can dial opened, and each
// front shows the direction it was opened from.
func TestAnACLThatBlocksOneDirectionStillGivesBothFrontsTheirHost(t *testing.T) {
	m := mesh(t,
		authtest.MeshSpec{Name: "a", UserID: 1},
		authtest.MeshSpec{Name: "b", UserID: 1, Block: []string{"a"}}, // b cannot dial a
	)
	a, b := m.Node("a"), m.Node("b")
	waitOthers(t, a, "b")
	waitOthers(t, b, "a")
	ca, cb := ownerFronts(t, a), ownerFronts(t, b)
	waitHost(t, a, ca, "b")
	waitHost(t, b, cb, "a")
	la := waitLink(t, a, ca, "b", func(l authtest.LinkOf) bool { return l.State == "up" && l.Via == "peer" })
	lb := waitLink(t, b, cb, "a", func(l authtest.LinkOf) bool { return l.State == "up" && l.Via == "peer" })
	if la.Opened != "here" || lb.Opened != "them" {
		t.Errorf("who opened it: a says %q, b says %q (a dials, b cannot)", la.Opened, lb.Opened)
	}
	// each front reaches the other's host, over the same link
	if r, err := a.Fyrd.Get(a.Fyrd.HostPath(ca, "b", "/commands"), ca); err != nil || r.Status == 502 || r.Status == 503 {
		t.Errorf("a's front to b: %v %v", err, r)
	}
	if r, err := b.Fyrd.Get(b.Fyrd.HostPath(cb, "a", "/commands"), cb); err != nil || r.Status == 502 || r.Status == 503 {
		t.Errorf("b's front to a: %v %v", err, r)
	}
	if r, _ := a.Fyrd.Roster(); r.WhoisLinkUses != 0 {
		t.Errorf("the whois link was used %d times", r.WhoisLinkUses)
	}
}

// Both directions blocked while no link is up: each shows the other down with the
// reason a silent drop leaves, a timeout; a flap of the ACL gives one link again.
func TestBothDirectionsBlockedShowsTheReasonAndAFlapGivesOneLink(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1})
	a, b := m.Node("a"), m.Node("b")
	allPaired(t, a, b)
	ca, cb := ownerFronts(t, a), ownerFronts(t, b)
	waitLink(t, a, ca, "b", func(l authtest.LinkOf) bool { return l.State == "up" })
	a.SetBlocked("b")
	b.SetBlocked("a")
	if err := b.Fyrd.Restart(); err != nil { // the link drops
		t.Fatal(err)
	}
	cb = ownerFronts(t, b)
	for _, c := range []struct {
		n      *authtest.MeshNode
		cookie string
		host   string
	}{{a, ca, "b"}, {b, cb, "a"}} {
		l := waitLink(t, c.n, c.cookie, c.host, func(l authtest.LinkOf) bool { return l.State == "down" && l.Error.Kind == "timeout" })
		if l.Error.Text == "" {
			t.Errorf("%s: no text with the reason", c.n.Name)
		}
	}
	// the ACL is lifted: one link again, from whichever side redials first
	a.SetBlocked()
	b.SetBlocked()
	waitLink(t, a, ca, "b", func(l authtest.LinkOf) bool { return l.State == "up" })
	waitLink(t, b, cb, "a", func(l authtest.LinkOf) bool { return l.State == "up" })
	// The link up is not yet a's front following b again: its stream to b
	// reconnects on its own retry, and until then a call says b is offline
	// (V0.fix-flakes).
	waitHost(t, a, ca, "b")
	if r, err := a.Fyrd.Get(a.Fyrd.HostPath(ca, "b", "/commands"), ca); err != nil || r.Status == 502 || r.Status == 503 {
		t.Errorf("a's front to b after the flap: %v %v", err, r)
	}
}

// A removal closes the link at once, the stream on it with it, from either end; a
// revoked device is refused per request on the link as before.
func TestRemovalClosesTheLinkAndItsOpenStream(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "c", UserID: 1, Block: []string{"a"}})
	a, c := m.Node("a"), m.Node("c")
	allPaired(t, a, c)
	ca := ownerFronts(t, a)
	waitLink(t, a, ca, "c", func(l authtest.LinkOf) bool { return l.State == "up" })
	// an open stream through a's front to c's host, over the link
	req, _ := http.NewRequest("GET", a.Fyrd.URL()+"/api/front/stream?host=c", nil)
	_ = req
	if out, err := a.Fyrd.Fyr("host", "remove", idOf(t, c)); err != nil || !strings.Contains(out, "removed") {
		t.Fatalf("%v %s", err, out)
	}
	end := time.Now().Add(5 * time.Second)
	for {
		if l, ok := authtest.HostLink(a.Fyrd, ca, "c"); !ok || l.State == "down" {
			break
		}
		if time.Now().After(end) {
			t.Fatal("a's front still shows c's link up after the removal")
		}
		time.Sleep(25 * time.Millisecond)
	}
	// c cannot bring it back: a is no longer a member there, and c is not one at a
	key, _ := identity.Load(c.Fyrd.Data)
	aKey, _ := identity.Load(a.Fyrd.Data)
	d := &peerlink.Dialer{Key: key}
	conn, proto, err := d.DialLink(t.Context(), aKey.ID(), net.JoinHostPort("127.0.0.1", strconv.Itoa(a.PeerPort)))
	if err != nil || proto != peerlink.ALPN2 {
		t.Fatalf("dial: %v %q", err, proto)
	}
	defer conn.Close()
	// the hello is refused with a reason, not a silent close
	conn.Write([]byte("{\"v\":2}\n"))
	buf := make([]byte, 128)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ := conn.Read(buf)
	if !strings.Contains(string(buf[:n]), "not-a-member") {
		t.Errorf("a removed install's link attempt: %q (want a not-a-member reply)", buf[:n])
	}
}

// A stranger gets no fyr-peer/2 link and writes nothing: its hello is answered
// not-a-member and the connection closed.
func TestAStrangerGetsNoLinkAndWritesNothing(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1})
	a := m.Node("a")
	aKey, _ := identity.Load(a.Fyrd.Data)
	stranger, _ := identity.Load(t.TempDir())
	d := &peerlink.Dialer{Key: stranger}
	conn, proto, err := d.DialLink(t.Context(), aKey.ID(), net.JoinHostPort("127.0.0.1", strconv.Itoa(a.PeerPort)))
	if err != nil || proto != peerlink.ALPN2 {
		t.Fatalf("%v %q", err, proto)
	}
	defer conn.Close()
	conn.Write([]byte("{\"v\":2}\n"))
	buf := make([]byte, 128)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ := conn.Read(buf)
	if !strings.Contains(string(buf[:n]), "not-a-member") {
		t.Errorf("a stranger's hello: %q", buf[:n])
	}
	// and it cannot open a tunnel or send an h2 preface before the hello
	conn2, _, err := d.DialLink(t.Context(), aKey.ID(), net.JoinHostPort("127.0.0.1", strconv.Itoa(a.PeerPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	conn2.Write([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"))
	conn2.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, _ := conn2.Read(buf); n > 0 && !strings.Contains(string(buf[:n]), "not-a-member") {
		t.Errorf("an HTTP/2 preface before the hello was answered: %q", buf[:n])
	}
}
