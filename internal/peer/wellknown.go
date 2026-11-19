package peer

import (
	"encoding/json"
	"net/http"
)

// WellKnown is an install's answer at /.well-known/fyr (V0.123, decision 0025),
// which the pairing reads over the node's Serve endpoint (condition 1). It
// lived in internal/peers with the tailnet discovery that V0.retire-discovery
// removed.
type WellKnown struct {
	Fyr   int    `json:"fyr"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Build string `json:"build"`
	Owner string `json:"owner"`
	// ID is the install's key id (V0.124, decision 0026).
	ID string `json:"id,omitempty"`
	// Host is where this install serves the host protocol to fronts on other
	// machines (its hostListen, V0.51), e.g. http://100.64.0.2:7778; empty
	// when it serves none. An older install's discovery follows it there.
	Host string `json:"host,omitempty"`
	// Peer is this install's peer port (decision 0030 3.6): the port other
	// installs of the same tailnet user dial, pinned to ID, once they have
	// checked this answer over WebPKI.
	Peer int `json:"peer,omitempty"`
}

// WellKnownHandler answers /.well-known/fyr with self().
func WellKnownHandler(self func() WellKnown) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(self())
	})
}

// OnlyWellKnown answers GET /.well-known/fyr and nothing else, for a
// loopback port Tailscale Serve proxies to over TCP (the macOS app cannot
// reach a Unix socket). Any local process may connect there, so it trusts
// nothing and serves no page or session; a tailnet login other than owner
// is refused.
func OnlyWellKnown(owner string, self func() WellKnown) http.Handler {
	h := WellKnownHandler(self)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/.well-known/fyr" {
			http.Error(w, "this port answers only GET /.well-known/fyr", http.StatusNotFound)
			return
		}
		if login := r.Header.Get("Tailscale-User-Login"); login != "" && login != owner {
			http.Error(w, "this Fyr belongs to another tailnet user", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}
