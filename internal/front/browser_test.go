package front

import (
	"encoding/json"
	"github.com/meros/fyr/internal/httpguard"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/meros/fyr/internal/push"
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

type endpointPush struct {
	subs   map[string]string
	hidden push.Hidden
	prefs  *push.Prefs
	one    []string // the endpoints NotifyOne was asked for
}

func (f *endpointPush) PublicKey() string { return "BPUB" }
func (f *endpointPush) Subscribe(user string, sub push.Subscription, device string) error {
	f.subs[sub.Endpoint] = user + "/" + device
	return nil
}
func (f *endpointPush) Unsubscribe(endpoint string) error       { delete(f.subs, endpoint); return nil }
func (f *endpointPush) Notify(user string, pl push.Payload) int { return len(f.subs) }
func (f *endpointPush) Known(endpoint string) bool              { _, ok := f.subs[endpoint]; return ok }
func (f *endpointPush) SetHidden(endpoint string, h push.Hidden) (bool, error) {
	f.hidden = h
	return f.Known(endpoint), nil
}

func (f *endpointPush) SetPrefs(endpoint string, pr push.Prefs) (bool, error) {
	f.prefs = &pr
	return f.Known(endpoint), nil
}
func (f *endpointPush) NotifyOne(user, endpoint string, pl push.Payload) int {
	f.one = append(f.one, endpoint)
	if f.Known(endpoint) {
		return 1
	}
	return 0
}

// SPEC 0.5 V0.15: key, subscribe, test and unsubscribe.
func TestPushEndpoints(t *testing.T) {
	fp := &endpointPush{subs: map[string]string{}}
	mux := http.NewServeMux()
	pushRoutes(mux, fp, nil)
	hs := httptest.NewServer(httpguard.Browser("", mux))
	defer hs.Close()
	r := do(t, "GET", hs.URL+"/api/front/push/key", "", nil, "")
	var k map[string]string
	json.NewDecoder(r.Body).Decode(&k)
	if k["publicKey"] != "BPUB" {
		t.Fatalf("key %v", k)
	}
	h := map[string]string{"X-Fyr": "1"}
	body := `{"subscription":{"endpoint":"https://push/e1","keys":{"p256dh":"p","auth":"a"}},"device":"phone"}`
	if r := do(t, "POST", hs.URL+"/api/front/push/subscribe", "", nil, body); r.StatusCode != 403 {
		t.Fatalf("subscribe without X-Fyr: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/front/push/subscribe", "", h, body); r.StatusCode != 200 || fp.subs["https://push/e1"] != "owner/phone" {
		t.Fatalf("subscribe: %d %v", r.StatusCode, fp.subs)
	}
	// V0.207: what the device hides reaches the pusher.
	hide := `{"endpoint":"https://push/e1","hosts":["lab"],"projects":[{"host":"desk","id":"p1"}]}`
	if r := do(t, "POST", hs.URL+"/api/front/push/hidden", "", h, hide); r.StatusCode != 200 {
		t.Fatalf("hidden: %d", r.StatusCode)
	}
	if len(fp.hidden.Hosts) != 1 || fp.hidden.Hosts[0] != "lab" || len(fp.hidden.Projects) != 1 || fp.hidden.Projects[0].ID != "p1" {
		t.Fatalf("hidden: %+v", fp.hidden)
	}
	r = do(t, "POST", hs.URL+"/api/front/push/test", "", h, "")
	var res map[string]int
	json.NewDecoder(r.Body).Decode(&res)
	if res["delivered"] != 1 {
		t.Fatalf("test: %v", res)
	}
	// V0.notification-polish: the test goes to the device that asked, and a
	// device says whether it wants finished threads (on when it says nothing).
	r = do(t, "POST", hs.URL+"/api/front/push/test", "", h, `{"endpoint":"https://push/e1"}`)
	res = nil
	json.NewDecoder(r.Body).Decode(&res)
	if res["delivered"] != 1 || len(fp.one) != 1 || fp.one[0] != "https://push/e1" {
		t.Fatalf("test to one device: %v %v", res, fp.one)
	}
	if r := do(t, "POST", hs.URL+"/api/front/push/prefs", "", h, `{"endpoint":"https://push/e1","finished":false,"quotaReset":true}`); r.StatusCode != 200 || fp.prefs == nil || fp.prefs.Finished || !fp.prefs.QuotaReset {
		t.Fatalf("prefs off+quota: %d %+v", r.StatusCode, fp.prefs)
	}
	if r := do(t, "POST", hs.URL+"/api/front/push/prefs", "", h, `{"endpoint":"https://push/e1"}`); r.StatusCode != 200 || !fp.prefs.Finished || fp.prefs.QuotaReset {
		t.Fatalf("prefs without a choice: %d %+v", r.StatusCode, fp.prefs)
	}
	if r := do(t, "POST", hs.URL+"/api/front/push/prefs", "", h, `{"finished":false}`); r.StatusCode != 400 {
		t.Fatalf("prefs without an endpoint: %d", r.StatusCode)
	}
	known := func() bool {
		r := do(t, "POST", hs.URL+"/api/front/push/status", "", h, `{"endpoint":"https://push/e1"}`)
		var k map[string]bool
		json.NewDecoder(r.Body).Decode(&k)
		return k["known"]
	}
	if !known() {
		t.Fatal("status: a stored subscription is not known")
	}
	do(t, "POST", hs.URL+"/api/front/push/unsubscribe", "", h, `{"endpoint":"https://push/e1"}`)
	if len(fp.subs) != 0 || known() {
		t.Fatal("unsubscribe")
	}
}

// SPEC 0.5 V0.15: the manifest has its media type; the service worker is
// never cached, so a new build's worker is picked up.
func TestPWAFiles(t *testing.T) {
	hs := httptest.NewServer(httpguard.Browser("", webClient(fstest.MapFS{
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
