package provider

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/secrets"
)

// Found is a login, endpoint or key found on this host (V0.50). Detection
// reads; it never changes anything, never runs a program and never keeps
// a secret value: a key is known by where it is (KeyRef), not by value.
type Found struct {
	ID        string            `json:"id"` // the provider id an import creates
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Source    string            `json:"source"` // where it was found, for the owner
	ConfigDir string            `json:"configDir,omitempty"`
	BaseURL   string            `json:"baseURL,omitempty"`
	ModelEnv  map[string]string `json:"modelEnv,omitempty"`
	Models    []cli.Model       `json:"models"`
	Default   string            `json:"defaultModel,omitempty"`
	Color     string            `json:"color,omitempty"`
	QuotaKind string            `json:"quotaKind,omitempty"`
	// KeyFrom says where the key is, without the key ("OPENROUTER_API_KEY
	// in fyrd's environment", "/run/secrets/x").
	KeyFrom  string `json:"keyFrom,omitempty"`
	KeyRef   string `json:"-"`
	LoggedIn bool   `json:"loggedIn,omitempty"`
	// TokenLogin: the account logs in with a token (CLAUDE_CODE_OAUTH_TOKEN),
	// not with its folder's login, which may be another account's.
	TokenLogin bool `json:"tokenLogin,omitempty"`
	// Status is what the provider would be after the import.
	Status   string `json:"status"`
	Note     string `json:"note,omitempty"`
	Imported bool   `json:"imported"`
}

// Host is what detection looks at.
type Host struct {
	Home string
	// Profiles: what fyrd runs today (the config's profiles or the ones
	// seeded from T3's settings), with their ids, binaries, env and quota.
	Profiles []agent.Profile
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Adapter  cli.Adapter
}

const (
	deepseekAnthropic   = "https://api.deepseek.com/anthropic"
	openrouterAnthropic = "https://openrouter.ai/api"
)

// knownKeys are endpoint keys detection looks for in fyrd's environment.
var knownKeys = []struct{ env, id, name, url, quota string }{
	{"OPENROUTER_API_KEY", "openrouter", "OpenRouter", openrouterAnthropic, quota.KindOpenRouter},
	{"DEEPSEEK_API_KEY", "deepseek", "DeepSeek", deepseekAnthropic, quota.KindDeepSeek},
}

// Detect lists what can be imported, the profiles of today first.
func Detect(h Host) []Found {
	if h.Getenv == nil {
		h.Getenv = os.Getenv
	}
	if h.Adapter == nil {
		h.Adapter = claudecli.Adapter{}
	}
	var out []Found
	usedDirs := map[string]bool{}
	usedURL := map[string]bool{}
	for _, p := range h.Profiles {
		f := fromProfile(h, p)
		if f.Kind == KindAccount {
			usedDirs[h.dir(f.ConfigDir)] = true
		} else {
			usedURL[f.BaseURL] = true
		}
		out = append(out, f)
	}
	// Claude logins: ~/.claude and ~/.claude-* folders.
	dirs := []string{filepath.Join(h.Home, ".claude")}
	if m, _ := filepath.Glob(filepath.Join(h.Home, ".claude-*")); len(m) > 0 {
		sort.Strings(m)
		dirs = append(dirs, m...)
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() || usedDirs[d] {
			continue
		}
		base := filepath.Base(d)
		f := Found{Kind: KindAccount, Source: "~/" + base, Models: modelsFor(ids(claudecli.Models), false)}
		if base == ".claude" {
			f.ID, f.Name = "claude", "Claude"
		} else {
			suffix := strings.TrimPrefix(base, ".claude-")
			f.ID, f.Name, f.ConfigDir = "claude-"+slug(suffix), "Claude ("+suffix+")", "~/"+base
		}
		f.LoggedIn = h.Adapter.LoggedIn(cli.Auth{Kind: cli.CLILogin, ConfigDir: h.dir(f.ConfigDir)})
		if !f.LoggedIn {
			continue // a folder without a login is nothing to import
		}
		f.Status, f.QuotaKind = Ready, quota.KindAnthropic
		f.Default = f.Models[0].ID
		out = append(out, f)
	}
	// Keys in fyrd's environment, by name.
	for _, k := range knownKeys {
		if h.Getenv(k.env) == "" || usedURL[k.url] {
			continue
		}
		out = append(out, Found{ID: k.id, Name: k.name, Kind: KindEndpoint, Source: k.env + " in fyrd's environment",
			BaseURL: k.url, KeyFrom: k.env + " in fyrd's environment", KeyRef: "env:" + k.env, Status: Ready,
			QuotaKind: k.quota, Models: []cli.Model{}, Note: "Add the model ids this endpoint should offer."})
	}
	return out
}

func ids(ms []cli.Model) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func (h Host) dir(d string) string {
	if d == "" {
		return filepath.Join(h.Home, ".claude")
	}
	if d == "~" || strings.HasPrefix(d, "~/") {
		return filepath.Join(h.Home, d[1:])
	}
	return filepath.Clean(d)
}

func slug(s string) string {
	s = regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(s, "-")
	if s == "" || !ValidID("x"+s) {
		return "x"
	}
	return s
}

// fromProfile turns one of today's profiles into a find with the same id,
// so threads keep their profile.
func fromProfile(h Host, p agent.Profile) Found {
	env := parseEnv(p.Env)
	src := "profile " + p.ID
	var note []string
	if p.Binary != "" && p.Binary != "claude" {
		src = "profile " + p.ID + " (" + p.Binary + ")"
		if w, ok := readWrapper(h, p.Binary); ok {
			env = mergeEnv(w, env)
		} else {
			note = append(note, "Could not read "+p.Binary+"; check the settings.")
		}
	}
	var q quota.Spec
	if p.Quota != nil {
		q = *p.Quota
	}
	f := Found{ID: p.ID, Name: p.Name, Color: p.Color, Source: src, QuotaKind: q.Kind, Default: p.DefaultModel}
	f.ConfigDir = firstNonEmpty(p.ConfigDir, env.vals["CLAUDE_CONFIG_DIR"])
	if f.ConfigDir == "" && q.Kind == quota.KindAnthropic {
		f.ConfigDir = q.ConfigDir
	}
	f.ConfigDir = tildeHome(f.ConfigDir, h.Home)
	f.BaseURL = env.vals["ANTHROPIC_BASE_URL"]
	low := strings.ToLower(p.ID + " " + p.Name + " " + p.Binary)
	if f.BaseURL == "" {
		switch {
		case q.Kind == quota.KindDeepSeek || (q.Kind == "" && strings.Contains(low, "deepseek")):
			f.BaseURL = deepseekAnthropic
			note = append(note, "Endpoint assumed for DeepSeek; check it.")
		case q.Kind == quota.KindOpenRouter || (q.Kind == "" && strings.Contains(low, "openrouter")):
			f.BaseURL = openrouterAnthropic
			note = append(note, "Endpoint assumed for OpenRouter; check it.")
		}
	}
	if f.BaseURL == "" {
		f.Kind = KindAccount
		f.Models = modelsFor(p.Models, false)
		if len(f.Models) == 0 {
			f.Models = modelsFor(ids(claudecli.Models), false)
		}
		f.LoggedIn = h.Adapter.LoggedIn(cli.Auth{Kind: cli.CLILogin, ConfigDir: h.dir(f.ConfigDir)})
		if tok := env.secrets["CLAUDE_CODE_OAUTH_TOKEN"]; tok != "" {
			// The wrapper logs in with a token: the folder's login (often
			// ~/.claude, the main account) is not this account's.
			f.TokenLogin, f.LoggedIn = true, false
			if ref, desc, ok := keyRef(h, tok); ok {
				f.KeyRef, f.KeyFrom = ref, desc
			}
		}
		switch {
		case f.LoggedIn || f.KeyRef != "":
			f.Status = Ready
		default:
			f.Status = NeedsLogin
			if env.secrets["CLAUDE_CODE_OAUTH_TOKEN"] == "literal" {
				note = append(note, "Its login token is inside "+p.Binary+", which Fyr does not read: paste a token from `claude setup-token` (logged in to this account) as its key.")
			}
		}
	} else {
		f.Kind = KindEndpoint
		f.Models = modelsFor(append(append([]string{}, p.Models...), env.vals["ANTHROPIC_MODEL"]), true)
		f.ModelEnv = map[string]string{}
		for k, v := range env.vals {
			if strings.HasSuffix(k, "_MODEL") {
				f.ModelEnv[k] = v
			}
		}
		if len(f.ModelEnv) == 0 {
			f.ModelEnv = nil
		}
		cands := []string{env.secrets["ANTHROPIC_AUTH_TOKEN"], env.secrets["ANTHROPIC_API_KEY"]}
		if q.KeyFile != "" {
			cands = append(cands, "file:"+tildeHome(q.KeyFile, h.Home))
		}
		if q.KeyEnv != "" {
			cands = append(cands, "env:"+q.KeyEnv)
		}
		for _, k := range knownKeys {
			if k.url == f.BaseURL {
				cands = append(cands, "env:"+k.env)
			}
		}
		for _, c := range cands {
			if ref, desc, ok := keyRef(h, c); ok {
				f.KeyRef, f.KeyFrom = ref, desc
				break
			}
		}
		f.Status = Ready
		if f.KeyRef == "" {
			f.Status = NeedsKey
			if env.secrets["ANTHROPIC_AUTH_TOKEN"] == "literal" || env.secrets["ANTHROPIC_API_KEY"] == "literal" {
				note = append(note, "Its key is inside the wrapper, which Fyr does not read.")
			}
		}
	}
	if f.Default == "" && len(f.Models) > 0 {
		f.Default = f.Models[0].ID
	}
	f.Note = strings.Join(note, " ")
	return f
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// tildeHome writes a path under home as ~/…; ~/.claude is the default "".
func tildeHome(p, home string) string {
	p = strings.ReplaceAll(strings.ReplaceAll(p, "${HOME}", home), "$HOME", home)
	if home != "" && strings.HasPrefix(p, home+"/") {
		p = "~" + p[len(home):]
	}
	if p == "~/.claude" || p == "~/.claude/" {
		return ""
	}
	return p
}

// keyRef checks a secret reference without reading a file.
func keyRef(h Host, ref string) (string, string, bool) {
	if ref == "" || ref == "literal" || !secrets.RefExists(ref, h.Getenv) {
		return "", "", false
	}
	if strings.HasPrefix(ref, "env:") {
		return ref, ref[4:] + " in fyrd's environment", true
	}
	return ref, strings.TrimPrefix(ref, "file:"), true
}

// envInfo is what a profile's env or wrapper sets: non-secret values by
// name, and for secrets only where they come from: "env:NAME",
// "file:/path", or "literal" (the value is in the text; Fyr drops it).
type envInfo struct {
	vals    map[string]string
	secrets map[string]string
}

var nonSecret = regexp.MustCompile(`^(CLAUDE_CONFIG_DIR|ANTHROPIC_BASE_URL|ANTHROPIC_MODEL|ANTHROPIC_SMALL_FAST_MODEL|ANTHROPIC_DEFAULT_[A-Z]+_MODEL|CLAUDE_CODE_SUBAGENT_MODEL)$`)

func newEnvInfo() envInfo { return envInfo{vals: map[string]string{}, secrets: map[string]string{}} }

func (e envInfo) set(k, v string) {
	switch {
	case nonSecret.MatchString(k):
		if !strings.Contains(v, "$(") && !strings.Contains(v, "`") {
			e.vals[k] = v
		}
	case secretName(k):
		e.secrets[k] = secretSource(v)
	}
}

var (
	varRef  = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)
	fileRef = regexp.MustCompile(`^\$\(\s*(?:cat\s+|<\s*)["']?(/[^"'\s)]+)["']?\s*\)$`)
)

func secretSource(v string) string {
	if m := varRef.FindStringSubmatch(v); m != nil {
		return "env:" + m[1]
	}
	if m := fileRef.FindStringSubmatch(v); m != nil {
		return "file:" + m[1]
	}
	if v == "" {
		return ""
	}
	return "literal"
}

func parseEnv(kvs []string) envInfo {
	e := newEnvInfo()
	for _, kv := range kvs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			e.set(k, v)
		}
	}
	return e
}

func mergeEnv(base, over envInfo) envInfo {
	for k, v := range over.vals {
		base.vals[k] = v
	}
	for k, v := range over.secrets {
		base.secrets[k] = v
	}
	return base
}

var (
	assignRE = regexp.MustCompile(`(?:^|[\s;])(?:export\s+)?([A-Z_][A-Z0-9_]*)=("(?:[^"\\]|\\.)*"|'[^']*'|\$\([^)]*\)|[^\s;]*)`)
	setRE    = regexp.MustCompile(`--set(?:-default)?\s+([A-Z_][A-Z0-9_]*)\s+("(?:[^"\\]|\\.)*"|'[^']*'|\S+)`)
	defRE    = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*:?-(.*)\}$`)
)

// readWrapper reads (never runs) a wrapper script on PATH and returns the
// variables it sets. A binary or a large file is skipped.
func readWrapper(h Host, bin string) (envInfo, bool) {
	look := h.LookPath
	if look == nil {
		return envInfo{}, false
	}
	path, err := look(bin)
	if err != nil {
		return envInfo{}, false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 256<<10 {
		return envInfo{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return envInfo{}, false
	}
	defer f.Close()
	e := newEnvInfo()
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if !strings.HasPrefix(line, "#!") {
				return envInfo{}, false
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, re := range []*regexp.Regexp{assignRE, setRE} {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				e.set(m[1], unquote(m[2]))
			}
		}
	}
	return e, true
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	}
	if m := defRE.FindStringSubmatch(v); m != nil {
		v = unquote(m[1])
	}
	return v
}
