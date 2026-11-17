package authn

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// SPEC V0.156 (decision 0030 3.3): the principal is one header on the hop,
// parsed strictly: a missing or malformed one is never turned into a default.
func TestHeaderRoundTrip(t *testing.T) {
	for _, p := range []Principal{
		{Kind: KindDevice, ID: "dev_ab-12", How: HowTailnet},
		{Kind: KindLocal, ID: "owner", How: HowLocal},
		{Kind: KindInstall, ID: "F7QK-2M9X-AB34-CD56", How: HowLink},
		{Kind: KindDevice, ID: "legacy", How: HowLegacy, Front: "F7QK-2M9X-AB34-CD56"},
	} {
		got, err := Parse(p.Header())
		if err != nil || got != p {
			t.Errorf("%+v -> %q -> %+v %v", p, p.Header(), got, err)
		}
	}
	if h := (Principal{Kind: KindDevice, ID: "d1", How: HowCode}).Header(); h != "v=1; kind=device; id=d1; how=code" {
		t.Errorf("the wire form changed: %q", h)
	}
}

func TestParseRefuses(t *testing.T) {
	for _, h := range []string{
		"", "kind=device; id=d; how=local", "v=2; kind=device; id=d; how=local",
		"v=1; kind=agent; id=d; how=local", "v=1; kind=root; id=d; how=local",
		"v=1; kind=device; id=; how=local", "v=1; kind=device; how=local", "v=1; kind=device; id=d",
		"v=1; kind=device; id=d; how=magic", "v=1; kind=device; id=a b; how=local",
		"v=1; kind=device; id=d; how=local; how=tailnet", "v=1; kind=device; id=d; how=local; id=e",
		"v=1; kind=device; id=" + strings.Repeat("a", 65) + "; how=local",
		"v=1; kind=device; id=d; how=local; x=1", "v=1;kind", "v=1; kind=device; id=d\r\nX: y; how=local",
	} {
		if p, err := Parse(h); err == nil {
			t.Errorf("%q was accepted as %+v", h, p)
		}
	}
}

func TestContext(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Fatal("a principal from nothing")
	}
	p := Principal{Kind: KindDevice, ID: "d", How: HowLocal}
	got, ok := From(With(context.Background(), p))
	if !ok || got != p {
		t.Errorf("%+v %v", got, ok)
	}
	if o, _ := OriginFrom(WithOrigin(context.Background(), "https://a.ts.net:7443")); o != "https://a.ts.net:7443" {
		t.Errorf("origin %q", o)
	}
}

// Strip leaves nothing a browser or Tailscale could have set on the way in.
func TestStrip(t *testing.T) {
	h := http.Header{}
	for _, k := range []string{"Cookie", "Authorization", "Tailscale-User-Login", "Tailscale-Headers-Info", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto",
		"Forwarded", "Origin", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Fyr-Actor", "Fyr-Origin", "Proxy-Authorization"} {
		h.Set(k, "x")
	}
	for _, k := range []string{"Content-Type", "X-Fyr", "X-Fyr-Filename", "Range", "If-None-Match", "Accept", "User-Agent"} {
		h.Set(k, "keep")
	}
	Strip(h)
	for k := range h {
		if h.Get(k) != "keep" {
			t.Errorf("%s survived", k)
		}
	}
	if len(h) != 7 {
		t.Errorf("Strip took something that is not a credential: %v", h)
	}
}
