package github

// V0.173: what this host spends of the account's GitHub API limits. Every
// gh call fyrd makes goes through one Meter, which counts it by kind and
// repo, logs an hourly summary to daemon.log, and reads the account's
// remaining calls from GET rate_limit (free: GitHub does not count it).

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// RateEvery is how often the Meter reads rate_limit.
const RateEvery = 10 * time.Minute

// Rate is one read of the account's limits (shared by every caller of the
// login: this host's fyrd and agents, other hosts, the owner's own gh).
type Rate struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
	GraphQL   int       `json:"graphqlRemaining"`
	At        time.Time `json:"at"`
}

// Use is one kind of call and how often this host's fyrd made it.
type Use struct {
	Kind  string `json:"kind"` // "rest deployments", "graphql pr view", ...
	Repo  string `json:"repo,omitempty"`
	Calls int    `json:"calls"`
	// Free counts the 304 answers among them, which cost nothing (V0.173).
	Free int `json:"free,omitempty"`
}

// MeterView is what Settings → Automation shows of a host.
type MeterView struct {
	Rate  *Rate     `json:"rate,omitempty"`
	Since time.Time `json:"since"`
	Uses  []Use     `json:"uses"`
	Calls int       `json:"calls"` // REST calls that counted, this hour
	// Paused is the budget's pause of the polls (V0.178), set by fyrd.
	Paused *Pause `json:"paused,omitempty"`
}

// Meter counts gh calls. The zero value works.
type Meter struct {
	Now func() time.Time
	// OnRate gets each read of rate_limit (V0.178: the budget).
	OnRate func(Rate)

	mu    sync.Mutex
	since time.Time
	uses  map[[2]string]*Use
	last  []Use // the hour before
	rate  *Rate
}

func (m *Meter) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Wrap returns gh with every call counted.
func (m *Meter) Wrap(gh Runner) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		out, err := gh(ctx, args...)
		kind, repo := Kind(args)
		m.Count(kind, repo, notModified(out, err))
		return out, err
	}
}

// Count records one call.
func (m *Meter) Count(kind, repo string, free bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uses == nil {
		m.uses = map[[2]string]*Use{}
		m.since = m.now()
	}
	k := [2]string{kind, repo}
	u := m.uses[k]
	if u == nil {
		u = &Use{Kind: kind, Repo: repo}
		m.uses[k] = u
	}
	u.Calls++
	if free {
		u.Free++
	}
}

// View is the current hour's counts, most first, and the last rate read.
func (m *Meter) View() MeterView {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := MeterView{Since: m.since, Rate: m.rate, Uses: sorted(m.uses)}
	if len(v.Uses) == 0 && m.last != nil {
		v.Uses = m.last
	}
	for _, u := range v.Uses {
		if strings.HasPrefix(u.Kind, "rest ") {
			v.Calls += u.Calls - u.Free
		}
	}
	return v
}

func sorted(uses map[[2]string]*Use) []Use {
	out := make([]Use, 0, len(uses))
	for _, u := range uses {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].Kind+out[i].Repo < out[j].Kind+out[j].Repo
	})
	return out
}

// Roll ends the hour: it returns the summary line and starts counting
// again.
func (m *Meter) Roll() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	uses := sorted(m.uses)
	since := m.since
	m.uses, m.last, m.since = nil, uses, m.now()
	return Summary(since, m.since, uses, m.rate)
}

// Summary is the hourly daemon.log line.
func Summary(from, to time.Time, uses []Use, r *Rate) string {
	var b strings.Builder
	total, rest := 0, 0
	for _, u := range uses {
		total += u.Calls
		if strings.HasPrefix(u.Kind, "rest ") {
			rest += u.Calls - u.Free
		}
	}
	fmt.Fprintf(&b, "gh calls %s–%s: %d (%d counted REST)", from.Format("15:04"), to.Format("15:04"), total, rest)
	for i, u := range uses {
		if i == 12 {
			fmt.Fprintf(&b, "; +%d more kinds", len(uses)-i)
			break
		}
		fmt.Fprintf(&b, "; %s", u.Kind)
		if u.Repo != "" {
			fmt.Fprintf(&b, " %s", u.Repo)
		}
		fmt.Fprintf(&b, " %d", u.Calls)
		if u.Free > 0 {
			fmt.Fprintf(&b, " (%d free)", u.Free)
		}
	}
	if r != nil {
		fmt.Fprintf(&b, "; account REST %d of %d left, resets %s", r.Remaining, r.Limit, r.Reset.Local().Format("15:04"))
	}
	return b.String()
}

// SetRate records a read of the account's limits.
func (m *Meter) SetRate(r Rate) {
	m.mu.Lock()
	m.rate = &r
	m.mu.Unlock()
}

// ReadRate reads GET rate_limit, which GitHub does not count.
func ReadRate(ctx context.Context, gh Runner, now time.Time) (Rate, error) {
	out, err := gh(ctx, "api", "rate_limit")
	if err != nil {
		return Rate{}, err
	}
	var v struct {
		Resources struct {
			Core struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"core"`
			GraphQL struct {
				Remaining int `json:"remaining"`
			} `json:"graphql"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return Rate{}, fmt.Errorf("rate_limit: %w", err)
	}
	c := v.Resources.Core
	return Rate{Limit: c.Limit, Remaining: c.Remaining, Reset: time.Unix(c.Reset, 0), GraphQL: v.Resources.GraphQL.Remaining, At: now}, nil
}

// Run reads the rate every RateEvery and logs the summary every hour,
// until ctx ends. gh is the unwrapped runner: rate_limit is not a call to
// count.
func (m *Meter) Run(ctx context.Context, gh Runner) {
	var failed string
	read := func() { m.readRate(ctx, gh, &failed) }
	read()
	rate := time.NewTicker(RateEvery)
	hour := time.NewTicker(time.Hour)
	defer rate.Stop()
	defer hour.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rate.C:
			read()
		case <-hour.C:
			log.Print(m.Roll())
		}
	}
}

// readRate reads the rate once. A failure is logged once, and again only when
// it changes or after a read succeeds (V0.fresh-ubuntu-run: a host with no gh
// login logged the same two lines every RateEvery); failed is the last one
// logged.
func (m *Meter) readRate(ctx context.Context, gh Runner, failed *string) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := ReadRate(c, gh, m.now())
	if err != nil {
		if e := strings.TrimSpace(err.Error()); e != *failed {
			log.Printf("github rate: %s (said again only when it changes)", e)
			*failed = e
		}
		return
	}
	if *failed != "" {
		log.Printf("github rate: read again")
		*failed = ""
	}
	m.SetRate(r)
	if m.OnRate != nil {
		m.OnRate(r)
	}
}

var (
	numSeg  = regexp.MustCompile(`^[0-9]+$`)
	shaSeg  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	repoArg = regexp.MustCompile(`github\.com/([^/]+/[^/]+)/`)
)

// valueFlags are the gh api flags that take a value.
var valueFlags = map[string]bool{"-H": true, "--header": true, "-f": true, "--raw-field": true, "-F": true, "--field": true,
	"-q": true, "--jq": true, "-X": true, "--method": true, "-t": true, "--template": true, "--hostname": true, "--input": true, "-p": true, "--preview": true, "--cache": true}

// Kind names a gh call for the counts, and the repo it is about: "rest
// deployments" for gh api repos/o/r/deployments?..., "rest deployments/:id/
// statuses", "graphql" for gh api graphql, "graphql pr view" for gh pr
// view, "rest run list" for gh run list.
func Kind(args []string) (kind, repo string) {
	if len(args) == 0 {
		return "other", ""
	}
	if args[0] == "api" {
		path := ""
		for i := 1; i < len(args); i++ {
			a := args[i]
			if strings.HasPrefix(a, "-") {
				if valueFlags[a] {
					i++ // its value
				}
				continue
			}
			path = a
			break
		}
		if path == "graphql" {
			for i, a := range args {
				if a == "-F" && i+1 < len(args) && strings.HasPrefix(args[i+1], "owner=") {
					repo = strings.TrimPrefix(args[i+1], "owner=")
				}
				if a == "-F" && i+1 < len(args) && strings.HasPrefix(args[i+1], "name=") {
					repo += "/" + strings.TrimPrefix(args[i+1], "name=")
				}
			}
			return "graphql", repo
		}
		path, _, _ = strings.Cut(path, "?")
		seg := strings.Split(strings.Trim(path, "/"), "/")
		if len(seg) >= 3 && seg[0] == "repos" {
			repo, seg = seg[1]+"/"+seg[2], seg[3:]
		}
		for i, s := range seg {
			if numSeg.MatchString(s) || shaSeg.MatchString(s) || strings.Contains(s, "...") {
				seg[i] = ":id"
			}
		}
		if len(seg) == 0 {
			return "rest repo", repo
		}
		// keep the shape, not the names: tags/deployable, branches/main
		if len(seg) > 3 {
			seg = seg[:3]
		}
		return "rest " + strings.Join(seg, "/"), repo
	}
	if len(args) >= 2 {
		kind = args[0] + " " + args[1]
	} else {
		kind = args[0]
	}
	for i, a := range args {
		if (a == "-R" || a == "--repo") && i+1 < len(args) {
			repo = args[i+1]
		}
		if m := repoArg.FindStringSubmatch(a); m != nil {
			repo = m[1]
		}
	}
	switch args[0] {
	case "pr":
		return "graphql " + kind, repo
	case "run", "release":
		return "rest " + kind, repo
	}
	return kind, repo
}
