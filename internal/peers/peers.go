// Package peers finds the owner's other Fyr hosts (SPEC 0.5 V0.123, decision
// 0025). A Fyr host is a tailnet node of the owner's own Tailscale user that
// answers GET https://<its MagicDNS name>:<port>/.well-known/fyr with the
// owner's login, or a static origin from the config that answers the same.
// The front follows every host found here as it follows a configured one, so
// the hosts keep the connections and the client keeps its one uplink.
package peers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WellKnown is the answer of /.well-known/fyr.
type WellKnown struct {
	Fyr   int    `json:"fyr"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Build string `json:"build"`
	Owner string `json:"owner"`
	// Host is where this install serves the host protocol to fronts on other
	// machines (its hostListen, V0.51), e.g. http://100.64.0.2:7778; empty
	// when it serves none.
	Host string `json:"host,omitempty"`
}

// Peer is one discovered Fyr host, as the client gets it.
type Peer struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Role   string `json:"role,omitempty"`
	Build  string `json:"build,omitempty"`
	// Host: its host protocol URL (WellKnown.Host), which a front follows.
	Host   string `json:"host,omitempty"`
	Online bool   `json:"online"`
	// Since: online since, or offline since (the first round it did not answer).
	Since time.Time `json:"since"`
	// LastSeen: the last answer.
	LastSeen time.Time `json:"lastSeen"`
}

// Forget is how long a host that stopped answering stays listed (0021 (7)).
const Forget = 30 * 24 * time.Hour

// Every is how often a round runs.
const Every = 60 * time.Second

type Options struct {
	// Owner is this fyrd's owner login; a peer must name the same.
	Owner string
	// Self is this install's own origin; never its own peer.
	Self string
	// Static origins (config peers), probed as they are.
	Static []string
	// Nodes lists tailnet candidates by MagicDNS name (tailnet.Candidates);
	// nil: no tailnet.
	Nodes func(context.Context) ([]string, error)
	// Ports on which a tailnet candidate is probed over HTTPS; default 7443.
	Ports []int
	// Client probes; default a 3 s timeout client.
	Client *http.Client
	// OnChange runs after a round that changed the list.
	OnChange func([]Peer)
	Now      func() time.Time
	Logf     func(string, ...any)
	// WellKnown is this install's own answer (Self).
	WellKnown WellKnown
}

type Discovery struct {
	opt   Options
	mu    sync.Mutex
	peers map[string]*Peer // by origin
}

func New(opt Options) *Discovery {
	if len(opt.Ports) == 0 {
		opt.Ports = []int{7443}
	}
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 3 * time.Second}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	opt.Self = strings.TrimRight(opt.Self, "/")
	return &Discovery{opt: opt, peers: map[string]*Peer{}}
}

// Run does a round at once and then every interval until ctx ends.
func (d *Discovery) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = Every
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		d.Round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (d *Discovery) origins(ctx context.Context) []string {
	seen := map[string]bool{}
	var out []string
	add := func(o string) {
		o = strings.TrimRight(o, "/")
		if o != "" && o != d.opt.Self && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	for _, o := range d.opt.Static {
		add(o)
	}
	if d.opt.Nodes != nil {
		names, err := d.opt.Nodes(ctx)
		if err != nil {
			d.opt.Logf("peers: tailnet: %v", err)
		}
		for _, n := range names {
			for _, p := range d.opt.Ports {
				add("https://" + n + ":" + strconv.Itoa(p))
			}
		}
	}
	return out
}

// Round probes every candidate once and updates the list.
func (d *Discovery) Round(ctx context.Context) {
	origins := d.origins(ctx)
	answers := make([]*WellKnown, len(origins))
	var wg sync.WaitGroup
	for i, o := range origins {
		wg.Add(1)
		go func() { defer wg.Done(); answers[i] = d.probe(ctx, o) }()
	}
	wg.Wait()
	now := d.opt.Now()
	d.mu.Lock()
	before := d.snapshot()
	for i, o := range origins {
		a, p := answers[i], d.peers[o]
		switch {
		case a != nil:
			if p == nil {
				p = &Peer{Origin: o}
				d.peers[o] = p
				d.opt.Logf("peers: found Fyr host %s at %s", a.Name, o)
			}
			if !p.Online {
				p.Since = now
			}
			p.Name, p.Role, p.Build, p.Host, p.Online, p.LastSeen = a.Name, a.Role, a.Build, a.Host, true, now
		case p != nil && p.Online:
			p.Online, p.Since = false, now
		}
	}
	for o, p := range d.peers {
		if !p.Online && now.Sub(p.LastSeen) > Forget {
			delete(d.peers, o)
		}
	}
	after := d.snapshot()
	d.mu.Unlock()
	if !reflect.DeepEqual(before, after) && d.opt.OnChange != nil {
		d.opt.OnChange(after)
	}
}

// probe asks one origin who it is; nil unless it is the owner's Fyr.
func (d *Discovery) probe(ctx context.Context, origin string) *WellKnown {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", origin+"/.well-known/fyr", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	resp, err := d.opt.Client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var w WellKnown
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&w) != nil || w.Fyr != 1 || w.Owner != d.opt.Owner {
		return nil
	}
	if w.Name == "" {
		w.Name = origin
	}
	return &w
}

// snapshot is the list sorted by name then origin; the caller holds d.mu.
func (d *Discovery) snapshot() []Peer {
	out := make([]Peer, 0, len(d.peers))
	for _, p := range d.peers {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Origin < out[j].Origin
	})
	return out
}

// List is the discovered hosts, online or not.
func (d *Discovery) List() []Peer {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snapshot()
}

// Self is this install's /.well-known/fyr answer.
func (d *Discovery) Self() WellKnown { return d.opt.WellKnown }

// Handler answers /.well-known/fyr.
func Handler(self func() WellKnown) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(self())
	})
}
