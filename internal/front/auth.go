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
	"encoding/json"
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
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/qr"
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
	case strings.HasPrefix(p, "/api/previews/"), hostPreviewPath(p):
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	return false
}

// previewRead: a GET or HEAD of a preview's page or one of its files.
func previewRead(r *http.Request) bool {
	p := r.URL.Path
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && (strings.HasPrefix(p, "/api/previews/") || hostPreviewPath(p))
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

// authRoutes: /auth/once (a one-time link), /auth/code (ask for an approval
// code) and /auth/status (what happened to it).
func (f *Front) authRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth/once":
		f.authOnce(w, r)
	case "/auth/code":
		f.authCode(w, r)
	case "/auth/status":
		f.authStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

// authOnce: GET is a page that posts the link's token (in the fragment, so
// it is in no log or Referer); POST redeems it for a session.
func (f *Front) authOnce(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Write([]byte(oncePage))
	case http.MethodPost:
		var body struct{ Token string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		session, d, err := f.opt.Auth.Redeem(body.Token, cookieOf(r), BrowserName(r.UserAgent()))
		if err != nil {
			api.WriteJSON(w, http.StatusGone, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if session != "" {
			setSession(w, session, r.TLS != nil || httpguard.IsViaServe(r))
			log.Printf("auth: %s signed in by a link", d.Name)
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "device": d.Name})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
	}
}

// authCode: a browser with no session asks to be let in. It gets a session
// that waits and an 8-character code, valid 5 minutes, which the owner types on
// a signed-in device or to `fyr device approve`. A browser that already waits
// gets its code again.
func (f *Front) authCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	st := f.opt.Auth
	if d, ok := st.Lookup(cookieOf(r)); ok {
		if d.Pending {
			api.WriteJSON(w, 200, codeBody(d))
		} else {
			api.WriteJSON(w, 200, map[string]any{"ok": true, "signedIn": true})
		}
		return
	}
	session, d, err := st.Pending(auth.HowCode, BrowserName(r.UserAgent()), "")
	if err != nil {
		api.WriteJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	setSession(w, session, r.TLS != nil || httpguard.IsViaServe(r))
	log.Printf("auth: %s asks to be let in, code %s", d.Name, d.Code)
	api.WriteJSON(w, 200, codeBody(d))
}

func codeBody(d *auth.Device) map[string]any {
	return map[string]any{"ok": true, "pending": true, "code": d.Code, "until": d.CodeUntil}
}

// authStatus tells a waiting browser whether it was let in.
func (f *Front) authStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	d, ok := f.opt.Auth.Lookup(cookieOf(r))
	switch {
	case !ok:
		api.WriteJSON(w, 200, map[string]any{"state": "none"})
	case d.Pending:
		api.WriteJSON(w, 200, map[string]any{"state": "waiting", "code": d.Code, "until": d.CodeUntil})
	default:
		api.WriteJSON(w, 200, map[string]any{"state": "in"})
	}
}

const oncePage = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Fyr</title><style>body{font:16px system-ui,sans-serif;margin:48px 16px;color:#555}</style>
<p id="m">Signing in…</p>
<script>
const t = location.hash.slice(1);
history.replaceState(null, "", "/auth/once");
fetch("/auth/once", {method: "POST", headers: {"Content-Type": "application/json", "X-Fyr": "1"}, body: JSON.stringify({token: t})})
  .then(r => r.json()).then(j => { if (j.ok) location.replace("/"); else document.getElementById("m").textContent = j.error; })
  .catch(() => { document.getElementById("m").textContent = "Fyr did not answer."; });
</script>`

// signedOutPage is what a browser without a session sees in enforce mode: a
// code to give the owner, and a wait that ends when it is approved. It is
// the whole page: the client is not loaded.
const signedOutPage = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Fyr</title>
<style>body{font:16px system-ui,sans-serif;margin:48px 16px;color:#444;max-width:30em}
#c{font:700 2em ui-monospace,monospace;letter-spacing:.12em;margin:.6em 0}p{line-height:1.45}</style>
<h1 style="font-size:1.3em">This device is not signed in</h1>
<p>Approve it from a device that is signed in: Settings, Devices, Approve, and type this code. Or run <code>fyr device approve</code> on the machine that runs Fyr.</p>
<div id="c">…</div><p id="m"></p>
<script>
const post = () => fetch("/auth/code", {method: "POST", headers: {"X-Fyr": "1"}}).then(r => r.json());
const status = () => fetch("/auth/status").then(r => r.json());
let until = 0;
const show = j => { if (j.code) { document.getElementById("c").textContent = j.code.slice(0, 4) + "-" + j.code.slice(4); until = Date.parse(j.until); } };
post().then(j => { if (j.signedIn) location.replace("/"); else if (j.error) document.getElementById("m").textContent = j.error; else show(j); });
const t = setInterval(() => status().then(j => {
  if (j.state === "in") { clearInterval(t); location.replace("/"); }
  else if (j.state === "waiting") show(j);
  else if (until && Date.now() > until) { clearInterval(t); document.getElementById("m").textContent = "The code ended. Reload to get a new one."; }
}).catch(() => {}), 2000);
</script>`

// SetLANOrigin is the https://host:port a phone reaches this front at (decision 0030
// 3.5); fyr device add makes its QR link there. "" is none.
func (f *Front) SetLANOrigin(o string) { f.lanOrigin.Store(&o) }

// OwnerHandler serves the owner socket (V0.130 (1), V0.157): the one local
// channel, which no mode gates. fyr open, fyr device add|approve|revoke|list
// and fyr auth status|observe|enforce come through it.
func (f *Front) OwnerHandler(origin string) http.Handler {
	mux := http.NewServeMux()
	needAuth := func(w http.ResponseWriter) bool {
		if f.opt.Auth == nil {
			api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "this front keeps no sessions"})
			return false
		}
		return true
	}
	link := func(tok string) string { return strings.TrimRight(origin, "/") + "/auth/once#" + tok }
	mux.HandleFunc("POST /session/once", func(w http.ResponseWriter, r *http.Request) {
		if needAuth(w) {
			api.WriteJSON(w, 200, map[string]any{"url": link(f.opt.Auth.Once(auth.HowLocal, "on "+f.opt.Name))})
		}
	})
	mux.HandleFunc("POST /device/add", func(w http.ResponseWriter, r *http.Request) {
		if !needAuth(w) {
			return
		}
		var b struct{ Name string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		add := link
		if o := f.lanOrigin.Load(); o != nil && *o != "" { // a phone is far from the owner socket's own origin: its link is the HTTPS one
			add = func(tok string) string { return strings.TrimRight(*o, "/") + "/auth/once#" + tok }
		}
		api.WriteJSON(w, 200, f.oneTimeLink(add, auth.HowLink, nameOr(b.Name, "a new device"), "", 10*time.Minute))
	})
	mux.HandleFunc("POST /device/approve", func(w http.ResponseWriter, r *http.Request) {
		if !needAuth(w) {
			return
		}
		var b struct{ Code string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		d, err := f.opt.Auth.ApproveCode(b.Code, "")
		if err != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "device": d.Name, "id": d.ID})
	})
	mux.HandleFunc("POST /device/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !needAuth(w) {
			return
		}
		var b struct{ ID string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		gone := f.opt.Auth.Revoke(b.ID, "owner.sock")
		if len(gone) == 0 {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "no such device"})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "revoked": gone})
	})
	mux.HandleFunc("GET /device/list", func(w http.ResponseWriter, r *http.Request) {
		if needAuth(w) {
			api.WriteJSON(w, 200, map[string]any{"devices": f.opt.Auth.List()})
		}
	})
	mux.HandleFunc("GET /auth/status", func(w http.ResponseWriter, r *http.Request) {
		if needAuth(w) {
			api.WriteJSON(w, 200, f.authState())
		}
	})
	mux.HandleFunc("POST /auth/mode", func(w http.ResponseWriter, r *http.Request) {
		if !needAuth(w) {
			return
		}
		var b struct{ Way, Mode string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		if err := f.opt.Auth.SetMode(b.Way, b.Mode, "owner.sock"); err != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, f.authState())
	})
	nativeRoutes(mux, f.opt.Push)
	if f.opt.OwnerRoutes != nil {
		f.opt.OwnerRoutes(mux)
	}
	return mux
}

// NativePusher is the part of a Pusher that serves native devices
// (*push.Pusher implements it; V0.mac-notify).
type NativePusher interface {
	NewNative(user, endpoint, device string) (string, error)
	Listen(endpoint string) (<-chan []byte, func())
}

// nativeRoutes serve Fyr.app on the owner socket (decision 0037,
// V0.mac-notify): POST /push/native stores the app as a device, keeping the
// endpoint it already holds, and GET /push/native/stream?endpoint= sends its
// pushes as one JSON line each, an empty line every 30 s to show the link
// lives.
func nativeRoutes(mux *http.ServeMux, p Pusher) {
	np, ok := p.(NativePusher)
	if !ok {
		return
	}
	mux.HandleFunc("POST /push/native", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Endpoint, Device string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		ep, err := np.NewNative(core.Owner, b.Endpoint, nameOr(b.Device, "Fyr.app"))
		if err != nil {
			api.WriteJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "endpoint": ep})
	})
	mux.HandleFunc("GET /push/native/stream", func(w http.ResponseWriter, r *http.Request) {
		ep := r.URL.Query().Get("endpoint")
		if !push.IsNative(ep) || !p.Known(ep) {
			api.WriteJSON(w, 404, map[string]any{"ok": false, "error": "no such native device"})
			return
		}
		fl, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		if fl != nil {
			fl.Flush()
		}
		ch, stop := np.Listen(ep)
		defer stop()
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case b, ok := <-ch:
				if !ok {
					return // another listener took over, or the device was removed
				}
				w.Write(append(b, '\n'))
			case <-tick.C:
				w.Write([]byte("\n"))
			}
			if fl != nil {
				fl.Flush()
			}
		}
	})
}

func nameOr(n, d string) string {
	if n = strings.TrimSpace(n); n != "" {
		return n
	}
	return d
}

// oneTimeLink makes a one-time link for a device added by by ("" for the owner
// socket) with its QR code as text and as an image.
func (f *Front) oneTimeLink(link func(string) string, how, name, by string, ttl time.Duration) map[string]any {
	u := link(f.opt.Auth.OnceFor(how, name, by, ttl))
	out := map[string]any{"url": u, "expires": time.Now().Add(ttl).UTC()}
	if m, err := qr.Encode(u); err == nil {
		out["qr"], out["svg"] = m.Text(), m.SVG()
	}
	return out
}

func (f *Front) legacyFronts() int64 {
	if f.opt.LegacyFronts == nil {
		return 0
	}
	return f.opt.LegacyFronts()
}

// authState is the view of Settings → Devices and `fyr auth status`.
func (f *Front) authState() map[string]any {
	st := f.opt.Auth
	audit := st.Audit()
	if len(audit) > 20 {
		audit = audit[len(audit)-20:]
	}
	return map[string]any{"devices": st.List(), "modes": st.Modes(), "audit": audit, "legacyFronts": f.legacyFronts()}
}

// authAPI is Settings → Devices (desktop and phone): the browser's calls
// that need a signed-in device. Trust-widening ones (approve, add, a mode)
// need a session; reading in observe mode does not.
func (f *Front) authAPI(mux muxer) {
	st := f.opt.Auth
	if st == nil {
		return
	}
	need := func(w http.ResponseWriter, r *http.Request) (authn.Principal, bool) {
		p, ok := signedIn(r)
		if !ok {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "sign in first: run fyr open on the machine that runs Fyr"})
		}
		return p, ok
	}
	both(mux, "GET", "/auth/state", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := signedIn(r); !ok && st.Mode(auth.WayBrowser) == auth.ModeEnforce {
			f.signedOut(w, r, false, nil)
			return
		}
		s := f.authState()
		if p, ok := signedIn(r); ok {
			s["you"] = p.ID
		}
		api.WriteJSON(w, 200, s)
	})
	wide := func(w http.ResponseWriter, r *http.Request) (authn.Principal, bool) { // trust-widening: a session that has lived a while
		p, ok := need(w, r)
		if !ok {
			return p, false
		}
		if err := st.StepUp(p.ID); err != nil {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": err.Error()})
			return p, false
		}
		return p, true
	}
	both(mux, "POST", "/auth/approve", func(w http.ResponseWriter, r *http.Request) {
		p, ok := wide(w, r)
		if !ok {
			return
		}
		var b struct{ Code, ID string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		var d *auth.Device
		var err error
		if b.ID != "" {
			d, err = st.ApproveDevice(b.ID, p.ID)
		} else {
			d, err = st.ApproveCode(b.Code, p.ID)
		}
		if err != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "device": d.Name})
	})
	both(mux, "POST", "/auth/deny", func(w http.ResponseWriter, r *http.Request) {
		p, ok := need(w, r)
		if !ok {
			return
		}
		var b struct{ ID string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		api.WriteJSON(w, 200, map[string]any{"ok": st.Deny(b.ID, "device:"+p.ID)})
	})
	both(mux, "POST", "/auth/revoke", func(w http.ResponseWriter, r *http.Request) {
		p, ok := need(w, r)
		if !ok {
			return
		}
		var b struct{ ID string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		gone := st.Revoke(b.ID, "device:"+p.ID)
		api.WriteJSON(w, 200, map[string]any{"ok": len(gone) > 0, "revoked": gone})
	})
	both(mux, "POST", "/auth/add", func(w http.ResponseWriter, r *http.Request) {
		p, ok := wide(w, r)
		if !ok {
			return
		}
		var b struct{ Name string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		origin := originOf(r)
		api.WriteJSON(w, 200, f.oneTimeLink(func(tok string) string { return origin + "/auth/once#" + tok }, auth.HowLink, nameOr(b.Name, "a new device"), p.ID, 10*time.Minute))
	})
	both(mux, "POST", "/auth/mode", func(w http.ResponseWriter, r *http.Request) {
		p, ok := need(w, r)
		if !ok {
			return
		}
		var b struct{ Way, Mode string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		if err := st.SetMode(b.Way, b.Mode, "device:"+p.ID); err != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, f.authState())
	})
}

// streams tracks each signed-in device's open streams, so a revoke closes them.
type streams struct {
	mu sync.Mutex
	by map[string]map[int]context.CancelFunc
	n  int
}

func (s *streams) add(device string, cancel context.CancelFunc) (remove func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.by == nil {
		s.by = map[string]map[int]context.CancelFunc{}
	}
	if s.by[device] == nil {
		s.by[device] = map[int]context.CancelFunc{}
	}
	s.n++
	id := s.n
	s.by[device][id] = cancel
	return func() { s.mu.Lock(); delete(s.by[device], id); s.mu.Unlock() }
}

func (s *streams) close(device string) {
	s.mu.Lock()
	cs := s.by[device]
	delete(s.by, device)
	s.mu.Unlock()
	for _, c := range cs {
		c()
	}
}

// watchAuth wires what a revoke ends beyond the session: the device's open
// streams and its push subscriptions (decision 0030 3.4), and rule 3's
// automatic enforce (every 10 minutes and once at the start).
func (f *Front) watchAuth(ctx context.Context) {
	st := f.opt.Auth
	if st == nil {
		return
	}
	st.OnRevoke(func(ids []string) {
		for _, id := range ids {
			f.streams.close(id)
			f.dropSubs(id)
		}
	})
	tick := func() {
		defer f.authTicks.Add(1)
		for _, way := range st.Tick() {
			msg := "Fyr now asks every browser to sign in (" + way + " way in). Run `fyr auth observe` to go back."
			if way == auth.WayPeer {
				msg = "Fyr now takes other installs only over their pinned links; the old host listeners are removed. Run `fyr auth observe peer` to go back."
			}
			log.Printf("auth: %s", msg)
			if f.opt.Push != nil {
				f.opt.Push.Notify(core.Owner, push.Payload{Title: "Fyr", Body: msg, State: "auth"})
			}
		}
	}
	tick()
	t := time.NewTicker(f.authEvery())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

func (f *Front) authEvery() time.Duration {
	if f.opt.AuthTick > 0 {
		return f.opt.AuthTick
	}
	return 10 * time.Minute
}

const subsKey = "auth.pushsubs"

// noteSub remembers which device's session subscribed an endpoint.
func (f *Front) noteSub(device, endpoint string) {
	if f.opt.Store == nil || device == "" || endpoint == "" {
		return
	}
	f.subMu.Lock()
	defer f.subMu.Unlock()
	m := f.loadSubs()
	for _, e := range m[device] {
		if e == endpoint {
			return
		}
	}
	m[device] = append(m[device], endpoint)
	b, _ := json.Marshal(m)
	f.opt.Store.SetKV(subsKey, string(b))
}

func (f *Front) loadSubs() map[string][]string {
	m := map[string][]string{}
	if raw, err := f.opt.Store.GetKV(subsKey); err == nil && raw != "" {
		json.Unmarshal([]byte(raw), &m)
	}
	return m
}

// dropSubs drops the push subscriptions a revoked device's session made.
func (f *Front) dropSubs(device string) {
	if f.opt.Store == nil || f.opt.Push == nil {
		return
	}
	f.subMu.Lock()
	defer f.subMu.Unlock()
	m := f.loadSubs()
	for _, e := range m[device] {
		f.opt.Push.Unsubscribe(e)
	}
	delete(m, device)
	b, _ := json.Marshal(m)
	f.opt.Store.SetKV(subsKey, string(b))
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
	for _, c := range []struct{ key, name string }{{"Edg/", "Edge"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"}} {
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
