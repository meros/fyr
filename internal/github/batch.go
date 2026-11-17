package github

// V0.178: the poller reads the PRs of one repo in one GraphQL query, up to
// BatchMax at a time, instead of one gh pr view per PR.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/meros/fyr/internal/core"
)

// BatchMax is the most PRs one query reads.
const BatchMax = 20

// prFields are gh pr view's fields of the poll, in GraphQL.
const prFields = `state mergeable reviewDecision title headRefName headRefOid baseRefName isDraft mergeStateStatus mergedAt
  mergeCommit { oid } autoMergeRequest { enabledAt }
  commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
    __typename ... on CheckRun { name status conclusion } ... on StatusContext { context state } } } } } } }`

// FetchViews reads the PRs (URLs of one repo) in one query and returns, by
// URL, each one's JSON in gh pr view's shape. A PR GitHub did not return
// is absent.
func FetchViews(ctx context.Context, gh Runner, repo string, urls []string) (map[string][]byte, error) {
	owner, name, ok := strings.Cut(core.RepoKey(repo), "/")
	if !ok {
		return nil, fmt.Errorf("not a repo: %s", repo)
	}
	var q strings.Builder
	q.WriteString("query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {")
	for i, u := range urls {
		_, n, ok := core.ParsePR(u)
		if !ok {
			return nil, fmt.Errorf("not a PR URL: %s", u)
		}
		fmt.Fprintf(&q, " p%d: pullRequest(number: %d) { %s }", i, n, prFields)
	}
	q.WriteString(" } }")
	out, err := gh(ctx, "api", "graphql", "-f", "query="+q.String(), "-F", "owner="+owner, "-F", "name="+name)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	// A PR that does not exist is null with an error; the others still come.
	var v struct {
		Data struct {
			Repository map[string]*batchPR `json:"repository"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(out, &v); jerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("batched PR read: %w", jerr)
	}
	views := map[string][]byte{}
	for i, u := range urls {
		p := v.Data.Repository[fmt.Sprintf("p%d", i)]
		if p == nil {
			continue
		}
		b, _ := json.Marshal(p.view())
		views[u] = b
	}
	return views, nil
}

type batchPR struct {
	State            string          `json:"state"`
	Mergeable        string          `json:"mergeable"`
	ReviewDecision   *string         `json:"reviewDecision"`
	Title            string          `json:"title"`
	HeadRefName      string          `json:"headRefName"`
	HeadRefOid       string          `json:"headRefOid"`
	BaseRefName      string          `json:"baseRefName"`
	IsDraft          bool            `json:"isDraft"`
	MergeStateStatus string          `json:"mergeStateStatus"`
	MergedAt         *string         `json:"mergedAt"`
	MergeCommit      json.RawMessage `json:"mergeCommit"`
	AutoMergeRequest json.RawMessage `json:"autoMergeRequest"`
	Commits          struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					Contexts struct {
						Nodes []map[string]any `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// view is the PR as gh pr view --json prints it.
func (p *batchPR) view() map[string]any {
	rollup := []map[string]any{}
	for _, c := range p.Commits.Nodes {
		if c.Commit.Rollup != nil {
			rollup = append(rollup, c.Commit.Rollup.Contexts.Nodes...)
		}
	}
	rd := ""
	if p.ReviewDecision != nil {
		rd = *p.ReviewDecision
	}
	v := map[string]any{"state": p.State, "mergeable": p.Mergeable, "reviewDecision": rd, "title": p.Title,
		"headRefName": p.HeadRefName, "headRefOid": p.HeadRefOid, "baseRefName": p.BaseRefName, "isDraft": p.IsDraft,
		"mergeStateStatus": p.MergeStateStatus, "mergeCommit": p.MergeCommit, "autoMergeRequest": p.AutoMergeRequest,
		"statusCheckRollup": rollup}
	if len(p.MergeCommit) == 0 {
		v["mergeCommit"] = nil
	}
	if len(p.AutoMergeRequest) == 0 {
		v["autoMergeRequest"] = nil
	}
	if p.MergedAt != nil {
		v["mergedAt"] = *p.MergedAt
	}
	return v
}
