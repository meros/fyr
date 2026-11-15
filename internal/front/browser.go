package front

import (
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/push"
)

// The browser-only surface (decision 0016, V0.51, V0.54): the web client,
// the push endpoints and presence. The front serves them; a host does not.

// Pusher is what the push endpoints and a front's notifications need
// (*push.Pusher implements it).
type Pusher interface {
	PublicKey() string
	Subscribe(user string, sub push.Subscription, device string) error
	Unsubscribe(endpoint string) error
	Notify(user string, pl push.Payload) int
	Known(endpoint string) bool
}

// webClient serves the built web client with fresh pages (V0.13, V0.15).
func webClient(web fs.FS) http.Handler { return noCacheIndex(http.FileServerFS(web)) }

func init() { mime.AddExtensionType(".webmanifest", "application/manifest+json") }

// noCacheIndex keeps the page, the manifest and the service worker fresh,
// so a new build is picked up at the next load (V0.13, V0.15).
func noCacheIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" || strings.HasSuffix(p, ".html") || p == "/sw.js" || p == "/manifest.webmanifest" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p == "/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		next.ServeHTTP(w, r)
	})
}

// pushRoutes serves the Web Push endpoints (V0.15) for p: a host's before
// decision 0016, a front's since V0.54 (the front holds the device data).
func pushRoutes(mux *http.ServeMux, p Pusher) {
	mux.HandleFunc("GET /api/push/key", func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, map[string]string{"publicKey": p.PublicKey()})
	})
	mux.HandleFunc("POST /api/push/subscribe", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Subscription push.Subscription `json:"subscription"`
			Device       string            `json:"device"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b); err != nil {
			api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON"})
			return
		}
		if err := p.Subscribe(core.Owner, b.Subscription, b.Device); err != nil {
			api.WriteJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/push/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b); err != nil || b.Endpoint == "" {
			api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "endpoint is required"})
			return
		}
		p.Unsubscribe(b.Endpoint)
		api.WriteJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/push/status", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Endpoint string `json:"endpoint"`
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b)
		api.WriteJSON(w, 200, map[string]bool{"known": b.Endpoint != "" && p.Known(b.Endpoint)})
	})
	mux.HandleFunc("POST /api/push/test", func(w http.ResponseWriter, r *http.Request) {
		n := p.Notify(core.Owner, push.Payload{Title: "Fyr", Body: "Notifications work on this device.", State: "test"})
		api.WriteJSON(w, 200, map[string]int{"delivered": n})
	})
}

// presenceRoute records which thread each device shows (V0.20).
func presenceRoute(mux *http.ServeMux, p *notify.Presence) {
	mux.HandleFunc("POST /api/presence", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ThreadID string `json:"threadId"`
			Device   string `json:"device"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b); err != nil || b.Device == "" {
			http.Error(w, "threadId and device are required", 400)
			return
		}
		p.Beat(b.Device, b.ThreadID, time.Now())
		w.WriteHeader(http.StatusNoContent)
	})
}
