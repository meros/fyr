package peerwire

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/qr"
)

// Owner is what the owner socket's peer routes act on.
type Owner struct {
	Env  *peer.Env
	Key  *identity.Key
	Data string // where a rotated key waits (identity.WriteNext)
	// WhoisLinkUses counts the front's requests over the old whois link
	// (fyr host list); nil: 0.
	WhoisLinkUses func() int64
	// ConfigNote is "" while the config file is what fyrd read, else what to do
	// (V0.180, fyr host list); nil: "".
	ConfigNote func() string
	// Restart restarts fyrd after a key rotation (a SIGHUP: the launcher
	// restarts it on exit 75, as for a release).
	Restart func()
}

// OwnerRoutes adds the roster's calls to the owner socket: fyr host list,
// fyr pair, fyr host remove, fyr rotate-key and fyr host drive.
func OwnerRoutes(p Owner) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /host/list", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"members": p.Env.Members(), "candidates": p.Env.Candidates(r.Context()),
				"whoisLinkUses": p.whoisLinkUses(), "rounds": p.Env.Rounds.Load(), "ownerLogin": p.Env.Roster.OwnerLogin(),
				"configChanged": p.configNote()})
		})
		// The owner's invite (decision 0030 3.6 condition 3): open one here, or join
		// with one from another install. The owner socket is never gated.
		mux.HandleFunc("POST /pair/invite", func(w http.ResponseWriter, r *http.Request) {
			code, until := p.Env.NewInvite()
			out := map[string]any{"code": code, "until": until}
			if m, err := qr.Encode(code); err == nil {
				out["qr"] = m.Text()
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(out)
		})
		mux.HandleFunc("POST /pair/join", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ Code, Addr string }
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
			res, err := p.Env.Join(r.Context(), b.Code, b.Addr)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(joinStatus(err))
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": res.Name, "id": res.ID, "addr": res.Addr})
		})
		mux.HandleFunc("POST /host/remove", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ ID string }
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
			w.Header().Set("Content-Type", "application/json")
			if err := p.Env.Remove(b.ID); err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		})
		// fyr rotate-key: a new key waits beside the old, the roster says so (signed by the
		// old key), and fyrd restarts on the new one. A key that may be stolen is removed
		// and paired again instead.
		mux.HandleFunc("POST /host/rotate", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			nk, err := identity.New()
			if err == nil {
				err = identity.WriteNext(p.Data, nk)
			}
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			p.Env.Roster.Rotate(nk.Public)
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "from": p.Key.ID(), "to": nk.ID()})
			go func() { // the launcher restarts fyrd on exit 75, as for a release
				time.Sleep(300 * time.Millisecond)
				p.Restart()
			}()
		})
		mux.HandleFunc("POST /host/drive", func(w http.ResponseWriter, r *http.Request) {
			var b struct {
				ID string
				On bool
			}
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
			if _, ok := p.Env.Roster.Member(b.ID); !ok || b.ID == p.Env.Key.ID() {
				http.Error(w, `{"error":"no such install in the roster"}`, http.StatusNotFound)
				return
			}
			p.Env.Drive.Set(b.ID, b.On)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		})
	}
}

func (p Owner) whoisLinkUses() int64 {
	if p.WhoisLinkUses == nil {
		return 0
	}
	return p.WhoisLinkUses()
}

func (p Owner) configNote() string {
	if p.ConfigNote == nil {
		return ""
	}
	return p.ConfigNote()
}

// joinStatus is the owner route's status for a failed join (V0.180, #261): the
// inviter's own when it answered no (a 5xx of its is a bad gateway here), 409 when
// this install could not get that far.
func joinStatus(err error) int {
	var r *peer.JoinRefused
	switch {
	case errors.As(err, &r) && r.Status >= 500:
		return http.StatusBadGateway
	case errors.As(err, &r) && r.Status >= 400:
		return r.Status
	}
	return http.StatusConflict
}
