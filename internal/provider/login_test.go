package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/cli"
)

// The fake CLI is this test binary run again with FYR_FAKE_LOGIN set: it
// prints a sign-in link like `claude setup-token` / `claude auth login`,
// waits for a code and then prints a token (setup-token) or writes a login
// into its config folder (auth login).
const (
	fakeToken = "sk-ant-oat01-FAKELOGINTOKEN0123456789abcdefghijklmn_opq-rstuvwxyzAA"
	goodCode  = "good-code#state123"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("FYR_FAKE_LOGIN"); mode != "" {
		fakeLogin(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeLogin(mode string) {
	if f := os.Getenv("FYR_FAKE_LOG"); f != "" {
		_, tok := os.LookupEnv("CLAUDE_CODE_OAUTH_TOKEN")
		wd, _ := os.Getwd()
		os.WriteFile(f, []byte(fmt.Sprintf("args=%s\nconfig=%s\ncwd=%s\noauthEnv=%v\nbrowser=%s\n",
			strings.Join(os.Args[1:], " "), os.Getenv("CLAUDE_CONFIG_DIR"), wd, tok, os.Getenv("BROWSER"))), 0o600)
	}
	link := "https://claude.com/cai/oauth/authorize?code=true&client_id=fake&state=abc"
	if mode == "token" || mode == "hang" {
		// As setup-token draws it: an OSC 8 link whose visible text wraps.
		fmt.Print("\x1b[2J Welcome\x1b[1Cto\x1b[1CClaude\x1b[1CCode\r\n\r\n Browser didn't open? Use the url below to sign in\r\n\r\n")
		fmt.Printf("\x1b]8;id=x1;%s\x07%s\x1b]8;;\x07\r\n", link, link[:30])
		fmt.Printf("\x1b]8;id=x1;%s\x07%s\x1b]8;;\x07\r\n\r\n", link, link[30:])
	} else {
		fmt.Printf("Opening browser to sign in…\nIf the browser didn't open, visit: %s\n", link)
	}
	if mode == "hang" {
		time.Sleep(time.Minute)
		return
	}
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Paste code here if prompted > ")
		if !in.Scan() {
			os.Exit(1)
		}
		if strings.TrimSpace(in.Text()) != goodCode {
			if mode != "token" {
				fmt.Print("\nInvalid code. Please make sure the full code was copied.\n")
				continue
			}
			// As setup-token does: an error, then Enter starts over with a new link.
			fmt.Print("\x1b[2K\x1b[1A\r\x1b[1C\x1b[38;5;211mOAuth error: That code wasn't accepted.\x1b[39m\r\x1b[1B Press \x1b[1mEnter\x1b[22m to retry.\r\n")
			if !in.Scan() || in.Text() != "" {
				os.Exit(2)
			}
			link = strings.Replace(link, "state=abc", "state=def", 1)
			fmt.Printf("\x1b[38;5;246m%s\x1b[39m\r\n", link)
			continue
		}
		break
	}
	if mode == "token" {
		fmt.Print("\r\n\x1b[32m✓\x1b[39m Long-lived authentication token created successfully!\r\n\r\nYour OAuth token (valid for 1 year):\r\n\r\n" + fakeToken + "\r\n\r\nStore this token securely.\r\n")
		time.Sleep(time.Minute) // like the real screen, it waits for a key
		return
	}
	os.WriteFile(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".credentials.json"), []byte(`{"fake":true}`), 0o600)
	fmt.Println("Login successful.")
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type loginFix struct {
	r       *Registry
	f       fixture
	logs    *syncBuf
	replies []string
	fakeLog string
	n       int
	changes int
	mu      sync.Mutex
}

func newLoginFix(t *testing.T, mode string) *loginFix {
	t.Helper()
	f := newFixture(t)
	r := newReg(t, f)
	lf := &loginFix{r: r, f: f, logs: &syncBuf{}, fakeLog: filepath.Join(t.TempDir(), "fake.log")}
	r.LoginBinary = os.Args[0]
	r.LoginTemp = t.TempDir()
	// The daemon's own credentials must not reach the login.
	r.LoginEnv = append(os.Environ(), "FYR_FAKE_LOGIN="+mode, "FYR_FAKE_LOG="+lf.fakeLog, "CLAUDE_CODE_OAUTH_TOKEN=daemon-token", "CLAUDE_CONFIG_DIR="+filepath.Join(f.home, ".claude"))
	r.OnChange = func() { lf.mu.Lock(); lf.changes++; lf.mu.Unlock() }
	prev := log.Writer()
	log.SetOutput(lf.logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	return lf
}

func (lf *loginFix) cmd(action string, args map[string]any) api.Reply {
	lf.n++
	rep := lf.r.Run(api.Cmd{CommandID: fmt.Sprintf("c%d", lf.n), Action: action, Args: args})
	b, _ := json.Marshal(rep)
	lf.replies = append(lf.replies, string(b))
	return rep
}

func (lf *loginFix) dto(id string) DTO {
	for _, d := range lf.r.List() {
		if d.ID == id {
			b, _ := json.Marshal(d)
			lf.replies = append(lf.replies, string(b))
			return d
		}
	}
	return DTO{}
}

func (lf *loginFix) wait(t *testing.T, id string, what string, ok func(*LoginDTO) bool) DTO {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		d := lf.dto(id)
		if d.Login != nil && ok(d.Login) {
			return d
		}
		time.Sleep(20 * time.Millisecond)
	}
	d := lf.dto(id)
	t.Fatalf("login for %s never got %s: %+v", id, what, d.Login)
	return d
}

// noToken: the token is in no log line, reply, DTO or error.
func (lf *loginFix) noToken(t *testing.T) {
	t.Helper()
	all := lf.logs.String() + strings.Join(lf.replies, "\n")
	for _, s := range []string{fakeToken, fakeToken[13:30], goodCode} {
		if strings.Contains(all, s) {
			t.Fatalf("%q leaked into logs or replies", s)
		}
	}
}

func (lf *loginFix) fake(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(lf.fakeLog)
	if err != nil {
		t.Fatalf("the fake CLI did not run: %v", err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = v
		}
	}
	return m
}

// SPEC 0.5 V0.53: a token account logs in through Fyr. setup-token runs on
// a terminal in a throwaway folder, the page gets the link, a wrong code
// can be retried, the token lands in the secret store only, and the
// provider turns Ready.
func TestLoginTokenAccount(t *testing.T) {
	lf := newLoginFix(t, "token")
	r := lf.r
	if err := r.add(Provider{ID: "claudeAcme", Name: "Claude (acme)", CLI: "claude", TokenLogin: true,
		Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "~/.claude"}, Models: []cli.Model{}}); err != nil {
		t.Fatal(err)
	}
	if d := lf.dto("claudeAcme"); d.Status != NeedsLogin || !d.CanLogin || d.Login != nil {
		t.Fatalf("before: %+v", d)
	}
	if rep := lf.cmd("provider.login", map[string]any{"id": "claudeAcme"}); !rep.OK {
		t.Fatalf("login: %+v", rep)
	}
	if rep := lf.cmd("provider.login", map[string]any{"id": "claudeAcme"}); rep.OK || rep.Status != 409 {
		t.Fatalf("a second login for the same provider: %+v", rep)
	}
	d := lf.wait(t, "claudeAcme", "the link", func(l *LoginDTO) bool { return l.State == LoginWaiting && l.URL != "" })
	if d.Login.URL != "https://claude.com/cai/oauth/authorize?code=true&client_id=fake&state=abc" || d.Login.Kind != "token" {
		t.Fatalf("link: %+v", d.Login)
	}
	if rep := lf.cmd("provider.loginCode", map[string]any{"id": "claudeAcme", "code": "wrong#code"}); !rep.OK {
		t.Fatalf("code: %+v", rep)
	}
	// setup-token starts over on Enter; Fyr presses it and shows the new link.
	d = lf.wait(t, "claudeAcme", "the new link", func(l *LoginDTO) bool { return l.State == LoginWaiting && strings.HasSuffix(l.URL, "state=def") })
	if !strings.Contains(d.Login.Message, "wasn't accepted") || !strings.Contains(d.Login.Message, "new link is ready") {
		t.Fatalf("message after a wrong code: %q", d.Login.Message)
	}
	if rep := lf.cmd("provider.loginCode", map[string]any{"id": "claudeAcme", "code": " " + goodCode + " "}); !rep.OK {
		t.Fatalf("code: %+v", rep)
	}
	start := time.Now()
	d = lf.wait(t, "claudeAcme", "done", func(l *LoginDTO) bool { return l.State == LoginDone })
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the login waited for the CLI to end after the token (%s)", time.Since(start))
	}
	if d.Status != Ready || !d.KeySet || d.KeyLast4 != fakeToken[len(fakeToken)-4:] || d.Need != "" {
		t.Fatalf("after: %+v", d)
	}
	if v, err := r.Secrets.Get(SecretName("claudeAcme")); err != nil || v != fakeToken {
		t.Fatalf("secret store: %v", err)
	}
	for _, pr := range r.Profiles() {
		if pr.ID == "claudeAcme" && pr.Unavailable != "" {
			t.Fatalf("still unavailable: %q", pr.Unavailable)
		}
	}
	fk := lf.fake(t)
	if fk["args"] != "setup-token" || fk["oauthEnv"] != "false" || fk["browser"] != "true" ||
		!strings.HasPrefix(fk["config"], r.LoginTemp) || fk["config"] == filepath.Join(lf.f.home, ".claude") {
		t.Fatalf("the CLI ran wrong: %+v", fk)
	}
	// The throwaway folder is gone, and the account's folder was not touched.
	end := time.Now().Add(5 * time.Second)
	for {
		es, _ := os.ReadDir(r.LoginTemp)
		if len(es) == 0 {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("throwaway folder left: %v", es)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b, _ := os.ReadFile(filepath.Join(lf.f.home, ".claude", ".credentials.json")); string(b) != "{}" {
		t.Fatalf("the main login changed: %q", b)
	}
	lf.mu.Lock()
	changes := lf.changes
	lf.mu.Unlock()
	if changes == 0 {
		t.Fatal("clients were not told")
	}
	lf.noToken(t)
}

// SPEC 0.5 V0.53: a folder account logs in with `claude auth login` in its
// own config folder; the CLI keeps the login there.
func TestLoginFolderAccount(t *testing.T) {
	lf := newLoginFix(t, "folder")
	r := lf.r
	if err := r.add(Provider{ID: "work", Name: "Claude (work)", CLI: "claude",
		Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "~/.claude-work"}, Models: []cli.Model{}}); err != nil {
		t.Fatal(err)
	}
	if d := lf.dto("work"); d.Status != NeedsLogin || !strings.Contains(d.Need, "Log in") {
		t.Fatalf("before: %+v", d)
	}
	if rep := lf.cmd("provider.login", map[string]any{"id": "work"}); !rep.OK {
		t.Fatalf("login: %+v", rep)
	}
	d := lf.wait(t, "work", "the link", func(l *LoginDTO) bool { return l.State == LoginWaiting && l.URL != "" })
	if d.Login.Kind != "folder" || !strings.HasPrefix(d.Login.URL, "https://claude.com/cai/oauth/authorize") {
		t.Fatalf("link: %+v", d.Login)
	}
	if rep := lf.cmd("provider.loginCode", map[string]any{"id": "work", "code": "wrong"}); !rep.OK {
		t.Fatalf("code: %+v", rep)
	}
	lf.wait(t, "work", "the invalid-code line", func(l *LoginDTO) bool { return l.State == LoginWaiting && strings.Contains(l.Message, "Invalid code") })
	if rep := lf.cmd("provider.loginCode", map[string]any{"id": "work", "code": goodCode}); !rep.OK {
		t.Fatalf("code: %+v", rep)
	}
	d = lf.wait(t, "work", "done", func(l *LoginDTO) bool { return l.State == LoginDone })
	dir := filepath.Join(lf.f.home, ".claude-work")
	if d.Status != Ready || d.KeySet {
		t.Fatalf("after: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(dir, ".credentials.json")); err != nil {
		t.Fatal("no login in the account's folder")
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", st.Mode().Perm())
	}
	fk := lf.fake(t)
	if fk["args"] != "auth login --claudeai" || fk["config"] != dir || fk["oauthEnv"] != "false" {
		t.Fatalf("the CLI ran wrong: %+v", fk)
	}
	lf.noToken(t)
}

// SPEC 0.5 V0.53: a login times out, can be cancelled, and only a Claude
// account has one.
func TestLoginTimeoutCancel(t *testing.T) {
	lf := newLoginFix(t, "hang")
	r := lf.r
	r.LoginTimeout = 400 * time.Millisecond
	r.add(Provider{ID: "tok", Name: "Tok", CLI: "claude", TokenLogin: true, Auth: cli.Auth{Kind: cli.CLILogin}, Models: []cli.Model{}})
	r.add(Provider{ID: "ds", Name: "DeepSeek", CLI: "claude", Auth: cli.Auth{Kind: cli.APIKey, Secret: "ds.key"}, Endpoint: cli.Endpoint{BaseURL: "https://api.deepseek.com"}, Models: []cli.Model{}})
	if rep := lf.cmd("provider.login", map[string]any{"id": "ds"}); rep.OK {
		t.Fatal("an endpoint got a login")
	}
	if rep := lf.cmd("provider.loginCode", map[string]any{"id": "tok", "code": "x"}); rep.OK {
		t.Fatal("a code without a login")
	}
	lf.cmd("provider.login", map[string]any{"id": "tok"})
	d := lf.wait(t, "tok", "timeout", func(l *LoginDTO) bool { return l.State == LoginTimedOut })
	if !strings.Contains(d.Login.Message, "timed out") || d.Status != NeedsLogin {
		t.Fatalf("timeout: %+v", d)
	}
	r.LoginTimeout = time.Minute
	if rep := lf.cmd("provider.login", map[string]any{"id": "tok"}); !rep.OK {
		t.Fatalf("again after a timeout: %+v", rep)
	}
	lf.wait(t, "tok", "the link", func(l *LoginDTO) bool { return l.State == LoginWaiting })
	if rep := lf.cmd("provider.loginCancel", map[string]any{"id": "tok"}); !rep.OK {
		t.Fatalf("cancel: %+v", rep)
	}
	start := time.Now()
	lf.wait(t, "tok", "cancelled", func(l *LoginDTO) bool { return l.State == LoginCancelled })
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancel did not stop the CLI")
	}
	if d := lf.dto("tok"); d.Status != NeedsLogin || d.KeySet {
		t.Fatalf("after cancel: %+v", d)
	}
}
