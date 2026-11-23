package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/cli"
)

// loginSeen is what Settings reads of a login: its kind, its state and
// whether it has a link yet.
type loginSeen struct {
	Kind  string `json:"kind"`
	State string `json:"state"`
	URL   bool   `json:"url"`
}

// SPEC 0.5 V0.mock-contract (#457), V0.login-poll-one-host: the web mock
// once started a login already "waiting" with its link, so the page never had
// to follow a running login and a one-host Settings stayed on "Starting
// claude…". This test starts a folder account's login on a real registry
// with the fake CLI and writes what the page sees before, right after the
// reply and once the CLI printed its link to
// web/test/fixtures/provider-login.json; mock-contract.test.ts holds the
// mock's provider.login to it.
func TestProviderLoginFixture(t *testing.T) {
	lf := newLoginFix(t, "folder")
	gate := filepath.Join(t.TempDir(), "print")
	lf.r.LoginEnv = append(lf.r.LoginEnv, "FYR_FAKE_LOGIN_GATE="+gate)
	if err := lf.r.add(Provider{ID: "work", Name: "Claude (work)", CLI: "claude",
		Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "~/.claude-work"}, Models: []cli.Model{}}); err != nil {
		t.Fatal(err)
	}
	seen := func(l *LoginDTO) *loginSeen {
		if l == nil {
			return nil
		}
		return &loginSeen{Kind: l.Kind, State: l.State, URL: l.URL != ""}
	}
	before := seen(lf.dto("work").Login)
	if rep := lf.cmd("provider.login", map[string]any{"id": "work"}); !rep.OK {
		t.Fatalf("login: %+v", rep)
	}
	after := seen(lf.dto("work").Login)
	os.WriteFile(gate, nil, 0o600)
	link := seen(lf.wait(t, "work", "the link", func(l *LoginDTO) bool { return l.URL != "" }).Login)
	lf.cmd("provider.loginCancel", map[string]any{"id": "work"})
	b, err := json.MarshalIndent(map[string]any{"before": before, "after": after, "link": link}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("..", "..", "web", "test", "fixtures", "provider-login.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
