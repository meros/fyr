package front

// Settings → Hosts (decision 0030 3.6, V0.159): the installs this one is paired
// with and how each came in, the tailnet nodes that are not (and why), and the
// owner's invite, both ways: Pair (open an invite here) and Add (type one from
// another install). Desktop and phone from the same page.

import (
	"encoding/json"
	"net/http"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/qr"
)

func (f *Front) peerAPI(mux muxer) {
	p := f.opt.Peer
	if p == nil {
		return
	}
	need := func(w http.ResponseWriter, r *http.Request) bool {
		if _, ok := signedIn(r); !ok {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "sign in first: run fyr open on the machine that runs Fyr"})
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/peer/state", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := signedIn(r); !ok && f.opt.Auth != nil && f.opt.Auth.Mode("browser") == "enforce" {
			f.signedOut(w, r, false, nil)
			return
		}
		out := map[string]any{"members": p.Members(), "candidates": p.Candidates(r.Context()), "whoisLinkUses": f.WhoisLinkUses()}
		if until, ok := p.Invites.Open(); ok {
			out["inviteUntil"] = until
		}
		_, signed := signedIn(r)
		out["signedIn"] = signed
		api.WriteJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/peer/invite", func(w http.ResponseWriter, r *http.Request) {
		if !need(w, r) {
			return
		}
		code, until := p.NewInvite()
		out := map[string]any{"ok": true, "code": code, "until": until, "link": originOf(r) + "/?dialog=settings&page=hosts&settings=hosts&pair=" + code}
		if m, err := qr.Encode(code); err == nil {
			out["svg"] = m.SVG()
		}
		api.WriteJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/peer/join", func(w http.ResponseWriter, r *http.Request) {
		if !need(w, r) {
			return
		}
		var b struct{ Code, Addr string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		res, err := p.Join(r.Context(), b.Code, b.Addr)
		if err != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "name": res.Name, "addr": res.Addr})
	})
}

