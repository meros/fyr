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
	repo, n, ok := agent.ParsePR(url)
	if !ok {
		return core.PR{}, fmt.Errorf("not a PR URL: %s", url)
	}
	out, err := gh(ctx, "pr", "view", url, "--json", "state,mergeable,reviewDecision,statusCheckRollup,title,headRefName")
	if err != nil {
		return core.PR{}, err
	}
	var v struct {
		State          string `json:"state"`
		Mergeable      string `json:"mergeable"`
		ReviewDecision string `json:"reviewDecision"`
		Title          string `json:"title"`
		HeadRefName    string `json:"headRefName"`
		Rollup         []struct {
			Typename   string `json:"__typename"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return core.PR{}, fmt.Errorf("gh pr view: %w", err)
	}
	checks := "none"
	if len(v.Rollup) > 0 {
		checks = "pass"
	}
	for _, c := range v.Rollup {
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "CANCELLED" || c.Conclusion == "TIMED_OUT" ||
			c.Conclusion == "ACTION_REQUIRED" || c.Conclusion == "STARTUP_FAILURE" || c.State == "FAILURE" || c.State == "ERROR":
			checks = "fail"
		case checks != "fail" && ((c.Typename == "CheckRun" && c.Status != "COMPLETED") || c.State == "PENDING" || c.State == "EXPECTED"):
			checks = "pending"
		}
	}
	now := time.Now()
	return core.PR{URL: url, Repo: repo, Number: n, Title: v.Title, State: v.State, Checks: checks,
		Review: v.ReviewDecision, Conflict: v.Mergeable == "CONFLICTING", Branch: v.HeadRefName, CheckedAt: &now}, nil
}

// Merge merges a PR with gh (the pr.merge effect).
func Merge(ctx context.Context, gh Runner, url, method string) (string, error) {
	if _, _, ok := agent.ParsePR(url); !ok {
		return "", fmt.Errorf("not a PR URL: %s", url)
	}
	if method == "" {
		method = "squash"
	}
	out, err := gh(ctx, "pr", "merge", url, "--"+method)
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
}

func (p *Poller) poll(ctx context.Context, t PRThread) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pr, err := Fetch(c, p.Gh, t.URL)
	if err != nil {
		log.Printf("pr %s: %v", t.URL, err)
		return
	}
	p.Src.Observe(t.ID, pr)
}
