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

// SPEC 0.5 V0.54: hostListen "tailnet:PORT" listens on this node's tailnet
// IPv4 address, read from tailscaled.
func TestIPv4FromLocalAPI(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ts.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"TailscaleIPs":["fd7a:115c:a1e0::1","100.101.102.103"]}`))
	}))
	if ip, err := IPv4(sock); err != nil || ip != "100.101.102.103" {
		t.Fatalf("%q %v", ip, err)
	}
}

// SPEC 0.5 V0.123 (decision 0025): a discovery candidate is a peer that is
// online, untagged and owned by this node's own Tailscale user.
func TestCandidatesAreTheOwnersOnlineNodes(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ts.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/status" || r.Header.Get("Sec-Tailscale") != "localapi" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"Self":{"UserID":42,"DNSName":"me.tail.ts.net."},"Peer":{
			"k1":{"DNSName":"server.tail.ts.net.","UserID":42,"Online":true},
			"k2":{"DNSName":"sleeping.tail.ts.net.","UserID":42,"Online":false},
			"k3":{"DNSName":"tagged.tail.ts.net.","UserID":42,"Online":true,"Tags":["tag:ci"]},
			"k4":{"DNSName":"friend.tail.ts.net.","UserID":7,"Online":true},
			"k5":{"DNSName":"","UserID":42,"Online":true}}}`))
	}))
	got, err := Candidates(context.Background(), sock)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "server.tail.ts.net" {
		t.Fatalf("candidates %v", got)
	}
	if _, err := Candidates(context.Background(), filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Fatal("missing socket gave no error")
	}
}
