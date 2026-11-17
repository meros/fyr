package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *engine.Engine) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(st)
	s := &Server{Engine: e, BuildID: "b1"}
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
	if r := do(t, "GET", hs.URL+"/api/state", "localhost:7777", nil, ""); r.StatusCode != 200 {
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
	if s.Host == "" || s.BuildID != "b1" || len(s.Threads) != 1 || len(s.Actions) < 10 || len(s.Projects) != 0 { // V0.28: no auto project
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
	ts := "host-a.example.ts.net"
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

// review 2026-11-15 (SPEC 0.5 V0.14): the tailnet identity header does not
// lift the Host check. A DNS-rebinding page reaches loopback with its own
// Host and can set any header, so a header request must name a ts.net host,
// the only Host Tailscale Serve forwards (https://<host>.ts.net).
func TestTailnetHeaderNeedsTailnetHost(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	s := &Server{Engine: engine.New(st), BuildID: "b1", OwnerLogin: "owner@example.com"}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	owner := map[string]string{"Tailscale-User-Login": "owner@example.com", "X-Fyr": "1"}
	for _, host := range []string{"evil.example", "evil.example:7443", "ts.net.evil.example", "evil-ts.net"} {
		if r := do(t, "GET", hs.URL+"/api/state", host, owner, ""); r.StatusCode != 403 {
			t.Fatalf("rebinding host %q with identity header: %d", host, r.StatusCode)
		}
	}
	for _, host := range []string{"box.tail1234.ts.net", "box.tail1234.ts.net:7443", "BOX.Tail1234.TS.NET."} {
		if r := do(t, "GET", hs.URL+"/api/state", host, owner, ""); r.StatusCode != 200 {
			t.Fatalf("tailnet host %q: %d", host, r.StatusCode)
		}
	}
}

// SPEC 0.5 V0.18: upload with X-Fyr and a file name; read it back; limits.
func TestAttachmentEndpoints(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	s := &Server{Engine: engine.New(st), BuildID: "b1", Attach: &attach.Store{Dir: t.TempDir(), Max: 16}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	h := map[string]string{"X-Fyr": "1", "X-Fyr-Filename": "sk%C3%A4rm.png", "Content-Type": "image/png"}
	if r := do(t, "POST", hs.URL+"/api/attachments", "", map[string]string{"X-Fyr-Filename": "a.png"}, "x"); r.StatusCode != 403 {
		t.Fatalf("no X-Fyr: %d", r.StatusCode)
	}
	r := do(t, "POST", hs.URL+"/api/attachments", "", h, "PNGDATA")
	var m attach.Meta
	json.NewDecoder(r.Body).Decode(&m)
	if r.StatusCode != 200 || m.Name != "skärm.png" || m.Size != 7 || m.MediaType != "image/png" || m.ID == "" {
		t.Fatalf("upload %d %+v", r.StatusCode, m)
	}
	r = do(t, "GET", hs.URL+"/api/attachments/"+m.ID, "", nil, "")
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || string(b) != "PNGDATA" || r.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("get %d %q %v", r.StatusCode, b, r.Header)
	}
	if r := do(t, "POST", hs.URL+"/api/attachments", "", h, strings.Repeat("x", 17)); r.StatusCode != 413 {
		t.Fatalf("over the limit: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/attachments/nope", "", nil, ""); r.StatusCode != 404 {
		t.Fatalf("unknown: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.19, V0.47: previews are served sandboxed: scripts run, but the
// page has no origin, so it cannot read the API or cookies.
func TestPreviewSandbox(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	att := &attach.Store{Dir: t.TempDir()}
	s := &Server{Engine: engine.New(st), BuildID: "b1", Attach: att}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	m, _ := att.Save("mock.html", "text/html", strings.NewReader("<p>hi</p>"))
	r := do(t, "GET", hs.URL+"/api/previews/"+m.ID, "", nil, "")
	csp := r.Header.Get("Content-Security-Policy")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") || !strings.HasPrefix(csp, "sandbox allow-scripts;") || strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "'self' ") {
		t.Fatalf("%d %v", r.StatusCode, r.Header)
	}
	img, _ := att.Save("a.png", "image/png", strings.NewReader("x"))
	if r := do(t, "GET", hs.URL+"/api/previews/"+img.ID, "", nil, ""); r.StatusCode != 404 {
		t.Fatalf("a non-HTML attachment served as a preview: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/previews/"+m.ID+"/mock.html", "", nil, ""); r.StatusCode != 404 {
		t.Fatalf("a single page served as a folder: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.47: the CSP allows the preview's own folder (an absolute
// prefix: 'self' matches nothing without an origin) and the CDN list. The
// layout test serves its folder preview with the same policy from
// web/test/fixtures/preview-csp.txt.
func TestPreviewCSP(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "test", "fixtures", "preview-csp.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	base := "http://127.0.0.1:7777/api/previews/0123456789abcdef0123/"
	want := strings.NewReplacer("{base}", base, "{cdn}", lines[1]).Replace(lines[0])
	req := httptest.NewRequest("GET", "/api/previews/0123456789abcdef0123", nil)
	req.Host = "127.0.0.1:7777"
	if got := previewCSP(req, "0123456789abcdef0123"); got != want {
		t.Fatalf("CSP\n got %s\nwant %s", got, want)
	}
	req.Host = "desk.tail.ts.net"
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := previewCSP(req, "0123456789abcdef0123"); !strings.Contains(got, "script-src https://desk.tail.ts.net/api/previews/0123456789abcdef0123/ https://cdnjs") {
		t.Fatal(got)
	}
	req.Host = "x;script-src *"
	if got := previewCSP(req, "0123456789abcdef0123"); strings.Contains(got, "*") || strings.Contains(got, "x;") {
		t.Fatalf("a strange Host reached the policy: %s", got)
	}
}

// SPEC 0.5 V0.47: a folder preview redirects to its page and serves its files
// with their media types, never a path outside the folder.
func TestPreviewFolder(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	att := &attach.Store{Dir: t.TempDir()}
	s := &Server{Engine: engine.New(st), BuildID: "b1", Attach: att}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	root := t.TempDir()
	files := map[string]string{"index.html": "<script type=module src=app.mjs></script>", "app.mjs": "x", "lib/three.js": "x", "duck.glb": "glTF", "duck.gltf": "{}", "dec.wasm": "\x00asm", "sky.hdr": "#?RADIANCE", "t.ktx2": "x", "a.png": "x", "s.css": "x", "d.json": "{}", "f.woff2": "x"}
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(c), 0o644)
	}
	list, _, err := attach.ScanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	m, err := att.SaveTree("site", "index.html", list)
	if err != nil {
		t.Fatal(err)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, _ := noFollow.Get(hs.URL + "/api/previews/" + m.ID)
	if r.StatusCode != 302 || r.Header.Get("Location") != "/api/previews/"+m.ID+"/index.html" {
		t.Fatalf("redirect %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	types := map[string]string{"index.html": "text/html; charset=utf-8", "app.mjs": "text/javascript; charset=utf-8", "lib/three.js": "text/javascript; charset=utf-8",
		"duck.glb": "model/gltf-binary", "duck.gltf": "model/gltf+json", "dec.wasm": "application/wasm", "sky.hdr": "image/vnd.radiance", "t.ktx2": "image/ktx2",
		"a.png": "image/png", "s.css": "text/css; charset=utf-8", "d.json": "application/json", "f.woff2": "font/woff2"}
	for p, ct := range types {
		r := do(t, "GET", hs.URL+"/api/previews/"+m.ID+"/"+p, "", nil, "")
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 || r.Header.Get("Content-Type") != ct || string(body) != files[p] {
			t.Fatalf("%s: %d %q %q", p, r.StatusCode, r.Header.Get("Content-Type"), body)
		}
		if r.Header.Get("Access-Control-Allow-Origin") != "*" || r.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(r.Header.Get("Content-Security-Policy"), "sandbox allow-scripts;") {
			t.Fatalf("%s: headers %v", p, r.Header)
		}
	}
	for _, bad := range []string{"../" + m.ID + "/meta.json", "%2e%2e/" + m.ID + "/meta.json", "lib/..%2f..%2fmeta.json", "lib", "missing.js", "..%2fmeta.json"} {
		if r := do(t, "GET", hs.URL+"/api/previews/"+m.ID+"/"+bad, "", nil, ""); r.StatusCode == 200 {
			body, _ := io.ReadAll(r.Body)
			t.Fatalf("%s served: %q", bad, body)
		}
	}
}

// SPEC 0.5 V0.25: the Changes tab's data: the branch diff of the thread's
// folder, one file's patch, {git:false} outside git, 404 for no thread.
func TestThreadChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	hs, _ := newServer(t)
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	run("add", "a.txt")
	run("commit", "-q", "-m", "x")
	run("checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)

	create := func(id, cwd string) string {
		r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"},
			`{"commandId":"`+id+`","action":"thread.create","args":{"cwd":`+strconv.Quote(cwd)+`,"text":"hi"}}`)
		var rep struct {
			ThreadID string `json:"threadId"`
		}
		json.NewDecoder(r.Body).Decode(&rep)
		if rep.ThreadID == "" {
			t.Fatalf("create: %d", r.StatusCode)
		}
		return rep.ThreadID
	}
	id := create("cg1", dir)

	r := do(t, "GET", hs.URL+"/api/threads/"+id+"/changes", "", nil, "")
	var c struct {
		Git    bool   `json:"git"`
		Branch string `json:"branch"`
		Base   string `json:"base"`
		Files  []struct {
			Path      string `json:"path"`
			Status    string `json:"status"`
			Additions int    `json:"additions"`
		} `json:"files"`
	}
	json.NewDecoder(r.Body).Decode(&c)
	if r.StatusCode != 200 || !c.Git || c.Branch != "feature" || c.Base != "main" || len(c.Files) != 1 || c.Files[0].Path != "a.txt" || c.Files[0].Additions != 1 {
		t.Fatalf("%d %+v", r.StatusCode, c)
	}

	r = do(t, "GET", hs.URL+"/api/threads/"+id+"/changes?path=a.txt", "", nil, "")
	var p struct {
		Path  string `json:"path"`
		Patch string `json:"patch"`
	}
	json.NewDecoder(r.Body).Decode(&p)
	if r.StatusCode != 200 || p.Path != "a.txt" || !strings.Contains(p.Patch, "+two") {
		t.Fatalf("%d %+v", r.StatusCode, p)
	}

	plain := create("cg2", t.TempDir())
	r = do(t, "GET", hs.URL+"/api/threads/"+plain+"/changes", "", nil, "")
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || strings.TrimSpace(string(body)) != `{"git":false}` {
		t.Fatalf("not git: %d %s", r.StatusCode, body)
	}

	if r := do(t, "GET", hs.URL+"/api/threads/nope/changes", "", nil, ""); r.StatusCode != 404 {
		t.Fatalf("unknown thread: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/threads/"+id+"/changes", "evil.example", nil, ""); r.StatusCode != 403 {
		t.Fatalf("foreign host: %d", r.StatusCode)
	}
}

// SPEC 0.5 V0.65: the owner's look and Viewed marks shape the reply: the
// files new since the look, viewed until the content changes, the since
// patch; the look stays out of the thread DTO.
func TestThreadChangesSinceYouLooked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	hs, _ := newServer(t)
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bee\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "x")
	run("checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bee\nb2\n"), 0o644)
	run("commit", "-qam", "work")

	hdr := map[string]string{"X-Fyr": "1"}
	r := do(t, "POST", hs.URL+"/api/command", "", hdr, `{"commandId":"cs1","action":"thread.create","args":{"cwd":`+strconv.Quote(dir)+`,"text":"hi"}}`)
	var rep struct {
		ThreadID string `json:"threadId"`
	}
	json.NewDecoder(r.Body).Decode(&rep)
	id := rep.ThreadID
	type file struct {
		Path      string `json:"path"`
		Blob      string `json:"blob"`
		Viewed    bool   `json:"viewed"`
		Additions int    `json:"additions"`
	}
	type reply struct {
		Head  string `json:"head"`
		Files []file `json:"files"`
		Since *struct {
			Head  string `json:"head"`
			Mode  string `json:"mode"`
			Files []file `json:"files"`
		} `json:"since"`
	}
	get := func() reply {
		t.Helper()
		r := do(t, "GET", hs.URL+"/api/threads/"+id+"/changes", "", nil, "")
		var c reply
		json.NewDecoder(r.Body).Decode(&c)
		if r.StatusCode != 200 {
			t.Fatalf("changes: %d", r.StatusCode)
		}
		return c
	}
	command := func(cid, action, args string) {
		t.Helper()
		r := do(t, "POST", hs.URL+"/api/command", "", hdr, `{"commandId":"`+cid+`","action":"`+action+`","threadId":"`+id+`","args":`+args+`}`)
		if r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("%s: %d %s", action, r.StatusCode, b)
		}
	}
	c := get()
	if c.Head == "" || len(c.Files) != 2 || c.Files[0].Blob == "" || c.Since != nil {
		t.Fatalf("first look: %+v", c)
	}
	files := map[string]string{}
	for _, f := range c.Files {
		files[f.Path] = f.Blob
	}
	fb, _ := json.Marshal(files)
	command("cs2", "thread.changesSeen", `{"head":"`+c.Head+`","files":`+string(fb)+`}`)
	command("cs3", "thread.fileViewed", `{"path":"a.txt","blob":"`+files["a.txt"]+`","viewed":true}`)
	command("cs4", "thread.fileViewed", `{"path":"b.txt","blob":"`+files["b.txt"]+`","viewed":true}`)

	r = do(t, "GET", hs.URL+"/api/threads/"+id, "", nil, "")
	body, _ := io.ReadAll(r.Body)
	if strings.Contains(string(body), "changesLook") || strings.Contains(string(body), files["a.txt"]) {
		t.Fatalf("the look is in the thread DTO: %s", body)
	}

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	c = get()
	if c.Since == nil || c.Since.Mode != "ancestor" || c.Since.Head != c.Head || len(c.Since.Files) != 1 || c.Since.Files[0].Path != "a.txt" || c.Since.Files[0].Additions != 1 {
		t.Fatalf("since: %+v", c.Since)
	}
	v := map[string]bool{}
	for _, f := range c.Files {
		v[f.Path] = f.Viewed
	}
	if v["a.txt"] || !v["b.txt"] {
		t.Fatalf("viewed resets for a.txt only: %+v", c.Files)
	}

	r = do(t, "GET", hs.URL+"/api/threads/"+id+"/changes?path=a.txt&since="+c.Head, "", nil, "")
	var p struct {
		Patch string `json:"patch"`
	}
	json.NewDecoder(r.Body).Decode(&p)
	if r.StatusCode != 200 || !strings.Contains(p.Patch, "+three") || strings.Contains(p.Patch, "+two") {
		t.Fatalf("since patch: %d %q", r.StatusCode, p.Patch)
	}
	if r := do(t, "GET", hs.URL+"/api/threads/"+id+"/changes?path=a.txt&since=HEAD", "", nil, ""); r.StatusCode != 400 {
		t.Fatalf("since=HEAD: %d", r.StatusCode)
	}
}

type fakeDetails struct {
	fresh []bool
	err   error
}

func (f *fakeDetails) Get(_ context.Context, url string, fresh bool) (github.Detail, error) {
	f.fresh = append(f.fresh, fresh)
	if f.err != nil {
		return github.Detail{}, f.err
	}
	return github.Detail{URL: url, Number: 42, Title: "T", State: "OPEN", Checks: []github.Check{{Name: "lint", State: "fail"}}}, nil
}

// SPEC 0.5 V0.43: GET /api/threads/{id}/pr returns the detail, errors as 200 {error}.
func TestThreadPRDetail(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := engine.New(st)
	fd := &fakeDetails{}
	hs := httptest.NewServer((&Server{Engine: e, PR: fd}).Handler())
	defer hs.Close()
	r := e.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": t.TempDir(), "text": "hi"}})
	id := r.ThreadID
	get := func(path string) (int, map[string]any) {
		resp := do(t, "GET", hs.URL+path, "", nil, "")
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	if code, _ := get("/api/threads/t_nope/pr"); code != 404 {
		t.Fatalf("unknown thread: %d", code)
	}
	if code, m := get("/api/threads/" + id + "/pr"); code != 200 || m["error"] == nil {
		t.Fatalf("no PR: %d %v", code, m)
	}
	coretest.Patch(e, id, func(th *core.Thread, now time.Time) []core.Event {
		core.PRObserved(th, core.PR{URL: "https://github.com/a/b/pull/42", Repo: "a/b", Number: 42, State: "OPEN"}, now)
		return nil
	})
	code, m := get("/api/threads/" + id + "/pr?fresh=1")
	if code != 200 || m["number"] != float64(42) || m["error"] != nil {
		t.Fatalf("detail: %d %v", code, m)
	}
	if checks, _ := m["checks"].([]any); len(checks) != 1 {
		t.Fatalf("checks %v", m["checks"])
	}
	get("/api/threads/" + id + "/pr")
	if len(fd.fresh) != 2 || !fd.fresh[0] || fd.fresh[1] {
		t.Fatalf("fresh flags %v", fd.fresh)
	}
	fd.err = errors.New("gh is not signed in on this host")
	if code, m := get("/api/threads/" + id + "/pr"); code != 200 || m["error"] != "gh is not signed in on this host" {
		t.Fatalf("error: %d %v", code, m)
	}
}

// SPEC 0.5 V0.45: the dialog asks which rung a handover would use.
func TestHandoverPlan(t *testing.T) {
	hs, e := newServer(t)
	r := e.Command(api.Cmd{CommandID: "h1", Action: "thread.create", Args: map[string]any{"cwd": "/tmp", "text": "hi"}})
	if got := do(t, "GET", hs.URL+"/api/threads/nope/handover?profile=b", "", nil, ""); got.StatusCode != 404 {
		t.Fatalf("unknown thread: %d", got.StatusCode)
	}
	resp := do(t, "GET", hs.URL+"/api/threads/"+r.ThreadID+"/handover?profile=b", "", nil, "")
	var p struct{ Rung string }
	json.NewDecoder(resp.Body).Decode(&p)
	if resp.StatusCode != 200 || p.Rung != "adopt" {
		t.Fatalf("%d %+v", resp.StatusCode, p)
	}
}

type fakePeers map[string]string

func (f fakePeers) Login(_ context.Context, addr string) (string, error) {
	if l, ok := f[addr]; ok {
		return l, nil
	}
	return "", errors.New("unknown peer")
}

// SPEC 0.5 V0.51: on a host's tailnet address only peers whose Tailscale
// identity is the owner's are served; an identity header is not trusted.
func TestPeerGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	g := PeerGuard("owner@example.com", fakePeers{"100.64.0.2:5000": "owner@example.com", "100.64.0.3:5000": "other@example.com"}, ok)
	try := func(addr, method string, hdr map[string]string) int {
		r := httptest.NewRequest(method, "http://100.64.0.1:7778/api/state", nil)
		r.RemoteAddr = addr
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w.Code
	}
	if c := try("100.64.0.2:5000", "GET", nil); c != 204 {
		t.Fatalf("owner peer: %d", c)
	}
	if c := try("100.64.0.3:5000", "GET", map[string]string{"Tailscale-User-Login": "owner@example.com"}); c != 403 {
		t.Fatalf("other peer with a forged header: %d", c)
	}
	if c := try("100.64.0.9:5000", "GET", nil); c != 403 {
		t.Fatalf("unknown peer: %d", c)
	}
	if c := try("100.64.0.2:5000", "POST", nil); c != 403 {
		t.Fatalf("POST without X-Fyr: %d", c)
	}
	if c := try("100.64.0.2:5000", "POST", map[string]string{"X-Fyr": "1"}); c != 204 {
		t.Fatalf("owner POST: %d", c)
	}
	if g := PeerGuard("", fakePeers{"100.64.0.2:5000": ""}, ok); g != nil {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://100.64.0.1:7778/api/state", nil)
		r.RemoteAddr = "100.64.0.2:5000"
		g.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("no owner configured: %d", w.Code)
		}
	}
}

// A request on Serve's socket carries tailscaled's identity header with a
// Host that is not a ts.net name; the owner is served, any other login is not.
func TestServeSocketNeedsNoTailnetHost(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db"))
	defer st.Close()
	s := &Server{Engine: engine.New(st), BuildID: "b1", OwnerLogin: "owner@example.com"}
	hs := httptest.NewServer(ViaServe(s.Handler()))
	defer hs.Close()
	if r := do(t, "GET", hs.URL+"/api/state", "localhost", map[string]string{"Tailscale-User-Login": "owner@example.com"}, ""); r.StatusCode != 200 {
		t.Fatalf("owner on the Serve socket: %d", r.StatusCode)
	}
	if r := do(t, "GET", hs.URL+"/api/state", "localhost", map[string]string{"Tailscale-User-Login": "guest@example.com"}, ""); r.StatusCode != 403 {
		t.Fatalf("other login on the Serve socket: %d", r.StatusCode)
	}
}
