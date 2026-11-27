package github

// V0.thread-issues: the poller reads the title and state of the issues the
// threads link, the issues of one repo in one GraphQL query, each at most
// every core.IssueWatchEvery, and reports only what changed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
)

// IssueSource is what the poller needs from the engine for linked issues.
type IssueSource interface {
	Issues() []IssueThread
	ObserveIssue(threadID string, is core.Issue)
}

// IssueThread is a thread's linked issue as the thread has it now.
type IssueThread struct {
	ID           string
	URL          string
	Title, State string
}

// batchIssue is one issue of a GraphQL read.
type batchIssue struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// issue is the read as a core.Issue.
func (b batchIssue) issue(at time.Time) (core.Issue, bool) {
	u, ok := core.CanonicalIssue(b.URL)
	if !ok {
		return core.Issue{}, false
	}
	repo, n, _ := core.ParseIssue(u)
	return core.Issue{URL: u, Repo: repo, Number: n, Title: b.Title, State: b.State, CheckedAt: &at}, true
}

// FetchIssues reads issues (URLs of one repo) in one query and returns
// each one GitHub answered, by URL.
func FetchIssues(ctx context.Context, gh Runner, repo string, urls []string) (map[string]core.Issue, error) {
	owner, name, ok := strings.Cut(core.RepoKey(repo), "/")
	if !ok {
		return nil, fmt.Errorf("not a repo: %s", repo)
	}
	var q strings.Builder
	q.WriteString("query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {")
	for i, u := range urls {
		_, n, ok := core.ParseIssue(u)
		if !ok {
			return nil, fmt.Errorf("not an issue URL: %s", u)
		}
		fmt.Fprintf(&q, " i%d: issue(number: %d) { url number title state }", i, n)
	}
	q.WriteString(" } }")
	asked := time.Now()
	out, err := gh(ctx, "api", "graphql", "-f", "query="+q.String(), "-F", "owner="+owner, "-F", "name="+name)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	// An issue that does not exist (or is a PR) is null; the others still come.
	var v struct {
		Data struct {
			Repository map[string]*batchIssue `json:"repository"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(out, &v); jerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("batched issue read: %w", jerr)
	}
	got := map[string]core.Issue{}
	for i, u := range urls {
		b := v.Data.Repository[fmt.Sprintf("i%d", i)]
		if b == nil {
			continue
		}
		if is, ok := b.issue(asked); ok {
			is.URL = u // the URL the thread has, so the report finds it
			got[u] = is
		}
	}
	return got, nil
}

// pollIssues reads the linked issues that are due, by repo.
func (p *Poller) pollIssues(ctx context.Context, src IssueSource) {
	now := p.now()
	if p.issueAt == nil {
		p.issueAt = map[string]time.Time{}
	}
	byRepo := map[string][]IssueThread{}
	var order []string
	watched := map[string]bool{}
	for _, t := range src.Issues() {
		k := strings.ToLower(t.URL)
		watched[k] = true
		if at, ok := p.issueAt[k]; ok && now.Sub(at) < core.IssueWatchEvery {
			continue
		}
		repo, _, ok := core.ParseIssue(t.URL)
		rk := core.RepoKey(repo)
		if !ok || rk == "" {
			continue
		}
		if _, seen := byRepo[rk]; !seen {
			order = append(order, rk)
		}
		byRepo[rk] = append(byRepo[rk], t)
	}
	for k := range p.issueAt {
		if !watched[k] {
			delete(p.issueAt, k)
		}
	}
	for _, rk := range order {
		ts := byRepo[rk]
		for len(ts) > 0 {
			chunk := ts[:min(BatchMax, len(ts))]
			ts = ts[len(chunk):]
			if errors.Is(p.readIssues(ctx, src, rk, chunk, now), ErrBudget) {
				return
			}
		}
	}
}

func (p *Poller) readIssues(ctx context.Context, src IssueSource, repo string, ts []IssueThread, now time.Time) error {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var urls []string
	for _, t := range ts {
		if !containsURL(urls, t.URL) {
			urls = append(urls, t.URL)
		}
	}
	got, err := FetchIssues(c, p.Gh, repo, urls)
	if err != nil {
		if !errors.Is(err, ErrBudget) {
			log.Printf("issues %s: %v", repo, err) // transient: read again at the next poll
		}
		return err
	}
	for _, t := range ts {
		p.issueAt[strings.ToLower(t.URL)] = now
		is, ok := got[t.URL]
		if !ok || (is.Title == t.Title && is.State == t.State) {
			continue
		}
		src.ObserveIssue(t.ID, is)
	}
	return nil
}

func containsURL(list []string, u string) bool {
	for _, x := range list {
		if x == u {
			return true
		}
	}
	return false
}
