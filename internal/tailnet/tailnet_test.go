package tailnet

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestMain keeps the tests off this machine's Tailscale app (a Mac's real
// /Library/Tailscale would answer for a missing socket).
func TestMain(m *testing.M) {
	MacDir = filepath.Join(os.TempDir(), "fyr-tailnet-test-none")
	os.Exit(m.Run())
}

// SPEC 0.5 V0.14: the owner is the Tailscale user of this machine.
func TestOwnerLoginFromLocalAPI(t *testing.T) {
	sock := sockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/status" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"Self":{"UserID":42,"DNSName":"box.tail.ts.net."},"User":{"42":{"LoginName":"me@example.com"}}}`))
	}))
	login, dns, err := Self(context.Background(), sock)
	if err != nil || login != "me@example.com" || dns != "box.tail.ts.net" {
		t.Fatalf("%q %q %v", login, dns, err)
	}
	if _, _, err := Self(context.Background(), filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Fatal("missing socket gave no error")
	}
}

// SPEC 0.5 V0.51: whois names the owner of a peer connection; a tagged node
// has none.
func TestPeersWhois(t *testing.T) {
	sock := sockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/whois" || r.Header.Get("Sec-Tailscale") != "localapi" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("addr") {
		case "100.64.0.2:4000":
			w.Write([]byte(`{"Node":{"Name":"phone"},"UserProfile":{"LoginName":"me@example.com"}}`))
		case "100.64.0.3:4000":
			w.Write([]byte(`{"Node":{"Tags":["tag:server"]},"UserProfile":{"LoginName":"tagged-devices"}}`))
		default:
			http.Error(w, "no match", 404)
		}
	}))
	p := Peers{Socket: sock}
	if l, err := p.Login(context.Background(), "100.64.0.2:4000"); err != nil || l != "me@example.com" {
		t.Fatalf("%q %v", l, err)
	}
	if _, err := p.Login(context.Background(), "100.64.0.3:4000"); err == nil {
		t.Fatal("a tagged node got a login")
	}
	if _, err := p.Login(context.Background(), "100.64.0.9:4000"); err == nil {
		t.Fatal("an unknown peer got a login")
	}
}

// SPEC 0.5 V0.54: hostListen "tailnet:PORT" listens on this node's tailnet
// IPv4 address, read from tailscaled.
func TestIPv4FromLocalAPI(t *testing.T) {
	sock := sockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"TailscaleIPs":["fd7a:115c:a1e0::1","100.101.102.103"]}`))
	}))
	if ip, err := IPv4(context.Background(), sock); err != nil || ip != "100.101.102.103" {
		t.Fatalf("%q %v", ip, err)
	}
}

// sockPath is a socket path short enough for macOS (104 bytes): t.TempDir()
// there carries the test's name under /var/folders/….
func sockPath(t *testing.T) string {
	dir, err := os.MkdirTemp("", "ts")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "ts.sock")
}

// SPEC V0.129 (4), decision 0028 slice 0: a tagged node has no owner. Its
// tailscaled names a user for it whose login is the node itself; Self must
// not hand that out as the owner.
func TestSelfOfATaggedNodeNamesNoOwner(t *testing.T) {
	sock := sockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Self":{"UserID":7,"Tags":["tag:server"],"DNSName":"host-b.tail.ts.net."},"User":{"7":{"LoginName":"host-b"}}}`))
	}))
	login, _, err := Self(context.Background(), sock)
	if err == nil || login != "" {
		t.Fatalf("tagged node gave owner %q, err %v", login, err)
	}
	if !errors.Is(err, ErrTagged) {
		t.Fatalf("want ErrTagged, got %v", err)
	}
}

// The macOS app has no Unix socket: its LocalAPI is a port on 127.0.0.1
// (/Library/Tailscale/ipnport) that takes the token in sameuserproof-<port>.
func TestMacLocalAPIPortAndToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pw, ok := r.BasicAuth(); !ok || pw != "tok123" || r.URL.Path != "/localapi/v0/status" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		io.WriteString(w, `{"Self":{"UserID":7,"DNSName":"mac.tail.ts.net."},"User":{"7":{"LoginName":"owner@example.com"}}}`)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	dir := t.TempDir()
	if err := os.Symlink(port, filepath.Join(dir, "ipnport")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sameuserproof-"+port), []byte("tok123\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	old := MacDir
	MacDir = dir
	defer func() { MacDir = old }()
	missing := filepath.Join(dir, "no.sock")
	if !Available(missing) {
		t.Fatal("not available")
	}
	login, dns, err := Self(context.Background(), missing)
	if err != nil || login != "owner@example.com" || dns != "mac.tail.ts.net" {
		t.Fatalf("%q %q %v", login, dns, err)
	}
	MacDir = t.TempDir()
	if Available(missing) {
		t.Fatal("available without a port")
	}
}

// V0.review-hangs (2): a server asks tailscaled on every request, so every
// call shares one client per socket and its kept-alive connection; a
// transport per call left its idle connection and goroutines behind each
// time.
func TestLocalAPIReusesOneConnection(t *testing.T) {
	sock := sockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var conns atomic.Int32
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Sec-Tailscale") != "localapi" {
				http.Error(w, "no header", 403)
				return
			}
			w.Write([]byte(`{"Self":{"UserID":1,"DNSName":"box.tail.ts.net."},"User":{"1":{"LoginName":"me@example.com"}},"Node":{"Name":"phone"},"UserProfile":{"LoginName":"me@example.com"}}`))
		}),
		ConnState: func(_ net.Conn, s http.ConnState) {
			if s == http.StateNew {
				conns.Add(1)
			}
		},
	}
	go srv.Serve(ln)
	defer srv.Close()
	p := Peers{Socket: sock}
	for range 50 {
		if _, err := p.Login(context.Background(), "100.64.0.2:4000"); err != nil {
			t.Fatal(err)
		}
		if names := p.Names(context.Background()); len(names) != 2 {
			t.Fatalf("names %v", names)
		}
	}
	// The transport puts a finished connection back from its own goroutine,
	// so a call can now and then race it and dial a second one; a client
	// per call dialled 100.
	if n := conns.Load(); n > 5 {
		t.Fatalf("100 calls opened %d connections to tailscaled, want them to share one", n)
	}
	if localClient(sock) != localClient(sock) {
		t.Fatal("a second client for the same socket")
	}
}

// A refusal names the call and its status, and an answer that is not JSON
// is an error, not a zero value.
func TestLocalAPIErrors(t *testing.T) {
	sock := sockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/localapi/v0/status" {
			w.Write([]byte(`not json`))
			return
		}
		http.Error(w, "nope", 403)
	}))
	if _, err := (Peers{Socket: sock}).Login(context.Background(), "100.64.0.2:4000"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("whois refusal: %v", err)
	}
	if _, err := IPv4(context.Background(), sock); err == nil {
		t.Fatal("a status that is not JSON gave no error")
	}
}
