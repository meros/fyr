// SPEC 0.5 V0.183: the request guards a host and a front share, moved out of
// internal/server, mark and check requests as before (V0.14, V0.130, decision
// 0030 3.5).
package httpguard

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestV183TheWayInIsMarked(t *testing.T) {
	var lan, serve bool
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { lan, serve = IsViaLAN(r), IsViaServe(r) })
	probe.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if lan || serve {
		t.Fatal("a plain request is marked")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{}
	ViaLAN(probe).ServeHTTP(httptest.NewRecorder(), r)
	if !lan || serve {
		t.Fatalf("lan %v serve %v", lan, serve)
	}
	w := httptest.NewRecorder()
	ViaLAN(probe).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plain HTTP on the LAN listener: %d", w.Code)
	}
	ViaServe(probe).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if lan || !serve {
		t.Fatalf("lan %v serve %v", lan, serve)
	}
}

func TestV183BrowserGuardsLoopbackAndTheOwner(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := Browser("me@example.com", ok)
	for _, c := range []struct {
		host, login, method, xfyr string
		want                      int
	}{
		{"127.0.0.1:7777", "", "GET", "", 200},
		{"fyr.localhost:7777", "", "GET", "", 200},
		{"evil.example:7777", "", "GET", "", 403},
		{"127.0.0.1:7777", "", "POST", "", 403},
		{"127.0.0.1:7777", "", "POST", "1", 200},
		{"desk.tail1.ts.net", "me@example.com", "GET", "", 200},
		{"desk.tail1.ts.net", "other@example.com", "GET", "", 403},
		{"127.0.0.1:7777", "me@example.com", "GET", "", 403}, // the header needs a ts.net host
	} {
		r := httptest.NewRequest(c.method, "/api/x", nil)
		r.Host = c.host
		if c.login != "" {
			r.Header.Set("Tailscale-User-Login", c.login)
		}
		if c.xfyr != "" {
			r.Header.Set("X-Fyr", c.xfyr)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%+v: %d", c, w.Code)
		}
	}
}

func TestV183Hosts(t *testing.T) {
	for s, want := range map[string]bool{"desk.ts.net": true, "desk.tail1.ts.net:443": true, ".ts.net": false, "example.com": false} {
		if TailnetHost(s) != want {
			t.Errorf("TailnetHost(%q) != %v", s, want)
		}
	}
	for s, want := range map[string]bool{"a.b:80": true, "[::1]:7": true, "a b": false, "http://x": false} {
		if ValidHost(s) != want {
			t.Errorf("ValidHost(%q) != %v", s, want)
		}
	}
}
