package front

// Browser sessions (SPEC V0.130, decision 0028 slice 1, observe): every
// browser gets a session cookie, minted by a one-time link from the owner
// socket (fyr open) or by the owner's tailnet login through Serve's socket.
// This release refuses nothing new: a request without a session is served
// as before and logged as one the next release will refuse.

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/server"
)

type deviceKey struct{}

// DeviceOf is the device whose session a request carries, or nil.
func DeviceOf(r *http.Request) *auth.Device {
	d, _ := r.Context().Value(deviceKey{}).(*auth.Device)
	return d
}

// withAuth serves /auth/once, finds each request's device by its cookie,
// mints a tailnet session for the owner through Serve's socket, and logs
// what the enforce release would refuse.
func (f *Front) withAuth(next http.Handler) http.Handler {
	st := f.opt.Auth
	if st == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/once" {
			f.authOnce(w, r)
			return
		}
		var have string
		if c, err := r.Cookie(auth.Cookie); err == nil {
			have = c.Value
		}
		d, ok := st.Lookup(have)
		if !ok && server.IsViaServe(r) && f.opt.OwnerLogin != "" && r.Header.Get("Tailscale-User-Login") == f.opt.OwnerLogin {
			// The guard checked the login; on Serve's socket tailscaled set it.
			var session string
			session, d = st.Mint(auth.HowTailnet, BrowserName(r.UserAgent()))
			setSession(w, session, true)
			log.Printf("auth: %s signed in through the tailnet as %s", d.Name, f.opt.OwnerLogin)
			ok = true
		}
		if ok {
			r = r.WithContext(context.WithValue(r.Context(), deviceKey{}, d))
		} else if strings.HasPrefix(r.URL.Path, "/api/") {
			f.wouldRefuse(r)
		}
		next.ServeHTTP(w, r)
	})
}

func setSession(w http.ResponseWriter, session string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: auth.Cookie, Value: session, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
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
		var have string
		if c, err := r.Cookie(auth.Cookie); err == nil {
			have = c.Value
		}
		session, d, err := f.opt.Auth.Redeem(body.Token, have, BrowserName(r.UserAgent()))
		if err != nil {
			api.WriteJSON(w, http.StatusGone, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if session != "" {
			setSession(w, session, r.TLS != nil || server.IsViaServe(r))
			log.Printf("auth: %s signed in on this machine", d.Name)
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "device": d.Name})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
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

// OwnerHandler serves the owner socket (V0.130 (1)): POST /session/once
// answers a one-time link into origin (http://fyr.localhost:PORT).
func (f *Front) OwnerHandler(origin string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session/once", func(w http.ResponseWriter, r *http.Request) {
		if f.opt.Auth == nil {
			api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "this front keeps no sessions"})
			return
		}
		tok := f.opt.Auth.Once(auth.HowLocal, "on "+f.opt.Name)
		api.WriteJSON(w, 200, map[string]any{"url": strings.TrimRight(origin, "/") + "/auth/once#" + tok})
	})
	return mux
}

var refused struct {
	sync.Mutex
	last map[string]time.Time
}

// wouldRefuse logs, at most once per way in every 10 minutes, a request the
// enforce release will refuse for want of a session.
func (f *Front) wouldRefuse(r *http.Request) {
	way := "loopback"
	switch {
	case server.IsViaServe(r):
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
	log.Printf("auth (observe): %s %s from %s has no session; the next release refuses it (run fyr open, or sign in through the tailnet)", r.Method, r.URL.Path, way)
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
