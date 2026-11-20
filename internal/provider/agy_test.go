package provider

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/agycli"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/store"
)

func TestDetectAndImportAgy(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini", "antigravity-cli", "cache"), 0o700)
	os.WriteFile(filepath.Join(home, ".gemini", "antigravity-cli", "cache", "onboarding.json"), []byte(`{"onboardingComplete":true}`), 0o600)
	os.MkdirAll(filepath.Join(home, ".gemini-work", "antigravity-cli", "cache"), 0o700)
	os.WriteFile(filepath.Join(home, ".gemini-work", "antigravity-cli", "cache", "onboarding.json"), []byte(`{"onboardingComplete":true}`), 0o600)
	os.MkdirAll(filepath.Join(home, ".gemini-empty"), 0o700)

	h := Host{Home: home, Getenv: func(k string) string {
		if k == "GEMINI_API_KEY" {
			return "gk-env-secret-123456"
		}
		return ""
	}}
	fs := Detect(h)
	m := byID(fs)
	a := m["agy"]
	if a.Kind != KindAgyAccount || a.CLI != agycli.ID || a.Status != Ready || !a.LoggedIn || a.ConfigDir != "" || a.QuotaKind != quota.KindNone {
		t.Fatalf("main agy: %+v", a)
	}
	w := m["agy-work"]
	if w.Kind != KindAgyAccount || w.CLI != agycli.ID || w.ConfigDir != "~/.gemini-work" || !w.LoggedIn {
		t.Fatalf("work agy: %+v", w)
	}
	if _, ok := m["agy-empty"]; ok {
		t.Fatal("unauthenticated .gemini-empty must not be detected")
	}
	g := m["gemini"]
	if g.Kind != KindEndpoint || g.CLI != agycli.ID || g.KeyRef != "env:GEMINI_API_KEY" || g.Status != Ready {
		t.Fatalf("gemini api key: %+v", g)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &Registry{
		DB: st, Secrets: secrets.Files(filepath.Join(t.TempDir(), "secrets")), Home: home,
		Getenv: h.Getenv, Detect: func() []Found { return Detect(h) },
	}
	if rep := r.Run(cmd("provider.import", map[string]any{"foundId": "agy"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	if rep := r.Run(cmd("provider.create", map[string]any{
		"name": "Antigravity Lab", "kind": KindAgyAccount, "configDir": filepath.Join(home, ".gemini-lab"),
		"defaultEffort": "xhigh",
	})); !rep.OK {
		t.Fatal(rep.Error)
	}
	if rep := r.Run(cmd("provider.create", map[string]any{
		"name": "Gemini Key", "kind": KindEndpoint, "cli": agycli.ID, "key": "gk-test-00009999",
	})); !rep.OK {
		t.Fatal(rep.Error)
	}
	dtos := map[string]DTO{}
	for _, d := range r.List() {
		dtos[d.ID] = d
	}
	if d := dtos["agy"]; d.CLI != agycli.ID || d.Kind != KindAgyAccount || d.Status != Ready || !d.CanLogin || !slices.Equal(d.Efforts, agycli.Efforts) || d.Quota != quota.KindNone {
		t.Fatalf("agy dto: %+v", d)
	}
	if d := dtos["antigravity-lab"]; d.CLI != agycli.ID || d.Status != NeedsLogin || !strings.Contains(d.Need, "agy acp --login") || d.DefaultEffort != "xhigh" {
		t.Fatalf("lab dto: %+v", d)
	}
	if d := dtos["gemini-key"]; d.CLI != agycli.ID || d.Kind != KindEndpoint || d.Status != Ready || !d.KeySet || d.KeyLast4 != "9999" {
		t.Fatalf("gemini-key dto: %+v", d)
	}
	profs := map[string]Profile{}
	for _, p := range r.Profiles() {
		profs[p.ID] = p
	}
	if profs["agy"].Binary != "agy" || profs["agy"].CLI != agycli.ID {
		t.Fatalf("agy profile: %+v", profs["agy"])
	}
}

func TestAgyLoginFlow(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "agy")
	script := `#!/bin/sh
echo "Open this link in a browser to log in with Google:"
echo ""
echo "  https://accounts.google.com/o/oauth2/auth?client_id=test&state=xyz"
echo ""
echo "Then paste the authorization code, or the full URL of the page you land on, and press Enter:"
read code
if [ "$code" = "good-code" ]; then
  mkdir -p "$PWD/antigravity-cli/cache"
  echo '{"onboardingComplete":true}' > "$PWD/antigravity-cli/cache/onboarding.json"
  exit 0
fi
echo "Error: invalid authorization code"
exit 1
`
	os.WriteFile(bin, []byte(script), 0o755)
	st, err := store.Open(filepath.Join(t.TempDir(), "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &Registry{
		DB: st, Secrets: secrets.Files(filepath.Join(t.TempDir(), "secrets")), Home: home,
		LoginBinary: bin, LoginTimeout: 10 * time.Second,
	}
	if rep := r.Run(cmd("provider.create", map[string]any{"name": "Agy", "kind": KindAgyAccount, "configDir": "~/.gemini-login"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	if rep := r.Run(cmd("provider.login", map[string]any{"id": "agy"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if l := r.List()[0].Login; l != nil && l.State == LoginWaiting && strings.Contains(l.URL, "accounts.google.com") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if l := r.List()[0].Login; l == nil || l.State != LoginWaiting {
		t.Fatalf("login did not reach waiting: %+v", l)
	}
	if rep := r.Run(cmd("provider.loginCode", map[string]any{"id": "agy", "code": "good-code"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	for time.Now().Before(deadline) {
		d := r.List()[0]
		if d.Login != nil && d.Login.State == LoginDone && d.Status == Ready {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("login did not finish ready: %+v", r.List()[0])
}

func TestAgyIdentityDistinctFromClaude(t *testing.T) {
	home := "/tmp/h"
	c := Provider{ID: "claude", CLI: "claude", Auth: cli.Auth{Kind: cli.CLILogin}}
	a := Provider{ID: "agy", CLI: agycli.ID, Auth: cli.Auth{Kind: cli.CLILogin}}
	a2 := Provider{ID: "agy2", CLI: agycli.ID, Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "~/.gemini-work"}}
	if c.identity(home) == a.identity(home) {
		t.Fatalf("agy shares claude identity: %q", a.identity(home))
	}
	if a.identity(home) == a2.identity(home) {
		t.Fatalf("two agy config dirs share identity: %q", a.identity(home))
	}
	epClaude := Provider{ID: "ep1", CLI: "claude", Auth: cli.Auth{Kind: cli.APIKey, Secret: "ep1.key"}}
	epAgy := Provider{ID: "ep2", CLI: agycli.ID, Auth: cli.Auth{Kind: cli.APIKey, Secret: "ep1.key"}}
	if epClaude.identity(home) == epAgy.identity(home) {
		t.Fatalf("agy endpoint shares claude endpoint identity: %q", epAgy.identity(home))
	}
}
