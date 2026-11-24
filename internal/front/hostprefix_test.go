package front

// Decision 0031 slice 1 (the front speaks both): /api/h/<hostId>/… forwards to
// one host by its install id, /api/front/… is what belongs to no host, and the
// old routes keep their behaviour, marked and counted. The hosts here are fakes
// that count what reaches them, and two of them share thread and project ids.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

const (
	idOne = "AAAA-1111-AAAA-1111"
	idTwo = "BBBB-2222-BBBB-2222"
)

// fakeHost answers the host protocol with a thread t_same and a project p_same
// (the same ids on every fake), and counts the requests that carry X-Probe.
type fakeHost struct {
	srv    *httptest.Server
	name   string
	id     string
	down   atomic.Bool
	frames chan string

	mu    sync.Mutex
	probe []seenReq
	body  []string
	focus []string
}

func newFakeHost(t *testing.T, name, id string) *fakeHost {
	h := &fakeHost{name: name, id: id, frames: make(chan string, 16)}
	h.srv = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(func() { h.srv.CloseClientConnections(); h.srv.Close() })
	return h
}

func (h *fakeHost) serve(w http.ResponseWriter, r *http.Request) {
	if h.down.Load() {
		panic(http.ErrAbortHandler)
	}
	js := func(v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
	if r.Header.Get("X-Probe") != "" {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.probe = append(h.probe, seenReq{r.Method, r.URL.RequestURI(), r.Host, r.Header.Clone()})
		h.body = append(h.body, string(b))
		h.mu.Unlock()
	}
	if strings.HasSuffix(r.URL.Path, "/focus") {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.focus = append(h.focus, r.URL.Path+" "+string(b))
		h.mu.Unlock()
		js(map[string]any{"ok": true})
		return
	}
	thread := map[string]any{"id": "t_same", "rev": 100, "title": "on " + h.name, "state": "idle", "projectId": "p_same"}
	switch p := r.URL.Path; {
	case p == "/api/stream":
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: hello\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case f := <-h.frames:
				fmt.Fprint(w, f)
				w.(http.Flusher).Flush()
			}
		}
	case p == "/api/state":
		js(map[string]any{"now": time.Now().UTC(), "host": h.name, "hostId": h.id, "buildId": "host-build",
			"threads": []any{thread}, "projects": []any{map[string]any{"id": "p_same", "name": "project of " + h.name}},
			"profiles": []any{map[string]any{"id": "prof-" + h.name}}, "actions": []any{}})
	case p == "/api/threads/t_same":
		js(thread)
	case p == "/api/threads/t_same/events":
		js(map[string]any{"host": h.name, "events": []any{}})
	case p == "/api/command":
		js(map[string]any{"ok": true, "host": h.name})
	case p == "/api/attachments":
		js(map[string]any{"id": "a_" + h.name})
	case p == "/api/providers":
		js(map[string]any{"providers": []any{h.name}})
	case p == "/api/previews/p1/":
		fmt.Fprint(w, `<script src="app.js"></script>`)
	case p == "/api/previews/p1/app.js":
		fmt.Fprint(w, "js of "+h.name)
	default:
		http.NotFound(w, r)
	}
}

func (h *fakeHost) probed() []seenReq {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]seenReq(nil), h.probe...)
}

func (h *fakeHost) bodies() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.body...)
}

func (h *fakeHost) focused() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.focus...)
}

func (h *fakeHost) reset() {
	h.mu.Lock()
	h.probe, h.body = nil, nil
	h.mu.Unlock()
}

func (h *fakeHost) setDown(down bool) {
	h.down.Store(down)
	if down {
		h.srv.CloseClientConnections()
	}
}

func startFakes(t *testing.T, cache string, hosts ...*fakeHost) (*Front, *httptest.Server) {
	t.Helper()
	var cfg []HostConfig
	for _, h := range hosts {
		cfg = append(cfg, HostConfig{Name: h.name, URL: h.srv.URL})
	}
	f := New(Options{ID: "FRNT-0000-0000-0000", Name: "front", BuildID: "front-build", CacheDir: cache, MinBackoff: 50 * time.Millisecond,
		Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}}, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	fs := httptest.NewServer(f.Handler())
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close() })
	eventually(t, "every host online", func() bool {
		var st struct{ Hosts []struct{ Online bool } }
		getJSON(t, fs.URL+"/api/front/state", &st)
		n := 0
		for _, h := range st.Hosts {
			if h.Online {
				n++
			}
		}
		return n == len(hosts) && n > 0
	})
	return f, fs
}

func probeGet(t *testing.T, url string) (int, http.Header, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Probe", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func probePost(t *testing.T, url, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("X-Probe", "1")
	req.Header.Set("X-Fyr", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func getJSON(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(v)
	return resp.StatusCode
}

// Test 3 (Go): one id on two hosts is two records; a call reaches only the host
// the path names.
func TestPrefixReachesOnlyTheNamedHost(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)

	for _, c := range []struct {
		id, name string
		other    *fakeHost
	}{{idOne, "one", two}, {idTwo, "two", one}} {
		code, _, b := probeGet(t, fs.URL+"/api/h/"+c.id+"/threads/t_same/events?after=40")
		var out struct{ Host string }
		json.Unmarshal(b, &out)
		if code != 200 || out.Host != c.name {
			t.Errorf("events through %s: %d %s", c.id, code, b)
		}
		if n := len(c.other.probed()); n != 0 {
			t.Errorf("a call to %s reached the other host %d times", c.id, n)
		}
		// the host sees its own protocol path, with the query and without the prefix
		if got := probedPaths(c.name, one, two); len(got) != 1 || got[0] != "/api/threads/t_same/events?after=40" {
			t.Errorf("%s saw %v", c.name, got)
		}
		one.reset()
		two.reset()
	}

	// a command: the body goes through unchanged and only to the named host
	one.reset()
	two.reset()
	body := `{"commandId":"c1","action":"thread.send","threadId":"t_same","args":{"text":"hi"}}`
	if code, b := probePost(t, fs.URL+"/api/h/"+idTwo+"/command", body); code != 200 || !strings.Contains(string(b), `"host":"two"`) {
		t.Errorf("command: %d %s", code, b)
	}
	if len(one.probed()) != 0 || len(two.probed()) != 1 || two.bodies()[0] != body {
		t.Errorf("command reached one=%d two=%d body=%q", len(one.probed()), len(two.probed()), two.bodies())
	}
}

func probedPaths(name string, hs ...*fakeHost) []string {
	var out []string
	for _, h := range hs {
		if h.name == name {
			for _, r := range h.probed() {
				out = append(out, r.path)
			}
		}
	}
	return out
}

// Test 2 (an unknown id, with the reads of each kind): 404 no-host, reaching no host.
func TestUnknownHostIsNotFoundAndReachesNoHost(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	for _, p := range []string{"/threads/t_same/events", "/state", "/providers", "/previews/p1/app.js"} {
		code, _, b := probeGet(t, fs.URL+"/api/h/ZZZZ-9999-ZZZZ-9999"+p)
		var out struct {
			OK   bool
			Code string
		}
		json.Unmarshal(b, &out)
		if code != 404 || out.OK || out.Code != "no-host" {
			t.Errorf("unknown id, %s: %d %s", p, code, b)
		}
	}
	// the name is not an id
	if code, _, _ := probeGet(t, fs.URL+"/api/h/one/threads/t_same/events"); code != 404 {
		t.Errorf("a host's name is not its id: %d", code)
	}
	if code, b := probePost(t, fs.URL+"/api/h/ZZZZ-9999-ZZZZ-9999/command", `{"action":"x"}`); code != 404 {
		t.Errorf("command to an unknown id: %d %s", code, b)
	}
	if n := len(one.probed()) + len(two.probed()); n != 0 {
		t.Errorf("%d requests reached a host", n)
	}
}

// Test 5: while a host is offline its state, a thread and a settings read come
// from the front's cache with X-Fyr-Read-At; a command is 503 host-offline with
// the link; nothing goes to the other host.
func TestOfflineHostIsServedFromTheCache(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	// read the settings page once while it is up, so the cache holds it
	if code, _, _ := probeGet(t, fs.URL+"/api/h/"+idOne+"/providers"); code != 200 {
		t.Fatalf("providers: %d", code)
	}
	one.setDown(true)
	eventually(t, "host one offline", func() bool {
		var st struct {
			Hosts []struct {
				ID     string
				Online bool
			}
		}
		getJSON(t, fs.URL+"/api/front/state", &st)
		for _, h := range st.Hosts {
			if h.ID == idOne {
				return !h.Online
			}
		}
		return false
	})
	one.reset()
	two.reset()

	code, hdr, b := probeGet(t, fs.URL+"/api/h/"+idOne+"/state?since=0")
	var st struct {
		Host    string
		HostID  string
		Threads []map[string]any
	}
	json.Unmarshal(b, &st)
	if code != 200 || hdr.Get("X-Fyr-Read-At") == "" || st.HostID != idOne || len(st.Threads) != 1 {
		t.Fatalf("state of an offline host: %d %v %s", code, hdr, b)
	}
	if _, stamped := st.Threads[0]["host"]; stamped || st.Threads[0]["hostOffline"] != nil || fmt.Sprint(st.Threads[0]["rev"]) != "100" {
		t.Errorf("the host's own state is not stamped or raised: %v", st.Threads[0])
	}
	// V0.state-index: an offline host's cached state answers the index and ?ids= as a host does.
	if code, _, b := probeGet(t, fs.URL+"/api/h/"+idOne+"/state?index=1"); code != 200 || !strings.Contains(string(b), `"index":[{"id":"t_same","rev":100,`) || strings.Contains(string(b), "on one") {
		t.Errorf("index of an offline host: %d %s", code, b)
	}
	if code, _, b := probeGet(t, fs.URL+"/api/h/"+idOne+"/state?ids=t_same,t_nope"); code != 200 || !strings.Contains(string(b), "on one") || strings.Contains(string(b), "t_nope") {
		t.Errorf("ids of an offline host: %d %s", code, b)
	}
	if code, hdr, b := probeGet(t, fs.URL+"/api/h/"+idOne+"/threads/t_same"); code != 200 || hdr.Get("X-Fyr-Read-At") == "" || !strings.Contains(string(b), "on one") {
		t.Errorf("thread of an offline host: %d %s", code, b)
	}
	if code, hdr, b := probeGet(t, fs.URL+"/api/h/"+idOne+"/providers"); code != 200 || hdr.Get("X-Fyr-Read-At") == "" || !strings.Contains(string(b), "one") {
		t.Errorf("settings read of an offline host: %d %s", code, b)
	}
	code, b = probePost(t, fs.URL+"/api/h/"+idOne+"/command", `{"action":"thread.send","threadId":"t_same","args":{"text":"x"}}`)
	var rep struct {
		OK   bool
		Code string
		Link map[string]any
	}
	json.Unmarshal(b, &rep)
	if code != 503 || rep.OK || rep.Code != "host-offline" || rep.Link["state"] != "down" {
		t.Errorf("command to an offline host: %d %s", code, b)
	}
	if code, _, _ := probeGet(t, fs.URL+"/api/h/"+idOne+"/threads/t_same/events"); code != 503 {
		t.Errorf("a read the front does not keep: %d", code)
	}
	if n := len(two.probed()); n != 0 {
		t.Errorf("%d requests reached the other host", n)
	}
	// the other host is unaffected
	if code, _, b := probeGet(t, fs.URL+"/api/h/"+idTwo+"/threads/t_same/events"); code != 200 || !strings.Contains(string(b), `"two"`) {
		t.Errorf("host two: %d %s", code, b)
	}
}

// /api/front/state: what belongs to no host, and every host with its id and link.
func TestFrontStateListsHostsByIdWithTheirLink(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	var st map[string]any
	if code := getJSON(t, fs.URL+"/api/front/state", &st); code != 200 {
		t.Fatalf("front state: %d", code)
	}
	for _, k := range []string{"threads", "projects", "profiles", "actions", "deploy", "host", "hostId"} {
		if _, ok := st[k]; ok {
			t.Errorf("/api/front/state has %q: it belongs to a host", k)
		}
	}
	fr, _ := st["front"].(map[string]any)
	if fr["id"] != "FRNT-0000-0000-0000" || fr["name"] != "front" || st["buildId"] != "front-build" || st["user"] == nil || st["now"] == nil {
		t.Errorf("front/state head: %v", st)
	}
	hs, _ := st["hosts"].([]any)
	if len(hs) != 2 {
		t.Fatalf("hosts: %v", st["hosts"])
	}
	for i, want := range []string{idOne, idTwo} {
		h := hs[i].(map[string]any)
		link, _ := h["link"].(map[string]any)
		if h["id"] != want || h["online"] != true || link["state"] != "up" || link["via"] == nil {
			t.Errorf("host %d: %v", i, h)
		}
	}
}

// Test 3 (Go, stream) and 2.4: one stream; every frame from a host carries
// host: <id>, so a thread with the same id on two hosts is two frames.
func TestFrontStreamNamesTheHostOnEveryFrame(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	resp, err := http.Get(fs.URL + "/api/front/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	frame := func() (string, map[string]any) {
		var ev string
		var data map[string]any
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				line, err := rd.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimRight(line, "\r\n")
				switch {
				case strings.HasPrefix(line, "event: "):
					ev = line[7:]
				case strings.HasPrefix(line, "data: "):
					json.Unmarshal([]byte(line[6:]), &data)
				case line == "" && ev != "":
					return
				}
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("no frame")
		}
		return ev, data
	}
	if ev, _ := frame(); ev != "hello" {
		t.Fatalf("first frame %q", ev)
	}
	one.frames <- "event: thread\ndata: {\"id\":\"t_same\",\"rev\":200,\"title\":\"changed on one\"}\n\n"
	two.frames <- "event: event\ndata: {\"threadId\":\"t_same\",\"seq\":1}\n\n"
	got := map[string]map[string]any{}
	for len(got) < 2 {
		ev, d := frame()
		if ev == "thread" || ev == "event" {
			got[ev] = d
		}
	}
	if got["thread"]["host"] != idOne || got["thread"]["title"] != "changed on one" || fmt.Sprint(got["thread"]["rev"]) != "200" {
		t.Errorf("thread frame: %v", got["thread"])
	}
	if got["event"]["host"] != idTwo || got["event"]["threadId"] != "t_same" {
		t.Errorf("event frame: %v", got["event"])
	}
}

// Test 2, whole (decision 0031 Part 3, V0.old-routes-end): no default host.
// Every route of the front's mux is a host's by id (/api/h/<id>/), the
// front's own (/api/front/), or no API at all; any other /api/ path is 404
// and reaches no host, whichever host is first; an unknown id is 404 no-host
// and reaches no host. Each fake host counts the requests that reach it.
func TestNoDefaultHost(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	f, fs := startFakes(t, "", one, two)

	// The table: every pattern the mux has.
	for _, pat := range f.RoutePatterns() {
		path := pat
		if _, p, ok := strings.Cut(pat, " "); ok {
			path = p
		}
		switch {
		case strings.HasPrefix(path, hostPrefix), strings.HasPrefix(path, frontPrefix):
		case path == "/api/": // the 404 below
		case path == "/", path == "/mcp", path == "/.well-known/fyr": // not the browser API
		case path == "/forward/open": // V0.forwarded-ports: a link's address, opened as a page
		default:
			t.Errorf("route %q is neither a host's by id nor the front's", pat)
		}
	}

	// The paths of the old routes (and a few that never were), each method:
	// 404 no-route, and no host hears of them.
	old := []string{"/api/state", "/api/stream", "/api/command", "/api/threads/t_same", "/api/threads/t_same/events",
		"/api/attachments", "/api/attachments/a_one", "/api/previews/p1/", "/api/previews/p1/app.js", "/api/projects/p_same/icon",
		"/api/quota", "/api/load", "/api/profiles", "/api/providers", "/api/providers/found", "/api/systemAgents",
		"/api/autoDeploy", "/api/autoMerge", "/api/updates", "/api/presence", "/api/push/key", "/api/auth/state",
		"/api/peer/state", "/api/fs/list?path=/", "/api/state?host=one", "/api/command?host=two", "/api/h/", "/api/front/nope"}
	for _, p := range old {
		code, _, b := probeGet(t, fs.URL+p)
		var out struct {
			OK   bool
			Code string
		}
		json.Unmarshal(b, &out)
		if code != 404 || out.OK || out.Code != "no-route" {
			t.Errorf("GET %s: %d %s", p, code, b)
		}
		if code, b := probePost(t, fs.URL+p, `{"commandId":"c-old","action":"thread.create","args":{"host":"one"}}`); code != 404 && code != 405 {
			t.Errorf("POST %s: %d %s", p, code, b)
		}
	}

	// An unknown id: 404 no-host, for a read and a command.
	for _, p := range []string{"/state", "/threads/t_same", "/command", "/previews/p1/app.js"} {
		code, _, b := probeGet(t, fs.URL+"/api/h/ZZZZ-9999-ZZZZ-9999"+p)
		var out struct{ Code string }
		json.Unmarshal(b, &out)
		if code != 404 || out.Code != "no-host" {
			t.Errorf("unknown id, GET %s: %d %s", p, code, b)
		}
		if code, b := probePost(t, fs.URL+"/api/h/ZZZZ-9999-ZZZZ-9999"+p, `{"action":"thread.create"}`); code != 404 {
			t.Errorf("unknown id, POST %s: %d %s", p, code, b)
		}
	}
	if n := len(one.probed()) + len(two.probed()); n != 0 {
		t.Errorf("%d requests without a known host id reached a host: %v %v", n, one.probed(), two.probed())
	}

	// And the prefix still reaches the host it names, so the zero above is not
	// a probe that never counts.
	if code, _, _ := probeGet(t, fs.URL+"/api/h/"+idTwo+"/threads/t_same"); code != 200 || len(two.probed()) != 1 || len(one.probed()) != 0 {
		t.Errorf("the named host: %d, one %d two %d", code, len(one.probed()), len(two.probed()))
	}
}

// A preview page's relative URLs resolve under the prefix, and nothing is
// remembered by the front: a restart changes nothing.
func TestPreviewFilesFollowThePrefixAcrossARestart(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	code, _, page := probeGet(t, fs.URL+"/api/h/"+idTwo+"/previews/p1/")
	if code != 200 || !strings.Contains(string(page), `src="app.js"`) {
		t.Fatalf("page: %d %s", code, page)
	}
	// a new front, nothing learned
	_, fs2 := startFakes(t, "", one, two)
	code, _, js := probeGet(t, fs2.URL+"/api/h/"+idTwo+"/previews/p1/app.js")
	if code != 200 || string(js) != "js of two" {
		t.Errorf("file after a restart: %d %q", code, js)
	}
}

// Same id on two hosts in the state each host gives: the host's own shape,
// the host's own rev, not stamped.
func TestHostStateIsTheHostsOwn(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	for _, c := range []struct{ id, name string }{{idOne, "one"}, {idTwo, "two"}} {
		var st struct {
			HostID   string
			Threads  []map[string]any
			Projects []map[string]any
			Profiles []map[string]any
		}
		if code := getJSON(t, fs.URL+"/api/h/"+c.id+"/state", &st); code != 200 {
			t.Fatalf("state %s: %d", c.id, code)
		}
		if st.HostID != c.id || len(st.Threads) != 1 || st.Threads[0]["title"] != "on "+c.name || len(st.Profiles) != 1 || st.Profiles[0]["id"] != "prof-"+c.name {
			t.Errorf("state of %s: %+v", c.name, st)
		}
	}
}

// The same thread id on two hosts is two threads on the stream: the second
// whole frame is not a patch against the first host's.
func TestFrontStreamKeepsTwoThreadsWithOneId(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	resp, err := http.Get(fs.URL + "/api/front/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	pad := strings.Repeat("x", 300) // so a patch would be smaller than a whole frame
	one.frames <- "event: thread\ndata: {\"id\":\"t_same\",\"rev\":300,\"title\":\"one\",\"pad\":\"" + pad + "\"}\n\n"
	time.Sleep(150 * time.Millisecond) // past the stream's 100 ms per-thread spacing
	two.frames <- "event: thread\ndata: {\"id\":\"t_same\",\"rev\":301,\"title\":\"two\",\"pad\":\"" + pad + "\"}\n\n"
	seen := map[string]string{} // host -> event name
	end := time.Now().Add(5 * time.Second)
	var ev string
	for len(seen) < 2 && time.Now().Before(end) {
		line, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			ev = line[7:]
		case strings.HasPrefix(line, "data: ") && strings.Contains(line, `"t_same"`) && strings.Contains(line, `"rev":30`):
			var m map[string]any
			json.Unmarshal([]byte(line[6:]), &m)
			seen[fmt.Sprint(m["host"])] = ev
		}
	}
	if seen[idOne] != "thread" || seen[idTwo] != "thread" {
		t.Errorf("frames by host: %v (each should be a whole thread)", seen)
	}
}

// front.hostOrder takes host ids and refuses one the front does not follow.
func TestFrontCommandOrdersHostsByID(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	order := func() []string {
		var st struct{ Hosts []struct{ ID string } }
		getJSON(t, fs.URL+"/api/front/state", &st)
		var ids []string
		for _, h := range st.Hosts {
			ids = append(ids, h.ID)
		}
		return ids
	}
	if got := order(); got[0] != idOne {
		t.Fatalf("start order %v", got)
	}
	if code, b := probePost(t, fs.URL+"/api/front/command", `{"commandId":"o1","action":"front.hostOrder","args":{"ids":["`+idTwo+`","`+idOne+`"]}}`); code != 200 {
		t.Fatalf("hostOrder: %d %s", code, b)
	}
	if got := order(); got[0] != idTwo {
		t.Errorf("order after front.hostOrder: %v", got)
	}
	if code, b := probePost(t, fs.URL+"/api/front/command", `{"action":"front.hostOrder","args":{"ids":["ZZZZ-9999-ZZZZ-9999"]}}`); code != 404 || !strings.Contains(string(b), "no-host") {
		t.Errorf("hostOrder with an unknown id: %d %s", code, b)
	}
	if code, b := probePost(t, fs.URL+"/api/front/command", `{"commandId":"m1","action":"front.hostMark","args":{"id":"`+idTwo+`","icon":"house","color":"teal"}}`); code != 200 {
		t.Errorf("front.hostMark on the front's route: %d %s", code, b)
	}
	if code, _ := probePost(t, fs.URL+"/api/front/command", `{"action":"thread.send","threadId":"t_same","args":{}}`); code != 400 {
		t.Errorf("a host's command on the front's route: %d", code)
	}
	if n := len(one.probed()) + len(two.probed()); n != 0 {
		t.Errorf("a front command reached a host as a probe: %d", n)
	}
}

// /api/front/presence names the host: the focus goes to that host and to no other.
func TestFrontPresenceFocusesTheNamedHost(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	if code, _ := probePost(t, fs.URL+"/api/front/presence", `{"host":"ZZZZ-9999-ZZZZ-9999","threadId":"t_same","device":"d1"}`); code != 404 {
		t.Errorf("presence on an unknown host: %d", code)
	}
	if code, _ := probePost(t, fs.URL+"/api/front/presence", `{"host":"`+idTwo+`","threadId":"t_same","device":"d1"}`); code != 204 {
		t.Fatalf("presence: %d", code)
	}
	eventually(t, "the focus on host two", func() bool { return len(two.focused()) == 1 })
	if len(one.focused()) != 0 {
		t.Errorf("host one heard the focus: %v", one.focused())
	}
	// moving to the same thread id on host one turns host two's off
	if code, _ := probePost(t, fs.URL+"/api/front/presence", `{"host":"`+idOne+`","threadId":"t_same","device":"d1"}`); code != 204 {
		t.Fatalf("presence: %d", code)
	}
	eventually(t, "off on two and on on one", func() bool { return len(two.focused()) == 2 && len(one.focused()) == 1 })
	if got := two.focused()[1]; !strings.Contains(got, `"off":true`) {
		t.Errorf("host two was not told off: %v", two.focused())
	}
}

// The cache of a host is its install id's file (decision 0031 4.3), so a
// rename keeps it; a file the old way named after the host is still read.
func TestCacheFileIsNamedByTheInstallID(t *testing.T) {
	dir := t.TempDir()
	one := newFakeHost(t, "one", idOne)
	f, _ := startFakes(t, dir, one)
	f.saveDirty()
	if _, err := os.Stat(filepath.Join(dir, idOne+".json")); err != nil {
		t.Errorf("no cache file for the id: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "one.json")); err == nil {
		t.Errorf("the host's name still names a cache file")
	}
}

// The front's own frames (hosts, projectOrder) are on /api/front/stream too, with no host.
func TestFrontStreamCarriesTheFrontsOwnFrames(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	resp, err := http.Get(fs.URL + "/api/front/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := make(chan string, 16)
	go func() {
		rd := bufio.NewReader(resp.Body)
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "event: ") {
				got <- strings.TrimSpace(line[7:])
			}
		}
	}()
	if code, b := probePost(t, fs.URL+"/api/front/command", `{"commandId":"o9","action":"front.hostOrder","args":{"ids":["`+idTwo+`","`+idOne+`"]}}`); code != 200 {
		t.Fatalf("hostOrder: %d %s", code, b)
	}
	if code, b := probePost(t, fs.URL+"/api/front/command", `{"commandId":"o10","action":"front.projectOrder","args":{"ids":[]}}`); code != 200 {
		t.Logf("projectOrder: %d %s (no store in this front)", code, b)
	}
	seen := map[string]bool{}
	for end := time.After(5 * time.Second); !seen["hosts"]; {
		select {
		case ev := <-got:
			seen[ev] = true
		case <-end:
			t.Fatalf("frames %v: no hosts frame on /api/front/stream", seen)
		}
	}
}

// V0.forget-front (found by decision 0031 test 1 through the browser): a
// folder preview's own files are read by its sandboxed frame, which has no
// origin, so the browser says the request is cross-site and a module
// script's Origin is "null". The front serves them; any other /api/ call
// that says so is still refused.
func TestSandboxedPreviewReadsItsOwnFiles(t *testing.T) {
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	_, fs := startFakes(t, "", one, two)
	get := func(path string, hdr map[string]string) int {
		req, _ := http.NewRequest("GET", fs.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	sandboxed := map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": "script", "Origin": "null"}
	if code := get("/api/h/"+idTwo+"/previews/p1/app.js", sandboxed); code != 200 {
		t.Errorf("a preview's own file from its sandboxed page: %d", code)
	}
	if code := get("/api/h/"+idTwo+"/state", sandboxed); code != 403 {
		t.Errorf("a cross-site call that is not a preview's: %d", code)
	}
	if code := get("/api/h/"+idTwo+"/state", map[string]string{"Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Errorf("a cross-site state read: %d", code)
	}
}
