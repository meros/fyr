package tailnet

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

// SPEC 0.5 V0.14: the owner is the Tailscale user of this machine.
func TestOwnerLoginFromLocalAPI(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ts.sock")
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
	login, dns, err := Self(sock)
	if err != nil || login != "me@example.com" || dns != "box.tail.ts.net" {
		t.Fatalf("%q %q %v", login, dns, err)
	}
	if _, _, err := Self(filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Fatal("missing socket gave no error")
	}
}

// SPEC 0.5 V0.51: whois names the owner of a peer connection; a tagged node
// has none.
func TestPeersWhois(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ts.sock")
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
