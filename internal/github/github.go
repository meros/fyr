// Package github watches the PRs of threads with the gh CLI and runs the
// pr.merge effect (SPEC 0.5 V0.8, 12.5, 5.10).
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	out, err := gh(ctx, "pr", "view", "--json", "state,mergeable,reviewDecision,statusCheckRollup,title,headRefName,headRefOid,mergeCommit,mergedAt,isDraft,mergeStateStatus,autoMergeRequest,baseRefName", "--", canon)
	if err != nil {
		return core.PR{}, 0, err
	}
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
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return core.PR{}, 0, fmt.Errorf("gh pr view: %w", err)
	}
	states := make([]string, 0, len(v.Rollup))
	for _, c := range v.Rollup {
		states = append(states, CheckState(c.Typename, c.Status, c.Conclusion, c.State))
	}
	checks, failed := core.ChecksNone, 0
	if v.State == "OPEN" || len(states) > 0 {
		checks, failed = Summarize(ctx, gh, repo, states)
	}
	now := time.Now()
	p := core.PR{URL: url, Repo: repo, Number: n, Title: v.Title, State: v.State, Checks: checks,
		Review: v.ReviewDecision, Conflict: v.Mergeable == "CONFLICTING", Branch: v.HeadRefName, CheckedAt: &now, Head: v.HeadRefOid,
		Draft: v.IsDraft, MergeState: v.MergeState, AutoMerge: len(v.AutoMerge) > 0 && string(v.AutoMerge) != "null"}
	if v.MergeCommit != nil {
		p.MergeSHA = v.MergeCommit.Oid
	}
	if v.MergedAt != nil && !v.MergedAt.IsZero() {
		p.MergedAt = v.MergedAt
	}
	p.Base = v.BaseRefName
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

	// kick is made once, by whichever of Kick and Run comes first, so a
	// kick before Run waits for it and no goroutine writes the field
	// while another reads it (review 2026-11-15).
	kickOnce sync.Once
	kick     chan string
}

func (p *Poller) kicks() chan string {
	p.kickOnce.Do(func() { p.kick = make(chan string, 16) })
	return p.kick
}

func (p *Poller) Kick(threadID string) {
	select {
	case p.kicks() <- threadID:
	default:
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
		case id := <-kick:
			for _, t := range p.Src.Snapshot() {
				if t.ID == id {
					p.poll(ctx, t)
				}
			}
		}
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	for _, t := range p.Src.Snapshot() {
		p.poll(ctx, t)
	}
	if ds, ok := p.Src.(DeploySource); ok {
		for _, d := range ds.Deploys() {
			p.pollDeploy(ctx, ds, d)
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
		log.Printf("pr %s: %v", t.URL, err) // transient: nothing changes
		return
	}
	delete(p.missing, t.URL)
	if pr.State == "OPEN" && pr.Checks == core.ChecksFail && pr.Head != "" {
		pr.Annotations = p.annotations(c, t.URL, pr, failed)
	} else {
		delete(p.notes, t.URL)
	}
	p.Src.Observe(t.ID, pr)
}
