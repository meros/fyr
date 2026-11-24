// Package provider keeps the host's providers (SPEC 0.5 V0.50, decision
// 0015): a provider is a CLI adapter + an auth + an endpoint + the models it
// offers. Fyr runs every provider through its adapter (the plain `claude`
// binary today), never through a wrapper. Secrets live in package secrets;
// a provider only names one.
package provider

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/agycli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/quota"
)

// The kinds the UI shows.
const (
	KindAccount    = "claude-account" // a Claude cli-login
	KindAgyAccount = "agy-account"    // an Antigravity (agy) cli-login
	KindEndpoint   = "endpoint"       // an api-key with an endpoint
)

// AdapterFor returns the built-in CLI adapter for cliID ("" is claude).
func AdapterFor(cliID string) cli.Adapter {
	if cliID == agycli.ID {
		return agycli.Adapter{}
	}
	return claudecli.Adapter{}
}

func defaultBinary(cliID string) string {
	if d, ok := AdapterFor(cliID).(cli.Defaults); ok {
		return d.DefaultBinary()
	}
	return "claude"
}

func defaultConfigDir(cliID string) string {
	if d, ok := AdapterFor(cliID).(cli.Defaults); ok {
		return d.DefaultConfigDir()
	}
	return "~/.claude"
}

// Statuses.
const (
	Ready      = "ready"
	NeedsLogin = "needs-login"
	NeedsKey   = "needs-key"
)

// Provider is what the store keeps. It never holds a secret value.
type Provider struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Color    string       `json:"color,omitempty"`
	CLI      string       `json:"cli"`
	Auth     cli.Auth     `json:"auth"`
	Endpoint cli.Endpoint `json:"endpoint"`
	// KeyRef: where an auto-imported key is read ("env:NAME" or
	// "file:/path") until the owner stores one.
	KeyRef        string      `json:"keyRef,omitempty"`
	Models        []cli.Model `json:"models"`
	DefaultModel  string      `json:"defaultModel,omitempty"`
	DefaultEffort string      `json:"defaultEffort,omitempty"`
	// TokenLogin: a cli-login that logs in with its stored token only; its
	// folder's login (maybe another account's) does not count, and it does
	// not start without the token.
	TokenLogin bool `json:"tokenLogin,omitempty"`
	// Projects limits the provider to these project ids on this host; empty
	// offers it to every project.
	Projects []string `json:"projects,omitempty"`
	// QuotaKind: "" picks by auth and endpoint (V0.44 kinds).
	QuotaKind string `json:"quotaKind,omitempty"`
	// Imported: where an imported provider came from ("" when made here).
	Imported  string    `json:"imported,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// V0.120: what the provider reports (Models is the part that is on),
	// when a check last succeeded, why the last one failed ("" when it did
	// not), and when one last ran (the daily check's clock).
	Catalog    []CatalogModel `json:"catalog,omitempty"`
	CheckedAt  time.Time      `json:"checkedAt,omitzero"`
	CheckError string         `json:"checkError,omitempty"`
	// CheckNote: the last successful check is only Fyr's known list (the
	// CLI named its aliases only); "" when the provider gave its own list.
	CheckNote string    `json:"checkNote,omitempty"`
	TriedAt   time.Time `json:"triedAt,omitzero"`
}

// Kind is the UI's word for the auth and CLI.
func (p Provider) Kind() string {
	if p.Auth.Kind == cli.APIKey {
		return KindEndpoint
	}
	if p.CLI == agycli.ID {
		return KindAgyAccount
	}
	return KindAccount
}

func (p Provider) binary() string { return defaultBinary(p.CLI) }

// SecretName is the provider's name in the secret store.
func SecretName(id string) string { return id + ".key" }

var idRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,47}$`)

// ValidID: ids name secrets and appear in URLs.
func ValidID(id string) bool { return idRE.MatchString(id) }

var envNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func (p Provider) validate() error {
	if !ValidID(p.ID) {
		return fmt.Errorf("bad provider id %q", p.ID)
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("a provider needs a name")
	}
	switch p.Auth.Kind {
	case cli.CLILogin, cli.NoAuth:
	case cli.APIKey:
		if p.Endpoint.BaseURL != "" && !strings.HasPrefix(p.Endpoint.BaseURL, "https://") && !strings.HasPrefix(p.Endpoint.BaseURL, "http://") {
			return fmt.Errorf("the endpoint must be an http(s) URL")
		}
	default:
		return fmt.Errorf("unknown auth %q", p.Auth.Kind)
	}
	for k := range p.Endpoint.ModelEnv {
		if !envNameRE.MatchString(k) || secretName(k) {
			return fmt.Errorf("bad model variable %q", k)
		}
	}
	switch p.QuotaKind {
	case "", quota.KindAnthropic, quota.KindDeepSeek, quota.KindOpenRouter, quota.KindNone:
	default:
		return fmt.Errorf("unknown quota kind %q", p.QuotaKind)
	}
	return nil
}

// quotaSpec is where the provider's allowance is read (V0.44).
func (p Provider) quotaSpec() *quota.Spec {
	kind := p.QuotaKind
	if kind == "" {
		switch {
		case p.Auth.Kind == cli.CLILogin && (p.CLI == "" || p.CLI == claudecli.ID):
			kind = quota.KindAnthropic
		case strings.Contains(p.Endpoint.BaseURL, "openrouter.ai"):
			kind = quota.KindOpenRouter
		case strings.Contains(p.Endpoint.BaseURL, "deepseek.com"):
			kind = quota.KindDeepSeek
		default:
			kind = quota.KindNone
		}
	}
	s := &quota.Spec{Kind: kind}
	if kind == quota.KindAnthropic {
		s.ConfigDir = p.Auth.ConfigDir
		s.Token = p.TokenLogin // V0.50: its own token, never the folder's login
	}
	return s
}

// identity: two providers on one account share one quota entry.
func (p Provider) identity(home string) string {
	if p.TokenLogin {
		// The token is the account (V0.50); the folder may be another account's.
		return "token:" + p.ID
	}
	if p.Auth.Kind == cli.CLILogin {
		dir := p.Auth.ConfigDir
		if dir == "" {
			dir = defaultConfigDir(p.CLI)
		}
		if p.CLI != "" && p.CLI != claudecli.ID {
			return p.CLI + ":" + filepath.Clean(hostfs.ExpandHome(dir, home))
		}
		return quota.Identity(quota.Resolve(quota.Profile{Spec: &quota.Spec{Kind: quota.KindAnthropic, ConfigDir: dir}}, home))
	}
	src := "secret:" + p.Auth.Secret
	if p.KeyRef != "" {
		src = p.KeyRef
	}
	if p.CLI != "" && p.CLI != claudecli.ID {
		return p.CLI + ":" + p.Kind() + ":" + src + "@" + p.Endpoint.BaseURL
	}
	return p.Kind() + ":" + src + "@" + p.Endpoint.BaseURL
}

// Profile is the launch config the agent runs (threads, handover and quota
// keep using profiles by id).
func (p Provider) Profile(key func() (string, error), permissionMode string) Profile {
	ids := make([]string, 0, len(p.Models))
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	def := p.DefaultModel
	if def == "" && len(ids) > 0 {
		def = ids[0]
	}
	return Profile{ID: p.ID, Name: p.Name, Binary: p.binary(), Color: p.Color, Models: ids, DefaultModel: def,
		PermissionMode: permissionMode, Quota: p.quotaSpec(), ConfigDir: p.Auth.ConfigDir,
		CLI: p.CLI, Auth: p.Auth, Endpoint: p.Endpoint, Key: key, KeyRequired: p.Auth.Kind == cli.APIKey || p.TokenLogin,
		ModelCaps: p.Models, DefaultEffort: p.DefaultEffort, Projects: p.Projects}
}

// modelsFor gives each id its name and effort levels: the catalog's
// (V0.120), else a known model of the adapter; other ids get none.
func modelsFor(cliID string, ids []string, endpoint bool, catalog ...CatalogModel) []cli.Model {
	known := AdapterFor(cliID).Caps().Models
	out := []cli.Model{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m := cli.Model{ID: id}
		if c, ok := (Provider{Catalog: catalog}).catalogModel(id); ok && !c.Manual {
			m = cli.Model{ID: id, Name: c.Name, Efforts: c.Efforts}
		} else if !endpoint || cliID == agycli.ID {
			for _, k := range known {
				if k.ID == id {
					m = k
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// secretName: an environment variable that holds a secret.
func secretName(k string) bool {
	for _, w := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASS", "AUTH"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}
