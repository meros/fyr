package provider

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/quota"
)

// Profile is a claude-compatible binary with its settings (V0.11).
type Profile struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Binary         string   `json:"binary"`
	Color          string   `json:"color"`
	Models         []string `json:"models"`
	DefaultModel   string   `json:"defaultModel"`
	PermissionMode string   `json:"permissionMode,omitempty"`
	Env            []string `json:"env,omitempty"`
	// Quota: where the provider allowance is read (V0.44); clients see the kind only.
	Quota *quota.Spec `json:"quota,omitempty"`
	// ConfigDir is the CLI's config dir (CLAUDE_CONFIG_DIR) when the
	// profile has its own; Fyr then sets it. See ConfigDirPath (V0.45).
	ConfigDir string `json:"configDir,omitempty"`

	// V0.50 (decision 0015): a profile made from a provider. CLI is the
	// adapter id ("" is claude); Auth and Endpoint are applied by the
	// adapter; Key reads the provider's secret at launch and never leaves
	// the host.
	CLI      string                 `json:"cli,omitempty"`
	Auth     cli.Auth               `json:"-"`
	Endpoint cli.Endpoint           `json:"-"`
	Key      func() (string, error) `json:"-"`
	// KeyRequired: the profile does not start without its key (an endpoint,
	// or an account that logs in with a token).
	KeyRequired bool `json:"-"`
	// Unavailable says why the provider cannot run now (V0.50: no token or
	// key yet); clients do not offer it for new threads or Hand over.
	Unavailable string `json:"unavailable,omitempty"`
	// ModelCaps: the offered models with their effort levels; DefaultEffort
	// is used when the thread names none and the model takes it.
	ModelCaps     []cli.Model `json:"modelCaps,omitempty"`
	DefaultEffort string      `json:"defaultEffort,omitempty"`
	// Projects: the provider is offered only to these project ids; empty is all.
	Projects []string `json:"projects,omitempty"`
}

// ConfigDirPath is where the profile's CLI keeps its sessions: the
// profile's configDir, else CLAUDE_CONFIG_DIR in its env, else the
// daemon's, else ~/.claude.
func (p Profile) ConfigDirPath() string {
	if p.ConfigDir != "" {
		return hostfs.ExpandHome(p.ConfigDir, "")
	}
	for _, kv := range p.Env {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok && v != "" {
			return hostfs.ExpandHome(v, "")
		}
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return hostfs.ExpandHome(v, "")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}
