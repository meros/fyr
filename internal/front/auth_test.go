package front

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/meros/fyr/internal/httpguard"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/auth"
)

type kv map[string]string

func (k kv) GetKV(key string) (string, error) { return k[key], nil }
func (k kv) SetKV(key, v string) error        { k[key] = v; return nil }

func authFront(t *testing.T) (*Front, *auth.Store) {
	st, err := auth.Open(kv{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newHost(t, "desk")
	f := New(Options{Name: "desk", BuildID: "b", OwnerLogin: "me@example.com", Auth: st,
		Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}, MinBackoff: 50 * time.Millisecond},
		[]HostConfig{{Name: "desk", URL: h.hs.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	t.Cleanup(cancel)
	return f, st
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == auth.Cookie {
			return c
		}
	}
	return nil
}

func serveOne(h http.Handler, r *http.Request) *http.Response {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Result()
}

// SPEC V0.130 (2): the owner socket's one-time link, opened on
// fyr.localhost, sets an HttpOnly, SameSite=Strict session once.
func TestV130OnceLinkSetsTheSession(t *testing.T) {
	f, st := authFront(t)
	h := f.Handler()
	// The owner socket hands out the link.
	r := httptest.NewRequest("POST", "http://owner/session/once", nil)
	resp := serveOne(f.OwnerHandler("http://fyr.localhost:7777"), r)
	var got struct{ URL string }
	json.NewDecoder(resp.Body).Decode(&got)
	pre := "http://fyr.localhost:7777/auth/once#"
	if resp.StatusCode != 200 || !strings.HasPrefix(got.URL, pre) {
		t.Fatalf("owner socket: %d %q", resp.StatusCode, got.URL)
	}
	tok := strings.TrimPrefix(got.URL, pre)

	page := serveOne(h, httptest.NewRequest("GET", "http://fyr.localhost:7777/auth/once", nil))
	if page.StatusCode != 200 || !strings.Contains(page.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("the page: %d", page.StatusCode)
	}
	post := func() *http.Response {
		r := httptest.NewRequest("POST", "http://fyr.localhost:7777/auth/once", bytes.NewBufferString(`{"token":"`+tok+`"}`))
		r.Header.Set("X-Fyr", "1")
		r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Chrome/140.0")
		return serveOne(h, r)
	}
	resp = post()
	c := sessionCookie(resp)
	if resp.StatusCode != 200 || c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("redeem: %d %+v", resp.StatusCode, c)
	}
	if d, ok := st.Lookup(c.Value); !ok || d.How != auth.HowLocal || !strings.HasPrefix(d.Name, "Chrome on Linux") {
		t.Fatalf("device %+v", d)
	}
	if again := post(); again.StatusCode != http.StatusGone || sessionCookie(again) != nil {
		t.Fatalf("a used link: %d", again.StatusCode)
	}
}

// SPEC V0.130 (3): through Serve's socket the owner's login mints a tailnet
// session (Secure: the browser is on HTTPS); with it, no second one.
func TestV130ServeSocketMintsATailnetSession(t *testing.T) {
	f, st := authFront(t)
	h := httpguard.ViaServe(f.Handler())
	req := func(c *http.Cookie) *http.Request {
		r := httptest.NewRequest("GET", "http://desk.tail.ts.net/api/front/state", nil)
		r.Host = "desk.tail.ts.net"
		r.Header.Set("Tailscale-User-Login", "me@example.com")
		r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 15; Pixel 9) Chrome/140.0 Mobile")
		if c != nil {
			r.AddCookie(c)
		}
		return r
	}
	resp := serveOne(h, req(nil))
	c := sessionCookie(resp)
	if resp.StatusCode != 200 || c == nil || !c.Secure || !c.HttpOnly {
		t.Fatalf("serve: %d %+v", resp.StatusCode, c)
	}
	if d, ok := st.Lookup(c.Value); !ok || d.How != auth.HowTailnet || !strings.HasPrefix(d.Name, "Chrome on Android") {
		t.Fatalf("device %+v", d)
	}
	if again := serveOne(h, req(c)); sessionCookie(again) != nil || len(st.Devices()) != 1 {
		t.Fatalf("a second session for a browser that has one (%d devices)", len(st.Devices()))
	}
}

// V0.one-device-per-browser (1), (2): another install's probe of
// /.well-known/fyr and a file of the client make no device; the requests a page
// sends together without a cookie make one.
func TestServeSocketMakesOneDevicePerBrowser(t *testing.T) {
	f, st := authFront(t)
	h := httpguard.ViaServe(f.Handler())
	get := func(path, agent string) *http.Response {
		r := httptest.NewRequest("GET", "http://desk.tail.ts.net"+path, nil)
		r.Host = "desk.tail.ts.net"
		r.Header.Set("Tailscale-User-Login", "me@example.com")
		r.Header.Set("User-Agent", agent)
		return serveOne(h, r)
	}
	for i := 0; i < 5; i++ {
		for _, p := range []string{"/.well-known/fyr", "/assets/app.js"} {
			if resp := get(p, "Go-http-client/2.0"); sessionCookie(resp) != nil {
				t.Fatalf("%s set a session", p)
			}
		}
	}
	if n := len(st.Devices()); n != 0 {
		t.Fatalf("%d devices from requests that need no session", n)
	}
	// Without tailscaled (this front) the node is unknown: each such request
	// still gets its own session, as before.
	if resp := get("/api/front/state", "Go-http-client/2.0"); resp.StatusCode != 200 || sessionCookie(resp) == nil {
		t.Fatalf("an API request through Serve: %d", resp.StatusCode)
	}
}

// SPEC V0.130 (3), (5): the same header over TCP mints nothing; in observe
// mode the request is still served (and logged).
func TestV130HeaderOverTCPMintsNothing(t *testing.T) {
	f, st := authFront(t)
	r := httptest.NewRequest("GET", "http://desk.tail.ts.net/api/front/state", nil)
	r.Host = "desk.tail.ts.net"
	r.Header.Set("Tailscale-User-Login", "me@example.com")
	resp := serveOne(f.Handler(), r)
	if resp.StatusCode != 200 || sessionCookie(resp) != nil || len(st.Devices()) != 0 {
		t.Fatalf("tcp header: %d, %d devices", resp.StatusCode, len(st.Devices()))
	}
}

// SPEC V0.130 (4): fyr.localhost is a loopback name for the guard.
func TestV130FyrLocalhostPassesTheGuard(t *testing.T) {
	f, _ := authFront(t)
	resp := serveOne(f.Handler(), httptest.NewRequest("GET", "http://fyr.localhost:7777/api/front/state", nil))
	if resp.StatusCode != 200 {
		t.Fatalf("fyr.localhost: %d", resp.StatusCode)
	}
}
