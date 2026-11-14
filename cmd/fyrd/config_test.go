package main

import (
	"os"
	"path/filepath"
	"testing"
)

// SPEC 0.5 V0.44: the config's quota map, by profile id, wins over a
// profile's own quota; a profile in neither keeps none.
func TestConfigQuotaByProfile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{
	  "profiles": [
	    {"id": "claudeAgent", "name": "Claude", "binary": "claude"},
	    {"id": "claudeNorban", "name": "Claude (norban)", "binary": "claude-norban", "quota": {"kind": "none"}},
	    {"id": "claudeDeepseek", "name": "DeepSeek", "binary": "claude-deepseek", "quota": {"kind": "deepseek", "keyEnv": "A"}}
	  ],
	  "quota": {
	    "claudeNorban": {"kind": "anthropic-oauth", "configDir": "~/.claude-norban"},
	    "claudeDeepseek": {"kind": "deepseek", "keyFile": "/run/secrets/deepseek"}
	  }
	}`), 0o600)
	c := loadConfig(p, filepath.Join(t.TempDir(), "none.json"))
	ps := c.Profiles
	if ps[0].Quota != nil || ps[1].Quota.Kind != "anthropic-oauth" || ps[1].Quota.ConfigDir != "~/.claude-norban" ||
		ps[2].Quota.KeyFile != "/run/secrets/deepseek" || ps[2].Quota.KeyEnv != "" {
		t.Fatalf("%+v %+v %+v", ps[0].Quota, ps[1].Quota, ps[2].Quota)
	}
	qs := quotaProfiles(ps)
	if qs[1].Binary != "claude-norban" || qs[2].Spec.Kind != "deepseek" {
		t.Fatalf("%+v", qs)
	}
}
