package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/t3"
	"github.com/meros/fyr/internal/tailnet"
)

// buildID is set with -ldflags "-X main.buildID=…"; else from VCS info.
var buildID = ""

func BuildID() string {
	if buildID != "" {
		return buildID
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if dirty {
				exe, _ := os.Executable()
				if st, err := os.Stat(exe); err == nil {
					rev += fmt.Sprintf("-dirty-%d", st.ModTime().Unix())
				}
			}
			return rev
		}
	}
	return fmt.Sprintf("dev-%d", time.Now().Unix())
}

type config struct {
	Listen          string             `json:"listen"`
	PermissionMode  string             `json:"permissionMode"`
	IdleStopMinutes int                `json:"idleStopMinutes"`
	// UpdateRepo is where auto-update reads releases (decision 0027);
	// empty is meros/fyr. UpdateQuietMinutes is how long `deployable` must
	// stay put before a host installs it (#153); 0 is 10.
	UpdateRepo         string `json:"updateRepo,omitempty"`
	UpdateQuietMinutes int    `json:"updateQuietMinutes,omitempty"`
	OwnerLogin      string             `json:"ownerLogin"` // Tailscale login allowed over the tailnet (V0.14)
	Profiles        []provider.Profile `json:"profiles"`
	// ScratchRoots: folders whose children are scratch folders (V0.28);
	// empty means ~/.t3/scratch and ~/.local/share/fyr/scratch.
	ScratchRoots []string `json:"scratchRoots"`
	// ScanRoots: extra folders the folder picker scans for git repositories,
	// two levels deep (V0.42); `~` is the home folder.
	ScanRoots []string `json:"scanRoots"`
	// Quota: per profile id, where its provider allowance is read (V0.44);
	// wins over the profile's own quota.
	Quota map[string]*quota.Spec `json:"quota"`
	// Role: host, front or both (V0.51); empty is both.
	Role string `json:"role"`
	// Hosts: a front's other hosts (V0.51), reached over the tailnet. In
	// both, the local host comes first and needs no entry.
	Hosts []front.HostConfig `json:"hosts"`
	// HostListen: a tailnet address where the host also serves the host
	// protocol to fronts on other machines (V0.51); empty is none.
	HostListen string `json:"hostListen"`
	// Cgroups: the server/ and agents/ split and the agents' budget
	// (V0.80); absent is off.
	Cgroups *cgroup.Budget `json:"cgroups"`
	// Slots: how many heavy jobs (test suites, builds, e2e runs) run at
	// once across the threads (V0.86); absent or negative is 2, 0 is off.
	Slots *int `json:"slots"`
	// HeavyCommands: the patterns of heavy commands and their classes
	// (decision 0029, #185, #174); absent: heavy.Defaults.
	HeavyCommands []heavy.Pattern `json:"heavyCommands"`
	// Peers: Fyr page origins probed as they are (V0.123, decision 0025),
	// besides the tailnet's own nodes; for development and non-tailnet hosts.
	Peers []string `json:"peers"`
	// PeerPorts: the HTTPS ports a tailnet node is probed on for
	// /.well-known/fyr (V0.123); empty is 7443, Tailscale Serve's port in the
	// NixOS module.
	PeerPorts []int `json:"peerPorts"`
	// Name: this install's name in the clients; empty is the machine's
	// hostname. Two installs on one machine need two names (V0.123).
	Name string `json:"name"`
}

func home() string { h, _ := os.UserHomeDir(); return h }

func loadConfig(path, t3Settings string) config {
	c := config{Listen: "127.0.0.1:7777", IdleStopMinutes: 10}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			log.Fatalf("config %s: %v", path, err)
		}
	}
	if len(c.Profiles) == 0 {
		if ps, err := t3.Profiles(t3Settings); err == nil {
			c.Profiles = ps
		}
	}
	if len(c.Profiles) == 0 {
		c.Profiles = []provider.Profile{{ID: "claudeAgent", Name: "Claude", Binary: "claude", Color: "#d97757",
			Models: t3.DefaultModels, DefaultModel: t3.DefaultModels[0]}}
	}
	for i := range c.Profiles {
		if c.Profiles[i].PermissionMode == "" {
			c.Profiles[i].PermissionMode = c.PermissionMode
		}
		if q := c.Quota[c.Profiles[i].ID]; q != nil {
			c.Profiles[i].Quota = q
		}
	}
	return c
}

// override applies the command line over the config file and fills the
// defaults: the role (V0.51), the front's hosts and the tailnet owner (V0.14).
func (c *config) override(listen, ownerLogin, role, hosts, hostListen string) {
	if listen != "" {
		c.Listen = listen
	}
	if ownerLogin != "" {
		c.OwnerLogin = ownerLogin
	}
	if role != "" {
		c.Role = role
	}
	if c.Role == "" {
		c.Role = "both"
	}
	if c.Role != "both" && c.Role != "host" && c.Role != "front" {
		log.Fatalf("role %q: want host, front or both", c.Role)
	}
	if hosts != "" {
		hs, err := parseHosts(hosts)
		if err != nil {
			log.Fatal(err)
		}
		c.Hosts = hs
	}
	if hostListen != "" {
		c.HostListen = hostListen
	}
	for i := range c.Hosts {
		c.Hosts[i].Remote = true
	}
	if c.OwnerLogin == "" {
		// V0.14: the Tailscale user of this machine owns it over the tailnet.
		if login, dns, err := tailnet.Self(tailnet.DefaultSocket); err == nil {
			c.OwnerLogin = login
			log.Printf("tailnet owner %s on %s", login, dns)
		} else {
			log.Printf("tailnet: %v; serving loopback only", err)
		}
	}
}

// parseHosts reads -hosts: name=url pairs separated by commas.
func parseHosts(s string) ([]front.HostConfig, error) {
	var out []front.HostConfig
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name, u, ok := strings.Cut(p, "=")
		if !ok || name == "" || u == "" {
			return nil, fmt.Errorf("-hosts: %q is not name=url", p)
		}
		out = append(out, front.HostConfig{Name: name, URL: u})
	}
	return out, nil
}

// providerAPI adapts the registry to the server.
type providerAPI struct{ r *provider.Registry }

func (p providerAPI) List() any  { return p.r.List() }
func (p providerAPI) Found() any { return p.r.Found() }

func quotaProfiles(ps []provider.Profile) []quota.Profile {
	out := make([]quota.Profile, 0, len(ps))
	for _, p := range ps {
		out = append(out, quota.Profile{ID: p.ID, Binary: p.Binary, Env: p.Env, Spec: p.Quota})
	}
	return out
}
