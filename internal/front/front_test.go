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
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// host is a real host (engine, store, server without the web client) behind
// a switch, so a test can take it off the network and bring it back.
type host struct {
	hs   *httptest.Server
	eng  *engine.Engine
	down atomic.Bool
}

func newHost(t *testing.T, name string) *host {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	h := &host{eng: engine.New(st)}
	s := &server.Server{Engine: h.eng, BuildID: "host-build", Host: name}
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
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close() })
	return fs
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
	BuildID string           `json:"buildId"`
	Threads []map[string]any `json:"threads"`
	Hosts   []map[string]any `json:"hosts"`
	Actions []map[string]any `json:"actions"`
}

func getState(t *testing.T, base string) state {
	resp, err := http.Get(base + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s state
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber() // revs are nanosecond counts above 2^53
	dec.Decode(&s)
	return s
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
// command sent to the front reaches the host's engine, and the host's change
// comes back to the browser on the front's state and stream, stamped with the
// host's name.
func TestFrontCommandAndStateRoundTrip(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)

	resp, err := http.Get(fs.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	st := sse{bufio.NewReader(resp.Body)}
	if ev, data := st.next(t); ev != "hello" || !strings.Contains(data, `"buildId":"front-build"`) {
		t.Fatalf("hello: %s %s", ev, data)
	}

	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	id, _ := rep["threadId"].(string)
	if code != 200 || rep["ok"] != true || id == "" {
		t.Fatalf("create via front: %d %v", code, rep)
	}
	if _, ok := h.eng.Thread(id); !ok {
		t.Fatalf("the host's engine has no thread %s", id)
	}
	// The stream brings the new thread, with its host.
	for {
		ev, data := st.next(t)
		if ev != "thread" {
			continue
		}
		var d map[string]any
		json.Unmarshal([]byte(data), &d)
		if d["id"] == id {
			if d["host"] != "host-a" || d["title"] != "hi" {
				t.Fatalf("streamed thread: %s", data)
			}
			break
		}
	}
	s := getState(t, fs.URL)
	if s.BuildID != "front-build" || len(s.Actions) < 10 {
		t.Fatalf("state envelope: %+v", s)
	}
	if d := threadOf(s, id); d == nil || d["host"] != "host-a" {
		t.Fatalf("state thread: %v", d)
	}

	// A command on the thread goes to its host; the change comes back.
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c2","action":"thread.rename","threadId":"`+id+`","args":{"title":"Renamed"}}`); code != 200 || rep["ok"] != true {
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
	r, err := http.Get(fs.URL + "/api/threads/" + id + "/events")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !strings.Contains(string(b), `"events"`) {
		t.Fatalf("events via front: %d %s", r.StatusCode, b)
	}
	// The same commandId twice is still once (I7 across the hop).
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`); code != 200 || rep["threadId"] != id {
		t.Fatalf("replayed command: %d %v", code, rep)
	}
}

// SPEC 0.5 V0.51: with two hosts, the front sends each command to the host
// that owns the thread (its id-to-host map).
func TestFrontRoutesToTheThreadsHost(t *testing.T) {
	a, b := newHost(t, "a"), newHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	_, rep := post(t, fs.URL+"/api/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"on b","host":"b"}}`)
	id, _ := rep["threadId"].(string)
	if _, ok := b.eng.Thread(id); !ok || id == "" {
		t.Fatalf("args.host did not pick host b: %v", rep)
	}
	if _, ok := a.eng.Thread(id); ok {
		t.Fatal("host a has host b's thread")
	}
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c2","action":"thread.rename","threadId":"`+id+`","args":{"title":"B"}}`); code != 200 {
		t.Fatalf("rename: %d %v", code, rep)
	}
	if th, _ := b.eng.Thread(id); th.Title != "B" {
		t.Fatalf("host b title %q", th.Title)
	}
	eventually(t, "thread stamped b", func() bool {
		d := threadOf(getState(t, fs.URL), id)
		return d != nil && d["host"] == "b"
	})
}

// SPEC 0.5 V0.51: when a host goes away the front keeps its last threads,
// marks them hostOffline and read-only; when it returns the mark clears.
func TestFrontMarksOfflineHost(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)
	_, rep := post(t, fs.URL+"/api/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	id, _ := rep["threadId"].(string)
	var rev0 int64
	eventually(t, "thread on the front", func() bool {
		d := threadOf(getState(t, fs.URL), id)
		if d != nil {
			rev0, _ = d["rev"].(json.Number).Int64()
		}
		return d != nil
	})

	h.setDown(true)
	var d map[string]any
	eventually(t, "offline mark", func() bool {
		d = threadOf(getState(t, fs.URL), id)
		return d != nil && d["hostOffline"] != nil
	})
	if rev, _ := d["rev"].(json.Number).Int64(); rev <= rev0 {
		t.Fatalf("the offline mark must raise rev: %v <= %v", rev, rev0)
	}
	if d["title"] != "hi" {
		t.Fatalf("cached thread lost its data: %v", d)
	}
	s := getState(t, fs.URL)
	if len(s.Hosts) != 1 || s.Hosts[0]["online"] != false || s.Hosts[0]["name"] != "host-a" {
		t.Fatalf("hosts: %v", s.Hosts)
	}
	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c2","action":"thread.rename","threadId":"`+id+`","args":{"title":"x"}}`)
	if code != 503 || !strings.Contains(rep["error"].(string), "host-a is offline") {
		t.Fatalf("command to an offline host: %d %v", code, rep)
	}
	if r, _ := http.Get(fs.URL + "/api/threads/" + id + "/events"); r.StatusCode != 503 {
		t.Fatalf("events of an offline host: %d", r.StatusCode)
	}

	h.setDown(false)
	eventually(t, "mark cleared", func() bool {
		d := threadOf(getState(t, fs.URL), id)
		return d != nil && d["hostOffline"] == nil
	})
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c3","action":"thread.rename","threadId":"`+id+`","args":{"title":"back"}}`); code != 200 {
		t.Fatalf("command after return: %d %v", code, rep)
	}
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
	req, _ := http.NewRequest("POST", fs.URL+"/api/command", strings.NewReader(`{}`))
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 403 {
		t.Fatalf("POST without X-Fyr: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.58: project.delete through the front reaches the project's
// host, and the front drops the project from its state.
func TestFrontDropsDeletedProject(t *testing.T) {
	h := newHost(t, "host-a")
	fs := newFront(t, HostConfig{Name: "host-a", URL: h.hs.URL})
	online(t, fs.URL, 1)
	projects := func() []any {
		resp, err := http.Get(fs.URL + "/api/state")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var s struct {
			Projects []any `json:"projects"`
		}
		json.NewDecoder(resp.Body).Decode(&s)
		return s.Projects
	}
	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"p1","action":"project.create","args":{"name":"Garden"}}`)
	res, _ := rep["result"].(map[string]any)
	pid, _ := res["id"].(string)
	if code != 200 || pid == "" {
		t.Fatalf("create via front: %d %v", code, rep)
	}
	eventually(t, "the project on the front", func() bool { return len(projects()) == 1 })
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"p2","action":"project.delete","args":{"id":"`+pid+`"}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("delete via front: %d %v", code, rep)
	}
	if len(h.eng.Projects()) != 0 {
		t.Fatal("the host still has the project")
	}
	eventually(t, "the project gone from the front", func() bool { return len(projects()) == 0 })
}
