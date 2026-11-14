//go:build live

package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// SPEC 0.5 V0.53 (live): the real `claude` shows its sign-in link to Fyr for
// both kinds of login, in throwaway folders under a throwaway HOME. No code
// is used: a made-up code must come back as an error; both logins are cancelled. Run: go test -tags live -run LoginLive ./internal/provider/
func TestLoginLiveShowsLink(t *testing.T) {
	for _, mode := range []string{"token", "folder"} {
		t.Run(mode, func(t *testing.T) {
			lf := newLoginFix(t, "")
			home := t.TempDir()
			lf.r.Home = home
			lf.r.LoginBinary = ""
			lf.r.LoginEnv = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
			p := Provider{ID: "live", Name: "Live", CLI: "claude", Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: filepath.Join(home, "cfg")}, Models: []cli.Model{}}
			p.TokenLogin = mode == "token"
			if err := lf.r.add(p); err != nil {
				t.Fatal(err)
			}
			if rep := lf.cmd("provider.login", map[string]any{"id": "live"}); !rep.OK {
				t.Fatalf("%+v", rep)
			}
			d := lf.wait(t, "live", "the link", func(l *LoginDTO) bool { return l.State == LoginWaiting })
			if !strings.HasPrefix(d.Login.URL, "https://") || !strings.Contains(d.Login.URL, "oauth") {
				t.Fatalf("link %q", d.Login.URL)
			}
			t.Logf("%s link host: %s", mode, strings.SplitN(d.Login.URL, "?", 2)[0])
			first := d.Login.URL
			// A made-up code: the CLI must take it and say it is wrong.
			lf.cmd("provider.loginCode", map[string]any{"id": "live", "code": "not-a-real-code#not-a-real-state"})
			d = lf.wait(t, "live", "an answer to the code", func(l *LoginDTO) bool { return l.Message != "" })
			t.Logf("%s after a wrong code: %s %q", mode, d.Login.State, d.Login.Message)
			if mode == "token" {
				lf.wait(t, "live", "a new link", func(l *LoginDTO) bool { return l.URL != first })
			}
			time.Sleep(2 * time.Second)
			t.Logf("%s 2 s later: %s", mode, lf.dto("live").Login.State)
			lf.cmd("provider.loginCancel", map[string]any{"id": "live"})
			lf.wait(t, "live", "ended", func(l *LoginDTO) bool { return !l.Active() })
		})
	}
}
