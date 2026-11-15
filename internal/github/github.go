// Package github watches the PRs of threads with the gh CLI and runs the
// pr.merge effect (SPEC 0.5 V0.8, 12.5, 5.10).
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
)

// Runner runs gh with arguments and returns its combined output.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Gh runs the real gh CLI.
func Gh(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "gh", args...).CombinedOutput()
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
	repo, n, ok := agent.ParsePR(url)
	canon, _ := agent.CanonicalPR(url)
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
	checks, failed := "none", 0
	if len(v.Rollup) > 0 {
		checks = "pass"
	}
	for _, c := range v.Rollup {
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "CANCELLED" || c.Conclusion == "TIMED_OUT" ||
			c.Conclusion == "ACTION_REQUIRED" || c.Conclusion == "STARTUP_FAILURE" || c.State == "FAILURE" || c.State == "ERROR":
			checks = "fail"
			failed++
		case checks != "fail" && ((c.Typename == "CheckRun" && c.Status != "COMPLETED") || c.State == "PENDING" || c.State == "EXPECTED"):
			checks = "pending"
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

// Merge merges a PR with gh (the pr.merge effect).
func Merge(ctx context.Context, gh Runner, url, method string) (string, error) {
	canon, ok := agent.CanonicalPR(url)
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
	kick     chan string
	notes    map[string]noteCache // V0.66 (7): by PR URL
}

func (p *Poller) Kick(threadID string) {
	if p.kick == nil {
		return
	}
	select {
	case p.kick <- threadID:
	default:
	}
}

func (p *Poller) Run(ctx context.Context) {
	p.kick = make(chan string, 16)
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
		case id := <-p.kick:
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
	if err != nil {
		log.Printf("pr %s: %v", t.URL, err)
		return
	}
	if pr.State == "OPEN" && pr.Checks == "fail" && pr.Head != "" {
		pr.Annotations = p.annotations(c, t.URL, pr, failed)
	} else {
		delete(p.notes, t.URL)
	}
	p.Src.Observe(t.ID, pr)
}
