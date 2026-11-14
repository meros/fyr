package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *engine.Engine) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(st)
	s := &Server{Engine: e, BuildID: "b1", Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}}
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(func() { hs.Close(); st.Close() })
	return hs, e
}

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

// SPEC 0.5 V0.2: non-loopback Host is refused (DNS rebinding).
func TestGuardRefusesForeignHost(t *testing.T) {
	hs, _ := newServer(t)
	if r := do(t, "GET", hs.URL+"/api/state", "evil.example:80", nil, ""); r.StatusCode != 403 {
		t.Fatalf("foreign host: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/state", "", nil, ""); r.StatusCode != 200 {
		t.Fatalf("loopback host: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/", "localhost:7777", nil, ""); r.StatusCode != 200 {
		t.Fatalf("localhost: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.2: commands need X-Fyr (blocks cross-site form posts).
func TestCommandNeedsHeader(t *testing.T) {
	hs, _ := newServer(t)
	body := `{"commandId":"c1","action":"thread.markSeen","threadId":"nope"}`
	if r := do(t, "POST", hs.URL+"/api/command", "", nil, body); r.StatusCode != 403 {
		t.Fatalf("no header: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, body); r.StatusCode != 404 {
		t.Fatalf("unknown thread: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.2, V0.13: the stream says hello with the build id, then
// carries thread upserts.
func TestStreamHelloAndUpserts(t *testing.T) {
	hs, _ := newServer(t)
	resp := do(t, "GET", hs.URL+"/api/stream", "", nil, "")
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	next := func() (string, string) {
		var ev, data string
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				ev = line[7:]
			case strings.HasPrefix(line, "data: "):
				data = line[6:]
			case line == "" && ev != "":
				return ev, data
			}
		}
	}
	ev, data := next()
	if ev != "hello" || !strings.Contains(data, `"buildId":"b1"`) {
		t.Fatalf("%s %s", ev, data)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"},
			`{"commandId":"c2","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	}()
	ev, data = next()
	var dto map[string]any
	json.Unmarshal([]byte(data), &dto)
	if ev != "thread" || dto["title"] != "hi" || dto["attention"] == nil {
		t.Fatalf("%s %s", ev, data)
	}
}

func TestStateHasThreadsAndActions(t *testing.T) {
	hs, _ := newServer(t)
	do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"},
		`{"commandId":"c3","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	r := do(t, "GET", hs.URL+"/api/state", "", nil, "")
	var s struct {
		BuildID  string           `json:"buildId"`
		Host     string           `json:"host"`
		Threads  []map[string]any `json:"threads"`
		Actions  []map[string]any `json:"actions"`
		Projects []map[string]any `json:"projects"`
	}
	json.NewDecoder(r.Body).Decode(&s)
	if s.Host == "" || s.BuildID != "b1" || len(s.Threads) != 1 || len(s.Actions) < 10 || len(s.Projects) != 1 {
		t.Fatalf("%+v", s)
	}
}

// SPEC 0.5 V0.14: over the tailnet (Tailscale Serve) the identity header
// must be the owner's login; any other login, or none from a non-loopback
// host, is refused.
func TestTailnetIdentity(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	s := &Server{Engine: engine.New(st), BuildID: "b1", OwnerLogin: "owner@example.com"}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	ts := "meros-laptop.example.ts.net"
	if r := do(t, "GET", hs.URL+"/api/state", ts, map[string]string{"Tailscale-User-Login": "owner@example.com"}, ""); r.StatusCode != 200 {
		t.Fatalf("owner over tailnet: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/state", ts, map[string]string{"Tailscale-User-Login": "guest@example.com"}, ""); r.StatusCode != 403 {
		t.Fatalf("other login: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/state", ts, nil, ""); r.StatusCode != 403 {
		t.Fatalf("no login from tailnet host: %d", r.StatusCode)
	}
	if r := do(t, "POST", hs.URL+"/api/command", ts, map[string]string{"Tailscale-User-Login": "owner@example.com"}, `{}`); r.StatusCode != 403 {
		t.Fatalf("POST without X-Fyr over tailnet: %d", r.StatusCode)
	}
	s2 := &Server{Engine: engine.New(st), BuildID: "b1"}
	hs2 := httptest.NewServer(s2.Handler())
	defer hs2.Close()
	if r := do(t, "GET", hs2.URL+"/api/state", ts, map[string]string{"Tailscale-User-Login": "owner@example.com"}, ""); r.StatusCode != 403 {
		t.Fatalf("no owner configured must refuse tailnet: %d", r.StatusCode)
	}
}

type fakePush struct{ subs map[string]string }

func (f *fakePush) PublicKey() string { return "BPUB" }
func (f *fakePush) Subscribe(user string, sub push.Subscription, device string) error {
	f.subs[sub.Endpoint] = user + "/" + device
	return nil
}
func (f *fakePush) Unsubscribe(endpoint string) error       { delete(f.subs, endpoint); return nil }
func (f *fakePush) Notify(user string, pl push.Payload) int { return len(f.subs) }

// SPEC 0.5 V0.15: key, subscribe, test and unsubscribe.
func TestPushEndpoints(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	fp := &fakePush{subs: map[string]string{}}
	s := &Server{Engine: engine.New(st), BuildID: "b1", Push: fp}
	hs := httptest.NewServer(s.Handler())
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
	do(t, "POST", hs.URL+"/api/push/unsubscribe", "", h, `{"endpoint":"https://push/e1"}`)
	if len(fp.subs) != 0 {
		t.Fatal("unsubscribe")
	}
}

// SPEC 0.5 V0.15: the manifest has its media type; the service worker is
// never cached, so a new build's worker is picked up.
func TestPWAFiles(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	s := &Server{Engine: engine.New(st), BuildID: "b1", Web: fstest.MapFS{
		"index.html": {Data: []byte("x")}, "manifest.webmanifest": {Data: []byte("{}")}, "sw.js": {Data: []byte("//")}}}
	hs := httptest.NewServer(s.Handler())
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
