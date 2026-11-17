package authtest_test

// Decision 0030 slice 6 (the LAN): installs with no tailscaled find each other by
// mDNS (an in-memory bus here), the address follows the key, an answer proves
// nothing, nothing is served over plain HTTP off loopback, and a phone with the
// owner's certificate signs in by QR.

import (
	"crypto/tls"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/authtest"
)

type lanNode struct {
	*authtest.Fyrd
	id   string
	name string
}

func (n *lanNode) addr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(n.port())) }
func (n *lanNode) port() int {
	r, err := n.Roster()
	if err != nil {
		return 0
	}
	for _, m := range r.Members {
		if m.Self {
			return m.Port
		}
	}
	return 0
}

// lanInstall starts an install with no tailscaled, a peer port and the bus as its mDNS.
func lanInstall(t *testing.T, name, bus string, extra map[string]any) *lanNode {
	t.Helper()
	x := map[string]any{"peerListen": "127.0.0.1:0", "peerEverySeconds": 1, "lanBusFile": bus, "linkBackoffMillis": 100, "linkDialMillis": 700}
	for k, v := range extra {
		x[k] = v
	}
	f, err := authtest.Start(authtest.Options{Name: name, Extra: x})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Stop)
	n := &lanNode{Fyrd: f, name: name}
	r, err := f.WaitRoster(30*time.Second, func(r *authtest.Roster) bool {
		for _, m := range r.Members {
			if m.Self && m.Port != 0 {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range r.Members {
		if m.Self {
			n.id = m.ID
		}
	}
	return n
}

func lanInvite(t *testing.T, n *lanNode) string {
	t.Helper()
	out, err := n.Fyr("pair")
	if err != nil {
		t.Fatalf("fyr pair: %v\n%s", err, out)
	}
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "fyr1-") {
			return f
		}
	}
	t.Fatalf("no invite in:\n%s", out)
	return ""
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func hostList(n *lanNode) string { out, _ := n.Fyr("host", "list"); return out }

// Two installs with no tailscaled: each is announced on the bus; a shows b as "not
// paired" with the address it was heard at, and b pairs with a's invite with no -addr,
// because the code names the id and the bus supplies the address.
func TestTwoInstallsOnALANFindEachOtherAndPairByInvite(t *testing.T) {
	var lan authtest.LAN
	bus := filepath.Join(t.TempDir(), "lan.json")
	lan.File(bus)
	a := lanInstall(t, "a", bus, nil)
	b := lanInstall(t, "b", bus, nil)
	lan.Advertise(a.id, a.addr())
	lan.Advertise(b.id, b.addr())

	waitFor(t, "a lists b as not paired, heard on the LAN", func() bool {
		out := hostList(a)
		return strings.Contains(out, "not paired") && strings.Contains(out, b.addr()) && strings.Contains(out, "LAN")
	})
	if r, _ := a.Roster(); len(r.Others()) != 0 {
		t.Fatalf("an mDNS answer paired by itself: %v", r.Others())
	}
	code := lanInvite(t, a)
	if out, err := b.Fyr("pair", code); err != nil || !strings.Contains(out, "Paired with a") {
		t.Fatalf("fyr pair CODE (no -addr): %v\n%s\n%s", err, out, b.Log())
	}
	waitOthersLAN(t, a, "b")
	waitOthersLAN(t, b, "a")
}

func waitOthersLAN(t *testing.T, n *lanNode, want string) {
	t.Helper()
	if _, err := n.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return strings.Contains(strings.Join(r.Others(), ","), want) }); err != nil {
		t.Fatalf("%s never learned %s: %v", n.name, want, err)
	}
}

// A spoofed answer is an address: the handshake must prove the id that was meant.
// The bus claims a paired id at an attacker, and an unknown id at the same attacker:
// the paired one is tried and refused (the attacker is sent a ClientHello and nothing
// else), the unknown one is shown "not paired" and never dialed, and nothing changes in
// the roster; when the real install is back at its own address the link is up again.
func TestASpoofedAnswerIsNeverFollowed(t *testing.T) {
	var lan authtest.LAN
	bus := filepath.Join(t.TempDir(), "lan.json")
	lan.File(bus)
	a := lanInstall(t, "a", bus, nil)
	b := lanInstall(t, "b", bus, nil)
	lan.Advertise(a.id, a.addr())
	lan.Advertise(b.id, b.addr())
	if out, err := b.Fyr("pair", lanInvite(t, a), "-addr", a.addr()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	waitOthersLAN(t, a, "b")
	waitOthersLAN(t, b, "a")
	if err := b.PinPeerPort(); err != nil {
		t.Fatal(err)
	}
	oldB := b.addr()

	var hits, plain atomic.Int64
	atk, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { atk.Close() })
	go func() {
		for {
			c, err := atk.Accept()
			if err != nil {
				return
			}
			hits.Add(1)
			go func() {
				defer c.Close()
				buf := make([]byte, 4096)
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				n, _ := c.Read(buf)
				if n > 0 && strings.Contains(string(buf[:n]), "fyr1-") {
					plain.Add(1) // a secret in clear text: never
				}
			}()
		}
	}()
	b.StopKeep()
	lan.Withdraw(oldB)
	lan.Spoof(b.id, atk.Addr().String())
	lan.Spoof("AAAA-BBBB-CCCC-DDDD", atk.Addr().String())
	waitFor(t, "a dials the spoofed address of a paired id", func() bool { return hits.Load() > 0 })
	waitFor(t, "a shows the unknown id as not paired", func() bool { return strings.Contains(hostList(a), "AAAA-BBBB") })
	waitFor(t, "a's link to b names the refusal", func() bool {
		out := hostList(a)
		return strings.Contains(out, "key-mismatch") || strings.Contains(out, "key does not match") || strings.Contains(out, "down")
	})
	if r, _ := a.Roster(); len(r.Others()) != 1 {
		t.Fatalf("the roster changed on an answer: %v", r.Others())
	}
	if plain.Load() != 0 {
		t.Fatal("something was sent to the attacker in clear")
	}
	// the unknown id is never dialed: only the paired one was
	h := hits.Load()
	time.Sleep(1500 * time.Millisecond) // a bounded look at a counter that must not move per round beyond backoff
	if hits.Load() > h+8 {
		t.Errorf("the attacker was dialed %d more times in 1.5 s: backoff is not holding", hits.Load()-h)
	}

	lan.Withdraw(atk.Addr().String())
	if err := b.Restart(); err != nil {
		t.Fatal(err)
	}
	lan.Advertise(b.id, b.addr())
	waitFor(t, "a's link to b is up again at b's own address", func() bool {
		out := hostList(a)
		return strings.Contains(out, "b") && strings.Contains(out, "link up")
	})
}

// The address follows the key: both installs restart on new ports, nobody knows the
// other's address any more, and the bus alone brings the link back (the roster is
// unchanged: the same two ids).
func TestAPairedIdIsFollowedAtANewAddress(t *testing.T) {
	var lan authtest.LAN
	bus := filepath.Join(t.TempDir(), "lan.json")
	lan.File(bus)
	a := lanInstall(t, "a", bus, nil)
	b := lanInstall(t, "b", bus, nil)
	if out, err := b.Fyr("pair", lanInvite(t, a), "-addr", a.addr()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	waitOthersLAN(t, a, "b")
	waitOthersLAN(t, b, "a")
	oldA, oldB := a.addr(), b.addr()
	a.StopKeep()
	b.StopKeep()
	if err := a.Restart(); err != nil {
		t.Fatal(err)
	}
	if err := b.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "new peer ports", func() bool { return a.port() != 0 && b.port() != 0 && (a.addr() != oldA || b.addr() != oldB) })
	lan.Advertise(a.id, a.addr())
	lan.Advertise(b.id, b.addr())
	waitFor(t, "a's link to b is up at its new address", func() bool { return strings.Contains(hostList(a), "link up") })
	waitFor(t, "b's link to a is up at its new address", func() bool { return strings.Contains(hostList(b), "link up") })
	if r, _ := a.Roster(); len(r.Others()) != 1 {
		t.Errorf("the roster changed: %v", r.Others())
	}
}

// 3.5: nothing is served over plain HTTP off loopback.
func TestNoPlainHTTPOffLoopback(t *testing.T) {
	_, err := authtest.Start(authtest.Options{Name: "x", Extra: map[string]any{"listen": "0.0.0.0:0"}})
	if err == nil {
		t.Fatal("fyrd started and listens on every interface over plain HTTP")
	}
}

var tlsLine = regexp.MustCompile(`tls: serving the front at https://([^ ]+)`)

// 3.5: a phone with no Tailscale and the owner's certificate: `tls: {cert, key}` serves
// the front over HTTPS on the LAN; nothing is trusted but a session (not the observe
// mode, not loopback-ness, not a Tailscale header); the QR/link from `fyr device add` is
// on that HTTPS address and signs the device in.
func TestAPhoneWithTheOwnersCertificateSignsInByQR(t *testing.T) {
	ca, err := authtest.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cert, key, err := ca.WriteCert(dir, "localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := authtest.Start(authtest.Options{Name: "lan", Extra: map[string]any{"tls": map[string]any{"cert": cert, "key": key, "listen": "127.0.0.1:0", "name": "localhost"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Stop)
	var addr string
	waitFor(t, "the tls line", func() bool {
		if m := tlsLine.FindStringSubmatch(f.Log()); m != nil {
			addr = m[1]
			return true
		}
		return false
	})
	_, port, _ := net.SplitHostPort(addr)
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.Pool}}}
	base := "https://localhost:" + port

	if resp, err := http.Get("http://localhost:" + port + "/api/state"); err == nil && resp.StatusCode == 200 {
		t.Error("the TLS port answered plain HTTP with the state")
	}
	get := func(path string, hdr map[string]string, cookie string) int {
		req, _ := http.NewRequest("GET", base+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "fyr_session", Value: cookie})
		}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if s := get("/api/state", nil, ""); s != 401 {
		t.Errorf("no session over the LAN: %d, want 401 (the observe mode never serves this way)", s)
	}
	if s := get("/api/state", map[string]string{"Tailscale-User-Login": "owner@example.com"}, ""); s != 400 {
		t.Errorf("a Tailscale header over the LAN: %d, want 400", s)
	}
	out, err := f.Fyr("device", "add", "-name", "Kitchen phone")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var link string
	for _, w := range strings.Fields(out) {
		if strings.HasPrefix(w, "https://") {
			link = w
		}
	}
	if !strings.HasPrefix(link, base+"/auth/once#") {
		t.Fatalf("the QR link is not on the HTTPS address %s:\n%s", base, out)
	}
	_, tok, _ := strings.Cut(link, "#")
	req, _ := http.NewRequest("POST", base+"/auth/once", strings.NewReader(`{"token":"`+tok+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var session string
	for _, c := range resp.Cookies() {
		if c.Name == "fyr_session" {
			session = c.Value
			if !c.Secure {
				t.Error("the session cookie is not Secure over HTTPS")
			}
		}
	}
	if resp.StatusCode != 200 || session == "" {
		t.Fatalf("redeem: %d, session %q", resp.StatusCode, session)
	}
	if s := get("/api/state", nil, session); s != 200 {
		t.Errorf("signed in over HTTPS: %d", s)
	}
}
