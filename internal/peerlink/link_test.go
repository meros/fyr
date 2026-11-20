package peerlink

// SPEC V0.172 (decision 0030 Amendment 1): one link, both ways.

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/identity"
)

// pair makes a link between a dialer and an acceptor over real TLS on loopback:
// handlers answer each side's requests with who asked.
func pair(t *testing.T) (dialer, acceptor *Link, dk, ak *identity.Key) {
	t.Helper()
	dk, ak = key(t), key(t)
	ah := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pub, _ := Caller(r)
		w.Write([]byte("acceptor-served " + identity.IDOf(pub) + " " + r.URL.Path))
	})
	dh := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pub, _ := Caller(r)
		if r.URL.Path == "/stream" {
			w.WriteHeader(200)
			fl := w.(http.Flusher)
			for i := 0; i < 3; i++ {
				w.Write([]byte("tick\n"))
				fl.Flush()
				time.Sleep(20 * time.Millisecond)
			}
			return
		}
		w.Write([]byte("dialer-served " + identity.IDOf(pub) + " " + r.URL.Path))
	})
	dl, al := linkBetween(t, dk, ak, dh, ah)
	return dl, al, dk, ak
}

// linkBetween makes a link opened by dk to ak over TLS on loopback, each side
// serving the other's requests with its handler. Both say in the hello that they
// announce the end of a replaced link (V0.peerlink-replace-drain).
func linkBetween(t *testing.T, dk, ak *identity.Key, dh, ah http.Handler) (dl, al *Link) {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	pl, err := Listen(ln, ak)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pl.Close() })
	got := make(chan *Link, 1)
	go func() {
		lc := <-Links(pl)
		c := lc.Conn
		h, _ := readHello(c)
		writeHello(c, hello{V: 2, OK: true, Drain: true})
		l := NewAcceptorLink(c, ak.ID(), identity.IDOf(lc.Key), lc.Key, ah)
		l.setPeerDrains(h.Drain)
		got <- l
	}()
	d := &Dialer{Key: dk}
	conn, proto, err := d.DialLink(context.Background(), ak.ID(), ln.Addr().String())
	if err != nil || proto != ALPN2 {
		t.Fatalf("dial: %v %q", err, proto)
	}
	writeHello(conn, hello{V: 2, Drain: true})
	h, err := readHello(conn)
	if err != nil || !h.OK {
		t.Fatalf("hello: %+v %v", h, err)
	}
	dl, err = NewDialerLink(conn, dk.ID(), ak.ID(), ak.Public, dh)
	if err != nil {
		t.Fatal(err)
	}
	dl.setPeerDrains(h.Drain)
	al = <-got
	for _, l := range []*Link{dl, al} {
		select {
		case <-l.Ready():
		case <-time.After(5 * time.Second):
			t.Fatal("a link did not become ready")
		}
	}
	t.Cleanup(func() { dl.Close(); al.Close() })
	return dl, al
}

func get(t *testing.T, l *Link, path string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://x"+path, nil)
	resp, err := l.RoundTrip(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// Requests go both ways over the one connection, each side serving the other as
// the key the handshake proved; a stream passes.
func TestOneLinkCarriesRequestsBothWays(t *testing.T) {
	dl, al, dk, ak := pair(t)
	if got := get(t, dl, "/hello"); got != "acceptor-served "+dk.ID()+" /hello" {
		t.Errorf("dialer -> acceptor: %q", got)
	}
	if got := get(t, al, "/hello"); got != "dialer-served "+ak.ID()+" /hello" {
		t.Errorf("acceptor -> dialer: %q", got)
	}
	if dl.Dialer != dk.ID() || al.Dialer != dk.ID() {
		t.Errorf("who opened it: %q %q (want %s)", dl.Dialer, al.Dialer, dk.ID())
	}
	// a live stream from the acceptor's side to the dialer's server
	req, _ := http.NewRequest("GET", "http://x/stream", nil)
	resp, err := al.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	for i := 0; i < 3; i++ {
		if line, err := rd.ReadString('\n'); err != nil || strings.TrimSpace(line) != "tick" {
			t.Fatalf("stream line %d: %q %v", i, line, err)
		}
	}
}

// Closing either end closes the link for both, and the requests in flight end.
func TestClosingEitherEndEndsTheLink(t *testing.T) {
	for _, closeDialer := range []bool{true, false} {
		dl, al, _, _ := pair(t)
		req, _ := http.NewRequest("GET", "http://x/stream", nil)
		resp, err := al.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if closeDialer {
			dl.Close()
		} else {
			al.Close()
		}
		for _, l := range []*Link{dl, al} {
			select {
			case <-l.Done():
			case <-time.After(3 * time.Second):
				t.Fatalf("closeDialer=%v: a side stayed open", closeDialer)
			}
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if _, err := dl.RoundTrip(mustReq("/x")); err == nil {
			t.Error("a closed link took a request")
		}
	}
}

func mustReq(path string) *http.Request {
	r, _ := http.NewRequest("GET", "http://x"+path, nil)
	return r
}

// A second tunnel on the same link is refused.
func TestOnlyOneTunnelPerLink(t *testing.T) {
	dl, _, _, _ := pair(t)
	req, _ := http.NewRequest("POST", "http://x"+tunnelPath, strings.NewReader(""))
	resp, err := dl.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("a second tunnel: %d (want 409)", resp.StatusCode)
	}
}
