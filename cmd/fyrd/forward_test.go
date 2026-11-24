package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/meros/fyr/internal/front"
)

// V0.forwarded-ports: each forward port has a loopback listener and a socket
// for Tailscale Serve; the default is four ports, an empty list none.
func TestForwardPortsAreServedOnLoopbackAndASocket(t *testing.T) {
	if got := (config{}).forwardPorts(); len(got) != 4 || got[0] != 7450 || got[3] != 7453 {
		t.Fatalf("the default: %v", got)
	}
	none := []int{}
	if got := (config{ForwardPorts: &none}).forwardPorts(); len(got) != 0 {
		t.Fatalf("an empty list: %v", got)
	}
	odd := []int{80, 7460, 7460, 70000}
	if got := (config{ForwardPorts: &odd}).forwardPorts(); len(got) != 1 || got[0] != 7460 {
		t.Fatalf("ports out of range and twice: %v", got)
	}

	// Two free ports.
	var ports []int
	for range 2 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, ln.Addr().(*net.TCPAddr).Port)
		ln.Close()
	}
	cfg := config{ForwardPorts: &ports, Role: "both", ServeSocketUsers: []int{os.Getuid()}}
	data := t.TempDir()
	fr := front.New(front.Options{Name: "front", BuildID: "b", ForwardPorts: cfg.forwardPorts(), Web: fstest.MapFS{"index.html": {Data: []byte("x")}}}, nil)
	srvs := &servers{onErr: func(err error) { t.Logf("listener: %v", err) }}
	t.Cleanup(func() {
		for _, s := range srvs.list {
			s.Close()
		}
	})
	serveForwards(cfg, fr, data, "127.0.0.1:7777", srvs.serve)
	for _, p := range ports {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(p) + "/")
		if err != nil {
			t.Fatalf("port %d: %v", p, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 404 || !strings.Contains(string(b), "Nothing is forwarded here") {
			t.Fatalf("port %d before any link: %d %s", p, resp.StatusCode, b)
		}
		if st, err := os.Stat(forwardSocket(data, p)); err != nil || st.Mode()&os.ModeSocket == 0 {
			t.Fatalf("port %d: no socket for Tailscale Serve: %v", p, err)
		}
	}
	if got := forwardTargets(cfg, data); len(got) != 2 || got[ports[0]] != "unix:"+forwardSocket(data, ports[0]) {
		t.Fatalf("what Serve publishes: %v", got)
	}
	if got := forwardTargets(config{Role: "host"}, data); len(got) != 0 {
		t.Fatalf("a host-only install forwards nothing: %v", got)
	}
}
