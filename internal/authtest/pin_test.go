package authtest_test

// Decision 0030 slice 0: today's behaviour, pinned through the fake
// tailscaled. #225 and #226 were reproduced live, not in a test, because no
// test had a Serve in front of a socket. These pass on the code of main; a
// later slice that breaks one has broken a way the owner uses.

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/tailnet"
)

func live(t *testing.T) *authtest.Live {
	t.Helper()
	t.Parallel()
	l, err := authtest.NewLive()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l
}

// V0.130 (3), #225: tailscaled reaches serve.sock with Host: localhost and
// the owner's login. The owner is let in and gets a session.
func TestServeSocketTakesTheOwnerWithHostLocalhost(t *testing.T) {
	l := live(t)
	r, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/front/state"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != 200 {
		t.Fatalf("the owner's phone through Serve: %d %s", r.Status, r.Body)
	}
	session := r.Session()
	if session == "" {
		t.Fatal("no session was minted for the owner's node")
	}
	if !l.Laptop.WaitLog(`Host "localhost"`, 5*time.Second) {
		t.Fatalf("the emulator did not send Host: localhost, as tailscaled does\n%s", l.Laptop.Log())
	}
	// The session works, and is not minted again.
	r, err = authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/front/state", Cookie: session})
	if err != nil || r.Status != 200 || r.Session() != "" {
		t.Fatalf("with its session: %v %v %q", err, r.Status, r.Session())
	}
}

// The emulator is tailscaled's: a Tailscale-User-Login the client sent is
// replaced, and another tailnet user's node is another owner's.
func TestServeSocketRefusesAnotherTailnetUser(t *testing.T) {
	l := live(t)
	other, err := l.TS.ServeFor(l.Other, l.Laptop.Sock(auth.ServeSocket))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	r, err := authtest.Do(other.URL, authtest.Request{Path: "/api/front/state"})
	if err != nil || r.Status != 403 || r.Session() != "" {
		t.Fatalf("another user's node: %v %d %q (want 403 and no session)", err, r.Status, r.Session())
	}
	// A login the browser sent itself is stripped by Serve: the other user's
	// node cannot pose as the owner.
	r, err = authtest.Do(other.URL, authtest.Request{Path: "/api/front/state", Header: map[string]string{"Tailscale-User-Login": l.OwnerLogin}})
	if err != nil || r.Status != 403 || r.Session() != "" {
		t.Fatalf("a forged login through Serve: %v %d %q (want 403 and no session)", err, r.Status, r.Session())
	}
}

// A tagged node has no login: Serve sets no header, and no session is minted.
func TestServeSocketMintsNothingForATaggedNode(t *testing.T) {
	l := live(t)
	tagged, err := l.TS.ServeFor(l.Tag, l.Laptop.Sock(auth.ServeSocket))
	if err != nil {
		t.Fatal(err)
	}
	defer tagged.Close()
	r, err := authtest.Do(tagged.URL, authtest.Request{Path: "/api/front/state"})
	if err != nil || r.Session() != "" {
		t.Fatalf("a tagged node: %v %d session %q (want none)", err, r.Status, r.Session())
	}
}

// V0.130, #226: an upload, its read-back and a preview go through Serve to the
// front and on to its host. The preview's CSP names the public ts.net name.
func TestUploadAndPreviewThroughServe(t *testing.T) {
	l := live(t)
	first, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/front/state"})
	if err != nil || first.Session() == "" {
		t.Fatalf("sign in: %v %d", err, first.Status)
	}
	session := first.Session()
	page := "<!doctype html><title>pin</title><p>hello</p>"
	up, err := authtest.Do(l.Phones.URL, authtest.Request{Method: "POST", Path: "/api/h/" + l.ID + "/attachments", Cookie: session,
		Header: map[string]string{"Content-Type": "text/html", "X-Fyr-Filename": "pin.html"}, Body: strings.NewReader(page)})
	if err != nil || up.Status != 200 {
		t.Fatalf("upload through Serve: %v %d %s", err, up.Status, up.Body)
	}
	var id string
	if i := strings.Index(string(up.Body), `"id":"`); i >= 0 {
		id = strings.SplitN(string(up.Body)[i+6:], `"`, 2)[0]
	}
	if id == "" {
		t.Fatalf("no id in %s", up.Body)
	}
	got, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/h/" + l.ID + "/attachments/" + id, Cookie: session})
	if err != nil || got.Status != 200 || string(got.Body) != page {
		t.Fatalf("read back through Serve: %v %d %q", err, got.Status, got.Body)
	}
	pv, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/h/" + l.ID + "/previews/" + id, Cookie: session})
	if err != nil || pv.Status != 200 {
		t.Fatalf("preview through Serve: %v %d %s", err, pv.Status, pv.Body)
	}
	csp := pv.Header.Get("Content-Security-Policy")
	if want := "https://" + l.Self.Name + ":7443/api/h/" + l.ID + "/previews/" + id + "/"; !strings.Contains(csp, want) {
		t.Fatalf("the preview's CSP does not name the public origin %q:\n%s", want, csp)
	}
}

// V0.130, #226: the live view (SSE) goes through Serve and the front, and its
// first frame arrives.
func TestLiveViewThroughServe(t *testing.T) {
	l := live(t)
	first, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/front/state"})
	if err != nil || first.Session() == "" {
		t.Fatalf("sign in: %v %d", err, first.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.Phones.URL+"/api/front/stream", nil)
	req.AddCookie(&http.Cookie{Name: auth.Cookie, Value: first.Session()})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if resp.StatusCode != 200 || !strings.HasPrefix(line, "event: hello") {
		t.Fatalf("the stream through Serve: %d %q %v", resp.StatusCode, line, err)
	}
}

// V0.51: the front follows its host. The laptop's front, with a static hosts
// entry, shows the server online and reaches it.
func TestFrontFollowsItsHost(t *testing.T) {
	l := live(t)
	hosts, err := l.WaitHostsOnline(l.Local, 15*time.Second, "laptop", "server")
	if err != nil || !hosts["laptop"] || !hosts["server"] {
		t.Fatalf("hosts online: %v %v", hosts, err)
	}
	r, err := l.Laptop.Get(l.Laptop.HostPath(l.Local, "server", "/profiles"), l.Local)
	if err != nil || r.Status != 200 {
		t.Fatalf("a call to the server host through the front: %v %d %s", err, r.Status, r.Body)
	}
}

// V0.51 (way E): a host's tailnet listener serves a front by the whois of its
// connection. It cannot bind a 100.x address in a test, so PeerGuard runs on
// loopback against the fake tailscaled, which maps 127.0.0.1 to a node.
func TestPeerGuardWhoisLink(t *testing.T) {
	l := live(t)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	srv := newLoopbackServer(t, server.PeerGuard(l.OwnerLogin, tailnet.Peers{Socket: l.TS.Socket}, ok))
	cases := []struct {
		name string
		node func() // makes 127.0.0.1 this node
		want int
	}{
		{"the owner's untagged node", func() { l.TS.AddNode(withIP(l.Phone, "127.0.0.1")) }, 200},
		{"another tailnet user", func() { l.TS.RemoveNode(l.Phone.Name); l.TS.AddNode(withIP(l.Other, "127.0.0.1")) }, 403},
		{"a tagged node", func() { l.TS.RemoveNode(l.Other.Name); l.TS.AddNode(withIP(l.Tag, "127.0.0.1")) }, 403},
	}
	for _, c := range cases { // in order: one address, one node at a time
		c.node()
		r, err := authtest.Do(srv, authtest.Request{Path: "/api/front/state"})
		if err != nil || r.Status != c.want {
			t.Errorf("%s: %v %d (want %d)", c.name, err, r.Status, c.want)
		}
	}
}
