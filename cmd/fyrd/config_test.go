package main

import (
	"net"
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
	    {"id": "claudeAlt", "name": "Claude", "binary": "claude"},
	    {"id": "claudeAcme", "name": "Claude (acme)", "binary": "claude-acme", "quota": {"kind": "none"}},
	    {"id": "claudeDeepseek", "name": "DeepSeek", "binary": "claude-deepseek", "quota": {"kind": "deepseek", "keyEnv": "A"}}
	  ],
	  "quota": {
	    "claudeAcme": {"kind": "anthropic-oauth", "configDir": "~/.claude-acme"},
	    "claudeDeepseek": {"kind": "deepseek", "keyFile": "/run/secrets/deepseek"}
	  }
	}`), 0o600)
	c := loadConfig(p, filepath.Join(t.TempDir(), "none.json"))
	ps := c.Profiles
	if ps[0].Quota != nil || ps[1].Quota.Kind != "anthropic-oauth" || ps[1].Quota.ConfigDir != "~/.claude-acme" ||
		ps[2].Quota.KeyFile != "/run/secrets/deepseek" || ps[2].Quota.KeyEnv != "" {
		t.Fatalf("%+v %+v %+v", ps[0].Quota, ps[1].Quota, ps[2].Quota)
	}
	qs := quotaProfiles(ps)
	if qs[1].Binary != "claude-acme" || qs[2].Spec.Kind != "deepseek" {
		t.Fatalf("%+v", qs)
	}
}

// SPEC 0.5 V0.51: -hosts lists a front's hosts; hostListen is a tailnet
// address only.
func TestRoleFlags(t *testing.T) {
	hs, err := parseHosts("host-b=http://host-b:7778, laptop=http://100.64.0.5:7778")
	if err != nil || len(hs) != 2 || hs[0].Name != "host-b" || hs[1].URL != "http://100.64.0.5:7778" {
		t.Fatalf("%+v %v", hs, err)
	}
	if _, err := parseHosts("nourl"); err == nil {
		t.Fatal("a host without a url was accepted")
	}
	for ip, want := range map[string]bool{"100.101.2.3": true, "fd7a:115c:a1e0::1": true, "192.168.1.2": false, "127.0.0.1": false} {
		if got := tailnetIP(net.ParseIP(ip)); got != want {
			t.Errorf("tailnetIP(%s) = %v", ip, got)
		}
	}
	c := loadConfig(filepath.Join(t.TempDir(), "none.json"), "")
	if c.Role != "" || len(c.Hosts) != 0 || c.HostListen != "" {
		t.Fatalf("a missing config must leave the role to the default (both): %+v", c)
	}
}
