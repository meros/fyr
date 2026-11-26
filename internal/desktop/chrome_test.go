package desktop

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeChromium answers every command with {} (Browser.getVersion with a user
// agent) and lets the test send events, as Chromium does on its pipe.
type fakeChromium struct {
	t    *testing.T
	args []string

	mu   sync.Mutex
	cmds []cdpMsg
	out  io.WriteCloser // to the app
	got  chan cdpMsg
}

func (f *fakeChromium) start(args []string) (*exec.Cmd, io.ReadCloser, io.WriteCloser, error) {
	f.args = args
	appR, chromeW := io.Pipe() // Chromium -> app
	chromeR, appW := io.Pipe() // app -> Chromium
	f.out = chromeW
	f.got = make(chan cdpMsg, 256)
	go func() {
		br := bufio.NewReader(chromeR)
		for {
			b, err := br.ReadBytes(0)
			if err != nil {
				return
			}
			var m cdpMsg
			json.Unmarshal(b[:len(b)-1], &m)
			f.mu.Lock()
			f.cmds = append(f.cmds, m)
			f.mu.Unlock()
			res := `{}`
			if m.Method == "Browser.getVersion" {
				res = `{"userAgent":"Mozilla/5.0 Chrome/157.0.0.0"}`
			}
			f.write(`{"id":` + itoa(m.ID) + `,"result":` + res + `}`)
			f.got <- m
		}
	}()
	return nil, appR, appW, nil
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func (f *fakeChromium) write(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out.Write(append([]byte(s), 0))
}

func (f *fakeChromium) event(session, method, params string) {
	s := ""
	if session != "" {
		s = `,"sessionId":"` + session + `"`
	}
	f.write(`{"method":"` + method + `","params":` + params + s + `}`)
}

// waitFor waits for a command that matches, after the ones seen before.
func (f *fakeChromium) waitFor(method string, match func(cdpMsg) bool) cdpMsg {
	f.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-f.got:
			if m.Method == method && (match == nil || match(m)) {
				return m
			}
		case <-deadline:
			f.t.Fatalf("no %s", method)
		}
	}
}

func param(m cdpMsg, k string) string {
	var p map[string]any
	json.Unmarshal(m.Params, &p)
	s, _ := p[k].(string)
	return s
}

func newTestWindow(t *testing.T) (*Chrome, *fakeChromium, *testRecord) {
	f := &fakeChromium{t: t}
	rec := &testRecord{signedOut: true}
	w := &Chrome{
		Profile: "/p", Size: Geometry{Width: 1000, Height: 700},
		Origin:     func() string { return "http://fyr.localhost:7777" },
		SignedOut:  func() bool { rec.add("signedOut"); return rec.signedOut },
		LoadFailed: func() { rec.add("loadFailed") },
		External:   func(u string) { rec.add("external " + u) },
		Bridge: func(m []byte) (any, error) {
			rec.add("bridge " + string(m))
			return map[string]bool{"on": true}, nil
		},
		Start: f.start,
	}
	return w, f, rec
}

type testRecord struct {
	mu        sync.Mutex
	got       []string
	signedOut bool
}

func (r *testRecord) add(s string) { r.mu.Lock(); r.got = append(r.got, s); r.mu.Unlock() }
func (r *testRecord) has(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < 300; i++ {
		r.mu.Lock()
		for _, g := range r.got {
			if g == s {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q in %q", s, r.got)
}

// open starts the window and attaches its page as Chromium does.
func open(t *testing.T, w *Chrome, f *fakeChromium, u string) {
	t.Helper()
	w.Show(u)
	f.waitFor("Target.setAutoAttach", nil)
	f.event("", "Target.attachedToTarget", `{"sessionId":"S","targetInfo":{"targetId":"T","type":"page","url":"about:blank"},"waitingForDebugger":false}`)
}

// V0.desktop-chromium: the app window starts on Fyr's origin (a stable app id),
// gets the bridge, the user agent and the request hooks, and only then loads
// the page the app wants.
func TestChromeWindowHooksComeBeforeThePage(t *testing.T) {
	w, f, _ := newTestWindow(t)
	open(t, w, f, "http://fyr.localhost:7777/?open=t_1")
	if got := f.args[len(f.args)-1]; got != "--app=http://fyr.localhost:7777/" {
		t.Fatalf("boot flag = %q", got)
	}
	if !strings.Contains(strings.Join(f.args, " "), "--remote-debugging-pipe") || !strings.Contains(strings.Join(f.args, " "), "--window-size=1000,700") {
		t.Fatalf("args = %q", f.args)
	}
	ua := f.waitFor("Emulation.setUserAgentOverride", nil)
	if param(ua, "userAgent") != "Mozilla/5.0 Chrome/157.0.0.0 FyrDesktop/linux" {
		t.Fatalf("user agent = %s", ua.Params)
	}
	if sw := f.waitFor("Storage.clearDataForOrigin", nil); param(sw, "storageTypes") != "service_workers" || param(sw, "origin") != "http://fyr.localhost:7777" {
		t.Fatalf("clear = %s", sw.Params)
	}
	f.waitFor("Runtime.addBinding", nil)
	sc := f.waitFor("Page.addScriptToEvaluateOnNewDocument", nil)
	if !strings.Contains(param(sc, "source"), "messageHandlers: { fyrPush }") {
		t.Fatalf("bridge script = %s", sc.Params)
	}
	f.waitFor("Fetch.enable", nil)
	nav := f.waitFor("Page.navigate", nil)
	if param(nav, "url") != "http://fyr.localhost:7777/?open=t_1" || nav.SessionID != "S" {
		t.Fatalf("navigate = %s on %q", nav.Params, nav.SessionID)
	}
}

// A main-frame 401 asks for a new link instead of showing the signed-out page;
// a frame's own 401 is the frame's.
func TestChromeWindowSignsInOnA401(t *testing.T) {
	w, f, rec := newTestWindow(t)
	open(t, w, f, "")
	f.waitFor("Page.navigate", nil)
	f.event("S", "Fetch.requestPaused", `{"requestId":"r1","request":{"url":"http://fyr.localhost:7777/"},"frameId":"T","resourceType":"Document","responseStatusCode":401}`)
	if m := f.waitFor("Fetch.failRequest", nil); param(m, "requestId") != "r1" {
		t.Fatalf("failed %s", m.Params)
	}
	rec.has(t, "signedOut")
	f.event("S", "Fetch.requestPaused", `{"requestId":"r2","request":{"url":"http://fyr.localhost:7777/api/previews/p/"},"frameId":"F2","resourceType":"Document","responseStatusCode":401}`)
	if m := f.waitFor("Fetch.continueRequest", nil); param(m, "requestId") != "r2" {
		t.Fatalf("continued %s", m.Params)
	}
}

// A main-frame navigation off Fyr goes to the default browser; a failed load
// tells the app.
func TestChromeWindowSendsLinksOutAndReportsFailures(t *testing.T) {
	w, f, rec := newTestWindow(t)
	open(t, w, f, "")
	f.waitFor("Page.navigate", nil)
	f.event("S", "Fetch.requestPaused", `{"requestId":"r1","request":{"url":"https://github.com/acme/x/pull/1"},"frameId":"T","resourceType":"Document"}`)
	f.waitFor("Fetch.failRequest", func(m cdpMsg) bool { return param(m, "requestId") == "r1" })
	rec.has(t, "external https://github.com/acme/x/pull/1")
	f.event("S", "Fetch.requestPaused", `{"requestId":"r2","request":{"url":"http://fyr.localhost:7777/"},"frameId":"T","resourceType":"Document","responseErrorReason":"ConnectionRefused"}`)
	f.waitFor("Fetch.continueRequest", func(m cdpMsg) bool { return param(m, "requestId") == "r2" })
	rec.has(t, "loadFailed")
	// A window a page opens off Fyr closes and the link goes out.
	f.event("", "Target.attachedToTarget", `{"sessionId":"P","targetInfo":{"targetId":"T2","type":"page","url":"https://example.com/","openerId":"T"},"waitingForDebugger":true}`)
	f.waitFor("Target.closeTarget", func(m cdpMsg) bool { return param(m, "targetId") == "T2" })
	rec.has(t, "external https://example.com/")
}

// The bridge answers only the window's main frame on Fyr's origin.
func TestChromeWindowBridgeAnswersOnlyFyr(t *testing.T) {
	w, f, rec := newTestWindow(t)
	open(t, w, f, "")
	f.waitFor("Page.navigate", nil)
	f.event("S", "Runtime.executionContextCreated", `{"context":{"id":1,"origin":"http://fyr.localhost:7777","auxData":{"frameId":"T","isDefault":true}}}`)
	f.event("S", "Runtime.executionContextCreated", `{"context":{"id":2,"origin":"http://fyr.localhost:7777","auxData":{"frameId":"F2","isDefault":true}}}`)
	f.event("S", "Runtime.bindingCalled", `{"name":"fyrBridge","payload":"{\"id\":7,\"m\":{\"op\":\"from-frame\"}}","executionContextId":2}`)
	f.event("S", "Runtime.bindingCalled", `{"name":"fyrBridge","payload":"{\"id\":8,\"m\":{\"op\":\"status\"}}","executionContextId":1}`)
	ev := f.waitFor("Runtime.evaluate", nil)
	if e := param(ev, "expression"); e != `window.__fyrBridgeReply(8,true,{"on":true})` {
		t.Fatalf("reply = %s", e)
	}
	rec.has(t, `bridge {"op":"status"}`)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, g := range rec.got {
		if strings.Contains(g, "from-frame") {
			t.Fatal("a frame's call reached the bridge")
		}
	}
}

// Before Fyr's origin is known the window waits for the first link, then
// opens on its origin and loads it.
func TestChromeWindowWaitsForTheFirstLink(t *testing.T) {
	f := &fakeChromium{t: t}
	origin := ""
	var mu sync.Mutex
	w := &Chrome{Profile: "/p", Start: f.start, Origin: func() string { mu.Lock(); defer mu.Unlock(); return origin }}
	w.Show("")
	if w.Running() {
		t.Fatal("started without an origin")
	}
	mu.Lock()
	origin = "http://fyr.localhost:7777"
	mu.Unlock()
	w.Load("http://fyr.localhost:7777/auth/once?t=x")
	f.waitFor("Target.setAutoAttach", nil)
	f.event("", "Target.attachedToTarget", `{"sessionId":"S","targetInfo":{"targetId":"T","type":"page","url":"about:blank"}}`)
	if nav := f.waitFor("Page.navigate", nil); param(nav, "url") != "http://fyr.localhost:7777/auth/once?t=x" {
		t.Fatalf("navigate = %s", nav.Params)
	}
}

// The user agent is a flag built from the binary's version, so it holds from
// the first request.
func TestChromeUserAgentFromVersion(t *testing.T) {
	if got := UserAgent("Chromium 157.0.8103.50 \n"); got != "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36 FyrDesktop/linux" {
		t.Fatalf("UserAgent = %q", got)
	}
	if got := UserAgent("Google Chrome 158.0.8127.36"); !strings.Contains(got, "Chrome/158.0.0.0") {
		t.Fatalf("UserAgent = %q", got)
	}
	if UserAgent("nonsense") != "" {
		t.Fatal("a version without a number makes a user agent")
	}
	a := strings.Join(ChromeArgs("/p", "UA x", Geometry{}, "http://o/"), " ")
	if !strings.Contains(a, "--user-agent=UA x") {
		t.Fatalf("args = %s", a)
	}
}

// A page whose process dies loads again where it was.
func TestChromeWindowReloadsACrashedPage(t *testing.T) {
	w, f, _ := newTestWindow(t)
	open(t, w, f, "")
	f.waitFor("Page.navigate", nil)
	f.event("S", "Page.frameNavigated", `{"frame":{"id":"T","url":"http://fyr.localhost:7777/?open=t_9"}}`)
	f.event("S", "Inspector.targetCrashed", `{}`)
	if nav := f.waitFor("Page.navigate", nil); param(nav, "url") != "http://fyr.localhost:7777/?open=t_9" {
		t.Fatalf("navigate = %s", nav.Params)
	}
}
