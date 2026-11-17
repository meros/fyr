package authtest_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/tailnet"
)

// The fixture loads: the install key is the one fyrd reports, the two
// sessions are in the database, and the fyrd that read them is up.
func TestLiveFixtureLoads(t *testing.T) {
	l := live(t)
	k, err := identity.Load(l.Laptop.Data)
	if err != nil || k.ID() != l.ID {
		t.Fatalf("the install key: %v %v (want %s)", k, err, l.ID)
	}
	if !strings.Contains(l.Laptop.Log(), "install key id "+l.ID) {
		t.Errorf("fyrd reports another key id:\n%s", l.Laptop.Log())
	}
	if l.Local == "" || l.Tailnet == "" || l.Local == l.Tailnet {
		t.Fatalf("the fixture's sessions: %q %q", l.Local, l.Tailnet)
	}
	for _, p := range []string{auth.OwnerSocket, auth.ServeSocket} {
		if c, err := net.Dial("unix", l.Laptop.Sock(p)); err != nil {
			t.Errorf("%s: %v", p, err)
		} else {
			c.Close()
		}
	}
}

// A scratch fyrd gets no FYR_* variable (#167, V0.131), whatever the test
// process has: a scratch fyrd that inherited FYR_RUNNER once killed the live
// threads.
func TestScratchEnvironmentHasNoFYRVariables(t *testing.T) {
	for _, e := range authtest.CleanEnv([]string{"PATH=/bin", "FYR_RUNNER=/run/live", "FYR_READY_FILE=x", "FYRISH=1", "HOME=/h"}) {
		if strings.HasPrefix(e, "FYR_") {
			t.Errorf("%s survived", e)
		}
	}
	if got := authtest.CleanEnv([]string{"FYRISH=1"}); len(got) != 1 {
		t.Errorf("only FYR_ is dropped: %v", got)
	}
}

// The fake LocalAPI answers what internal/tailnet reads, and refuses a call
// without Sec-Tailscale as the real one does.
func TestFakeTailscaledAnswersLikeTheLocalAPI(t *testing.T) {
	l := live(t)
	login, dns, err := tailnet.Self(l.TS.Socket)
	if err != nil || login != l.OwnerLogin || dns != l.Self.Name {
		t.Fatalf("Self: %q %q %v", login, dns, err)
	}
	cands, err := tailnet.Candidates(context.Background(), l.TS.Socket)
	if err != nil || len(cands) != 1 || cands[0] != l.Phone.Name {
		t.Errorf("Candidates (online, untagged, the owner's user): %v %v", cands, err)
	}
	if ip, err := tailnet.IPv4(l.TS.Socket); err != nil || ip != l.Self.IP {
		t.Errorf("IPv4: %q %v", ip, err)
	}
	if r, err := authtest.Unix(l.TS.Socket).Get("http://local-tailscaled.sock/localapi/v0/status"); err != nil || r.StatusCode != 403 {
		t.Errorf("a LocalAPI call without Sec-Tailscale: %v %v", r, err)
	}
	for addr, want := range map[string]string{l.Phone.IP + ":1234": l.OwnerLogin, l.Other.IP + ":1": "guest@example.com"} {
		if got, err := (tailnet.Peers{Socket: l.TS.Socket}).Login(context.Background(), addr); err != nil || got != want {
			t.Errorf("whois %s: %q %v (want %q)", addr, got, err, want)
		}
	}
	if got, err := (tailnet.Peers{Socket: l.TS.Socket}).Login(context.Background(), l.Tag.IP+":1"); err == nil {
		t.Errorf("a tagged node has an owner: %q", got)
	}
}

// The Serve emulator does what tailscaled does to a request on its way to a
// Unix socket (#225): drops the client's Tailscale-* headers, sets its own
// from the node, sends Host: localhost and X-Forwarded-For.
func TestServeEmulatorDoesWhatTailscaledDoes(t *testing.T) {
	l := live(t)
	got := make(chan http.Header, 1)
	var host string
	sock, stop := authtest.UnixServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		got <- r.Header.Clone()
	}))
	defer stop()
	s, err := l.TS.ServeFor(l.Phone, sock)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = authtest.Do(s.URL, authtest.Request{Path: "/x", Host: "laptop.tail-test.ts.net:7443", Header: map[string]string{
		"Tailscale-User-Login": "forged@example.com", "Tailscale-Evil": "1", "X-Forwarded-For": "6.6.6.6", "Cookie": "a=b"}})
	if err != nil {
		t.Fatal(err)
	}
	h := <-got
	if host != "localhost" {
		t.Errorf("Host %q (tailscaled sends localhost on a Unix socket)", host)
	}
	if h.Get("Tailscale-User-Login") != l.OwnerLogin || h.Get("Tailscale-Evil") != "" {
		t.Errorf("Tailscale-* headers: %v", h)
	}
	if h.Get("X-Forwarded-For") != l.Phone.IP {
		t.Errorf("X-Forwarded-For %q (want the node's address)", h.Get("X-Forwarded-For"))
	}
	if h.Get("Cookie") != "a=b" {
		t.Errorf("Serve passes the browser's cookie on: %q", h.Get("Cookie"))
	}
}

// The test CA stands in for the WebPKI: a client that trusts it reaches a
// front by a MagicDNS name; one that does not, or a wrong name, fails.
func TestTestCAFront(t *testing.T) {
	t.Parallel()
	ca, err := authtest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	f, err := ca.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hi")) }), "peer.tail-test.ts.net")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if r, err := f.Client().Get("https://peer.tail-test.ts.net:7443/"); err != nil || r.StatusCode != 200 {
		t.Fatalf("with the CA: %v %v", r, err)
	}
	if _, err := f.Client().Get("https://other.tail-test.ts.net:7443/"); err == nil {
		t.Error("a name the certificate does not carry was accepted")
	}
	plain := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{}, DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, f.Addr)
	}}}
	if _, err := plain.Get("https://peer.tail-test.ts.net:7443/"); err == nil {
		t.Error("a client without the CA accepted the certificate")
	}
}

// The in-memory mDNS bus: add, spoof, withdraw.
func TestLANBus(t *testing.T) {
	t.Parallel()
	var lan authtest.LAN
	lan.Advertise("AAAA-BBBB-CCCC-DDDD", "10.0.0.5:7778")
	lan.Spoof("AAAA-BBBB-CCCC-DDDD", "10.0.0.66:7778")
	cs, _ := lan.Candidates(context.Background())
	if len(cs) != 2 || cs[1].Addr != "10.0.0.66:7778" || cs[0].Source != "mdns" {
		t.Fatalf("candidates: %+v", cs)
	}
	lan.Withdraw("10.0.0.66:7778")
	if cs, _ = lan.Candidates(context.Background()); len(cs) != 1 || cs[0].Addr != "10.0.0.5:7778" {
		t.Errorf("after withdraw: %+v", cs)
	}
}
