package front

import (
	"context"
	"github.com/meros/fyr/internal/httpguard"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/qr"
)

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
		if !api.DecodeBody(w, r, &body, httpguard.SmallBody) {
			return
		}
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
	return map[string]any{"devices": st.List(), "modes": st.Modes(), "audit": audit, "legacyFronts": f.legacyFronts(),
		"ownerLogin": f.ownerLogin(), "ownerSource": f.ownerSource()}
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
	mux.HandleFunc("GET /api/front/auth/state", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST /api/front/auth/approve", func(w http.ResponseWriter, r *http.Request) {
		p, ok := wide(w, r)
		if !ok {
			return
		}
		var b struct{ Code, ID string }
		if !api.DecodeBody(w, r, &b, httpguard.SmallBody) {
			return
		}
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
	mux.HandleFunc("POST /api/front/auth/deny", func(w http.ResponseWriter, r *http.Request) {
		p, ok := need(w, r)
		if !ok {
			return
		}
		var b struct{ ID string }
		if !api.DecodeBody(w, r, &b, httpguard.SmallBody) {
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": st.Deny(b.ID, "device:"+p.ID)})
	})
	mux.HandleFunc("POST /api/front/auth/revoke", func(w http.ResponseWriter, r *http.Request) {
		p, ok := need(w, r)
		if !ok {
			return
		}
		var b struct{ ID string }
		if !api.DecodeBody(w, r, &b, httpguard.SmallBody) {
			return
		}
		gone := st.Revoke(b.ID, "device:"+p.ID)
		api.WriteJSON(w, 200, map[string]any{"ok": len(gone) > 0, "revoked": gone})
	})
	mux.HandleFunc("POST /api/front/auth/add", func(w http.ResponseWriter, r *http.Request) {
		p, ok := wide(w, r)
		if !ok {
			return
		}
		var b struct{ Name string }
		if !api.DecodeOptional(w, r, &b, httpguard.SmallBody) {
			return
		}
		origin := originOf(r)
		api.WriteJSON(w, 200, f.oneTimeLink(func(tok string) string { return origin + "/auth/once#" + tok }, auth.HowLink, nameOr(b.Name, "a new device"), p.ID, 10*time.Minute))
	})
	mux.HandleFunc("POST /api/front/auth/mode", func(w http.ResponseWriter, r *http.Request) {
		p, ok := need(w, r)
		if !ok {
			return
		}
		var b struct{ Way, Mode string }
		if !api.DecodeBody(w, r, &b, httpguard.SmallBody) {
			return
		}
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
