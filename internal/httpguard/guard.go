// Package httpguard holds the request guards a host and a front share
// (V0.183): the loopback and tailnet guard for browsers, gzip, and the marks
// of the way a request came in (Tailscale Serve's socket, the owner's HTTPS
// listener on the LAN). It was internal/server's until V0.183; the front
// imports it instead of the host's server.
package httpguard

import (
	"context"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
)

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]+)?$|^\[[0-9A-Fa-f:.]+\](:[0-9]+)?$`)

// ValidHost: s is a host name or an IP literal, with an optional port, and
// nothing else (no scheme, path or space).
func ValidHost(s string) bool { return hostRe.MatchString(s) }

// Guard refuses requests that are neither local nor the owner over the
// tailnet, and POSTs without X-Fyr (cross-site form posts). The MCP endpoint
// uses its bearer token.
//
// Local: a loopback Host header (this also stops DNS rebinding).
// Tailnet (SPEC 0.5 V0.14): Tailscale Serve proxies from loopback, keeps
// the browser's Host (https://<host>.ts.net) and adds Tailscale-User-Login;
// it must equal owner. The header alone proves nothing: a DNS-rebinding page
// reaches loopback too and may set any header, but its Host is its own
// domain, never a ts.net name (review 2026-11-15). Serve's own socket is the
// exception: tailscaled reaches it through a root-only file, sets the header
// itself and does not pass the browser's Host on (the request arrives as
// localhost), so a request on that socket needs no ts.net Host.
func Guard(owner string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if login := r.Header.Get("Tailscale-User-Login"); login != "" {
			if !IsViaServe(r) && !TailnetHost(r.Host) {
				http.Error(w, "the tailnet identity header needs a ts.net host", http.StatusForbidden)
				return
			}
			if owner == "" || login != owner {
				http.Error(w, "this Fyr belongs to another tailnet user", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPost && r.URL.Path != "/mcp" && r.Header.Get("X-Fyr") != "1" {
				http.Error(w, "missing X-Fyr header", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		ip := net.ParseIP(host)
		// V0.130: fyr.localhost (any *.localhost) is loopback too; browsers
		// resolve it locally, so no rebinding page can carry it.
		if !IsViaLAN(r) && !localName(host) && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "Fyr v0 serves loopback only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path != "/mcp" && r.Header.Get("X-Fyr") != "1" {
			http.Error(w, "missing X-Fyr header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// localName: localhost or a name under it (RFC 6761).
func localName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || (strings.HasSuffix(host, ".localhost") && len(host) > len(".localhost"))
}

type viaServeKey struct{}

// ViaServe marks every request of a handler as come through Tailscale
// Serve's own socket (V0.130 (3)): only there is Tailscale-User-Login set by
// tailscaled itself, which strips any copy the browser sent.
func ViaServe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viaServeKey{}, true)))
	})
}

// ServeSocket serves Tailscale Serve's socket (V0.130 (3)): ViaServe, and
// the request's Host becomes the public name the browser used. tailscaled
// does not keep the browser's Host on this socket (#225), and a front proxies
// to its hosts with the Host it got: the host's guard needs a ts.net name for
// the identity header, and a preview's CSP names its folder by that Host.
// The name is X-Forwarded-Host when tailscaled sets a ts.net one, else
// public (<node>.ts.net:<Serve port>); "" keeps the Host. Only this socket
// trusts it: tailscaled alone reaches it, so no rebinding page sets it.
func ServeSocket(public string, next http.Handler) http.Handler {
	var logged atomic.Bool
	return ViaServe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logged.CompareAndSwap(false, true) {
			log.Printf("serve socket: first request: Host %q, X-Forwarded-Host %q, X-Forwarded-Proto %q; public %q",
				r.Host, r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"), public)
		}
		if fh := r.Header.Get("X-Forwarded-Host"); TailnetHost(fh) && ValidHost(fh) {
			r.Host = fh
		} else if public != "" {
			r.Host = public
		}
		r.Header.Set("X-Forwarded-Proto", "https") // Serve answers on HTTPS only
		next.ServeHTTP(w, r)
	}))
}

type viaLANKey struct{}

// ViaLAN marks every request of a handler as come over the owner's HTTPS listener
// (decision 0030 3.5): a browser on the LAN. Nothing is trusted for being near: the
// front serves such a request to a signed-in device only, whatever the modes say, and
// refuses a Tailscale header on it.
func ViaLAN(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			http.Error(w, "this listener serves HTTPS only", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viaLANKey{}, true)))
	})
}

// IsViaLAN reports whether a request came over the HTTPS listener.
func IsViaLAN(r *http.Request) bool { v, _ := r.Context().Value(viaLANKey{}).(bool); return v }

// IsViaServe reports whether a request came through Serve's socket.
func IsViaServe(r *http.Request) bool { v, _ := r.Context().Value(viaServeKey{}).(bool); return v }

// TailnetHost reports whether a Host header names a MagicDNS host, the only
// name Tailscale Serve answers on with HTTPS.
func TailnetHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.HasSuffix(host, ".ts.net") && len(host) > len(".ts.net")
}

// Browser wraps a browser-facing handler (the host's own, or a front's,
// V0.51) in the loopback and tailnet guard and gzip.
func Browser(owner string, next http.Handler) http.Handler { return Guard(owner, Gzip(next)) }
