package peerlink

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/meros/fyr/internal/identity"
)

func key(t *testing.T) *identity.Key {
	k, err := identity.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func serve(t *testing.T, k *identity.Key, h http.Handler) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := Serve(ln, k, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

// SPEC V0.158: the dialer requires the exact id; the listener learns the
// dialer's key from the handshake.
func TestLinkPinsTheServerAndLearnsTheClient(t *testing.T) {
	srv, cli := key(t), key(t)
	var seen string
	addr := serve(t, srv, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pub, ok := Caller(r); ok {
			seen = identity.IDOf(pub)
		}
		w.Write([]byte("hi"))
	}))
	d := &Dialer{Key: cli}
	resp, err := d.Client(srv.ID(), addr).Get("https://anything.example/x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hi" || seen != cli.ID() {
		t.Fatalf("body %q, server saw %q (want %s)", b, seen, cli.ID())
	}
}

// A wrong key fails the handshake, before any request.
func TestWrongKeyFailsTheHandshake(t *testing.T) {
	srv, cli, other := key(t), key(t), key(t)
	addr := serve(t, srv, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) }))
	d := &Dialer{Key: cli}
	if _, err := d.Client(other.ID(), addr).Get("https://x/"); err == nil {
		t.Fatal("a link to the wrong install worked")
	}
}

// The peer port speaks only TLS 1.3: plain HTTP and TLS 1.2 get nothing.
func TestOnlyTLS13(t *testing.T) {
	srv := key(t)
	addr := serve(t, srv, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) }))
	if resp, err := http.Get("http://" + addr + "/"); err == nil && resp.StatusCode == 200 {
		t.Error("plain HTTP answered on the peer port")
	}
	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS12})
	if err == nil {
		c.Close()
		t.Error("TLS 1.2 was accepted")
	}
}

func TestResolveMapsTheAddress(t *testing.T) {
	srv, cli := key(t), key(t)
	addr := serve(t, srv, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	d := &Dialer{Key: cli, Resolve: func(hp string) string {
		if hp == "peer.ts.net:7779" {
			return addr
		}
		return ""
	}}
	conn, err := d.DialTLS(context.Background(), srv.ID(), "peer.ts.net:7779")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	var _ net.Conn = conn
}
