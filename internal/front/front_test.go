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
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// host is a real host (engine, store, server without the web client) behind
// a switch, so a test can take it off the network and bring it back.
type host struct {
	hs   *httptest.Server
	eng  *engine.Engine
	down atomic.Bool
	// focus records the host's focus calls (V0.85): "device thread on|off".
	focusMu sync.Mutex
	focus   []string
}

func (h *host) focused() []string {
	h.focusMu.Lock()
	defer h.focusMu.Unlock()
	return append([]string(nil), h.focus...)
}

func (h *host) recordFocus(device, thread string, on bool) {
	h.focusMu.Lock()
	defer h.focusMu.Unlock()
	st := "on"
	if !on {
		st = "off"
	}
	h.focus = append(h.focus, device+" "+thread+" "+st)
}

func newHost(t *testing.T, name string, profiles ...provider.Profile) *host {
	return newHostID(t, name, testID(name), profiles...)
}

// newHostID is newHost reporting install id id.
func newHostID(t *testing.T, name, id string, profiles ...provider.Profile) *host {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	h := &host{eng: engine.New(st)}
	s := &server.Server{Engine: h.eng, BuildID: "host-build", Host: name, ID: id, Profiles: profiles, Focus: h.recordFocus}
	inner := s.Handler()
	h.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.down.Load() {
			panic(http.ErrAbortHandler) // the connection drops, as with a host that went away
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { h.hs.CloseClientConnections(); h.hs.Close(); st.Close() })
	return h
}

func (h *host) setDown(down bool) {
	h.down.Store(down)
	if down {
		h.hs.CloseClientConnections()
	}
}

func newFront(t *testing.T, hosts ...HostConfig) *httptest.Server {
	f := New(Options{Name: "front", BuildID: "front-build", Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}, MinBackoff: 50 * time.Millisecond}, hosts)
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	fs := httptest.NewServer(f.Handler())
	testFronts.Store(fs.URL, f)
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close(); testFronts.Delete(fs.URL) })
	return fs
}

// testFronts: the Front behind each newFront URL, so a test can wait for what
// the front itself holds, not only for what a host answers through it.
var testFronts sync.Map

// frontKnows reports whether the front behind url holds the project ref in
// its own copy of the host's state: what a front command such as a project
// link resolves (an online host's /api/h/<id>/state is the host's own answer
// and can be ahead of it).
func frontKnows(url, ref string) bool {
	v, ok := testFronts.Load(url)
	if !ok {
		return false
	}
	f := v.(*Front)
	f.mu.Lock()
	defer f.mu.Unlock()
	_, code, _ := f.projectAtLocked(ref)
	return code == 200
}

// testFront is the Front behind a newFront URL.
func testFront(t *testing.T, url string) *Front {
	t.Helper()
	v, ok := testFronts.Load(url)
	if !ok {
		t.Fatalf("no front at %s", url)
	}
	return v.(*Front)
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("X-Fyr", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

type state struct {
	BuildID  string           `json:"buildId"`
	Threads  []map[string]any `json:"threads"`
	Hosts    []map[string]any `json:"hosts"`
	Actions  []map[string]any `json:"actions"`
	Projects []map[string]any `json:"projects"`
}

// testID is the install id a test host reports: every host is addressed by
// one (decision 0031).
func testID(name string) string { return "id-" + name }

func getDoc(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber() // revs are nanosecond counts above 2^53
	dec.Decode(v)
	return resp.StatusCode
}

// getState is what a client reads, as the client reads it (decision 0031
// 2.4): /api/front/state, then each host's own /api/h/<id>/state. A test
// reads the records of every host in one list, each marked with its host's
// name; the actions are the first host's.
func getState(t *testing.T, base string) state {
	t.Helper()
	var s state
	getDoc(t, base+"/api/front/state", &s)
	for _, h := range s.Hosts {
		id, _ := h["id"].(string)
		if id == "" {
			continue
		}
		var hs state
		getDoc(t, base+"/api/h/"+id+"/state", &hs)
		for _, d := range hs.Threads {
			d["host"] = h["name"]
			s.Threads = append(s.Threads, d)
		}
		for _, p := range hs.Projects {
			p["host"] = h["name"]
			s.Projects = append(s.Projects, p)
		}
		if s.Actions == nil {
			s.Actions = hs.Actions
		}
	}
	return s
}

// hostURL is base's prefix for the host named name: /api/h/<its id>.
func hostURL(t *testing.T, base, name string) string {
	t.Helper()
	var s state
	getDoc(t, base+"/api/front/state", &s)
	for _, h := range s.Hosts {
		if h["name"] == name {
			id, _ := h["id"].(string)
			return base + "/api/h/" + id
		}
	}
	t.Fatalf("the front lists no host %s", name)
	return ""
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func online(t *testing.T, base string, n int) {
	eventually(t, "hosts online", func() bool {
		s := getState(t, base)
		c := 0
		for _, h := range s.Hosts {
			if h["online"] == true {
				c++
			}
		}
		return c == n
	})
}

func threadOf(s state, id string) map[string]any {
	for _, d := range s.Threads {
		if d["id"] == id {
			return d
		}
	}
	return nil
}

// sse reads one stream's frames.
type sse struct{ rd *bufio.Reader }

func (s sse) next(t *testing.T) (string, string) {
	t.Helper()
	var ev, data string
	for {
		line, err := s.rd.ReadString('\n')
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

// SPEC 0.5 V0.51: a front talks to its host only over the host protocol: a
// command sent to the front under the host's prefix reaches the host's engine,
// and the host's change comes back on the front's stream, named by the host's
// install id (decision 0031 2.4).
func TestFrontCommandAndStateRoundTrip(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)
	hu := hostURL(t, fs.URL, "host-a")

	resp, err := http.Get(fs.URL + "/api/front/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	st := sse{bufio.NewReader(resp.Body)}
	if ev, data := st.next(t); ev != "hello" || !strings.Contains(data, `"buildId":"front-build"`) {
		t.Fatalf("hello: %s %s", ev, data)
	}

	code, rep := post(t, hu+"/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	id, _ := rep["threadId"].(string)
	if code != 200 || rep["ok"] != true || id == "" {
		t.Fatalf("create via front: %d %v", code, rep)
	}
	if _, ok := h.eng.Thread(id); !ok {
		t.Fatalf("the host's engine has no thread %s", id)
	}
	// The stream brings the new thread, with its host's id.
	for {
		ev, data := st.next(t)
		if ev != "thread" {
			continue
		}
		var d map[string]any
		json.Unmarshal([]byte(data), &d)
		if d["id"] == id {
			if d["host"] != testID("host-a") || d["title"] != "hi" {
				t.Fatalf("streamed thread: %s", data)
			}
			break
		}
	}
	s := getState(t, fs.URL)
	if s.BuildID != "front-build" || len(s.Actions) < 10 {
		t.Fatalf("state envelope: %+v", s)
	}
	if d := threadOf(s, id); d == nil {
		t.Fatalf("no thread %s in the host's state", id)
	}

	// A command on the thread goes to its host; the change comes back.
	if code, rep := post(t, hu+"/command", `{"commandId":"c2","action":"thread.rename","threadId":"`+id+`","args":{"title":"Renamed"}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("rename via front: %d %v", code, rep)
	}
	if th, _ := h.eng.Thread(id); th.Title != "Renamed" {
		t.Fatalf("host title %q", th.Title)
	}
	eventually(t, "renamed thread on the front", func() bool {
		d := threadOf(getState(t, fs.URL), id)
		return d != nil && d["title"] == "Renamed"
	})
	// Thread calls pass through to the host.
	r, err := http.Get(hu + "/threads/" + id + "/events")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !strings.Contains(string(b), `"events"`) {
		t.Fatalf("events via front: %d %s", r.StatusCode, b)
	}
	// The same commandId twice is still once (I7 across the hop).
	if code, rep := post(t, hu+"/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`); code != 200 || rep["threadId"] != id {
		t.Fatalf("replayed command: %d %v", code, rep)
	}
}

// SPEC 0.5 V0.51, decision 0031 2.2: with two hosts, a command goes to the
// host its path names, and to no other.
func TestFrontRoutesToTheNamedHost(t *testing.T) {
	a, b := newHost(t, "a"), newHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	bu := hostURL(t, fs.URL, "b")
	_, rep := post(t, bu+"/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"on b"}}`)
	id, _ := rep["threadId"].(string)
	if _, ok := b.eng.Thread(id); !ok || id == "" {
		t.Fatalf("the prefix did not pick host b: %v", rep)
	}
	if _, ok := a.eng.Thread(id); ok {
		t.Fatal("host a has host b's thread")
	}
	if code, rep := post(t, bu+"/command", `{"commandId":"c2","action":"thread.rename","threadId":"`+id+`","args":{"title":"B"}}`); code != 200 {
		t.Fatalf("rename: %d %v", code, rep)
	}
	if th, _ := b.eng.Thread(id); th.Title != "B" {
		t.Fatalf("host b title %q", th.Title)
	}
	eventually(t, "thread on b", func() bool {
		d := threadOf(getState(t, fs.URL), id)
		return d != nil && d["host"] == "b"
	})
}

// SPEC 0.5 V0.51: when a host goes away the front keeps its last threads
// (read from its cache) and lists the host offline; commands are refused
// until it returns.
func TestFrontMarksOfflineHost(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)
	hu := hostURL(t, fs.URL, "host-a")
	framed := threadFrames(t, fs.URL)
	_, rep := post(t, hu+"/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	id, _ := rep["threadId"].(string)
	framed(id) // in the front's cache, which serves it while the host is offline

	h.setDown(true)
	eventually(t, "host offline", func() bool {
		s := getState(t, fs.URL)
		return len(s.Hosts) == 1 && s.Hosts[0]["online"] == false
	})
	if d := threadOf(getState(t, fs.URL), id); d == nil || d["title"] != "hi" {
		t.Fatalf("cached thread lost its data: %v", d)
	}
	code, rep := post(t, hu+"/command", `{"commandId":"c2","action":"thread.rename","threadId":"`+id+`","args":{"title":"x"}}`)
	if code != 503 || rep["code"] != "host-offline" || !strings.Contains(rep["error"].(string), "host-a is offline") {
		t.Fatalf("command to an offline host: %d %v", code, rep)
	}
	if r, _ := http.Get(hu + "/threads/" + id + "/events"); r.StatusCode != 503 {
		t.Fatalf("events of an offline host: %d", r.StatusCode)
	}

	h.setDown(false)
	online(t, fs.URL, 1)
	if code, rep := post(t, hu+"/command", `{"commandId":"c3","action":"thread.rename","threadId":"`+id+`","args":{"title":"back"}}`); code != 200 {
		t.Fatalf("command after return: %d %v", code, rep)
	}
}

// A host that is unreachable for a moment (a dead pooled connection, a link
// that resets as the Mac wakes) is not marked offline: the dot does not blink.
func TestFrontHostDotSurvivesABlip(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)
	h.setDown(true)
	time.AfterFunc(60*time.Millisecond, func() { h.setDown(false) })
	for end := time.Now().Add(700 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if s := getState(t, fs.URL); len(s.Hosts) == 1 && s.Hosts[0]["online"] == false {
			t.Fatal("the host showed offline for a blip")
		}
	}
	online(t, fs.URL, 1)
}

// SPEC 0.5 V0.51: the front serves the web client; the browser guard holds.
func TestFrontServesClientAndGuards(t *testing.T) {
	h := newHost(t, "x")
	fs := newFront(t, HostConfig{Name: "x", URL: h.hs.URL})
	r, _ := http.Get(fs.URL + "/")
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), "<title>Fyr</title>") {
		t.Fatalf("index: %s", b)
	}
	req, _ := http.NewRequest("POST", fs.URL+"/api/front/command", strings.NewReader(`{}`))
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 403 {
		t.Fatalf("POST without X-Fyr: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.58: project.delete through the front reaches the project's
// host, and the front drops the project from its copy of that host's state.
func TestFrontDropsDeletedProject(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)
	hu := hostURL(t, fs.URL, "host-a")
	projects := func() []map[string]any { return getState(t, fs.URL).Projects }
	code, rep := post(t, hu+"/command", `{"commandId":"p1","action":"project.create","args":{"name":"Garden"}}`)
	res, _ := rep["result"].(map[string]any)
	pid, _ := res["id"].(string)
	if code != 200 || pid == "" {
		t.Fatalf("create via front: %d %v", code, rep)
	}
	eventually(t, "the project on the front", func() bool { return len(projects()) == 1 })
	if code, rep := post(t, hu+"/command", `{"commandId":"p2","action":"project.delete","args":{"id":"`+pid+`"}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("delete via front: %d %v", code, rep)
	}
	if len(h.eng.Projects()) != 0 {
		t.Fatal("the host still has the project")
	}
	eventually(t, "the project gone from the host", func() bool { return len(projects()) == 0 })
	// The front's copy, served while the host is offline, dropped it too.
	h.setDown(true)
	eventually(t, "host offline", func() bool { s := getState(t, fs.URL); return len(s.Hosts) == 1 && s.Hosts[0]["online"] == false })
	if ps := projects(); len(ps) != 0 {
		t.Fatalf("the front's cache kept the deleted project: %v", ps)
	}
}

// threadFrames opens base's stream; the func it returns waits until the front
// streamed thread id, so the front's own copy (its cache) has it. A test that
// reads a thread through the prefix while the host is online reads the host,
// not that copy.
func threadFrames(t *testing.T, base string) func(id string) {
	t.Helper()
	resp, err := http.Get(base + "/api/front/stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	frames := sse{bufio.NewReader(resp.Body)}
	return func(id string) {
		t.Helper()
		for {
			if ev, data := frames.next(t); ev == "thread" && strings.Contains(data, `"id":"`+id+`"`) {
				return
			}
		}
	}
}
