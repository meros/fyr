// Package provider keeps the host's providers (SPEC 0.5 V0.50, decision
// 0015): a provider is a CLI adapter + an auth + an endpoint + the models it
// offers. Fyr runs every provider through its adapter (the plain `claude`
// binary today), never through a wrapper. Secrets live in package secrets;
// a provider only names one.
package provider

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/quota"
)

// The kinds the UI shows.
const (
	KindAccount  = "claude-account" // a cli-login
	KindEndpoint = "endpoint"       // an api-key with an endpoint
)

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
	// QuotaKind: "" picks by auth and endpoint (V0.44 kinds).
	QuotaKind string `json:"quotaKind,omitempty"`
	// Imported: where an imported provider came from ("" when made here).
	Imported  string    `json:"imported,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Kind is the UI's word for the auth.
func (p Provider) Kind() string {
	if p.Auth.Kind == cli.APIKey {
		return KindEndpoint
	}
	return KindAccount
}

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
		case p.Auth.Kind == cli.CLILogin:
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
	}
	return s
}

// identity: two providers on one account share one quota entry.
func (p Provider) identity(home string) string {
	if p.Auth.Kind == cli.CLILogin {
		dir := p.Auth.ConfigDir
		if dir == "" {
			dir = "~/.claude"
		}
		return quota.Identity(quota.Resolve(quota.Profile{Spec: &quota.Spec{Kind: quota.KindAnthropic, ConfigDir: dir}}, home))
	}
	src := "secret:" + p.Auth.Secret
	if p.KeyRef != "" {
		src = p.KeyRef
	}
	return p.Kind() + ":" + src + "@" + p.Endpoint.BaseURL
}

// Profile is the launch config the agent runs (threads, handover and quota
// keep using profiles by id).
func (p Provider) Profile(key func() (string, error), permissionMode string) agent.Profile {
	ids := make([]string, 0, len(p.Models))
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	def := p.DefaultModel
	if def == "" && len(ids) > 0 {
		def = ids[0]
	}
	return agent.Profile{ID: p.ID, Name: p.Name, Binary: "claude", Color: p.Color, Models: ids, DefaultModel: def,
		PermissionMode: permissionMode, Quota: p.quotaSpec(), ConfigDir: p.Auth.ConfigDir,
		CLI: p.CLI, Auth: p.Auth, Endpoint: p.Endpoint, Key: key, KeyRequired: p.Auth.Kind == cli.APIKey || p.TokenLogin,
		ModelCaps: p.Models, DefaultEffort: p.DefaultEffort}
}

// modelsFor gives known claude models their effort levels; other ids get
// none.
func modelsFor(ids []string, endpoint bool) []cli.Model {
	out := []cli.Model{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		m := cli.Model{ID: id}
		if !endpoint {
			for _, k := range claudecli.Models {
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
