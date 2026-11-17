package front

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// SPEC V0.130 (3), #225: through Serve's socket tailscaled does not pass the
// browser's Host on. The front proxies uploads and previews to its host, whose
// guard needs a ts.net Host for the identity header and whose preview CSP
// names the folder by Host: both must see the public name, over https.
// Loopback is unchanged.
func TestV130ServeSocketProxiesWithThePublicHost(t *testing.T) {
	const owner = "me@example.com"
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &server.Server{Engine: engine.New(st), BuildID: "host-build", Host: "desk", OwnerLogin: owner,
		Attach: &attach.Store{Dir: t.TempDir()}}
	hs := httptest.NewServer(s.PipeHandler()) // decision 0030: a front reaches its host through the pipe or the peer listener, which read Fyr-Origin
	t.Cleanup(func() { hs.Close(); st.Close() })

	f := New(Options{Name: "front", BuildID: "front-build", OwnerLogin: owner, MinBackoff: 50 * time.Millisecond,
		Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}}, []HostConfig{{Name: "desk", URL: hs.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	lo := httptest.NewServer(f.Handler())
	sv := httptest.NewServer(server.ServeSocket("desk.tail.ts.net:7443", f.Handler()))
	t.Cleanup(func() { cancel(); lo.Close(); sv.Close() })
	online(t, lo.URL, 1)

	// viaServe sends what tailscaled sends on the socket: Host localhost and
	// the login; fh is its X-Forwarded-Host, "" for none.
	viaServe := func(method, path, fh, ctype, body string) *http.Response {
		req, _ := http.NewRequest(method, sv.URL+path, strings.NewReader(body))
		req.Host = "localhost"
		req.Header.Set("Tailscale-User-Login", owner)
		if fh != "" {
			req.Header.Set("X-Forwarded-Host", fh)
		}
		if method == "POST" {
			req.Header.Set("X-Fyr", "1")
			req.Header.Set("X-Fyr-Filename", "page.html")
			req.Header.Set("Content-Type", ctype)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	r := viaServe("POST", "/api/attachments", "", "text/html", "<p>hi</p>")
	var up struct{ ID string }
	json.NewDecoder(r.Body).Decode(&up)
	r.Body.Close()
	if r.StatusCode != 200 || up.ID == "" {
		t.Fatalf("upload through Serve: %d %+v", r.StatusCode, up)
	}
	r = viaServe("GET", "/api/attachments/"+up.ID, "", "", "")
	if b, _ := io.ReadAll(r.Body); r.StatusCode != 200 || string(b) != "<p>hi</p>" {
		t.Fatalf("the upload read back through Serve: %d %q", r.StatusCode, b)
	}

	for _, c := range []struct{ fh, want string }{
		{"", "https://desk.tail.ts.net:7443/api/previews/" + up.ID + "/ "},
		{"phone.tail.ts.net:7443", "https://phone.tail.ts.net:7443/api/previews/" + up.ID + "/ "},
		{"evil.example", "https://desk.tail.ts.net:7443/api/previews/" + up.ID + "/ "}, // not a ts.net name
	} {
		r := viaServe("GET", "/api/previews/"+up.ID, c.fh, "", "")
		if csp := r.Header.Get("Content-Security-Policy"); r.StatusCode != 200 || !strings.Contains(csp, "script-src "+c.want) {
			t.Fatalf("preview through Serve (X-Forwarded-Host %q): %d %q", c.fh, r.StatusCode, csp)
		}
	}

	// Loopback: the browser's own Host, http, as before.
	req, _ := http.NewRequest("GET", lo.URL+"/api/previews/"+up.ID, nil)
	r, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if csp := r.Header.Get("Content-Security-Policy"); r.StatusCode != 200 || !strings.Contains(csp, "script-src http://"+strings.TrimPrefix(lo.URL, "http://")+"/api/previews/"+up.ID+"/ ") {
		t.Fatalf("preview over loopback: %d %q", r.StatusCode, csp)
	}
	// The identity header over loopback with a foreign Host is still refused.
	req, _ = http.NewRequest("GET", lo.URL+"/api/previews/"+up.ID, nil)
	req.Host = "localhost"
	req.Header.Set("Tailscale-User-Login", owner)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 403 {
		t.Fatalf("identity header over loopback without ts.net Host: %v %v", err, r.StatusCode)
	}
}
