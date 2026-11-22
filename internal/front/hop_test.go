package front

// SPEC V0.156, decision 0030 4.2 "the hop": every route of the front's mux is
// requested through the Serve emulator and through loopback, against a
// recording host. Each request that reaches the host carries Fyr-Actor for the
// session's device and Fyr-Origin, and none of Cookie, Authorization,
// Tailscale-*, X-Forwarded-*, Origin, Referer or the browser's Host.

import (
	"context"
	"github.com/meros/fyr/internal/httpguard"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/testkit/authtest"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) GetKV(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}
func (k *memKV) SetKV(key, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[key] = v
	return nil
}

type seenReq struct {
	method, path, host string
	h                  http.Header
}

// routeClass says what a route of the front does with a call. A new route must
// be added here: that is the point of the walk.
type routeClass int

const (
	hops  routeClass = iota // the call is forwarded to a host as the session's device
	local                   // the front answers it itself
)

var routes = map[string]routeClass{
	// decision 0031: the host in the path forwards, the front's own routes answer,
	// and any other /api/ path is 404 (V0.old-routes-end).
	"/api/":                 local,
	"/api/h/{id}/{rest...}": hops, "GET /api/front/state": local, "GET /api/front/stream": local, "POST /api/front/command": local,
	"POST /api/front/presence": local, "GET /api/front/auth/state": local, "POST /api/front/auth/approve": local, "POST /api/front/auth/deny": local,
	"POST /api/front/auth/revoke": local, "POST /api/front/auth/add": local, "POST /api/front/auth/mode": local, "/mcp": local, "/": local,
}

func TestHopCarriesThePrincipalOnEveryRoute(t *testing.T) {
	const owner = "me@example.com"
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hostSrv := &server.Server{Engine: engine.New(st), BuildID: "host-build", Host: "desk", ID: "HOST-0000-0000-0000", Attach: &attach.Store{Dir: t.TempDir()}}
	var mu sync.Mutex
	var seen []seenReq
	inner := hostSrv.PipeHandler()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, seenReq{r.Method, r.URL.RequestURI(), r.Host, r.Header.Clone()})
		mu.Unlock()
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(hs.Close)
	since := func(n int) []seenReq { mu.Lock(); defer mu.Unlock(); return append([]seenReq(nil), seen[n:]...) }
	mark := func() int { mu.Lock(); defer mu.Unlock(); return len(seen) }

	a, err := auth.Open(&memKV{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, dev := a.Mint(auth.HowTailnet, "test")
	f := New(Options{ID: "FRNT-0000-0000-0000", Name: "front", BuildID: "front-build", OwnerLogin: owner, Auth: a, Store: &memKV{},
		MinBackoff: 50 * time.Millisecond, Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}}, []HostConfig{{Name: "desk", URL: hs.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	lo := httptest.NewServer(f.Handler())
	t.Cleanup(func() { cancel(); lo.Close() })
	online(t, lo.URL, 1)

	tsDir, rm, err := authtest.NewDataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rm)
	phone := authtest.Node{Name: "phone.tail.ts.net", IP: "100.64.0.2", UserID: 1, Online: true}
	ts, err := authtest.NewTailscaled(tsDir, authtest.Node{Name: "desk.tail.ts.net", IP: "100.64.0.1", UserID: 1, Online: true}, authtest.User{ID: 1, Login: owner})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	sock, stop := authtest.UnixServer(t, httpguard.ServeSocket("desk.tail.ts.net:7443", f.Handler()))
	t.Cleanup(stop)
	sv, err := ts.ServeFor(phone, sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sv.Close)

	ways := []struct{ name, base, origin string }{
		{"loopback", lo.URL, lo.URL},
		{"Serve", sv.URL, "https://desk.tail.ts.net:7443"},
	}
	for _, pat := range f.RoutePatterns() {
		class, known := routes[pat]
		if !known {
			t.Errorf("route %q is not in the hop table: say whether the front forwards it", pat)
			continue
		}
		for _, w := range ways {
			for _, method := range methodsOf(pat) {
				path := concretize(pat)
				before := mark()
				req, _ := http.NewRequest(method, w.base+path, strings.NewReader(`{"commandId":"hop-1","action":"nope"}`))
				req.AddCookie(&http.Cookie{Name: auth.Cookie, Value: session})
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Fyr", "1")
				req.Header.Set("Authorization", "Bearer browser-token")
				req.Header.Set("Referer", w.base+"/somewhere")
				req.Header.Set("Origin", w.origin)
				req.Header.Set("X-Fyr-Filename", "a.html")
				if w.name == "Serve" {
					req.Header.Set("X-Forwarded-For", "6.6.6.6")
				}
				if strings.HasSuffix(pat, "/stream") {
					c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					req = req.WithContext(c)
					defer cancel()
				}
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
					resp.Body.Close()
				}
				var asDevice int
				for _, s := range since(before) {
					p, perr := authn.Parse(s.h.Get(authn.Header))
					if perr != nil {
						t.Errorf("%s %s %s: the host got Fyr-Actor %q: %v", w.name, method, pat, s.h.Get(authn.Header), perr)
						continue
					}
					if p.Kind == authn.KindDevice {
						asDevice++
						if p.ID != dev.ID || p.How != authn.HowTailnet {
							t.Errorf("%s %s %s: the host saw %+v (want device %s, tailnet)", w.name, method, pat, p, dev.ID)
						}
						if got := s.h.Get(authn.OriginHeader); got != w.origin {
							t.Errorf("%s %s %s: Fyr-Origin %q (want %q)", w.name, method, pat, got, w.origin)
						}
					}
				}
				if class == hops && asDevice == 0 {
					t.Errorf("%s %s %s: the call never reached the host as the device", w.name, method, pat)
				}
			}
		}
	}

	// V0.host-ids: the six settings reads on the prefix, which the front reads
	// itself to keep the answer, still reach the host as the device.
	for _, path := range settingsPaths {
		before := mark()
		req, _ := http.NewRequest("GET", lo.URL+"/api/h/HOST-0000-0000-0000"+strings.TrimPrefix(path, "/api"), nil)
		req.AddCookie(&http.Cookie{Name: auth.Cookie, Value: session})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		got := since(before)
		if len(got) != 1 {
			t.Errorf("settings read %s: the host saw %d requests", path, len(got))
			continue
		}
		if p, err := authn.Parse(got[0].h.Get(authn.Header)); err != nil || p.Kind != authn.KindDevice || p.ID != dev.ID {
			t.Errorf("settings read %s: the host saw %+v (%v), want device %s", path, p, err, dev.ID)
		}
	}

	// Whatever the host saw, from any caller, carries nothing of the browser.
	mu.Lock()
	defer mu.Unlock()
	for _, s := range seen {
		for _, h := range []string{"Cookie", "Authorization", "Origin", "Referer", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Tailscale-User-Login", "Sec-Fetch-Site"} {
			if _, ok := s.h[http.CanonicalHeaderKey(h)]; ok {
				t.Errorf("%s %s reached the host with %s", s.method, s.path, h)
			}
		}
		if s.host != strings.TrimPrefix(hs.URL, "http://") {
			t.Errorf("%s %s reached the host with Host %q (want its own %q)", s.method, s.path, s.host, strings.TrimPrefix(hs.URL, "http://"))
		}
		if _, err := authn.Parse(s.h.Get(authn.Header)); err != nil {
			t.Errorf("%s %s: Fyr-Actor %q: %v", s.method, s.path, s.h.Get(authn.Header), err)
		}
	}
}

// methodsOf: the patterns without a method take every method the browser API
// uses.
func methodsOf(pat string) []string {
	if m, _, ok := strings.Cut(pat, " "); ok {
		return []string{m}
	}
	return []string{"GET", "POST"}
}

func concretize(pat string) string {
	if _, p, ok := strings.Cut(pat, " "); ok {
		pat = p
	}
	id := "t_probe"
	if strings.HasPrefix(pat, "/api/h/") {
		id = "HOST-0000-0000-0000" // the host's install id
	}
	r := strings.NewReplacer("{id}", id, "{rest...}", "x", "{path...}", "x/y", "{seq}", "1", "{build}", "b", "{name}", "n")
	return r.Replace(pat)
}
