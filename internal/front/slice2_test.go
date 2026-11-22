package front

import (
	"bufio"
	"context"
	"encoding/json"
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
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// fakePush records what a front sends to its devices.
type fakePush struct {
	mu   sync.Mutex
	sent []push.Payload
	subs []string
}

func (p *fakePush) PublicKey() string { return "key" }
func (p *fakePush) Subscribe(user string, sub push.Subscription, device string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subs = append(p.subs, sub.Endpoint)
	return nil
}
func (p *fakePush) Unsubscribe(string) error                    { return nil }
func (p *fakePush) Known(string) bool                           { return false }
func (p *fakePush) SetHidden(string, push.Hidden) (bool, error) { return false, nil }
func (p *fakePush) SetPrefs(string, push.Prefs) (bool, error)   { return false, nil }
func (p *fakePush) NotifyOne(user, endpoint string, pl push.Payload) int {
	return p.Notify(user, pl)
}
func (p *fakePush) Notify(user string, pl push.Payload) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, pl)
	return 1
}
func (p *fakePush) payloads() []push.Payload {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]push.Payload(nil), p.sent...)
}

// fullHost is a host with attachments and its own profile, as fyrd runs it.
func fullHost(t *testing.T, name, profile string) *host {
	return fullHostID(t, name, profile, testID(name))
}

// fullHostID is fullHost reporting install id id (V0.124).
func fullHostID(t *testing.T, name, profile, id string) *host {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	h := &host{eng: engine.New(st)}
	s := &server.Server{Engine: h.eng, BuildID: "host-build", Host: name, ID: id,
		Attach:   &attach.Store{Dir: t.TempDir()},
		Profiles: []provider.Profile{{ID: profile, Name: profile, Binary: "claude"}}}
	inner := s.Handler()
	h.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.down.Load() {
			panic(http.ErrAbortHandler)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { h.hs.CloseClientConnections(); h.hs.Close(); st.Close() })
	return h
}

type runningFront struct {
	*httptest.Server
	stop func() // ends Run and waits for it (the cache is written)
	f    *Front
}

func startFront(t *testing.T, opt Options, hosts ...HostConfig) *runningFront {
	opt.Name, opt.BuildID, opt.MinBackoff = "front", "front-build", 50*time.Millisecond
	opt.Web = fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}
	f := New(opt, hosts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	fs := httptest.NewServer(f.Handler())
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-done
			fs.CloseClientConnections()
			fs.Close()
		})
	}
	t.Cleanup(stop)
	return &runningFront{fs, stop, f}
}

// frontAttention is the attention state of thread id in the front's own copy
// of its host's thread, as the front's pushes read it. getState does not
// show that copy: its thread list is read through to the host.
func (r *runningFront) frontAttention(id string) string {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	for _, l := range r.f.all() {
		if m := l.raw[id]; m != nil {
			return attentionOf(m)
		}
	}
	return ""
}

func create(t *testing.T, base, host, text string) string {
	t.Helper()
	code, rep := post(t, hostURL(t, base, host)+"/command", `{"commandId":"`+text+`","action":"thread.create","args":{"cwd":"/tmp","text":"`+text+`"}}`)
	id, _ := rep["threadId"].(string)
	if code != 200 || id == "" {
		t.Fatalf("create on %s: %d %v", host, code, rep)
	}
	eventually(t, "thread on the front", func() bool { return threadOf(getState(t, base), id) != nil })
	return id
}

func ask(h *host, id, q string) {
	coretest.Patch(h.eng, id, func(th *core.Thread, now time.Time) []core.Event {
		core.DecisionOpened(th, core.Decision{ID: "d1", Kind: "ask", CreatedAt: now, Questions: []core.Question{{Question: q}}})
		return nil
	})
}

// SPEC 0.5 V0.54: the front sends the push for a question on any of its
// hosts, also while another host is down; a thread a device of this front
// watches gets none.
func TestFrontPushesForAnyHost(t *testing.T) {
	a, b := fullHost(t, "a", "pa"), fullHost(t, "b", "pb")
	p := &fakePush{}
	fs := startFront(t, Options{Push: p}, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	idB := create(t, fs.URL, "b", "on-b")
	idB2 := create(t, fs.URL, "b", "watched")

	a.setDown(true) // one host stops mid-test
	eventually(t, "a offline", func() bool { s := getState(t, fs.URL); return s.Hosts[0]["online"] == false })

	ask(b, idB, "Which colour?")
	eventually(t, "a push for b's question", func() bool { return len(p.payloads()) == 1 })
	pl := p.payloads()[0]
	if pl.ThreadID != idB || pl.State != "asking" || !strings.Contains(pl.Body, "Which colour?") {
		t.Fatalf("payload: %+v", pl)
	}

	// Presence lives on the front now: a watched thread gets no push.
	req, _ := http.NewRequest("POST", fs.URL+"/api/front/presence", strings.NewReader(`{"host":"id-b","threadId":"`+idB2+`","device":"phone"}`))
	req.Header.Set("X-Fyr", "1")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 204 {
		t.Fatalf("presence: %v %v", r, err)
	}
	ask(b, idB2, "Shh?")
	eventually(t, "the watched thread asking on the front", func() bool {
		d := threadOf(getState(t, fs.URL), idB2)
		at, _ := d["attention"].(map[string]any)
		return at["state"] == "asking"
	})
	time.Sleep(100 * time.Millisecond)
	if n := len(p.payloads()); n != 1 {
		t.Fatalf("a watched thread pushed: %d payloads", n)
	}
	// The push endpoints are the front's.
	if code, rep := post(t, fs.URL+"/api/front/push/subscribe", `{"subscription":{"endpoint":"https://push.example/x","keys":{"p256dh":"k","auth":"a"}},"device":"phone"}`); code != 200 || rep["ok"] != true {
		t.Fatalf("subscribe on the front: %d %v", code, rep)
	}
	if len(p.subs) != 1 {
		t.Fatalf("the front did not keep the subscription: %v", p.subs)
	}
}

// SPEC 0.5 V0.54: the last state per host survives a front restart: a host
// that is offline when the front starts shows its threads from the cache,
// listed offline and read-only, until it returns.
func TestFrontCacheSurvivesRestart(t *testing.T) {
	a, b := fullHost(t, "a", "pa"), fullHost(t, "b", "pb")
	pins := &memKV{} // the host ids the front pinned, which name its cache files (decision 0031 4.3)
	dir := t.TempDir()
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	f1 := startFront(t, Options{CacheDir: dir, Store: pins}, hosts...)
	online(t, f1.URL, 2)
	framed := threadFrames(t, f1.URL)
	id := create(t, f1.URL, "b", "cached")
	framed(id) // in the front's cache, not only on b
	f1.stop()

	b.setDown(true)
	f2 := startFront(t, Options{CacheDir: dir, Store: pins}, hosts...)
	// At once, before any host answers: b's thread from the cache.
	d := threadOf(getState(t, f2.URL), id)
	if d == nil || d["host"] != "b" || d["title"] != "cached" {
		t.Fatalf("cached thread after restart: %v", d)
	}
	online(t, f2.URL, 1) // a only
	s := getState(t, f2.URL)
	if s.Hosts[1]["name"] != "b" || s.Hosts[1]["online"] != false || s.Hosts[1]["since"] == nil {
		t.Fatalf("hosts: %v", s.Hosts)
	}
	if code, rep := post(t, hostURL(t, f2.URL, "b")+"/command", `{"commandId":"r","action":"thread.rename","threadId":"`+id+`","args":{"title":"x"}}`); code != 503 || !strings.Contains(rep["error"].(string), "b is offline") {
		t.Fatalf("command to a cached thread: %d %v", code, rep)
	}
	b.setDown(false)
	online(t, f2.URL, 2)
}

// SPEC 0.5 V0.54: an upload goes to the host its path names, and the
// attachment is read back from that host only; profiles are per host.
func TestFrontRoutesUploadsAndStatePerHost(t *testing.T) {
	a, b := fullHost(t, "a", "pa"), fullHost(t, "b", "pb")
	fs := startFront(t, Options{}, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	au, bu := hostURL(t, fs.URL, "a"), hostURL(t, fs.URL, "b")

	req, _ := http.NewRequest("POST", bu+"/attachments", strings.NewReader("hello from b"))
	req.Header.Set("X-Fyr", "1")
	req.Header.Set("X-Fyr-Filename", "note.txt")
	req.Header.Set("Content-Type", "text/plain")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var up struct{ ID string }
	json.NewDecoder(r.Body).Decode(&up)
	r.Body.Close()
	if r.StatusCode != 200 || up.ID == "" {
		t.Fatalf("upload: %d %+v", r.StatusCode, up)
	}
	read := func(hu string) (int, string) {
		r, err := http.Get(hu + "/attachments/" + up.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if code, body := read(bu); code != 200 || body != "hello from b" {
		t.Fatalf("read on b: %d %q", code, body)
	}
	if code, _ := read(au); code != 404 {
		t.Fatalf("host a has b's file: %d", code)
	}

	for name, want := range map[string]string{"a": "pa", "b": "pb"} {
		var st struct {
			Profiles []map[string]any `json:"profiles"`
		}
		getDoc(t, hostURL(t, fs.URL, name)+"/state", &st)
		if len(st.Profiles) != 1 || st.Profiles[0]["id"] != want {
			t.Fatalf("%s's profiles: %v", name, st.Profiles)
		}
	}
}

// SPEC 0.5 V0.54: a deploy on a host shows on that host's row, and on the
// stream named by that host's id (decision 0031 2.6: the client's banner is
// the deploy of the host the front runs in), never as another host's.
func TestFrontDeployIsPerHost(t *testing.T) {
	a, b := fullHost(t, "a", "pa"), fullHost(t, "b", "pb")
	fs := startFront(t, Options{}, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	resp, err := http.Get(fs.URL + "/api/front/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	st := sse{bufio.NewReader(resp.Body)}
	if ev, _ := st.next(t); ev != "hello" {
		t.Fatalf("first frame %q", ev)
	}
	b.eng.SetDeploy(map[string]any{"phase": "drain", "startedAt": time.Now().UTC()}, "drain")
	for {
		ev, data := st.next(t)
		if ev != "deploy" {
			continue
		}
		var d map[string]any
		json.Unmarshal([]byte(data), &d)
		if d["host"] != testID("b") || d["phase"] != "drain" {
			t.Fatalf("deploy frame: %s", data)
		}
		break
	}
	eventually(t, "b's deploy on its row", func() bool {
		s := getState(t, fs.URL)
		d, _ := s.Hosts[1]["deploy"].(map[string]any)
		return len(s.Hosts) == 2 && d != nil && d["phase"] == "drain"
	})
	if d := getState(t, fs.URL).Hosts[0]["deploy"]; d != nil {
		t.Fatalf("b's deploy reached a's row: %v", d)
	}
}
