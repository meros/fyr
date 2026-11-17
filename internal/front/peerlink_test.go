package front

// SPEC V0.158: a front prefers the peer link to a host and falls back to the
// whois link when the peer link cannot be made; the fallback is counted.

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
)

func TestLinkTransportPrefersThePeerLinkAndFallsBack(t *testing.T) {
	hostKey, frontKey := mustKey(t), mustKey(t)
	var viaPeer, viaWhois int
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv, err := peerlink.Serve(ln, hostKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { viaPeer++; w.Write([]byte("peer")) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	whois := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { viaWhois++; w.Write([]byte("whois")) }))
	t.Cleanup(whois.Close)
	wu, _ := url.Parse(whois.URL)

	lt := &linkTransport{whois: http.DefaultTransport, whoisBase: wu}
	dial := &peerlink.Dialer{Key: frontKey}
	get := func() string {
		req, _ := http.NewRequest("GET", whois.URL+"/x", nil)
		resp, err := lt.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b := make([]byte, 16)
		n, _ := resp.Body.Read(b)
		return string(b[:n])
	}
	if got := get(); got != "whois" { // no peer link yet: followed as before, and not counted
		t.Fatalf("before a peer link: %q", got)
	}
	if lt.uses.Load() != 0 {
		t.Fatalf("a host with no peer link counted a whois use")
	}
	lt.attach(PeerEndpoint{ID: hostKey.ID(), Addr: ln.Addr().String(), Dial: dial})
	if got := get(); got != "peer" {
		t.Fatalf("with a peer link: %q", got)
	}
	if lt.uses.Load() != 0 {
		t.Error("the peer link was counted as a whois use")
	}
	// the peer link cannot be made (a wrong id pinned): the whois link serves, and it is counted
	lt.attach(PeerEndpoint{ID: mustKey(t).ID(), Addr: ln.Addr().String(), Dial: dial})
	if got := get(); got != "whois" {
		t.Fatalf("with the peer link down: %q", got)
	}
	if lt.uses.Load() != 1 {
		t.Errorf("whois uses %d (want 1)", lt.uses.Load())
	}
	if viaPeer != 1 || viaWhois != 2 {
		t.Errorf("peer %d whois %d", viaPeer, viaWhois)
	}
}

func mustKey(t *testing.T) *identity.Key {
	k, err := identity.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// V0.160: the peer way in may enforce by itself only when every followed host (the
// one in this process aside) is on a pinned link; a host removed from the roster
// is dropped, and one that also has a whois link keeps it.
func TestAllOnPeerLinkAndDropPeers(t *testing.T) {
	hostKey, frontKey := mustKey(t), mustKey(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv, err := peerlink.Serve(ln, hostKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	whois := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	t.Cleanup(whois.Close)
	f := New(Options{Name: "front", MinBackoff: 20 * time.Millisecond}, []HostConfig{{Name: "old", URL: whois.URL, Remote: true}})
	if ok, why := f.AllOnPeerLink(); ok || !strings.Contains(why, "old") {
		t.Fatalf("a host on the whois link: %v %q", ok, why)
	}
	dial := &peerlink.Dialer{Key: frontKey}
	f.UsePeerLink(hostKey.ID(), "old", PeerEndpoint{ID: hostKey.ID(), Addr: ln.Addr().String(), Dial: dial})
	if ok, why := f.AllOnPeerLink(); !ok {
		t.Fatalf("after the peer link: %q", why)
	}
	// a peer-only host
	other := mustKey(t)
	f.UsePeerLink(other.ID(), "peeronly", PeerEndpoint{ID: other.ID(), Addr: "127.0.0.1:1", Dial: dial})
	if n := len(f.all()); n != 2 {
		t.Fatalf("%d hosts", n)
	}
	// both are removed from the roster: the peer-only host goes; the other keeps its whois link
	f.DropPeers(func(string) bool { return false })
	if n := len(f.all()); n != 1 || f.all()[0].name() != "old" {
		t.Fatalf("after the removal: %d hosts", len(f.all()))
	}
	if f.all()[0].lt.hasPeer() {
		t.Error("the removed install's peer link stayed")
	}
	// a rotated key: the same host on a new id replaces the endpoint and keeps the pin
	k2 := mustKey(t)
	f.UsePeerLink(hostKey.ID(), "old", PeerEndpoint{ID: hostKey.ID(), Addr: ln.Addr().String(), Dial: dial})
	f.pins["old"] = hostKey.ID()
	f.UsePeerLink(k2.ID(), "old", PeerEndpoint{ID: k2.ID(), Addr: ln.Addr().String(), Dial: dial})
	if f.all()[0].lt.peerID() != k2.ID() {
		t.Errorf("the peer link is pinned to %s after the rotation", f.all()[0].lt.peerID())
	}
	if f.pins["old"] != k2.ID() {
		t.Errorf("the pinned host id is %s, not the rotated key's", f.pins["old"])
	}
}
