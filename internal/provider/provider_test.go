package provider

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/store"
)

const (
	routerKey = "sk-or-v1-ROUTERSECRET-9a8b"
	dsKey     = "sk-deepseek-FILESECRET-77aa"
	wrapTok   = "sk-ant-oat01-WRAPPERTOKEN-zzzz"
)

// fixture is a home with ~/.claude logged in, ~/.claude-norban without a
// login, two wrappers on a PATH dir and a key file.
type fixture struct {
	home, bin, keyFile string
	env                map[string]string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	f := fixture{home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"), keyFile: filepath.Join(root, "run", "ds.key"),
		env: map[string]string{"OPENROUTER_API_KEY": routerKey}}
	for _, d := range []string{".claude", ".claude-norban", ".claude-empty"} {
		os.MkdirAll(filepath.Join(f.home, d), 0o700)
	}
	os.WriteFile(filepath.Join(f.home, ".claude", ".credentials.json"), []byte(`{}`), 0o600)
	os.MkdirAll(f.bin, 0o755)
	os.MkdirAll(filepath.Dir(f.keyFile), 0o700)
	os.WriteFile(f.keyFile, []byte(dsKey+"\n"), 0o600)
	os.WriteFile(filepath.Join(f.bin, "claude-norban"), []byte("#!/usr/bin/env bash\nexport CLAUDE_CONFIG_DIR=\"$HOME/.claude-norban\"\nexport CLAUDE_CODE_OAUTH_TOKEN='"+wrapTok+"'\nexec claude \"$@\"\n"), 0o755)
	// As on the live host: a token and no folder, so ~/.claude (the main login) is not its login.
	os.WriteFile(filepath.Join(f.bin, "claude-tok"), []byte("#!/usr/bin/env bash\nexport CLAUDE_CODE_OAUTH_TOKEN="+wrapTok+"\nexec claude \"$@\"\n"), 0o755)
	os.WriteFile(filepath.Join(f.bin, "claude-deepseek"), []byte("#!/bin/sh\n# wrapper\nANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic ANTHROPIC_AUTH_TOKEN=\"$(cat "+f.keyFile+")\" ANTHROPIC_MODEL=deepseek-chat ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-chat exec claude \"$@\"\n"), 0o755)
	return f
}

func (f fixture) host() Host {
	return Host{Home: f.home, Getenv: func(k string) string { return f.env[k] },
		LookPath: func(b string) (string, error) {
			p := filepath.Join(f.bin, b)
			if _, err := os.Stat(p); err != nil {
				return "", exec.ErrNotFound
			}
			return p, nil
		},
		Profiles: []agent.Profile{
			{ID: "claudeAgent", Name: "Claude", Binary: "claude", Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"}},
			{ID: "claudeNorban", Name: "Claude (norban)", Binary: "claude-norban", Models: []string{"claude-opus-5-5"}},
			{ID: "claudeDeepseek", Name: "DeepSeek", Binary: "claude-deepseek", Models: []string{"deepseek-chat", "deepseek-reasoner"}},
			{ID: "claudeTok", Name: "Claude (tok)", Binary: "claude-tok"},
		}}
}

func byID(fs []Found) map[string]Found {
	m := map[string]Found{}
	for _, f := range fs {
		m[f.ID] = f
	}
	return m
}

// SPEC 0.5 V0.50 (3): detection on a fixture home reads, runs nothing and
// keeps no secret value.
func TestDetect(t *testing.T) {
	f := newFixture(t)
	fs := Detect(f.host())
	m := byID(fs)
	a, n, d, r := m["claudeAgent"], m["claudeNorban"], m["claudeDeepseek"], m["openrouter"]
	if a.Kind != KindAccount || a.ConfigDir != "" || a.Status != Ready || !a.LoggedIn {
		t.Fatalf("main: %+v", a)
	}
	if n.Kind != KindAccount || n.ConfigDir != "~/.claude-norban" || n.Status != NeedsLogin || !strings.Contains(n.Note, "which Fyr does not read") {
		t.Fatalf("norban: %+v", n)
	}
	if d.Kind != KindEndpoint || d.BaseURL != "https://api.deepseek.com/anthropic" || d.Status != Ready || d.KeyRef != "file:"+f.keyFile ||
		d.ModelEnv["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "deepseek-chat" || len(d.Models) != 2 || len(d.Models[0].Efforts) != 0 {
		t.Fatalf("deepseek: %+v", d)
	}
	if r.Kind != KindEndpoint || r.KeyRef != "env:OPENROUTER_API_KEY" || r.Status != Ready {
		t.Fatalf("openrouter: %+v", r)
	}
	if tk := m["claudeTok"]; tk.ConfigDir != "" || tk.Status != NeedsLogin || !tk.TokenLogin || tk.LoggedIn {
		t.Fatalf("token account must not count the main login: %+v", tk)
	}
	if _, ok := m["claude-empty"]; ok {
		t.Fatal("a folder without a login is not a find")
	}
	if _, ok := m["claude-norban"]; ok {
		t.Fatal("~/.claude-norban is the norban profile's, not a second find")
	}
	if len(a.Models[0].Efforts) == 0 {
		t.Fatal("claude models carry efforts")
	}
	b, _ := json.Marshal(fs)
	for _, s := range []string{routerKey, dsKey, wrapTok, "WRAPPERTOKEN"} {
		if strings.Contains(string(b), s) || strings.Contains(strings.Join(func() []string {
			var x []string
			for _, f := range fs {
				x = append(x, f.KeyRef, f.Note, f.KeyFrom)
			}
			return x
		}(), " "), s) {
			t.Fatalf("secret %q in finds", s)
		}
	}
}

func newReg(t *testing.T, f fixture) *Registry {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &Registry{DB: st, Secrets: secrets.Files(filepath.Join(t.TempDir(), "secrets")), Home: f.home,
		Getenv: func(k string) string { return f.env[k] }, Detect: func() []Found { return Detect(f.host()) }}
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

// (5) The first start imports today's profiles with their ids, copies no
// key, and runs once.
func TestCutoverKeepsIDs(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	n, err := r.Cutover(r.Found(), []string{"claudeAgent", "claudeNorban", "claudeDeepseek", "claudeTok"})
	if err != nil || n != 4 {
		t.Fatalf("%d %v", n, err)
	}
	var got []string
	for _, p := range r.Profiles() {
		got = append(got, p.ID)
		if p.Binary != "claude" {
			t.Fatalf("%s runs %s, not claude", p.ID, p.Binary)
		}
	}
	if !slices.Equal(got, []string{"claudeAgent", "claudeNorban", "claudeDeepseek", "claudeTok"}) {
		t.Fatal(got)
	}
	if r.Secrets.Has(SecretName("claudeDeepseek")) {
		t.Fatal("cut-over copied a key without a click")
	}
	st := map[string]string{}
	for _, d := range r.List() {
		st[d.ID] = d.Status
	}
	if st["claudeAgent"] != Ready || st["claudeNorban"] != NeedsLogin || st["claudeDeepseek"] != Ready || st["claudeTok"] != NeedsLogin {
		t.Fatal(st)
	}
	// The token account never starts as the main login: it needs its token.
	for _, p := range r.Profiles() {
		if p.ID == "claudeTok" && (!p.KeyRequired || p.ConfigDir != "") {
			t.Fatalf("%+v", p)
		}
	}
	if rep := r.Command(cmd("provider.setKey", map[string]any{"id": "claudeTok", "key": "sk-ant-oat01-pasted-123456"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	for _, d := range r.List() {
		if d.ID == "claudeTok" && (d.Status != Ready || d.KeyLast4 != "3456") {
			t.Fatalf("%+v", d)
		}
	}
	// Runs once: a second start (or after deleting everything) imports nothing.
	r2 := newReg(t, f)
	r2.DB = r.DB
	r2.Load()
	if n, _ := r2.Cutover(r2.Found(), []string{"claudeAgent"}); n != 0 {
		t.Fatal("cut-over ran twice")
	}
	// The deepseek profile starts plain claude with the key from the file.
	var ds agent.Profile
	for _, p := range r.Profiles() {
		if p.ID == "claudeDeepseek" {
			ds = p
		}
	}
	k, err := ds.Key()
	if err != nil || k != dsKey {
		t.Fatal("key ref")
	}
	env, _ := claudecli.Adapter{}.Env(cli.Launch{Auth: ds.Auth, Endpoint: ds.Endpoint, Key: k})
	if !slices.Contains(env, "ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic") || !slices.Contains(env, "ANTHROPIC_AUTH_TOKEN="+dsKey) {
		t.Fatal("deepseek env")
	}
}

func cmd(action string, args map[string]any) engine.Cmd {
	return engine.Cmd{CommandID: action + "-" + time.Now().Format("150405.000000000"), Action: action, Args: args}
}

// (1)(3) A key lives only in the secret store: not in the database, a DTO
// or a reply. Import on click copies a found key.
func TestKeysNeverLeave(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	rep := r.Command(cmd("provider.create", map[string]any{"name": "My Router", "kind": "endpoint", "baseURL": "https://openrouter.ai/api/",
		"key": routerKey, "models": []any{"anthropic/claude-opus", "z-ai/glm"}, "modelEnv": map[string]any{"ANTHROPIC_DEFAULT_HAIKU_MODEL": "z-ai/glm"}}))
	if !rep.OK {
		t.Fatal(rep.Error)
	}
	id := rep.Result.(map[string]string)["id"]
	if id != "my-router" {
		t.Fatal(id)
	}
	if !r.Secrets.Has(SecretName(id)) {
		t.Fatal("key not stored")
	}
	rows, _ := r.DB.Providers()
	list, _ := json.Marshal(r.List())
	repb, _ := json.Marshal(rep)
	for _, b := range append(rows, list, repb) {
		if strings.Contains(string(b), routerKey) || strings.Contains(string(b), "ROUTERSECRET") {
			t.Fatalf("key leaked: %s", b)
		}
	}
	d := r.List()[0]
	if !d.KeySet || d.KeyLast4 != "9a8b" || d.Status != Ready || d.Quota != quota.KindOpenRouter || d.BaseURL != "https://openrouter.ai/api" {
		t.Fatalf("%+v", d)
	}
	// Replace and clear the key.
	if rep := r.Command(cmd("provider.setKey", map[string]any{"id": id, "key": ""})); !rep.OK {
		t.Fatal(rep.Error)
	}
	if d := r.List()[0]; d.KeySet || d.Status != NeedsKey {
		t.Fatalf("%+v", d)
	}
	// Import the OpenRouter key from fyrd's environment on click.
	if rep := r.Command(cmd("provider.import", map[string]any{"foundId": "openrouter"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	if v, _ := r.Secrets.Get(SecretName("openrouter")); v != routerKey {
		t.Fatal("import did not copy the key")
	}
	if rep := r.Command(cmd("provider.import", map[string]any{"foundId": "openrouter"})); rep.OK || rep.Status != 409 {
		t.Fatal("double import")
	}
}

func TestUpdateDeleteAndQuotaIdentity(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	r.Cutover(r.Found(), []string{"claudeAgent", "claudeNorban"})
	r.Command(cmd("provider.create", map[string]any{"name": "Claude again", "kind": "claude-account", "configDir": f.home + "/.claude"}))
	qs := r.QuotaProfiles()
	if len(qs) != 3 || qs[0].Identity != qs[2].Identity || qs[0].Identity == qs[1].Identity {
		t.Fatalf("identities: %q %q %q", qs[0].Identity, qs[1].Identity, qs[2].Identity)
	}
	svc := quota.New(qs, f.home)
	if len(svc.Snapshot()) != 3 {
		t.Fatal("one status per profile")
	}
	if rep := r.Command(cmd("provider.update", map[string]any{"id": "claudeNorban", "models": []any{"claude-sonnet-5-5", "my-custom"}, "defaultEffort": "high"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	p, _ := r.get("claudeNorban")
	if len(p.Models) != 2 || p.DefaultModel != "claude-sonnet-5-5" || p.DefaultEffort != "high" || len(p.Models[1].Efforts) != 0 {
		t.Fatalf("%+v", p)
	}
	r.InUse = func(id string) int { return map[string]int{"claudeAgent": 2}[id] }
	if rep := r.Command(cmd("provider.delete", map[string]any{"id": "claudeAgent"})); rep.OK || rep.Status != 409 {
		t.Fatal("deleted a provider in use")
	}
	if rep := r.Command(cmd("provider.delete", map[string]any{"id": "claudeNorban"})); !rep.OK {
		t.Fatal(rep.Error)
	}
	if rep := r.Command(cmd("provider.create", map[string]any{"name": "x", "kind": "endpoint", "id": "../evil", "baseURL": "https://e"})); rep.OK {
		t.Fatal("bad id accepted")
	}
}

// SPEC 0.5 V0.50 (found live 14 Nov): an account that logs in with a token
// never shares the quota of the login folder it points at, and it is not
// offered for new threads or Hand over until its token is set.
func TestTokenLoginOwnIdentityAndUnavailable(t *testing.T) {
	f := newFixture(t)
	r := newReg(t, f)
	main := Provider{ID: "claudeAgent", Auth: cli.Auth{Kind: cli.CLILogin}}
	tok := Provider{ID: "claudeNorban", Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "~/.claude"}, TokenLogin: true}
	if main.identity(f.home) == tok.identity(f.home) {
		t.Fatalf("token account shares the folder's quota: %q", tok.identity(f.home))
	}
	if s := tok.quotaSpec(); !s.Token {
		t.Fatalf("token account reads the folder's login for quota: %+v", s)
	}
	r.Cutover(r.Found(), []string{"claudeAgent", "claudeNorban"})
	p, _ := r.get("claudeNorban")
	p.TokenLogin = true
	r.put(p, -1)
	for _, pr := range r.Profiles() {
		if pr.ID == "claudeNorban" && pr.Unavailable == "" {
			t.Fatal("a token account without its token is offered")
		}
		if pr.ID == "claudeAgent" && pr.Unavailable != "" {
			t.Fatalf("a ready account is unavailable: %q", pr.Unavailable)
		}
	}
}
