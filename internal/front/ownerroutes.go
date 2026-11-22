package front

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/push"
)

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
