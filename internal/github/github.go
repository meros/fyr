// Package github watches the PRs of threads with the gh CLI and runs the
// pr.merge effect (SPEC 0.5 V0.8, 12.5, 5.10).
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/proc"
)

// Runner runs gh with arguments and returns its combined output.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Gh runs the real gh CLI.
func Gh(ctx context.Context, args ...string) ([]byte, error) {
	out, err := proc.Command(ctx, "gh", args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("gh %s: %w: %s", strings.Join(args[:min(2, len(args))], " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Fetch reads one PR's state.
func Fetch(ctx context.Context, gh Runner, url string) (core.PR, error) {
	p, _, err := fetch(ctx, gh, url)
	return p, err
}

// fetch also returns the number of failed checks.
func fetch(ctx context.Context, gh Runner, url string) (core.PR, int, error) {
	repo, n, ok := core.ParsePR(url)
	canon, _ := core.CanonicalPR(url)
	if !ok {
		return core.PR{}, 0, fmt.Errorf("not a PR URL: %s", url)
	}
	asked := time.Now()
	out, err := gh(ctx, "pr", "view", "--json", "state,mergeable,reviewDecision,statusCheckRollup,title,headRefName,headRefOid,mergeCommit,mergedAt,isDraft,mergeStateStatus,autoMergeRequest,baseRefName", "--", canon)
	if err != nil {
		return core.PR{}, 0, err
	}
	return fromView(ctx, gh, url, repo, n, out, asked)
}

// fromView makes the PR of gh pr view's JSON, or of one PR of a batched
// read (V0.178), which has the same shape. asked is when GitHub was asked:
// the PR's CheckedAt, so a read that began before a watch was armed does
// not count as a read after it (V0.checks-follow-head).
func fromView(ctx context.Context, gh Runner, url, repo string, n int, out []byte, asked time.Time) (core.PR, int, error) {
	var v struct {
		State          string `json:"state"`
		Mergeable      string `json:"mergeable"`
		ReviewDecision string `json:"reviewDecision"`
		Title          string `json:"title"`
		HeadRefName    string `json:"headRefName"`
		HeadRefOid     string `json:"headRefOid"`
		BaseRefName    string `json:"baseRefName"`
		MergeCommit    *struct {
			Oid string `json:"oid"`
		} `json:"mergeCommit"`
		MergedAt   *time.Time      `json:"mergedAt"`
		IsDraft    bool            `json:"isDraft"`
		MergeState string          `json:"mergeStateStatus"`
		AutoMerge  json.RawMessage `json:"autoMergeRequest"`
		Rollup     []struct {
			Typename   string `json:"__typename"`
			Name       string `json:"name"`
			Context    string `json:"context"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return core.PR{}, 0, fmt.Errorf("gh pr view: %w", err)
	}
	states := make([]string, 0, len(v.Rollup))
	done, failedNames, passed := 0, []string(nil), map[string]bool{}
	for _, c := range v.Rollup {
		s := CheckState(c.Typename, c.Status, c.Conclusion, c.State)
		states = append(states, s)
		if s != checkRunning && s != checkFail {
			passed[c.Name+c.Context] = true
		}
		if s != checkRunning { // V0.136: "n of m checks done"
			done++
		}
		if s == checkFail {
			name := c.Name
			if name == "" {
				name = c.Context
			}
			failedNames = append(failedNames, name)
		}
	}
	checks, failed := core.ChecksNone, 0
	if v.State == "OPEN" || len(states) > 0 {
		checks, failed = Summarize(ctx, gh, repo, states)
	}
	p := core.PR{URL: url, Repo: repo, Number: n, Title: v.Title, State: v.State, Checks: checks,
		Review: v.ReviewDecision, Conflict: v.Mergeable == "CONFLICTING", Branch: v.HeadRefName, CheckedAt: &asked, Head: v.HeadRefOid,
		Draft: v.IsDraft, MergeState: v.MergeState, AutoMerge: len(v.AutoMerge) > 0 && string(v.AutoMerge) != "null",
		ChecksDone: done, ChecksTotal: len(states), FailedChecks: failedNames}
	if v.MergeCommit != nil {
		p.MergeSHA = v.MergeCommit.Oid
	}
	if v.MergedAt != nil && !v.MergedAt.IsZero() {
		p.MergedAt = v.MergedAt
	}
	p.Base = v.BaseRefName
	if p.State == "OPEN" && p.MergeState == "BLOCKED" && (checks == core.ChecksPass || checks == core.ChecksNone) {
		// V0.136: name what blocks it, once per repo and base (cached).
		rs := readRules(ctx, gh, repo, p.Base)
		p.Rules, p.Protected = rs.types, rs.protected
		for _, ctxName := range rs.required {
			if !passed[ctxName] {
				p.MissingChecks = append(p.MissingChecks, ctxName)
			}
		}
	}
	if p.State == "MERGED" && p.Base != "" {
		readStacked(ctx, gh, &p)
	}
	return p, failed, nil
}

// Summarize is the one place a PR's check list becomes its checks
// (V0.112): fail when a check failed, pending while one runs, pass when at
// least one check reported and none failed or runs. An empty list is
// pending on a repo with CI, because a run can be queued before GitHub
// lists it; it is none only on a repo with no CI (RepoHasCI). It also
// returns the number of failed checks.
func Summarize(ctx context.Context, gh Runner, repo string, states []string) (core.Checks, int) {
	if len(states) == 0 {
		if RepoHasCI(ctx, gh, repo) {
			return core.ChecksPending, 0
		}
		return core.ChecksNone, 0
	}
	markCI(repo)
	checks, failed := core.ChecksPass, 0
	for _, s := range states {
		switch s {
		case checkFail:
			checks = core.ChecksFail
			failed++
		case checkRunning:
			if checks != core.ChecksFail {
				checks = core.ChecksPending
			}
		}
	}
	return checks, failed
}

// CIRecheck is how long Fyr trusts that a repo has no CI before it reads
// the repo again (V0.112). That a repo has CI is kept for the process.
const CIRecheck = 10 * time.Minute

// ciRepos is what Fyr knows of each repo's CI, by RepoKey.
var ciRepos sync.Map

type ciFact struct {
	has bool
	at  time.Time
}

func markCI(repo string) {
	if k := core.RepoKey(repo); k != "" {
		ciRepos.Store(k, ciFact{has: true})
	}
}

// ciQuery reads the check rollup of the head commit of the repo's 20 newest
// PRs, open or not.
const ciQuery = `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    pullRequests(last: 20) { nodes { commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } } }
  }
}`

// RepoHasCI reports whether the repo has CI (V0.112): a check reported on
// the head of any of its 20 newest PRs, or on any PR Fyr read since it
// started. A read that fails counts as CI, so a merge waits rather than
// runs on a guess.
func RepoHasCI(ctx context.Context, gh Runner, repo string) bool {
	k := core.RepoKey(repo)
	if k == "" {
		return true
	}
	if v, ok := ciRepos.Load(k); ok {
		if f := v.(ciFact); f.has || time.Since(f.at) < CIRecheck {
			return f.has
		}
	}
	owner, name, _ := strings.Cut(k, "/")
	out, err := gh(ctx, "api", "graphql", "-f", "query="+ciQuery, "-F", "owner="+owner, "-F", "name="+name,
		"--jq", "[.data.repository.pullRequests.nodes[].commits.nodes[].commit.statusCheckRollup | select(. != null)] | length")
	if err != nil {
		return true
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return true
	}
	ciRepos.Store(k, ciFact{has: n > 0, at: time.Now()})
	return n > 0
}

// RulesRecheck is how long Fyr keeps what it read of a base branch's rules
// (V0.136).
const RulesRecheck = 30 * time.Minute

// baseRules caches the rules of a repo's base branch, by "repo@base".
var baseRules sync.Map

type ruleSet struct {
	types     []string // the rule types active on the branch (rulesets)
	required  []string // the required status checks, of rulesets and classic protection
	protected bool     // classic branch protection
	at        time.Time
}

// readRules reads what GitHub enforces on base (V0.136): the active rules
// of its rulesets (GET repos/{repo}/rules/branches/{base}, readable with
// read access) and, when they set none, whether classic branch protection
// is on and which checks it requires (GET repos/{repo}/branches/{base}).
// The poller asks only for a PR GitHub reads as BLOCKED with green checks,
// and keeps the answer RulesRecheck per repo and base. A failed read is an
// empty set, not cached: the words fall back to "the branch rules".
func readRules(ctx context.Context, gh Runner, repo, base string) ruleSet {
	k := core.RepoKey(repo)
	if k == "" || base == "" || strings.HasPrefix(base, "-") || strings.ContainsAny(base, " \n") {
		return ruleSet{}
	}
	key := k + "@" + base
	if v, ok := baseRules.Load(key); ok && time.Since(v.(ruleSet).at) < RulesRecheck {
		return v.(ruleSet)
	}
	out, err := gh(ctx, "api", "repos/"+k+"/rules/branches/"+url.PathEscape(base))
	if err != nil {
		return ruleSet{}
	}
	var rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			Checks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	}
	if json.Unmarshal(out, &rules) != nil {
		return ruleSet{}
	}
	rs := ruleSet{at: time.Now()}
	for _, r := range rules {
		if !slices.Contains(rs.types, r.Type) {
			rs.types = append(rs.types, r.Type)
		}
		for _, c := range r.Parameters.Checks {
			rs.required = append(rs.required, c.Context)
		}
	}
	if len(rs.types) == 0 {
		if out, err := gh(ctx, "api", "repos/"+k+"/branches/"+url.PathEscape(base)); err == nil {
			var b struct {
				Protected  bool `json:"protected"`
				Protection struct {
					Checks struct {
						Contexts []string `json:"contexts"`
					} `json:"required_status_checks"`
				} `json:"protection"`
			}
			if json.Unmarshal(out, &b) == nil {
				rs.protected, rs.required = b.Protected, b.Protection.Checks.Contexts
			}
		}
	}
	baseRules.Store(key, rs)
	return rs
}

// defaultBranches caches each repo's default branch (V0.98 (1)).
var defaultBranches sync.Map

// readStacked tells a merged PR whose base is not the repo's default branch
// (stacked), and whether its merge commit is in the default branch yet
// (V0.98 (1)): GitHub's compare of the default branch with the merge
// commit is "behind" or "identical". A read that fails leaves it unknown.
func readStacked(ctx context.Context, gh Runner, p *core.PR) {
	def, ok := defaultBranches.Load(p.Repo)
	if !ok {
		out, err := gh(ctx, "api", "repos/"+p.Repo, "--jq", ".default_branch")
		name := strings.TrimSpace(string(out))
		if err != nil || name == "" || strings.ContainsAny(name, " {\n") {
			return
		}
		def, _ = defaultBranches.LoadOrStore(p.Repo, name)
	}
	if def.(string) == p.Base {
		return
	}
	p.Stacked = true
	if p.MergeSHA == "" {
		return
	}
	out, err := gh(ctx, "api", fmt.Sprintf("repos/%s/compare/%s...%s", p.Repo, def, p.MergeSHA), "--jq", ".status")
	if err != nil {
		return
	}
	in := false
	switch strings.TrimSpace(string(out)) {
	case "behind", "identical":
		in = true
	case "ahead", "diverged":
	default:
		return
	}
	p.InDefault = &in
}

// Missing reports an error by which GitHub answered that the PR or its
// repository does not exist (V0.72): GraphQL's "Could not resolve to a
// Repository" or "... to a PullRequest", or HTTP 404. A network error, a
// rate limit, a 5xx or a failed login is not.
func Missing(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Could not resolve to a Repository") || strings.Contains(s, "Could not resolve to a PullRequest") || strings.Contains(s, "HTTP 404")
}

// MissingAfter is how long after GitHub first said a PR does not exist a
// second answer confirms it (V0.72): one poll later, so a PR GitHub has
// not finished making is not ended by a kick right after it opened.
const MissingAfter = time.Minute

// Merge merges a PR with gh (the pr.merge effect), only while its head is
// head (V0.97): GitHub refuses the merge when the head moved.
func Merge(ctx context.Context, gh Runner, url, method, head string) (string, error) {
	canon, ok := core.CanonicalPR(url)
	if !ok {
		return "", fmt.Errorf("not a PR URL: %s", url)
	}
	if head == "" || strings.HasPrefix(head, "-") {
		return "", errors.New("a merge needs the head commit")
	}
	if method == "" {
		method = "squash"
	}
	out, err := gh(ctx, "pr", "merge", "--"+method, "--match-head-commit", head, "--", canon)
	return strings.TrimSpace(string(out)), err
}

// Source is what the poller needs from the engine.
type Source interface {
	Snapshot() []PRThread
	Observe(threadID string, pr core.PR)
}

// PRThread is a thread with an open PR.
type PRThread struct{ ID, URL string }

// Poller refreshes open PRs every Interval, and one thread at once on Kick.
type Poller struct {
	Gh       Runner
	Src      Source
	Interval time.Duration
	notes    map[string]noteCache // V0.66 (7): by PR URL
	// Now is the clock (tests); nil is time.Now.
	Now func() time.Time
	// missing is when GitHub first said a PR does not exist (V0.72), by URL.
	missing map[string]time.Time
	// deployAt is when each deploy watch was last read (V0.173), by
	// repo@sha.
	deployAt map[string]time.Time

	// kick is made once, by whichever of Kick and Run comes first, so a
	// kick before Run waits for it and no goroutine writes the field
	// while another reads it (review 2026-11-15).
	kickOnce sync.Once
	kick     chan kickReq
}

// kickReq is one thread to read now; done, when set, closes after the read.
type kickReq struct {
	id   string
	done chan struct{}
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Poller) kicks() chan kickReq {
	p.kickOnce.Do(func() { p.kick = make(chan kickReq, 16) })
	return p.kick
}

func (p *Poller) Kick(threadID string) {
	select {
	case p.kicks() <- kickReq{id: threadID}:
	default:
	}
}

// Refresh reads the thread's PRs now and returns once they are observed, or
// when ctx ends first (V0.199). The read runs in Run, which owns the poller's
// maps.
func (p *Poller) Refresh(ctx context.Context, threadID string) {
	done := make(chan struct{})
	select {
	case p.kicks() <- kickReq{id: threadID, done: done}:
	case <-ctx.Done():
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (p *Poller) Run(ctx context.Context) {
	kick := p.kicks()
	if p.Interval == 0 {
		p.Interval = 2 * time.Minute
	}
	tick := time.NewTicker(p.Interval)
	defer tick.Stop()
	p.pollAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			p.pollAll(ctx)
		case k := <-kick:
			for _, t := range p.Src.Snapshot() {
				if t.ID == k.id {
					p.poll(ctx, t)
				}
			}
			if k.done != nil {
				close(k.done)
			}
		}
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	// V0.178: the PRs of one repo are read in one GraphQL query (up to
	// BatchMax each); a PR the batch did not answer is read alone.
	byRepo := map[string][]PRThread{}
	var order []string
	for _, t := range p.Src.Snapshot() {
		repo, _, ok := core.ParsePR(t.URL)
		k := core.RepoKey(repo)
		if !ok || k == "" {
			p.poll(ctx, t)
			continue
		}
		if _, seen := byRepo[k]; !seen {
			order = append(order, k)
		}
		byRepo[k] = append(byRepo[k], t)
	}
	for _, k := range order {
		ts := byRepo[k]
		if len(ts) == 1 {
			p.poll(ctx, ts[0])
			continue
		}
		for len(ts) > 0 {
			chunk := ts[:min(BatchMax, len(ts))]
			ts = ts[len(chunk):]
			p.pollBatch(ctx, k, chunk)
		}
	}
	if ds, ok := p.Src.(DeploySource); ok {
		watched := map[string]bool{}
		for _, d := range ds.Deploys() {
			watched[d.Repo+"@"+d.SHA] = true
			p.pollDeploy(ctx, ds, d)
		}
		for k := range p.deployAt {
			if !watched[k] {
				delete(p.deployAt, k)
			}
		}
	}
}

func (p *Poller) poll(ctx context.Context, t PRThread) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pr, failed, err := fetch(c, p.Gh, t.URL)
	if Missing(err) {
		now := time.Now()
		if p.Now != nil {
			now = p.Now()
		}
		first, seen := p.missing[t.URL]
		if !seen {
			if p.missing == nil {
				p.missing = map[string]time.Time{}
			}
			p.missing[t.URL] = now
		}
		if !seen || now.Sub(first) < MissingAfter {
			log.Printf("pr %s: %v", t.URL, err)
			return
		}
		delete(p.missing, t.URL)
		repo, n, _ := core.ParsePR(t.URL)
		p.Src.Observe(t.ID, core.PR{URL: t.URL, Repo: repo, Number: n, State: core.PRMissing, Checks: core.ChecksNone, CheckedAt: &now})
		return
	}
	if err != nil {
		if !errors.Is(err, ErrBudget) {
			log.Printf("pr %s: %v", t.URL, err) // transient: nothing changes
		}
		return
	}
	p.observe(c, t, pr, failed)
}

// pollBatch reads the PRs of one repo in one query (V0.178).
func (p *Poller) pollBatch(ctx context.Context, repo string, ts []PRThread) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	urls := make([]string, len(ts))
	for i, t := range ts {
		urls[i] = t.URL
	}
	asked := time.Now()
	views, err := FetchViews(c, p.Gh, repo, urls)
	if errors.Is(err, ErrBudget) {
		return
	}
	for _, t := range ts {
		out, ok := views[t.URL]
		if err != nil || !ok {
			p.poll(ctx, t) // read alone: a missing PR is told apart there
			continue
		}
		r, n, _ := core.ParsePR(t.URL)
		pr, failed, ferr := fromView(c, p.Gh, t.URL, r, n, out, asked)
		if ferr != nil {
			if !errors.Is(ferr, ErrBudget) {
				log.Printf("pr %s: %v", t.URL, ferr)
			}
			continue
		}
		p.observe(c, t, pr, failed)
	}
}

func (p *Poller) observe(c context.Context, t PRThread, pr core.PR, failed int) {
	delete(p.missing, t.URL)
	if pr.State == "OPEN" && pr.Checks == core.ChecksFail && pr.Head != "" {
		pr.Annotations = p.annotations(c, t.URL, pr, failed)
	} else {
		delete(p.notes, t.URL)
	}
	p.Src.Observe(t.ID, pr)
}
