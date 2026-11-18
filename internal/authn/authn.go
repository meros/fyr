// Package authn is the principal of decision 0030 (3.2, 3.3): who acts on a
// request, as a value. A listener makes one with its one authenticator; a
// handler that needs the owner takes it as an argument; a front sends it to a
// host as the one header Fyr-Actor, inside a channel that already proved which
// install speaks. Nothing here reads identity from a Host or a browser header.
package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Kind is what acts.
type Kind string

const (
	KindDevice  Kind = "device"  // a browser or PWA, by its session
	KindLocal   Kind = "local"   // the owner at this machine's owner socket
	KindInstall Kind = "install" // another install, or this one acting for itself
	// KindAgent is an agent session (the bearer on /mcp). It never crosses a
	// hop: Parse refuses it.
	KindAgent Kind = "agent"
)

// How the principal got in.
type How string

const (
	HowLocal   How = "local"
	HowTailnet How = "tailnet"
	HowCode    How = "code"
	HowLink    How = "link"
	// HowLegacy: no session, served the old way in observe mode and counted
	// (decision 0030 5.1). A later slice's enforce mode refuses it.
	HowLegacy How = "legacy"
)

// Header names. Fyr-Actor is read by a host on the pipe and the peer
// listeners only; a browser-facing listener refuses it.
const (
	Header       = "Fyr-Actor"
	OriginHeader = "Fyr-Origin"
	// PrefixHeader is the path prefix the browser's call had before the front
	// took it off ("/api/h/<hostId>"), so a preview's redirect and CSP name
	// the folder where the browser reads it (V0.preview-host-prefix).
	PrefixHeader = "Fyr-Prefix"
)

// Principal is who acts.
type Principal struct {
	Kind Kind
	ID   string // a device id, "owner", or an install id
	How  How
	// Front is the install whose front forwards the request, when it says.
	Front string
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// Header is the wire form: v=1; kind=…; id=…; how=…[; front=…].
func (p Principal) Header() string {
	s := fmt.Sprintf("v=1; kind=%s; id=%s; how=%s", p.Kind, p.ID, p.How)
	if p.Front != "" {
		s += "; front=" + p.Front
	}
	return s
}

// ErrActor: the Fyr-Actor header is not well formed.
var ErrActor = errors.New("malformed Fyr-Actor")

// Parse reads a Fyr-Actor value strictly: version 1, a known kind (not
// agent), an id of at most 64 plain characters, a known how, no repeated or
// unknown field.
func Parse(h string) (Principal, error) {
	var p Principal
	seen := map[string]bool{}
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || seen[k] {
			return Principal{}, ErrActor
		}
		seen[k] = true
		switch k {
		case "v":
			if v != "1" {
				return Principal{}, ErrActor
			}
		case "kind":
			p.Kind = Kind(v)
		case "id":
			p.ID = v
		case "how":
			p.How = How(v)
		case "front":
			p.Front = v
		default:
			return Principal{}, ErrActor
		}
	}
	if !seen["v"] || !idRe.MatchString(p.ID) || (p.Front != "" && !idRe.MatchString(p.Front)) {
		return Principal{}, ErrActor
	}
	switch p.Kind {
	case KindDevice, KindLocal, KindInstall:
	default:
		return Principal{}, ErrActor
	}
	switch p.How {
	case HowLocal, HowTailnet, HowCode, HowLink, HowLegacy:
	default:
		return Principal{}, ErrActor
	}
	return p, nil
}

type ctxKey int

const (
	principalKey ctxKey = iota
	originKey
	prefixKey
)

// With puts the principal in the request's context.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// From is the principal of a context.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

// WithOrigin puts the public origin the browser used (Fyr-Origin, read from a
// channel that is trusted to say it) in the context.
func WithOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, originKey, origin)
}

// OriginFrom is that origin, "" when none.
func OriginFrom(ctx context.Context) (string, bool) {
	o, ok := ctx.Value(originKey).(string)
	return o, ok && o != ""
}

// WithPrefix puts the host prefix of the browser's call ("/api/h/<hostId>")
// in the context: the front for its hop, the host from Fyr-Prefix.
func WithPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, prefixKey, prefix)
}

// PrefixFrom is that prefix, "" when the call had none.
func PrefixFrom(ctx context.Context) string {
	p, _ := ctx.Value(prefixKey).(string)
	return p
}

// Strip removes from an outgoing hop's headers everything a browser, Tailscale
// or a proxy put on the incoming request: the credentials and the identity
// headers a host must never see from a front (decision 0030 3.3).
func Strip(h http.Header) {
	for k := range h {
		l := strings.ToLower(k)
		switch {
		case l == "cookie", l == "authorization", l == "proxy-authorization", l == "forwarded",
			l == "origin", l == "referer", l == "fyr-actor", l == "fyr-origin", l == "fyr-prefix",
			strings.HasPrefix(l, "tailscale-"), strings.HasPrefix(l, "x-forwarded-"), strings.HasPrefix(l, "sec-fetch-"):
			delete(h, k)
		}
	}
}
