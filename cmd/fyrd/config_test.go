package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/tailnet"
)

// SPEC 0.5 V0.80: the module's cgroups block, as nix/module.nix writes it;
// a config without one has none (off).
func TestV080ConfigCgroups(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"cgroups": {"enable": true, "serverMemoryLow": "1G", "agentsCpuMax": "400000 100000",
	  "agentsMemoryHigh": "13G", "agentsMemoryMax": "14G", "agentsMemorySwapMax": "16G"}}`), 0o600)
	c := loadConfig(p)
	if g := c.Cgroups; g == nil || !g.Enable || g.ServerMemoryLow != "1G" || g.AgentsCPUMax != "400000 100000" ||
		g.AgentsMemoryHigh != "13G" || g.AgentsMemoryMax != "14G" || g.AgentsMemorySwapMax != "16G" {
		t.Fatalf("%+v", c.Cgroups)
	}
	if c := loadConfig(filepath.Join(t.TempDir(), "none.json")); c.Cgroups != nil {
		t.Fatalf("%+v", c.Cgroups)
	}
}

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
	c := loadConfig(p)
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
	c := loadConfig(filepath.Join(t.TempDir(), "none.json"))
	if c.Role != "" || len(c.Hosts) != 0 || c.HostListen != "" {
		t.Fatalf("a missing config must leave the role to the default (both): %+v", c)
	}
}

// SPEC 0.5 V0.123: a hostListen on loopback serves a second install on the
// same machine; anything else is a tailnet address.
func TestLoopbackHostListen(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:7778": true, "localhost:7778": true, "[::1]:7778": true, "100.64.0.2:7778": false, "tailnet:7778": false, "nope": false} {
		if got := loopbackAddr(addr); got != want {
			t.Errorf("loopbackAddr(%q) = %v", addr, got)
		}
	}
}

// V0.155 (decision 0030 slice 0): tailscaled's socket is configurable, so a
// scratch fyrd talks to a fake one; empty is the default.
func TestTailscaleSocketConfig(t *testing.T) {
	if got := (config{}).tsSocket(); got != tailnet.DefaultSocket {
		t.Errorf("default %q", got)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"tailscaleSocket": "/tmp/x/ts.sock"}`), 0o600)
	c := loadConfig(p)
	if got := c.tsSocket(); got != "/tmp/x/ts.sock" {
		t.Errorf("configured %q", got)
	}
}
