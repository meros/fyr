package front

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/pipe"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// forwardRig is a front with one host over the in-process pipe (as in role
// both), a thread on it, and that thread's server: a small HTTP server that
// says what it was sent.
type forwardRig struct {
	f      *Front
	fs     *httptest.Server // the front's own port
	fwd    *httptest.Server // forward port 7450
	thread string
	port   int    // the thread's server
	other  int    // a server of the host that is not the thread's
	stop   func() // stops the thread's server, as when its process ends
}

func newForwardRig(t *testing.T, authStore *auth.Store) *forwardRig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	eng := engine.New(st)
	created := eng.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": t.TempDir(), "text": "x", "profile": "claude"}})
	if !created.OK {
		t.Fatalf("create: %+v", created)
	}
	rig := &forwardRig{thread: created.ThreadID}

	mux := http.NewServeMux()
	mux.HandleFunc("/seen", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"host": r.Host, "origin": r.Header.Get("Origin"), "referer": r.Header.Get("Referer"), "cookie": r.Header.Get("Cookie"),
			"forwarded": r.Header.Get("X-Forwarded-For") + r.Header.Get("Tailscale-User-Login") + r.Header.Get("X-Fyr")})
	})
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", auth.Cookie+"=stolen; Path=/")
		w.Header().Add("Set-Cookie", "app=1; Path=/")
		http.Redirect(w, r, "http://"+r.Host+"/next?a=1", http.StatusFound)
	})
	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s %s", r.Method, b)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "no upgrade", http.StatusBadRequest)
			return
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		sc := bufio.NewScanner(rw)
		for sc.Scan() {
			rw.WriteString("echo " + sc.Text() + " from " + r.Header.Get("Origin") + "\n")
			rw.Flush()
		}
	})
	dev := httptest.NewServer(mux)
	t.Cleanup(dev.Close)
	rig.stop = func() {
		dev.CloseClientConnections()
		dev.Close()
	}
	rig.port = dev.Listener.Addr().(*net.TCPAddr).Port
	otherLn, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { otherLn.Close() })
	rig.other = otherLn.Addr().(*net.TCPAddr).Port
	eng.LeftoverDeps.ThreadPorts = func(thread string) []int {
		if thread == rig.thread {
			return []int{rig.port}
		}
		return nil
	}

	ln := pipe.New()
	hs := &http.Server{Handler: (&server.Server{Engine: eng, BuildID: "host-build", Host: "desk", ID: testID("desk")}).PipeHandler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	rig.f = New(Options{Name: "front", BuildID: "b", ID: testID("front"), OwnerLogin: "me@example.com", Auth: authStore, ForwardPorts: []int{7450, 7451},
		Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}, MinBackoff: 50 * time.Millisecond},
		[]HostConfig{{Name: "desk", URL: LocalURL, Dial: ln.Dial}})
	ctx, cancel := context.WithCancel(context.Background())
	go rig.f.Run(ctx)
	rig.fs = httptest.NewServer(rig.f.Handler())
	rig.fwd = httptest.NewServer(rig.f.ForwardHandler(7450))
	t.Cleanup(func() {
		cancel()
		for _, s := range []*httptest.Server{rig.fs, rig.fwd} {
			s.CloseClientConnections()
			s.Close()
		}
	})
	eventually(t, "the host online", func() bool { return rig.f.byID(testID("desk")) != nil && rig.f.byID(testID("desk")).reportedID() != "" })
	return rig
}

// grant asks the front for a forward, as the client does on a press.
func (rig *forwardRig) grant(t *testing.T, host, thread string, port int, cookie *http.Cookie) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"host": host, "threadId": thread, "port": port})
	req, _ := http.NewRequest("POST", rig.fs.URL+"/api/front/forward", bytes.NewReader(body))
	req.Header.Set("X-Fyr", "1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", rig.fs.URL)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (rig *forwardRig) get(t *testing.T, path string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", rig.fwd.URL+path, nil)
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// V0.forwarded-ports: a press makes a forward of a thread's port, and the
// forward port then serves that server: its pages, its posts and a
// connection it upgrades, with the server seeing its own address.
func TestForwardServesAThreadsServerThroughTheHostLink(t *testing.T) {
	rig := newForwardRig(t, nil)
	desk := testID("desk")
	code, m := rig.grant(t, desk, rig.thread, rig.port, nil)
	if code != 200 || m["port"] != float64(7450) {
		t.Fatalf("grant: %d %v", code, m)
	}
	if code, m := rig.grant(t, desk, rig.thread, rig.port, nil); code != 200 || m["port"] != float64(7450) {
		t.Fatalf("the same link again takes the same port: %d %v", code, m)
	}
	self := "localhost:" + strconv.Itoa(rig.port)

	resp := rig.get(t, "/seen", map[string]string{"Origin": rig.fwd.URL, "Referer": rig.fwd.URL + "/page?x=1", "Cookie": auth.Cookie + "=secret; app=2; theme=dark",
		"X-Forwarded-For": "10.0.0.9", "X-Fyr": "1"})
	var seen map[string]string
	json.NewDecoder(resp.Body).Decode(&seen)
	resp.Body.Close()
	want := map[string]string{"host": self, "origin": "http://" + self, "referer": "http://" + self + "/page?x=1", "cookie": "app=2; theme=dark", "forwarded": ""}
	if resp.StatusCode != 200 || fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("what the server saw: %d %v, want %v", resp.StatusCode, seen, want)
	}

	resp = rig.get(t, "/redir", nil)
	resp.Body.Close()
	if resp.StatusCode != 302 || resp.Header.Get("Location") != rig.fwd.URL+"/next?a=1" {
		t.Fatalf("a redirect to the server's own address: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if c := resp.Header.Values("Set-Cookie"); len(c) != 1 || !strings.HasPrefix(c[0], "app=1") {
		t.Fatalf("the server set Fyr's session: %v", c)
	}

	// A form's post needs no X-Fyr here: it is the server's own.
	pr, err := http.Post(rig.fwd.URL+"/post", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(pr.Body)
	pr.Body.Close()
	if pr.StatusCode != 200 || string(b) != "POST hello" {
		t.Fatalf("post: %d %q", pr.StatusCode, b)
	}

	// An upgraded connection (a WebSocket's handshake) is carried both ways.
	c, err := net.Dial("tcp", rig.fwd.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(c, "GET /ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nOrigin: %s\r\nSec-WebSocket-Key: x\r\n\r\n", rig.fwd.Listener.Addr(), rig.fwd.URL)
	rd := bufio.NewReader(c)
	status, _ := rd.ReadString('\n')
	if !strings.HasPrefix(status, "HTTP/1.1 101") {
		t.Fatalf("upgrade: %q", status)
	}
	for {
		if ln, err := rd.ReadString('\n'); err != nil || ln == "\r\n" {
			break
		}
	}
	for _, word := range []string{"one", "two"} {
		fmt.Fprintf(c, "%s\n", word)
		if ln, _ := rd.ReadString('\n'); ln != "echo "+word+" from http://"+self+"\n" {
			t.Fatalf("over the upgraded connection: %q", ln)
		}
	}
}

// V0.forwarded-ports: what a forward refuses, and what the front does not
// forward at all.
func TestForwardRefusals(t *testing.T) {
	rig := newForwardRig(t, nil)
	desk := testID("desk")
	if code, m := rig.grant(t, desk, rig.thread, rig.other, nil); code != 404 || m["code"] != "not-thread-port" {
		t.Fatalf("a port that is not the thread's: %d %v", code, m)
	}
	for name, c := range map[string]struct {
		host, thread string
		port         int
	}{"unknown host": {"id-nobody", rig.thread, rig.port}, "no thread": {desk, "", rig.port}, "port 0": {desk, rig.thread, 0}, "a path as thread": {desk, "a/b", rig.port}} {
		if code, m := rig.grant(t, c.host, c.thread, c.port, nil); code != 400 {
			t.Errorf("%s: %d %v", name, code, m)
		}
	}
	if code, m := rig.grant(t, desk, "t_none", rig.port, nil); code != 502 && code != 404 {
		t.Errorf("an unknown thread: %d %v", code, m)
	}
	// Nothing was granted: the port serves nothing.
	if resp := rig.get(t, "/seen", nil); resp.StatusCode != 404 {
		t.Fatalf("an empty forward port: %d", resp.StatusCode)
	}
	if code, _ := rig.grant(t, desk, rig.thread, rig.port, nil); code != 200 {
		t.Fatalf("grant: %d", code)
	}
	for name, hdr := range map[string]map[string]string{
		"another origin":   {"Origin": "https://evil.example"},
		"Fyr's own origin": {"Origin": rig.fs.URL}, // the forward is its own origin: Fyr's pages do not call it either
		"another site":     {"Sec-Fetch-Site": "cross-site"},
		"another Host":     {"Host": "evil.example"},
	} {
		resp := rig.get(t, "/seen", hdr)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("%s: %d, want 403", name, resp.StatusCode)
		}
	}
	if resp := rig.get(t, "/seen", map[string]string{"Fyr-Actor": "x"}); resp.StatusCode != 400 {
		t.Errorf("Fyr-Actor from a browser: %d", resp.StatusCode)
	}
	if resp := rig.get(t, "/seen", map[string]string{"Sec-Fetch-Site": "same-site"}); resp.StatusCode != 200 {
		t.Errorf("the press on the link in Fyr (same site): %d", resp.StatusCode)
	}
	// The server stopped: its kept-alive connection ends with it, and the
	// host refuses the next one (the port is no longer the thread's).
	rig.stop()
	stopped := rig.port
	rig.port = -1
	if resp := rig.get(t, "/seen", nil); resp.StatusCode != 502 {
		t.Errorf("after the server stopped: %d", resp.StatusCode)
	}
	rig.port = stopped

	// The pool: a third link takes the port used longest ago.
	f := rig.f
	if p := f.forwardSlot(desk, "t_b", 3000); p != 7451 {
		t.Fatalf("the second link: port %d", p)
	}
	f.fwd.slots[7450].used = time.Now().Add(-time.Hour)
	if p := f.forwardSlot(desk, "t_c", 3001); p != 7450 {
		t.Fatalf("the third link replaces the oldest: port %d", p)
	}
	if p := f.forwardSlot(desk, "t_b", 3000); p != 7451 {
		t.Fatalf("a link keeps its port: %d", p)
	}

	// A front without forward ports says so.
	bare := newFront(t)
	resp, err := http.Post(bare.URL+"/api/front/forward", "application/json", strings.NewReader(`{"host":"x","threadId":"t","port":1}`))
	if err == nil {
		resp.Body.Close()
	}
	req, _ := http.NewRequest("POST", bare.URL+"/api/front/forward", strings.NewReader(`{"host":"x","threadId":"t","port":1}`))
	req.Header.Set("X-Fyr", "1")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	resp.Body.Close()
	if resp.StatusCode != 409 || m["code"] != "no-forward-ports" {
		t.Fatalf("no pool: %d %v", resp.StatusCode, m)
	}
}

// V0.forwarded-ports: in enforce mode a forward port serves a signed-in
// browser only.
func TestForwardNeedsTheSessionInEnforce(t *testing.T) {
	st, err := auth.Open(kv{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rig := newForwardRig(t, st)
	// Sign in on the front's own port, as fyr open does.
	resp := serveOne(rig.f.OwnerHandler("http://fyr.localhost:7777"), httptest.NewRequest("POST", "http://owner/session/once", nil))
	var got struct{ URL string }
	json.NewDecoder(resp.Body).Decode(&got)
	_, tok, _ := strings.Cut(got.URL, "#")
	r := httptest.NewRequest("POST", "http://fyr.localhost:7777/auth/once", bytes.NewBufferString(`{"token":"`+tok+`"}`))
	r.Header.Set("X-Fyr", "1")
	cookie := sessionCookie(serveOne(rig.f.Handler(), r))
	if cookie == nil {
		t.Fatal("no session")
	}
	if err := st.SetMode(auth.WayBrowser, auth.ModeEnforce, "owner.sock"); err != nil {
		t.Fatal(err)
	}
	if code, m := rig.grant(t, testID("desk"), rig.thread, rig.port, cookie); code != 200 {
		t.Fatalf("grant: %d %v", code, m)
	}
	out := rig.get(t, "/seen", nil)
	body, _ := io.ReadAll(out.Body)
	out.Body.Close()
	if out.StatusCode != 401 || !strings.Contains(string(body), "Sign in to Fyr first") {
		t.Fatalf("without a session: %d %q", out.StatusCode, body)
	}
	if out := rig.get(t, "/seen", map[string]string{"Cookie": auth.Cookie + "=nope"}); out.StatusCode != 401 {
		t.Fatalf("with a wrong session: %d", out.StatusCode)
	}
	in := rig.get(t, "/seen", map[string]string{"Cookie": auth.Cookie + "=" + cookie.Value + "; app=2"})
	var seen map[string]string
	json.NewDecoder(in.Body).Decode(&seen)
	in.Body.Close()
	if in.StatusCode != 200 || seen["cookie"] != "app=2" {
		t.Fatalf("with the session: %d %v", in.StatusCode, seen)
	}
}

// V0.forwarded-ports: the link in a message points at /forward/open, which
// makes the forward and redirects to it; on the thread's own machine it
// redirects to the server itself.
func TestForwardOpenRedirectsToTheForwardOrTheServerItself(t *testing.T) {
	rig := newForwardRig(t, nil)
	desk := testID("desk")
	open := func(hostHeader, query string, via func(http.Handler) http.Handler) *http.Response {
		r := httptest.NewRequest("GET", "http://"+hostHeader+"/forward/open?"+query, nil)
		h := rig.f.mux().ServeMux
		if via != nil {
			return serveOne(via(h), r)
		}
		return serveOne(h, r)
	}
	q := fmt.Sprintf("host=%s&thread=%s&port=%d&path=%s", desk, rig.thread, rig.port, "%2Fdocs%2Fa%3Fb%3D1%23top")
	// A browser on another machine (here: the LAN listener's name).
	lan := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Forwarded-Proto", "https")
			h.ServeHTTP(w, r)
		})
	}
	resp := open("fyr.example.com:7443", q, lan)
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "https://fyr.example.com:7450/docs/a?b=1#top" {
		t.Fatalf("from another machine: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if rig.f.forwardOf(7450) == nil {
		t.Fatal("no forward was made")
	}
	// On the machine the server runs on (the front's own host, a loopback name).
	for _, name := range []string{"fyr.localhost:7777", "127.0.0.1:7777", "localhost:7777"} {
		resp := open(name, q, nil)
		if want := fmt.Sprintf("http://localhost:%d/docs/a?b=1#top", rig.port); resp.StatusCode != 302 || resp.Header.Get("Location") != want {
			t.Fatalf("%s: %d %q, want %q", name, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
	// A path that would leave the forward's address is dropped.
	for _, bad := range []string{"%2F%2Fevil.example%2Fx", "https%3A%2F%2Fevil.example", "x"} {
		resp := open("fyr.example.com:7443", fmt.Sprintf("host=%s&thread=%s&port=%d&path=%s", desk, rig.thread, rig.port, bad), lan)
		if resp.Header.Get("Location") != "https://fyr.example.com:7450/" {
			t.Errorf("path %s: %q", bad, resp.Header.Get("Location"))
		}
	}
	// A port that is not the thread's: a page that says so, and which ports are.
	resp = open("fyr.example.com:7443", fmt.Sprintf("host=%s&thread=%s&port=%d", desk, rig.thread, rig.other), lan)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 404 || !strings.Contains(string(body), "listen on: "+strconv.Itoa(rig.port)) {
		t.Fatalf("another port: %d %s", resp.StatusCode, body)
	}
	if resp := open("fyr.example.com:7443", "host=nobody&thread=t&port=1", lan); resp.StatusCode != 400 {
		t.Fatalf("an unknown host: %d", resp.StatusCode)
	}
}
