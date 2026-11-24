package front

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/httpguard"
)

// V0.forwarded-ports: a server that a thread's process runs on its host's
// loopback is opened in the owner's browser through one of a few ports of
// the front. Each such port has its own origin, so the served pages never
// run as Fyr's; the connection travels over the front's link to the host,
// which checks on every connection that the port is the thread's.

// forward is what one of the front's forward ports carries now.
type forward struct {
	host   string // the install id of the thread's host
	thread string // the host's own thread id
	target int    // the port on the host's loopback
	used   time.Time
	proxy  *httputil.ReverseProxy
	tr     *http.Transport
}

// forwards is the pool: public port → what it carries.
type forwards struct {
	mu    sync.Mutex
	slots map[int]*forward
}

// forwardIdle is how long a forward stays the port's after its last request
// when another link needs the port; a forward in use is replaced only when
// every port is in use, the one used longest ago first.
const forwardIdle = 30 * time.Minute

// forwardPorts is the pool, in the order ports are given out.
func (f *Front) forwardPorts() []int { return f.opt.ForwardPorts }

// forwardGrant opens (or finds) the forward of a thread's port and answers
// the front's port the browser opens: POST /api/front/forward {host,
// threadId, port} → {port}.
func (f *Front) forwardGrant(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Host     string `json:"host"`
		ThreadID string `json:"threadId"`
		Port     int    `json:"port"`
	}
	if !api.DecodeBody(w, r, &q, 1<<12) {
		return
	}
	if len(f.forwardPorts()) == 0 {
		api.WriteJSON(w, http.StatusConflict, map[string]any{"error": "this Fyr has no forward ports (forwardPorts in its config)", "code": "no-forward-ports"})
		return
	}
	l := f.byID(q.Host)
	if l == nil || q.ThreadID == "" || strings.ContainsAny(q.ThreadID, "/?#") || q.Port < 1 || q.Port > 65535 {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "a forward names a host, a thread and a port"})
		return
	}
	// Asked now for a clear answer on the press; the host checks again on
	// every connection.
	ports, err := f.threadPorts(r, l, q.ThreadID)
	if err != nil {
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if !slices.Contains(ports, q.Port) {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": fmt.Sprintf("no process of this thread listens on port %d", q.Port), "code": "not-thread-port", "ports": ports})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"port": f.forwardSlot(q.Host, q.ThreadID, q.Port)})
}

// threadPorts asks the thread's host which ports its processes listen on.
func (f *Front) threadPorts(r *http.Request, l *link, thread string) ([]int, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base.String()+"/api/threads/"+url.PathEscape(thread)+"/ports", nil)
	if err != nil {
		return nil, err
	}
	setActor(req, principalOf(r), f.opt.ID, originOf(r))
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the thread's host does not answer")
	}
	defer resp.Body.Close()
	var got struct {
		Ports []int  `json:"ports"`
		Error string `json:"error"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&got)
	if resp.StatusCode != http.StatusOK {
		if got.Error == "" {
			got.Error = "the thread's host cannot forward a port (it runs an older Fyr)"
		}
		return nil, fmt.Errorf("%s", got.Error)
	}
	return got.Ports, nil
}

// forwardSlot gives the triple a port of the pool: the one it has, else a
// free one, else an idle one, else the one used longest ago.
func (f *Front) forwardSlot(host, thread string, target int) int {
	now := time.Now()
	f.fwd.mu.Lock()
	defer f.fwd.mu.Unlock()
	if f.fwd.slots == nil {
		f.fwd.slots = map[int]*forward{}
	}
	pick, oldest := 0, 0
	for _, p := range f.forwardPorts() {
		s := f.fwd.slots[p]
		switch {
		case s == nil:
			if pick == 0 {
				pick = p
			}
		case s.host == host && s.thread == thread && s.target == target:
			s.used = now
			return p
		case oldest == 0 || s.used.Before(f.fwd.slots[oldest].used):
			oldest = p
		}
	}
	if pick == 0 {
		pick = oldest
		f.fwd.slots[pick].tr.CloseIdleConnections()
	}
	f.fwd.slots[pick] = f.newForward(host, thread, target, now)
	return pick
}

// forwardOf is what a port carries, and marks it used.
func (f *Front) forwardOf(port int) *forward {
	f.fwd.mu.Lock()
	defer f.fwd.mu.Unlock()
	s := f.fwd.slots[port]
	if s != nil {
		s.used = time.Now()
	}
	return s
}

// streamConn is one forwarded connection as a net.Conn: what is written goes
// up as the request's body, what the server answers comes down as the
// response's.
type streamConn struct {
	r      io.ReadCloser
	w      *io.PipeWriter
	cancel context.CancelFunc
	once   sync.Once
}

func (c *streamConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *streamConn) Write(p []byte) (int, error) { return c.w.Write(p) }
func (c *streamConn) Close() error {
	c.once.Do(func() {
		c.w.Close()
		c.cancel()
		c.r.Close()
	})
	return nil
}
func (c *streamConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *streamConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *streamConn) SetDeadline(time.Time) error      { return nil }
func (c *streamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *streamConn) SetWriteDeadline(time.Time) error { return nil }

// dialForward opens one connection to the thread's port through its host.
func (f *Front) dialForward(ctx context.Context, host, thread string, target int) (net.Conn, error) {
	l := f.byID(host)
	if l == nil {
		return nil, fmt.Errorf("the thread's host is gone")
	}
	// The stream outlives the dial: its own context, ended by Close.
	sctx, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(sctx, http.MethodPost, l.base.String()+"/api/threads/"+url.PathEscape(thread)+"/forward/"+strconv.Itoa(target), pr)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity") // bytes as they are: nothing gzips a connection
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := l.client.Transport.RoundTrip(req)
	if !stop() { // the dial was cancelled meanwhile
		err = ctx.Err()
		if resp != nil {
			resp.Body.Close()
			resp = nil
		}
	}
	if err != nil {
		pw.Close()
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var got struct {
			Error string `json:"error"`
		}
		json.NewDecoder(io.LimitReader(resp.Body, 1<<12)).Decode(&got)
		resp.Body.Close()
		pw.Close()
		cancel()
		if got.Error == "" {
			got.Error = "the thread's host answered " + resp.Status
		}
		return nil, fmt.Errorf("%s", got.Error)
	}
	return &streamConn{r: resp.Body, w: pw, cancel: cancel}, nil
}

// forwardTarget is how the forwarded server knows itself: a dev server
// refuses a Host it was not started for, and checks a WebSocket's Origin.
func forwardTarget(target int) string { return "localhost:" + strconv.Itoa(target) }

func (f *Front) newForward(host, thread string, target int, now time.Time) *forward {
	fw := &forward{host: host, thread: thread, target: target, used: now}
	fw.tr = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return f.dialForward(ctx, host, thread, target)
		},
		DisableCompression:    true, // the browser's own Accept-Encoding goes through
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 0,
	}
	self := forwardTarget(target)
	fw.proxy = &httputil.ReverseProxy{
		Transport:     fw.tr,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			origin := originOf(pr.In)
			pr.Out.URL.Scheme, pr.Out.URL.Host = "http", self
			pr.Out.Host = self
			forwardHeaders(pr.Out.Header, origin, "http://"+self)
		},
		ModifyResponse: func(resp *http.Response) error {
			forwardAnswer(resp.Header, "http://"+self, originOf(resp.Request.Context().Value(forwardInKey{}).(*http.Request)))
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			forwardPage(w, http.StatusBadGateway, "The server does not answer", fmt.Sprintf("Port %d of the thread's host: %v.", target, err))
		},
	}
	return fw
}

type forwardInKey struct{}

// forwardHeaders cleans a browser's request for the forwarded server: Fyr's
// session and everything that names who asks stay here, and the page's own
// origin reads as the server's own address.
func forwardHeaders(h http.Header, origin, self string) {
	for k := range h {
		l := strings.ToLower(k)
		if l == "authorization" || l == "cookie" || l == "origin" || l == "referer" {
			continue // the server's own: kept, cleaned below
		}
		if strings.HasPrefix(l, "tailscale-") || strings.HasPrefix(l, "x-forwarded-") || strings.HasPrefix(l, "fyr-") || l == "forwarded" || l == "x-fyr" {
			h.Del(k)
		}
	}
	var keep []string
	for _, line := range h.Values("Cookie") {
		for _, c := range strings.Split(line, ";") {
			c = strings.TrimSpace(c)
			if name, _, _ := strings.Cut(c, "="); c != "" && name != auth.Cookie {
				keep = append(keep, c)
			}
		}
	}
	h.Del("Cookie")
	if len(keep) > 0 {
		h.Set("Cookie", strings.Join(keep, "; "))
	}
	if origin == "" {
		return
	}
	if h.Get("Origin") == origin {
		h.Set("Origin", self)
	}
	if ref := h.Get("Referer"); strings.HasPrefix(ref, origin+"/") || ref == origin {
		h.Set("Referer", self+strings.TrimPrefix(ref, origin))
	}
}

// forwardAnswer cleans the server's answer for the browser: a redirect to
// its own address goes to the forward's, and it cannot set Fyr's session
// (the ports of one name share their cookies).
func forwardAnswer(h http.Header, self, origin string) {
	if loc := h.Get("Location"); origin != "" {
		for _, own := range []string{self, strings.Replace(self, "localhost", "127.0.0.1", 1)} {
			if loc == own || strings.HasPrefix(loc, own+"/") {
				h.Set("Location", origin+strings.TrimPrefix(loc, own))
			}
		}
	}
	if set := h.Values("Set-Cookie"); len(set) > 0 {
		h.Del("Set-Cookie")
		for _, c := range set {
			if name, _, _ := strings.Cut(strings.TrimSpace(c), "="); strings.TrimSpace(name) != auth.Cookie {
				h.Add("Set-Cookie", c)
			}
		}
	}
}

// forwardPage is the forward's own answer, as a small page.
func forwardPage(w http.ResponseWriter, code int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(code)
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
	}
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>body{font:16px/24px system-ui,sans-serif;max-width:32em;margin:15vh auto;padding:0 24px;color:#1f1b16;background:#f7f4ef}@media(prefers-color-scheme:dark){body{color:#eee8e0;background:#191714}}h1{font-size:20px}</style><h1>%s</h1><p>%s</p>`, esc(title), esc(title), esc(text))
}

// forwardAuthed: the browser has a session of this front, as its main port
// asks (the ports of one name share the cookie). In observe mode a request
// without one is served, as the front itself serves it.
func (f *Front) forwardAuthed(r *http.Request) bool {
	st := f.opt.Auth
	if st == nil {
		return true
	}
	if c, err := r.Cookie(auth.Cookie); err == nil {
		if d, ok := st.Lookup(c.Value); ok && !d.Pending {
			return true
		}
	}
	return st.Mode(auth.WayBrowser) != auth.ModeEnforce && !httpguard.IsViaLAN(r)
}

// ForwardHandler serves one forward port (the listeners of cmd/fyrd give it
// the port they listen for). Before anything reaches the thread's server:
// the Host is this front's (GuardHost), the request is not from another site
// or another origin, and the browser is signed in.
func (f *Front) ForwardHandler(port int) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(authn.Header) != "" || r.Header.Get(authn.OriginHeader) != "" || r.Header.Get(authn.PrefixHeader) != "" {
			http.Error(w, "this listener does not take "+authn.Header, http.StatusBadRequest)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != originOf(r) {
			http.Error(w, "this request comes from another origin", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "this request comes from another site", http.StatusForbidden)
			return
		}
		if !f.forwardAuthed(r) {
			forwardPage(w, http.StatusUnauthorized, "Sign in to Fyr first", "This address shows a server of one of your Fyr threads. Open Fyr on this device, sign in, and open the link from the thread again.")
			return
		}
		fw := f.forwardOf(port)
		if fw == nil {
			forwardPage(w, http.StatusNotFound, "Nothing is forwarded here", "Open the link in its Fyr thread again: a forward is made when you press it.")
			return
		}
		fw.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), forwardInKey{}, r)))
	})
	return f.refuseTailscale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpguard.GuardHost(f.ownerLogin(), inner).ServeHTTP(w, r)
	}))
}

// forwardOpen is where a link to a thread's local server points: GET
// /forward/open?host=&thread=&port=&path=. It makes the forward and sends the
// browser to it, so the link in the message is an ordinary one: it opens in a
// new tab, and its address can be copied and opened again later. A browser on
// the thread's own machine is sent to the server itself.
func (f *Front) forwardOpen(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	port, _ := strconv.Atoi(q.Get("port"))
	host, thread, path := q.Get("host"), q.Get("thread"), q.Get("path")
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") {
		path = "/"
	}
	l := f.byID(host)
	if l == nil || thread == "" || strings.ContainsAny(thread, "/?#") || port < 1 || port > 65535 {
		forwardPage(w, http.StatusBadRequest, "This link cannot be opened", "It names no host, thread or port that this Fyr knows.")
		return
	}
	name, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		name = r.Host
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if ip := net.ParseIP(strings.Trim(name, "[]")); l.cfg.Dial != nil && !httpguard.IsViaServe(r) && !httpguard.IsViaLAN(r) && (isLocalName(name) || ip != nil && ip.IsLoopback()) {
		// This browser is on the machine the server runs on.
		http.Redirect(w, r, "http://"+forwardTarget(port)+path, http.StatusFound)
		return
	}
	if len(f.forwardPorts()) == 0 {
		forwardPage(w, http.StatusConflict, "This Fyr forwards no ports", "Its config has an empty forwardPorts, so a server on a thread's host opens only on that machine.")
		return
	}
	ports, err := f.threadPorts(r, l, thread)
	if err != nil {
		forwardPage(w, http.StatusBadGateway, "The thread's host does not answer", err.Error())
		return
	}
	if !slices.Contains(ports, port) {
		now := "It has no server running now."
		if len(ports) > 0 {
			s := make([]string, len(ports))
			for i, p := range ports {
				s[i] = strconv.Itoa(p)
			}
			now = "Its servers now listen on: " + strings.Join(s, ", ") + "."
		}
		forwardPage(w, http.StatusNotFound, fmt.Sprintf("Nothing of this thread listens on port %d", port), "Fyr opens only the servers that the thread's own processes run. "+now)
		return
	}
	scheme, _, _ := strings.Cut(originOf(r), "://")
	http.Redirect(w, r, scheme+"://"+net.JoinHostPort(strings.Trim(name, "[]"), strconv.Itoa(f.forwardSlot(host, thread, port)))+path, http.StatusFound)
}

// isLocalName: localhost or a name under it (RFC 6761), as the guard reads it.
func isLocalName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}
