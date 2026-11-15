// Package github watches the PRs of threads with the gh CLI and runs the
// pr.merge effect (SPEC 0.5 V0.8, 12.5, 5.10).
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	out, err := gh(ctx, "pr", "view", "--json", "state,mergeable,reviewDecision,statusCheckRollup,title,headRefName,headRefOid,mergeCommit,mergedAt", "--", canon)
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
		MergeCommit    *struct {
			Oid string `json:"oid"`
		} `json:"mergeCommit"`
		MergedAt *time.Time `json:"mergedAt"`
		Rollup   []struct {
			Typename   string `json:"__typename"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return core.PR{}, 0, fmt.Errorf("gh pr view: %w", err)
	}
	checks, failed := core.ChecksNone, 0
	if len(v.Rollup) > 0 {
		checks = core.ChecksPass
	}
	for _, c := range v.Rollup {
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "CANCELLED" || c.Conclusion == "TIMED_OUT" ||
			c.Conclusion == "ACTION_REQUIRED" || c.Conclusion == "STARTUP_FAILURE" || c.State == "FAILURE" || c.State == "ERROR":
			checks = core.ChecksFail
			failed++
		case checks != core.ChecksFail && ((c.Typename == "CheckRun" && c.Status != "COMPLETED") || c.State == "PENDING" || c.State == "EXPECTED"):
			checks = core.ChecksPending
		}
	}
	now := time.Now()
	p := core.PR{URL: url, Repo: repo, Number: n, Title: v.Title, State: v.State, Checks: checks,
		Review: v.ReviewDecision, Conflict: v.Mergeable == "CONFLICTING", Branch: v.HeadRefName, CheckedAt: &now, Head: v.HeadRefOid}
	if v.MergeCommit != nil {
		p.MergeSHA = v.MergeCommit.Oid
	}
	if v.MergedAt != nil && !v.MergedAt.IsZero() {
		p.MergedAt = v.MergedAt
	}
	return p, failed, nil
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

// Merge merges a PR with gh (the pr.merge effect).
func Merge(ctx context.Context, gh Runner, url, method string) (string, error) {
	canon, ok := core.CanonicalPR(url)
	if !ok {
		return "", fmt.Errorf("not a PR URL: %s", url)
	}
	if method == "" {
		method = "squash"
	}
	out, err := gh(ctx, "pr", "merge", "--"+method, "--", canon)
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
