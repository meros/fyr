package desktop

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// V0.desktop-chromium: the Linux app's window is a Chromium app window
// (--app), which the app starts and drives over the DevTools pipe (cdp.go).
// Chromium draws the page on the GPU without the flicker WebKitGTK showed
// while the owner typed, and runs the web client's script faster.
//
// What WebKitGTK's view did, the window does here:
//   - a main-frame 401 asks the owner socket for a one-time link and the
//     signed-out page is not shown (Fetch at the response stage);
//   - a main-frame load that fails tells the app, which shows the offline
//     page or signs in again;
//   - a main-frame navigation off Fyr's origin, and a window a page opens off
//     it, go to the default browser (External, ChildWindow);
//   - the page's fyrPush bridge (web/src/pwa.ts) is window.webkit.messageHandlers.fyrPush
//     as in WKWebView and WebKitGTK, made by a script that runs before the
//     page's own and answered only for the main frame on Fyr's origin;
//   - its user agent ends in FyrDesktop/linux;
//   - the window's size is kept (Bounds).
//
// Closing the window ends Chromium; the tray stays, and Open starts it again
// on the page it showed last. Chromium keeps its cookies in Profile, so a
// start loads the app with the session it has.

// BridgeName is the binding the page's bridge script calls.
const bridgeBinding = "fyrBridge"

// bridgeScript makes window.webkit.messageHandlers.fyrPush in the main frame
// before the page's scripts run. Each postMessage is a promise that the app
// settles with window.__fyrBridgeReply(id, ok, value).
const bridgeScript = `(() => {
  if (window.top !== window) return;
  const pending = new Map();
  let n = 0;
  Object.defineProperty(window, "__fyrBridgeReply", { value: (id, ok, v) => {
    const p = pending.get(id);
    if (!p) return;
    pending.delete(id);
    if (ok) p.resolve(v); else p.reject(new Error(v));
  } });
  const fyrPush = { postMessage: (m) => new Promise((resolve, reject) => {
    const call = window.` + bridgeBinding + `;
    if (typeof call !== "function") return reject(new Error("not available"));
    const id = ++n;
    pending.set(id, { resolve, reject });
    call(JSON.stringify({ id, m }));
  }) };
  Object.defineProperty(window, "webkit", { value: { messageHandlers: { fyrPush } } });
})();`

// Chrome is the app's window. Its methods may be called from any goroutine.
type Chrome struct {
	Exec    string   // the Chromium binary
	Profile string   // its --user-data-dir
	Args    []string // more flags (tests)
	Size    Geometry // the first window's size

	Origin func() string // Fyr's origin now, "" before the first sign-in
	// SignedOut: a main-frame load answered 401. True means the app signs in
	// again, so the signed-out page is not shown.
	SignedOut  func() bool
	LoadFailed func()                                                                // a main-frame load failed
	External   func(url string)                                                      // open url in the default browser
	Bridge     func(msg []byte) (any, error)                                         // a fyrPush message from the page
	Bounds     func(Geometry)                                                        // the window's size changed
	Start      func(args []string) (*exec.Cmd, io.ReadCloser, io.WriteCloser, error) // tests replace it

	ua       string // the user agent flag, from the binary's version
	mu       sync.Mutex
	run      *chromeRun
	want     string // the page to load when the window opens or once it is up
	last     string // the Fyr page the window showed last
	showing  bool   // the owner asked for the window before Fyr's origin was known
	activate string // a Wayland activation token for the next start
}

type chromeRun struct {
	c       *cdpConn
	cmd     *exec.Cmd
	page    string // the app window's target id, which is also its main frame id
	session string
	ready   bool
	ctxs    map[int]execCtx
	popups  map[string]string // session -> target id
	bounds  Geometry
	stop    chan struct{}
}

type execCtx struct {
	origin string
	frame  string
	isMain bool
}

// Running reports whether the window is open.
func (w *Chrome) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.run != nil
}

// Load shows u in the window, or keeps it for when the window opens.
func (w *Chrome) Load(u string) {
	w.mu.Lock()
	r := w.run
	if r == nil || !r.ready {
		w.want = u
		show := r == nil && w.showing
		w.mu.Unlock()
		if show {
			w.Show("")
		}
		return
	}
	w.mu.Unlock()
	r.c.send(r.session, "Page.navigate", map[string]string{"url": u})
}

// Show opens the window (on u, else where it was) or brings it forward.
func (w *Chrome) Show(u string) {
	w.mu.Lock()
	if r := w.run; r != nil {
		if u != "" && !r.ready {
			w.want = u
		}
		w.mu.Unlock()
		if u != "" && r.ready {
			r.c.send(r.session, "Page.navigate", map[string]string{"url": u})
		}
		w.bringForward(r)
		return
	}
	if u != "" {
		w.want = u
	}
	boot := w.bootURL()
	if boot == "" {
		w.showing = true // opens once the first link arrives (Load)
		w.mu.Unlock()
		return
	}
	w.showing = false
	if err := w.launch(boot); err != nil {
		w.mu.Unlock()
		log.Printf("fyr-desktop: start the window: %v", err)
		return
	}
	w.mu.Unlock()
}

// ActivationToken is the desktop's token for the next start, so Wayland gives
// the new window the focus (a click on a notification).
func (w *Chrome) ActivationToken(t string) {
	w.mu.Lock()
	w.activate = t
	w.mu.Unlock()
}

// Quit closes the window.
func (w *Chrome) Quit() {
	w.mu.Lock()
	r := w.run
	w.mu.Unlock()
	if r == nil {
		return
	}
	_ = r.c.call("", "Browser.close", nil, nil)
	select {
	case <-r.c.done:
	case <-time.After(3 * time.Second):
		if r.cmd != nil && r.cmd.Process != nil {
			r.cmd.Process.Kill()
		}
	}
}

// bootURL is the --app page: always Fyr's origin, so the window's app id
// (chrome-<host>__-Default) and its icon stay the same; the page the app
// wants is loaded once the hooks are in place. w.mu is held.
func (w *Chrome) bootURL() string {
	o := ""
	if w.Origin != nil {
		o = w.Origin()
	}
	if o == "" {
		o = OriginOf(w.want)
	}
	if o == "" {
		return w.want // a data: page while nothing answers
	}
	return o + "/"
}

// UserAgent is the window's user agent for a Chromium whose --version
// printed version: Chromium's own reduced one with FyrDesktop/linux after it.
// It is a flag, so it holds from the first request: the web client reads it
// to know it runs in the app and registers no service worker (pwa.ts).
func UserAgent(version string) string {
	major := ""
	for _, f := range strings.Fields(version) {
		if i := strings.IndexByte(f, '.'); i > 0 && f[0] >= '0' && f[0] <= '9' {
			major = f[:i]
			break
		}
	}
	if major == "" {
		return ""
	}
	return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + major + ".0.0.0 Safari/537.36 FyrDesktop/linux"
}

// ChromeArgs are the flags of the window's Chromium.
func ChromeArgs(profile, ua string, g Geometry, boot string) []string {
	a := []string{
		"--remote-debugging-pipe",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--password-store=basic", // no keyring prompt: the profile holds only Fyr's session cookie
		"--disable-features=Translate",
		"--hide-crash-restore-bubble",
		"--ozone-platform-hint=auto",
		"--class=io.github.meros.Fyr",
	}
	if ua != "" {
		a = append(a, "--user-agent="+ua)
	}
	if g.Width > 0 && g.Height > 0 {
		a = append(a, fmt.Sprintf("--window-size=%d,%d", g.Width, g.Height))
	}
	if g.Maximized {
		a = append(a, "--start-maximized")
	}
	return append(a, "--app="+boot)
}

// launch starts Chromium. w.mu is held.
func (w *Chrome) launch(boot string) error {
	if w.ua == "" && w.Exec != "" && w.Start == nil {
		if out, err := exec.Command(w.Exec, "--version").Output(); err == nil {
			w.ua = UserAgent(string(out))
		}
	}
	args := append(ChromeArgs(w.Profile, w.ua, w.Size, boot), w.Args...)
	start := w.Start
	if start == nil {
		start = w.startProcess
	}
	cmd, rd, wr, err := start(args)
	if err != nil {
		return err
	}
	r := &chromeRun{cmd: cmd, ctxs: map[int]execCtx{}, popups: map[string]string{}, stop: make(chan struct{}), bounds: w.Size}
	r.c = newCDP(rd, wr, func(m cdpMsg) { w.event(r, m) })
	w.run = r
	go func() {
		<-r.c.done
		close(r.stop)
		rd.Close()
		wr.Close()
		if cmd != nil {
			cmd.Wait()
		}
		w.mu.Lock()
		if w.run == r {
			w.run = nil
		}
		w.mu.Unlock()
	}()
	r.c.send("", "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true})
	return nil
}

func (w *Chrome) startProcess(args []string) (*exec.Cmd, io.ReadCloser, io.WriteCloser, error) {
	// Chromium reads commands on fd 3 and writes on fd 4.
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, nil, nil, err
	}
	cmd := exec.Command(w.Exec, args...)
	cmd.ExtraFiles = []*os.File{inR, outW}
	cmd.Env = os.Environ()
	if w.activate != "" {
		cmd.Env = append(cmd.Env, "XDG_ACTIVATION_TOKEN="+w.activate)
		w.activate = ""
	}
	if err := cmd.Start(); err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, nil, nil, err
	}
	inR.Close()
	outW.Close()
	return cmd, outR, inW, nil
}

func (w *Chrome) origin() string {
	if w.Origin == nil {
		return ""
	}
	return w.Origin()
}

// event handles one event of the browser, in order.
func (w *Chrome) event(r *chromeRun, m cdpMsg) {
	switch m.Method {
	case "Target.attachedToTarget":
		var p struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				URL      string `json:"url"`
				OpenerID string `json:"openerId"`
			} `json:"targetInfo"`
			Waiting bool `json:"waitingForDebugger"`
		}
		json.Unmarshal(m.Params, &p)
		t := p.TargetInfo
		switch {
		case t.Type == "page" && r.page == "" && t.OpenerID == "":
			w.setupMain(r, p.SessionID, t.TargetID)
		case t.Type == "page":
			w.setupPopup(r, p.SessionID, t.TargetID, t.URL)
		default:
			if p.Waiting {
				r.c.send(p.SessionID, "Runtime.runIfWaitingForDebugger", nil)
			}
			r.c.send("", "Target.detachFromTarget", map[string]string{"sessionId": p.SessionID})
		}
	case "Target.detachedFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		json.Unmarshal(m.Params, &p)
		if p.SessionID == r.session && r.session != "" {
			// The owner closed the app's window: end the browser with its popups.
			r.c.send("", "Browser.close", nil)
		}
		delete(r.popups, p.SessionID)
	case "Inspector.targetCrashed":
		if m.SessionID == r.session && r.session != "" {
			w.mu.Lock()
			u := w.last
			w.mu.Unlock()
			if u == "" {
				if o := w.origin(); o != "" {
					u = o + "/"
				}
			}
			if u != "" {
				r.c.send(r.session, "Page.navigate", map[string]string{"url": u}) // the page's process died: load it again
			}
		}
	case "Fetch.requestPaused":
		w.paused(r, m.SessionID, m.Params)
	case "Runtime.executionContextCreated":
		if m.SessionID != r.session {
			return
		}
		var p struct {
			Context struct {
				ID      int    `json:"id"`
				Origin  string `json:"origin"`
				AuxData struct {
					FrameID   string `json:"frameId"`
					IsDefault bool   `json:"isDefault"`
				} `json:"auxData"`
			} `json:"context"`
		}
		json.Unmarshal(m.Params, &p)
		x := p.Context
		r.ctxs[x.ID] = execCtx{origin: x.Origin, frame: x.AuxData.FrameID, isMain: x.AuxData.IsDefault}
	case "Runtime.executionContextDestroyed":
		var p struct {
			ID int `json:"executionContextId"`
		}
		json.Unmarshal(m.Params, &p)
		delete(r.ctxs, p.ID)
	case "Runtime.executionContextsCleared":
		if m.SessionID == r.session {
			r.ctxs = map[int]execCtx{}
		}
	case "Runtime.bindingCalled":
		w.bindingCalled(r, m.SessionID, m.Params)
	case "Page.frameNavigated":
		if m.SessionID != r.session {
			return
		}
		var p struct {
			Frame struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		json.Unmarshal(m.Params, &p)
		if p.Frame.ParentID == "" && OriginOf(p.Frame.URL) == w.origin() && w.origin() != "" {
			w.mu.Lock()
			w.last = p.Frame.URL
			w.mu.Unlock()
		}
	}
}

// setupMain puts the hooks on the app's window before its first page runs,
// then loads the page the app wants. The --app page that started meanwhile is
// replaced, so no page of Fyr ever runs without the bridge.
func (w *Chrome) setupMain(r *chromeRun, session, target string) {
	r.session, r.page = session, target
	c := r.c
	w.mu.Lock()
	ua := w.ua
	w.mu.Unlock()
	if ua == "" { // no version to build the flag from: set it on the page instead
		var v struct {
			UserAgent string `json:"userAgent"`
		}
		if err := c.call("", "Browser.getVersion", nil, &v); err == nil && v.UserAgent != "" {
			c.call(session, "Emulation.setUserAgentOverride", map[string]string{"userAgent": v.UserAgent + " FyrDesktop/linux"}, nil)
		}
	}
	// The window registers no service worker (decision 0038): drop one an older build or the --app page left.
	if o := w.origin(); o != "" {
		c.call("", "Storage.clearDataForOrigin", map[string]string{"origin": o, "storageTypes": "service_workers"}, nil)
	}
	c.call(session, "Runtime.addBinding", map[string]string{"name": bridgeBinding}, nil)
	c.call(session, "Page.enable", nil, nil)
	c.call(session, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": bridgeScript}, nil)
	c.call(session, "Runtime.enable", nil, nil)
	c.call(session, "Fetch.enable", map[string]any{"patterns": []map[string]string{
		{"resourceType": "Document", "requestStage": "Request"},
		{"resourceType": "Document", "requestStage": "Response"},
	}}, nil)
	c.call(session, "Runtime.runIfWaitingForDebugger", nil, nil)
	w.mu.Lock()
	u := w.want
	w.want = ""
	if u == "" {
		u = w.last
	}
	if u == "" {
		if o := w.origin(); o != "" {
			u = o + "/"
		}
	}
	r.ready = true
	w.mu.Unlock()
	if u != "" {
		// Not awaited: its answer can wait on a paused request, which this
		// goroutine handles.
		c.send(session, "Page.navigate", map[string]string{"url": u})
	}
	go w.watchBounds(r)
}

// setupPopup: a window a page opened. A Fyr page stays in the app; a link off
// Fyr goes to the default browser and the window closes (V0.desktop-popup-links).
func (w *Chrome) setupPopup(r *chromeRun, session, target, u string) {
	r.popups[session] = target
	if u != "" && !ChildWindow(w.origin(), u) {
		w.leave(r, target, u)
		return
	}
	// In order: the hook holds before the page's first load goes out.
	r.c.call(session, "Fetch.enable", map[string]any{"patterns": []map[string]string{{"resourceType": "Document", "requestStage": "Request"}}}, nil)
	r.c.send(session, "Runtime.runIfWaitingForDebugger", nil)
}

func (w *Chrome) leave(r *chromeRun, target, u string) {
	r.c.send("", "Target.closeTarget", map[string]string{"targetId": target})
	if w.External != nil {
		w.External(u)
	}
}

type pausedReq struct {
	RequestID string `json:"requestId"`
	Request   struct {
		URL string `json:"url"`
	} `json:"request"`
	FrameID     string `json:"frameId"`
	Status      int    `json:"responseStatusCode"`
	ErrorReason string `json:"responseErrorReason"`
}

func (w *Chrome) paused(r *chromeRun, session string, raw json.RawMessage) {
	var p pausedReq
	json.Unmarshal(raw, &p)
	cont := func() { r.c.send(session, "Fetch.continueRequest", map[string]string{"requestId": p.RequestID}) }
	fail := func(reason string) {
		r.c.send(session, "Fetch.failRequest", map[string]string{"requestId": p.RequestID, "errorReason": reason})
	}
	response := p.Status != 0 || p.ErrorReason != ""
	if target, popup := r.popups[session]; popup {
		if !response && p.FrameID == target && External(w.origin(), p.Request.URL) {
			fail("BlockedByClient")
			w.leave(r, target, p.Request.URL)
			return
		}
		cont()
		return
	}
	if session != r.session || p.FrameID != r.page {
		cont() // a frame's own load stays
		return
	}
	switch {
	case !response && External(w.origin(), p.Request.URL):
		fail("BlockedByClient")
		if w.External != nil {
			w.External(p.Request.URL)
		}
	case p.ErrorReason != "" && p.ErrorReason != "Aborted":
		cont()
		if w.LoadFailed != nil {
			go w.LoadFailed()
		}
	case p.Status == 401 && w.SignedOut != nil && w.SignedOut():
		fail("Aborted")
	default:
		cont()
	}
}

func (w *Chrome) bindingCalled(r *chromeRun, session string, raw json.RawMessage) {
	var p struct {
		Name    string `json:"name"`
		Payload string `json:"payload"`
		Context int    `json:"executionContextId"`
	}
	json.Unmarshal(raw, &p)
	if p.Name != bridgeBinding || session != r.session {
		return
	}
	x, ok := r.ctxs[p.Context]
	if !ok || !x.isMain || x.frame != r.page || x.origin == "" || x.origin != w.origin() {
		return // only Fyr's own page, in the window's main frame, gets an answer
	}
	var msg struct {
		ID int             `json:"id"`
		M  json.RawMessage `json:"m"`
	}
	if json.Unmarshal([]byte(p.Payload), &msg) != nil || msg.ID == 0 {
		return
	}
	go func() {
		var res any
		err := fmt.Errorf("not available")
		if w.Bridge != nil {
			res, err = w.Bridge(msg.M)
		}
		ok, val := true, []byte(nil)
		if err != nil {
			ok = false
			val, _ = json.Marshal(err.Error())
		} else {
			val, _ = json.Marshal(res)
		}
		r.c.send(session, "Runtime.evaluate", map[string]any{
			"expression": fmt.Sprintf("window.__fyrBridgeReply(%d,%t,%s)", msg.ID, ok, val),
			"contextId":  p.Context,
		})
	}()
}

// watchBounds reports the window's size while it is open.
func (w *Chrome) watchBounds(r *chromeRun) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
		}
		var b struct {
			Bounds struct {
				Width       int    `json:"width"`
				Height      int    `json:"height"`
				WindowState string `json:"windowState"`
			} `json:"bounds"`
		}
		if r.c.call("", "Browser.getWindowForTarget", map[string]string{"targetId": r.page}, &b) != nil {
			continue
		}
		g := Geometry{Width: b.Bounds.Width, Height: b.Bounds.Height, Maximized: b.Bounds.WindowState == "maximized"}
		if b.Bounds.WindowState == "minimized" || b.Bounds.WindowState == "fullscreen" || g == r.bounds || g.Width == 0 {
			continue
		}
		r.bounds = g
		if w.Bounds != nil {
			w.Bounds(g)
		}
	}
}

func (w *Chrome) bringForward(r *chromeRun) {
	w.mu.Lock()
	ready := r.ready
	w.mu.Unlock()
	if !ready {
		return // it opens in front by itself
	}
	go func() {
		var b struct {
			WindowID int `json:"windowId"`
			Bounds   struct {
				WindowState string `json:"windowState"`
			} `json:"bounds"`
		}
		if r.c.call("", "Browser.getWindowForTarget", map[string]string{"targetId": r.page}, &b) == nil && b.Bounds.WindowState == "minimized" {
			r.c.call("", "Browser.setWindowBounds", map[string]any{"windowId": b.WindowID, "bounds": map[string]string{"windowState": "normal"}}, nil)
		}
		r.c.call("", "Target.activateTarget", map[string]string{"targetId": r.page}, nil)
	}()
}

// FindChromium is the Chromium the app starts: the one the package was built
// with, else one on PATH.
func FindChromium(built string) string {
	if built != "" {
		if _, err := os.Stat(built); err == nil {
			return built
		}
	}
	for _, n := range []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}
