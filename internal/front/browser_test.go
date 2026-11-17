package front

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/server"
)

// The browser-only routes moved here from the host's server (#38); the
// tests came with them.

// do sends one request to a browser handler.
func do(t *testing.T, method, url, host string, hdr map[string]string, body string) *http.Response {
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if host != "" {
		req.Host = host
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

type endpointPush struct{ subs map[string]string }

func (f *endpointPush) PublicKey() string { return "BPUB" }
func (f *endpointPush) Subscribe(user string, sub push.Subscription, device string) error {
	f.subs[sub.Endpoint] = user + "/" + device
	return nil
}
func (f *endpointPush) Unsubscribe(endpoint string) error       { delete(f.subs, endpoint); return nil }
func (f *endpointPush) Notify(user string, pl push.Payload) int { return len(f.subs) }
func (f *endpointPush) Known(endpoint string) bool              { _, ok := f.subs[endpoint]; return ok }

// SPEC 0.5 V0.15: key, subscribe, test and unsubscribe.
func TestPushEndpoints(t *testing.T) {
	fp := &endpointPush{subs: map[string]string{}}
	mux := http.NewServeMux()
	pushRoutes(mux, fp, nil)
	hs := httptest.NewServer(server.Browser("", mux))
	defer hs.Close()
	r := do(t, "GET", hs.URL+"/api/push/key", "", nil, "")
	var k map[string]string
	json.NewDecoder(r.Body).Decode(&k)
	if k["publicKey"] != "BPUB" {
		t.Fatalf("key %v", k)
	}
	h := map[string]string{"X-Fyr": "1"}
	body := `{"subscription":{"endpoint":"https://push/e1","keys":{"p256dh":"p","auth":"a"}},"device":"phone"}`
	if r := do(t, "POST", hs.URL+"/api/push/subscribe", "", nil, body); r.StatusCode != 403 {
		t.Fatalf("subscribe without X-Fyr: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/push/subscribe", "", h, body); r.StatusCode != 200 || fp.subs["https://push/e1"] != "owner/phone" {
		t.Fatalf("subscribe: %d %v", r.StatusCode, fp.subs)
	}
	r = do(t, "POST", hs.URL+"/api/push/test", "", h, "")
	var res map[string]int
	json.NewDecoder(r.Body).Decode(&res)
	if res["delivered"] != 1 {
		t.Fatalf("test: %v", res)
	}
	known := func() bool {
		r := do(t, "POST", hs.URL+"/api/push/status", "", h, `{"endpoint":"https://push/e1"}`)
		var k map[string]bool
		json.NewDecoder(r.Body).Decode(&k)
		return k["known"]
	}
	if !known() {
		t.Fatal("status: a stored subscription is not known")
	}
	do(t, "POST", hs.URL+"/api/push/unsubscribe", "", h, `{"endpoint":"https://push/e1"}`)
	if len(fp.subs) != 0 || known() {
		t.Fatal("unsubscribe")
	}
}

// SPEC 0.5 V0.15: the manifest has its media type; the service worker is
// never cached, so a new build's worker is picked up.
func TestPWAFiles(t *testing.T) {
	hs := httptest.NewServer(server.Browser("", webClient(fstest.MapFS{
		"index.html": {Data: []byte("x")}, "manifest.webmanifest": {Data: []byte("{}")}, "sw.js": {Data: []byte("//")}})))
	defer hs.Close()
	r := do(t, "GET", hs.URL+"/manifest.webmanifest", "", nil, "")
	if ct := r.Header.Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("manifest type %q", ct)
	}
	r = do(t, "GET", hs.URL+"/sw.js", "", nil, "")
	if r.Header.Get("Cache-Control") != "no-cache" || !strings.Contains(r.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("sw headers %v", r.Header)
	}
}

// SPEC 0.5 V0.20: a presence beat marks a thread as watched.
func TestPresenceEndpoint(t *testing.T) {
	p := &notify.Presence{}
	mux := http.NewServeMux()
	presenceRoute(mux, p)
	hs := httptest.NewServer(server.Browser("", mux))
	defer hs.Close()
	if r := do(t, "POST", hs.URL+"/api/presence", "", map[string]string{"X-Fyr": "1"}, `{"threadId":"t1","device":"d1"}`); r.StatusCode != 204 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if !p.Watched("t1", time.Now()) {
		t.Fatal("not watched after a beat")
	}
	do(t, "POST", hs.URL+"/api/presence", "", map[string]string{"X-Fyr": "1"}, `{"threadId":"","device":"d1"}`)
	if p.Watched("t1", time.Now()) {
		t.Fatal("still watched after the client stopped showing it")
	}
}
