package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/tailnet"
)

// buildID is set with -ldflags "-X main.buildID=…"; else from VCS info.
var buildID = ""

// version is the release's vX.Y.Z, set with -ldflags "-X main.version=…"
// when CI builds a version tag (V0.versioned-releases); empty on main.
var version = ""

// versionLine is what `fyrd --version` prints: the version and the build,
// or the build alone on main.
func versionLine() string {
	if version != "" {
		return "fyrd " + version + " (build " + BuildID() + ")"
	}
	return "fyrd " + BuildID() + " (main)"
}

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
	// watch remembers the file as it was read (V0.180): /healthz and Settings →
	// Hosts say when it changed since.
	watch           *configWatch
	Listen          string `json:"listen"`
	PermissionMode  string `json:"permissionMode"`
	IdleStopMinutes int    `json:"idleStopMinutes"`
	// UpdateRepo is where auto-update reads releases (decision 0027);
	// empty is meros/fyr. UpdateQuietMinutes is how long `deployable` must
	// stay put before a host installs it (#153); 0 is 10. UpdateEveryMinutes
	// is how often a host reads it; 0 is 60. UpdateKey is the base64
	// ed25519 public key a release must be signed with; empty is the
	// built-in release.PublicKey (V0.release-without-gh: a fork signs with
	// its own). UpdateChannel is "main" (follow `deployable`; empty is main),
	// "stable" (the newest version release) or a version to pin to
	// (V0.versioned-releases). All are read at start.
	UpdateRepo         string             `json:"updateRepo,omitempty"`
	UpdateKey          string             `json:"updateKey,omitempty"`
	UpdateChannel      string             `json:"updateChannel,omitempty"`
	UpdateQuietMinutes int                `json:"updateQuietMinutes,omitempty"`
	UpdateEveryMinutes int                `json:"updateEveryMinutes,omitempty"`
	OwnerLogin         string             `json:"ownerLogin"` // Tailscale login allowed over the tailnet (V0.14)
	Profiles           []provider.Profile `json:"profiles"`
	// ScratchRoots: folders whose children are scratch folders (V0.28);
	// empty means ~/.local/share/fyr/scratch.
	ScratchRoots []string `json:"scratchRoots"`
	// ScanRoots: extra folders the folder picker scans for git repositories,
	// two levels deep (V0.42); `~` is the home folder.
	ScanRoots []string `json:"scanRoots"`
	// AgentPath: folders put first on the agents' PATH, before the unit's
	// and the login shell's (V0.linux-without-nix); `~/` is the home folder.
	AgentPath []string `json:"agentPath"`
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
	// WellKnownListen: a loopback address where fyrd answers only
	// /.well-known/fyr, for a Tailscale Serve that cannot reach serve.sock
	// (the macOS app proxies to TCP only), so other installs find this one;
	// empty is none.
	WellKnownListen string `json:"wellKnownListen"`
	// Cgroups: the server/ and agents/ split and the agents' budget
	// (V0.80); absent is off.
	Cgroups *cgroup.Budget `json:"cgroups"`
	// Slots: how many heavy jobs (test suites, builds, e2e runs) run at
	// once across the threads (V0.86); absent or negative is 2, 0 is off.
	Slots *int `json:"slots"`
	// HeavyCommands: the patterns of heavy commands and their classes
	// (decision 0029, #185, #174); absent: heavy.Defaults.
	HeavyCommands []heavy.Pattern `json:"heavyCommands"`
	// HeavyGate: "off" lets a heavy command run without being admitted; anything
	// else (the default) refuses it at the gate hook (#174).
	HeavyGate string `json:"heavyGate"`
	// Decision 0029 S2: at this slowdown a job costing more than 0.25 waits
	// (default 1.5), and this share of the agents' memory stays free
	// (default 0.10); 0 is the default.
	SlotSlowdownMax   float64 `json:"slotSlowdownMax"`
	SlotMemoryReserve float64 `json:"slotMemoryReserve"`
	// V0.164: heavy work that nobody asked for. Undeclared "off" turns all of
	// it off on this host; the thresholds are shares of what the host gives
	// its agents (0 is the default: 0.25 of the cores, 0.08 of the memory,
	// 45 s, 30–120); what the owner saves in Settings wins over these.
	// HeavyAdded are patterns read after heavyCommands (or the defaults).
	Undeclared string `json:"undeclared"`
	// V0.176: Leftovers "off" turns off the leftover tracker and the temp
	// sweep on this host (nothing is listed or ended).
	Leftovers string `json:"leftovers"`
	// V0.worktree-sweep: Worktrees "list" only lists the worktrees nobody
	// works in; by default Fyr also removes the ones whose work is kept.
	Worktrees        string          `json:"worktrees"`
	UndeclaredCPU    float64         `json:"undeclaredCpu"`
	UndeclaredMemory float64         `json:"undeclaredMemory"`
	UndeclaredAfter  int             `json:"undeclaredAfter"`
	HeavyAdded       []heavy.Pattern `json:"heavyAdded"`
	// ServicePatterns are service patterns beside the built-in ones (V0.services-not-jobs).
	ServicePatterns []string `json:"servicePatterns"`
	// Peers: V0.123's static discovery origins; read only to say they are
	// ignored (V0.retire-discovery).
	Peers []string `json:"peers"`
	// PeerPorts: the HTTPS port of a node's Tailscale Serve, where the pairing
	// reads /.well-known/fyr (the first one); empty is 7443, the NixOS
	// module's.
	PeerPorts []int `json:"peerPorts"`
	// Name: this install's name in the clients; empty is the machine's
	// hostname. Two installs on one machine need two names (V0.123).
	Name string `json:"name"`
	// TailscaleSocket: tailscaled's LocalAPI socket; empty is
	// tailnet.DefaultSocket. Decision 0030 slice 0: the test harness points a
	// scratch fyrd at a fake tailscaled with it.
	TailscaleSocket string `json:"tailscaleSocket"`
	// ServeSocketUsers: user ids besides root that may connect to serve.sock
	// (decision 0030 3.2: its peer is tailscaled, which is root). Empty is
	// root only. The test harness adds its own user, since its Serve
	// emulator is not root.
	ServeSocketUsers []int `json:"serveSocketUsers"`
	// PeerListen: where this install's peer port listens (decision 0030 3.6,
	// V0.158): host:port, TLS 1.3 pinned to install keys, any interface; empty
	// is no peer port. The NixOS module sets it from services.fyr.peerPort.
	PeerListen string `json:"peerListen"`
	// Discovery.MDNS: false turns mDNS off, advertising and browsing (decision 0030 3.6);
	// absent is on, with a peer port. LANBusFile: a JSON list of candidates read as the
	// mDNS answers (the test harness's bus, or a LAN where multicast does not pass).
	Discovery struct {
		MDNS *bool `json:"mdns"`
	} `json:"discovery"`
	LANBusFile string `json:"lanBusFile"`
	// TLS: the owner's certificate and key (decision 0030 3.5). With them fyrd also
	// serves the front over HTTPS on Listen (an address on the LAN, "0.0.0.0:7778"); a
	// phone signs in there by QR or code. Name is the host a phone reaches it by (it is
	// in the certificate) and the one the QR link carries. Without them nothing is
	// served off loopback.
	TLS struct {
		Cert   string `json:"cert"`
		Key    string `json:"key"`
		Listen string `json:"listen"`
		Name   string `json:"name"`
	} `json:"tls"`
	// PeerEverySeconds: how often installs pair and sync; 0 is 60.
	PeerEverySeconds int `json:"peerEverySeconds"`
	// ResolveFile: a JSON object, host:port (or a host alone, for every port) → the
	// address to connect to, read at each dial (a network with no MagicDNS; the test harness). RootCAs: extra
	// PEM roots for the Serve probe of condition 1 (a Serve behind the owner's
	// own CA).
	ResolveFile string `json:"resolveFile"`
	RootCAs     string `json:"rootCAs"`
	// LinkBackoffMillis and LinkDialMillis tune the mesh's connectors (the wait between
	// dials to one install, doubling to five minutes, and the time one dial may take;
	// 1000 and 8000 when absent): the test harness makes them short.
	// ConfigCheckMillis is how often the config file is looked at (V0.180); absent
	// is 30 s. The test harness makes it short.
	ConfigCheckMillis int `json:"configCheckMillis"`
	LinkBackoffMillis int `json:"linkBackoffMillis"`
	LinkDialMillis    int `json:"linkDialMillis"`
	// StepUpMinutes: how old a browser session must be to widen trust (approve a
	// device, add one, pair an install); absent is 5, 0 turns the rule off. The owner
	// socket is never asked (decision 0030 3.4).
	StepUpMinutes *int `json:"stepUpMinutes"`
}

// stepUp is the session age that widens trust.
func (c config) stepUp() time.Duration {
	if c.StepUpMinutes == nil {
		return auth.DefaultStepUp
	}
	return time.Duration(*c.StepUpMinutes) * time.Minute
}

// configEvery is how often the config file is looked at.
func (c config) configEvery() time.Duration {
	if c.ConfigCheckMillis > 0 {
		return time.Duration(c.ConfigCheckMillis) * time.Millisecond
	}
	return configEvery
}

// tsSocket is the LocalAPI socket this install asks about the tailnet.
func (c config) tsSocket() string {
	if c.TailscaleSocket != "" {
		return c.TailscaleSocket
	}
	return tailnet.DefaultSocket
}

func home() string { h, _ := os.UserHomeDir(); return h }

func loadConfig(path string) config {
	c := config{Listen: "127.0.0.1:7777", IdleStopMinutes: 10}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			log.Fatalf("config %s: %v", path, err)
		}
	}
	c.watch = watchLoaded(path, b, err == nil)
	if len(c.Profiles) == 0 {
		models := make([]string, len(claudecli.Models))
		for i, m := range claudecli.Models {
			models[i] = m.ID
		}
		c.Profiles = []provider.Profile{{ID: "claudeAlt", Name: "Claude", Binary: "claude", Color: "#d97757",
			Models: models, DefaultModel: models[0]}}
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
		if login, dns, err := tailnet.Self(c.tsSocket()); err == nil {
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
