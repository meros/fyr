package front

// Browser sessions (SPEC V0.130, V0.157, decision 0030 3.4, 5.1): every
// browser is a device with a session cookie. Ways to get one: a one-time link
// (fyr open, fyr device add, Add a device), the owner's tailnet node through
// Serve's socket (the node is looked up, approved or blocked), an approval
// code. Each way in has a stored mode. In observe a request without a session
// is served the old way and counted; in enforce it is refused. Nothing but the
// owner, by hand or by the install's own rule (three clean days), moves a mode.

import (
	"context"
	"github.com/meros/fyr/internal/httpguard"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/tailnet"
)

// NodeAuth is tailscaled's whois for the node at an address (decision 0030
// 3.4); tailnet.Peers implements it.
type NodeAuth interface {
	Node(ctx context.Context, remoteAddr string) (tailnet.NodeInfo, error)
}

type deviceKey struct{}

// DeviceOf is the device whose session a request carries, or nil.
func DeviceOf(r *http.Request) *auth.Device {
	d, _ := r.Context().Value(deviceKey{}).(*auth.Device)
	return d
}

type pendingKey struct{}

// signedIn: the request's principal is a real device (a session), not the
// legacy one of observe mode.
func signedIn(r *http.Request) (authn.Principal, bool) {
	p := principalOf(r)
	return p, p.Kind == authn.KindDevice && p.How != authn.HowLegacy
}

// exempt: what a request without a session may still reach in enforce mode.
// The client's own files (a page that says how to sign in must load), the
// sign-in routes, the well-known answer, and a preview's files: a sandboxed
// page has no cookie, and the 80-bit id in its URL is its key (V0.19).
func exempt(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/.well-known/fyr":
		return true
	case !strings.HasPrefix(p, "/api/"):
		// The client's files, by what they are, not by what the browser says it is
		// asking for: a service worker that refetches a navigation does not say
		// "document", and the page itself (/, /index.html, a path the client
		// routes) must never go out without a session.
		return (r.Method == http.MethodGet || r.Method == http.MethodHead) && staticFile(p)
	case hostPreviewPath(p): // /api/h/<id>/previews/…
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	return false
}

// previewRead: a GET or HEAD of a preview's page or one of its files.
func previewRead(r *http.Request) bool {
	p := r.URL.Path
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && hostPreviewPath(p)
}

// staticFile: the path names an asset of the built client, not a page.
func staticFile(p string) bool {
	i := strings.LastIndexByte(p, '.')
	if i < 0 || i < strings.LastIndexByte(p, '/') {
		return false
	}
	switch strings.ToLower(p[i:]) {
	case ".js", ".mjs", ".css", ".map", ".woff", ".woff2", ".png", ".jpg", ".jpeg", ".webp", ".svg", ".ico", ".webmanifest", ".json", ".txt":
		return true
	}
	return false
}

// isDocument: a browser navigation, which gets the sign-in page in enforce.
func isDocument(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Mode") == "navigate" {
		return true
	}
	if d := r.Header.Get("Sec-Fetch-Dest"); d != "" {
		return d == "document" || d == "iframe"
	}
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

// refuseTailscale: in enforce, a Tailscale-* header on any listener but
// serve.sock is a local process's (way B), and is refused with 400 before
// anything else is looked at. Only tailscaled, through Serve's socket, sets one.
func (f *Front) refuseTailscale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httpguard.IsViaLAN(r) || (f.opt.Auth != nil && !httpguard.IsViaServe(r) && f.opt.Auth.Mode(auth.WayBrowser) == auth.ModeEnforce) {
			for k := range r.Header {
				if strings.HasPrefix(strings.ToLower(k), "tailscale-") {
					refuse(w, r, "this listener does not take Tailscale headers", http.StatusBadRequest)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// browserRules (decision 0030 3.2): before the cookie is looked at, a request
// with an Origin that is not this front's own is refused (a page on another
// site, DNS rebinding), and so is a credentialed /api/ call the browser says
// is cross-site. In enforce a call that changes things must carry the Origin.
func (f *Front) browserRules(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if previewRead(r) {
			// A preview's page and its files are read by a sandboxed frame,
			// which has no origin: its requests say cross-site (and a module
			// script's Origin is "null"). They carry no cookie and change
			// nothing; the 80-bit id in the URL is the key (V0.19, V0.47).
			next.ServeHTTP(w, r)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != originOf(r) {
			refuse(w, r, "this request comes from another origin", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			switch r.Header.Get("Sec-Fetch-Site") {
			case "", "same-origin":
			default:
				refuse(w, r, "this request comes from another site", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") == "" &&
				(httpguard.IsViaLAN(r) || f.opt.Auth != nil && f.opt.Auth.Mode(auth.WayBrowser) == auth.ModeEnforce) {
				refuse(w, r, "missing Origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth finds each request's device by its cookie, enrols the owner's
// tailnet node through Serve's socket, applies the browser way's mode, and
// names the caller (decision 0030 3.2).
func (f *Front) withAuth(next http.Handler) http.Handler {
	st := f.opt.Auth
	if st == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			f.authRoutes(w, r)
			return
		}
		var have string
		if c, err := r.Cookie(auth.Cookie); err == nil {
			have = c.Value
		}
		d, ok := st.Lookup(have)
		pending := ok && d.Pending
		if pending {
			ok = false
		}
		enforce := st.Mode(auth.WayBrowser) == auth.ModeEnforce || httpguard.IsViaLAN(r) // the LAN is never observed
		// V0.one-device-per-browser: a request that is served without a session
		// (another install's probe of /.well-known/fyr, a file of the client) makes
		// no device.
		if !ok && !exempt(r) && httpguard.IsViaServe(r) && f.ownerLogin() != "" && r.Header.Get("Tailscale-User-Login") == f.ownerLogin() {
			if nd, p, refused := f.enrolTailnet(w, r, enforce); refused {
				return
			} else if nd != nil {
				d, ok, pending = nd, !p, p
			}
		} else if ok && httpguard.IsViaServe(r) && d.How == auth.HowTailnet && d.Node == "" && f.opt.Tailnet != nil && !enforce {
			f.bindNode(r, d) // a V0.130 session signed in during observe: its node is approved (5.2)
		}
		if ok {
			r = r.WithContext(context.WithValue(r.Context(), deviceKey{}, d))
			r = r.WithContext(authn.With(r.Context(), authn.Principal{Kind: authn.KindDevice, ID: d.ID, How: authn.How(d.How)}))
			st.Success(auth.WayBrowser, d.How)
		} else {
			r = r.WithContext(authn.With(r.Context(), legacy))
			if pending {
				r = r.WithContext(context.WithValue(r.Context(), pendingKey{}, d))
			}
			if !exempt(r) {
				if enforce {
					f.signedOut(w, r, pending, d)
					return
				}
				// Observe (decision 0030 5.1): served the old way, counted.
				st.WouldRefuse(auth.WayBrowser)
				f.wouldRefuse(r)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// signedOut answers a request that has no session in enforce mode: the sign-in
// page for a browser navigation, a 401 for everything else.
func (f *Front) signedOut(w http.ResponseWriter, r *http.Request, pending bool, d *auth.Device) {
	drain(r)
	w.Header().Set("Cache-Control", "no-store")
	if isDocument(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(signedOutPage))
		return
	}
	body := map[string]any{"ok": false, "error": "this device is not signed in", "signedOut": true, "pending": pending}
	api.WriteJSON(w, http.StatusUnauthorized, body)
}

// enrolTailnet is serve.sock's door (decision 0030 3.4): whois says which node
// the request came from. refused: the answer is already written.
func (f *Front) enrolTailnet(w http.ResponseWriter, r *http.Request, enforce bool) (d *auth.Device, pending, refused bool) {
	st := f.opt.Auth
	agent := BrowserName(r.UserAgent())
	if f.opt.Tailnet == nil { // no tailscaled to ask: the V0.130 way
		session, nd := st.Mint(auth.HowTailnet, agent)
		setSession(w, session, true)
		log.Printf("auth: %s signed in through the tailnet as %s", nd.Name, f.ownerLogin())
		return nd, false, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	node, err := f.opt.Tailnet.Node(ctx, forwardedFor(r))
	switch {
	case err != nil && !enforce:
		// Observe serves the old way when tailscaled cannot say which node it was.
		session, nd := st.Mint(auth.HowTailnet, agent)
		setSession(w, session, true)
		log.Printf("auth (observe): %s signed in through the tailnet; whois failed: %v", nd.Name, err)
		return nd, false, false
	case err != nil:
		http.Error(w, "tailscaled could not say which device this is", http.StatusServiceUnavailable)
		return nil, false, true
	case len(node.Tags) > 0 || node.Shared || node.Login != f.ownerLogin():
		// A tagged node, a node shared in from another tailnet, or another
		// user's: never the owner's device through this door.
		http.Error(w, "this node is not the owner's", http.StatusForbidden)
		return nil, false, true
	}
	session, nd, res := st.EnrolTailnet(node.StableID, node.Name, agent, enforce)
	switch res {
	case auth.EnrolRefused:
		http.Error(w, "this device was removed", http.StatusForbidden)
		return nil, false, true
	case auth.EnrolPending:
		setSession(w, session, true)
		log.Printf("auth: %s (%s) waits for approval, code %s", nd.Name, node.Name, nd.Code)
		return nd, true, false
	}
	setSession(w, session, true)
	log.Printf("auth: %s signed in through the tailnet as %s from %s", nd.Name, f.ownerLogin(), node.Name)
	return nd, false, false
}

func (f *Front) bindNode(r *http.Request, d *auth.Device) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	node, err := f.opt.Tailnet.Node(ctx, forwardedFor(r))
	if err != nil || len(node.Tags) > 0 || node.Shared || node.Login != f.ownerLogin() {
		return
	}
	f.opt.Auth.BindNode(d.ID, node.StableID, node.Name)
}

// forwardedFor is the address Serve saw the browser at: the first of
// X-Forwarded-For, which tailscaled sets itself on serve.sock.
func forwardedFor(r *http.Request) string {
	v, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

func setSession(w http.ResponseWriter, session string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: auth.Cookie, Value: session, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
}

func cookieOf(r *http.Request) string {
	if c, err := r.Cookie(auth.Cookie); err == nil {
		return c.Value
	}
	return ""
}

var refused struct {
	sync.Mutex
	last map[string]time.Time
}

// wouldRefuse logs, at most once per way in every 10 minutes, a request the
// enforce mode will refuse for want of a session.
func (f *Front) wouldRefuse(r *http.Request) {
	way := "loopback"
	switch {
	case httpguard.IsViaServe(r):
		way = "Serve's socket"
	case r.Header.Get("Tailscale-User-Login") != "":
		way = "a tailnet header over TCP"
	}
	refused.Lock()
	defer refused.Unlock()
	if refused.last == nil {
		refused.last = map[string]time.Time{}
	}
	if time.Since(refused.last[way]) < 10*time.Minute {
		return
	}
	refused.last[way] = time.Now()
	log.Printf("auth (observe): %s %s from %s has no session; browser enforce will refuse it (run fyr open, or sign in through the tailnet)", r.Method, r.URL.Path, way)
}

// BrowserName names a browser from its User-Agent: "Chrome on Android".
func BrowserName(ua string) string {
	b := "A browser"
	// Decision 0038: the Linux app's window first; it runs on WebKit, so its
	// user agent also says Safari.
	for _, c := range []struct{ key, name string }{{"FyrDesktop/", "The Fyr app"}, {"Edg/", "Edge"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"}} {
		if strings.Contains(ua, c.key) {
			b = c.name
			break
		}
	}
	for _, c := range []struct{ key, name string }{{"Android", "Android"}, {"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Mac OS X", "macOS"}, {"Windows", "Windows"}, {"Linux", "Linux"}} {
		if strings.Contains(ua, c.key) {
			return b + " on " + c.name
		}
	}
	return b
}

// drain reads what a request that is about to be refused has sent of its body
// (at most 64 KB), so the refusal reaches a proxy that is still writing it
// (Tailscale Serve answers a browser 502 when the host closes first).
func drain(r *http.Request) {
	if r.Body != nil && r.Body != http.NoBody {
		io.Copy(io.Discard, io.LimitReader(r.Body, 64<<10))
	}
}

// refuse is http.Error after draining the body.
func refuse(w http.ResponseWriter, r *http.Request, msg string, code int) {
	drain(r)
	http.Error(w, msg, code)
}

// ownerLogin is the owner's tailnet login: the config's, else the roster's.
func (f *Front) ownerLogin() string {
	if f.opt.OwnerLogin != "" {
		return f.opt.OwnerLogin
	}
	if f.opt.OwnerFn != nil {
		return f.opt.OwnerFn()
	}
	return ""
}

func (f *Front) ownerSource() string {
	if f.opt.OwnerLogin != "" {
		return "config"
	}
	if f.opt.OwnerFn != nil && f.opt.OwnerFn() != "" {
		return "roster"
	}
	return ""
}
