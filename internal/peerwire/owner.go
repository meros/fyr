package peerwire

import (
	"errors"
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/httpguard"
	"net/http"
	"strings"
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
	// ConfigOwner is config's ownerLogin (wins over the roster when non-empty).
	ConfigOwner string
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
// fyr pair, fyr host remove, fyr rotate-key, fyr host drive and fyr auth owner.
func OwnerRoutes(p Owner) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /host/list", func(w http.ResponseWriter, r *http.Request) {
			owner, _ := p.ownerInfo()
			api.WriteJSON(w, http.StatusOK, map[string]any{"members": p.Env.Members(), "candidates": p.Env.Candidates(r.Context()),
				"whoisLinkUses": p.whoisLinkUses(), "rounds": p.Env.Rounds.Load(), "ownerLogin": owner,
				"configChanged": p.configNote()})
		})
		// V0.owner-without-tailnet (#197): first-run / owner-socket step to read or
		// set the Tailscale owner login without falling back to tailnet.Self.
		mux.HandleFunc("GET /auth/owner", func(w http.ResponseWriter, r *http.Request) {
			owner, source := p.ownerInfo()
			api.WriteJSON(w, http.StatusOK, map[string]any{
				"ownerLogin":  owner,
				"source":      source,
				"configOwner": p.ConfigOwner,
				"rosterOwner": p.Env.Roster.OwnerLogin(),
			})
		})
		mux.HandleFunc("POST /auth/owner", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ Login string }
			httpguard.DecodeLoose(w, r, &b, 4096)
			login := strings.TrimSpace(b.Login)
			if login == "" || strings.ContainsAny(login, " \t\r\n") {
				api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "owner login must be a non-empty Tailscale login without spaces (e.g. you@example.com)"})
				return
			}
			p.Env.Roster.SetOwnerLogin(login)
			owner, source := p.ownerInfo()
			api.WriteJSON(w, http.StatusOK, map[string]any{
				"ok":          true,
				"ownerLogin":  owner,
				"source":      source,
				"configOwner": p.ConfigOwner,
				"rosterOwner": p.Env.Roster.OwnerLogin(),
			})
		})
		// The owner's invite (decision 0030 3.6 condition 3): open one here, or join
		// with one from another install. The owner socket is never gated.
		mux.HandleFunc("POST /pair/invite", func(w http.ResponseWriter, r *http.Request) {
			code, until := p.Env.NewInvite()
			out := map[string]any{"code": code, "until": until}
			if m, err := qr.Encode(code); err == nil {
				out["qr"] = m.Text()
			}
			api.WriteJSON(w, http.StatusOK, out)
		})
		mux.HandleFunc("POST /pair/join", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ Code, Addr string }
			httpguard.DecodeLoose(w, r, &b, 4096)
			res, err := p.Env.Join(r.Context(), b.Code, b.Addr)
			if err != nil {
				api.WriteJSON(w, joinStatus(err), map[string]any{"error": err.Error()})
				return
			}
			api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "name": res.Name, "id": res.ID, "addr": res.Addr})
		})
		mux.HandleFunc("POST /host/remove", func(w http.ResponseWriter, r *http.Request) {
			var b struct{ ID string }
			httpguard.DecodeLoose(w, r, &b, 4096)
			if err := p.Env.Remove(b.ID); err != nil {
				api.WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		})
		// fyr rotate-key: a new key waits beside the old, the roster says so (signed by the
		// old key), and fyrd restarts on the new one. A key that may be stolen is removed
		// and paired again instead.
		mux.HandleFunc("POST /host/rotate", func(w http.ResponseWriter, r *http.Request) {
			nk, err := identity.New()
			if err == nil {
				err = identity.WriteNext(p.Data, nk)
			}
			if err != nil {
				api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			p.Env.Roster.Rotate(nk.Public)
			api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "from": p.Key.ID(), "to": nk.ID()})
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
			httpguard.DecodeLoose(w, r, &b, 4096)
			if _, ok := p.Env.Roster.Member(b.ID); !ok || b.ID == p.Env.Key.ID() {
				http.Error(w, `{"error":"no such install in the roster"}`, http.StatusNotFound)
				return
			}
			p.Env.Drive.Set(b.ID, b.On)
			api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		})
	}
}

func (p Owner) ownerInfo() (login, source string) {
	if p.ConfigOwner != "" {
		return p.ConfigOwner, "config"
	}
	if r := p.Env.Roster.OwnerLogin(); r != "" {
		return r, "roster"
	}
	return "", ""
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
