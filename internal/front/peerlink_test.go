package front

// SPEC V0.158: a front prefers the peer link to a host and falls back to the
// whois link when the peer link cannot be made; the fallback is counted.

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
