package github

// The PR tab's detail, the owner's merge and the re-run of failed checks
// (SPEC 0.5 V0.43).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
)

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type Review struct {
	Author string     `json:"author"`
	State  string     `json:"state"`
	At     *time.Time `json:"at,omitempty"`
}

// Check is a CheckRun or a commit status. State is fail, running, pass or skipped.
type Check struct {
	Name        string     `json:"name"`
	Workflow    string     `json:"workflow,omitempty"`
	State       string     `json:"state"`
	Status      string     `json:"status,omitempty"`
	Conclusion  string     `json:"conclusion,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	URL         string     `json:"url,omitempty"`
}

type Comment struct {
	Author string     `json:"author"`
	Body   string     `json:"body"`
	At     *time.Time `json:"at,omitempty"`
	URL    string     `json:"url,omitempty"`
}

// ReviewThread is an unresolved review thread.
type ReviewThread struct {
	Path     string    `json:"path"`
	Line     int       `json:"line,omitempty"`
	Outdated bool      `json:"outdated"`
	URL      string    `json:"url,omitempty"`
	Comments []Comment `json:"comments"`
}

// Detail is what GET /api/threads/{id}/pr returns.
type Detail struct {
	URL            string         `json:"url"`
	Repo           string         `json:"repo"`
	Number         int            `json:"number"`
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	State          string         `json:"state"`
	Draft          bool           `json:"draft"`
	Author         string         `json:"author"`
	Head           string         `json:"head"`
	Base           string         `json:"base"`
	HeadSha        string         `json:"headSha"`
	Additions      int            `json:"additions"`
	Deletions      int            `json:"deletions"`
	ChangedFiles   int            `json:"changedFiles"`
	Labels         []Label        `json:"labels"`
	Mergeable      string         `json:"mergeable"`
	MergeState     string         `json:"mergeState"`
	ReviewDecision string         `json:"reviewDecision"`
	Reviews        []Review       `json:"reviews"`
	Checks         []Check        `json:"checks"`
	Threads        []ReviewThread `json:"threads"`
	ThreadsError   string         `json:"threadsError,omitempty"`
	FetchedAt      time.Time      `json:"fetchedAt"`

	// Checked is the checks summed up (Summarize, V0.112): pending also
	// when no check has reported yet on a repo with CI.
	Checked core.Checks `json:"checked"`
}

const viewFields = "number,url,title,body,state,isDraft,author,headRefName,baseRefName,headRefOid,additions,deletions,changedFiles,labels,mergeable,mergeStateStatus,reviewDecision,latestReviews,statusCheckRollup"

type login struct {
	Login string `json:"login"`
}

func (l *login) name() string {
	if l == nil || l.Login == "" {
		return "ghost"
	}
	return l.Login
}

// ts drops GitHub's zero time ("0001-01-01T00:00:00Z").
func ts(t time.Time) *time.Time {
	if t.IsZero() || t.Year() < 2000 {
		return nil
	}
	return &t
}

// The states CheckState gives a check.
const (
	checkFail    string = "fail"
	checkRunning string = "running"
)

// Advisory reports whether a check only informs (V0.advisory-checks): its
// name ends in "(advisory)". Fyr leaves it out of a PR's CI state, so it
// never holds or fails a merge ask: a self-hosted runner that is asleep keeps
// such a check queued for hours, and GitHub ends no queued job by itself. The
// PR's detail still lists it with its state.
func Advisory(name string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "(advisory)")
}

// CheckState reads a CheckRun (status, conclusion) or a commit status (state)
// as fail, running, pass or skipped.
func CheckState(typename, status, conclusion, state string) string {
	if typename == "StatusContext" || (status == "" && state != "") {
		switch strings.ToUpper(state) {
		case "SUCCESS":
			return "pass"
		case "PENDING", "EXPECTED":
			return "running"
		default:
			return "fail"
		}
	}
	if strings.ToUpper(status) != "COMPLETED" {
		return "running"
	}
	switch strings.ToUpper(conclusion) {
	case "SUCCESS":
		return "pass"
	case "NEUTRAL", "SKIPPED", "STALE":
		return "skipped"
	default:
		return "fail"
	}
}

// FetchDetail reads one PR with gh pr view (without review threads).
func FetchDetail(ctx context.Context, gh Runner, url string) (Detail, error) {
	repo, n, ok := core.ParsePR(url)
	canon, _ := core.CanonicalPR(url)
	if !ok {
		return Detail{}, fmt.Errorf("not a PR URL: %s", url)
	}
	out, err := gh(ctx, "pr", "view", "--json", viewFields, "--", canon)
	if err != nil {
		return Detail{}, Friendly(err)
	}
	var v struct {
		Number         int     `json:"number"`
		URL            string  `json:"url"`
		Title          string  `json:"title"`
		Body           string  `json:"body"`
		State          string  `json:"state"`
		IsDraft        bool    `json:"isDraft"`
		Author         *login  `json:"author"`
		Head           string  `json:"headRefName"`
		Base           string  `json:"baseRefName"`
		HeadSha        string  `json:"headRefOid"`
		Additions      int     `json:"additions"`
		Deletions      int     `json:"deletions"`
		ChangedFiles   int     `json:"changedFiles"`
		Labels         []Label `json:"labels"`
		Mergeable      string  `json:"mergeable"`
		MergeState     string  `json:"mergeStateStatus"`
		ReviewDecision string  `json:"reviewDecision"`
		Reviews        []struct {
			Author      *login    `json:"author"`
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submittedAt"`
		} `json:"latestReviews"`
		Rollup []struct {
			Typename    string    `json:"__typename"`
			Name        string    `json:"name"`
			Context     string    `json:"context"`
			Workflow    string    `json:"workflowName"`
			Status      string    `json:"status"`
			Conclusion  string    `json:"conclusion"`
			State       string    `json:"state"`
			StartedAt   time.Time `json:"startedAt"`
			CompletedAt time.Time `json:"completedAt"`
			DetailsURL  string    `json:"detailsUrl"`
			TargetURL   string    `json:"targetUrl"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return Detail{}, fmt.Errorf("gh pr view: %w", err)
	}
	d := Detail{URL: url, Repo: repo, Number: n, Title: v.Title, Body: v.Body, State: v.State, Draft: v.IsDraft,
		Author: v.Author.name(), Head: v.Head, Base: v.Base, HeadSha: v.HeadSha, Additions: v.Additions,
		Deletions: v.Deletions, ChangedFiles: v.ChangedFiles, Labels: v.Labels, Mergeable: v.Mergeable,
		MergeState: v.MergeState, ReviewDecision: v.ReviewDecision, Reviews: []Review{}, Checks: []Check{}, Threads: []ReviewThread{}}
	if d.Labels == nil {
		d.Labels = []Label{}
	}
	for _, r := range v.Reviews {
		d.Reviews = append(d.Reviews, Review{Author: r.Author.name(), State: r.State, At: ts(r.SubmittedAt)})
	}
	for _, c := range v.Rollup {
		ck := Check{Name: c.Name, Workflow: c.Workflow, Status: c.Status, Conclusion: c.Conclusion,
			StartedAt: ts(c.StartedAt), CompletedAt: ts(c.CompletedAt), URL: c.DetailsURL}
		if c.Typename == "StatusContext" {
			ck = Check{Name: c.Context, Conclusion: c.State, StartedAt: ts(c.StartedAt), URL: c.TargetURL}
		}
		ck.State = CheckState(c.Typename, c.Status, c.Conclusion, c.State)
		d.Checks = append(d.Checks, ck)
	}
	d.Checked = core.ChecksNone
	if d.State == "OPEN" || len(d.Checks) > 0 {
		states := make([]string, 0, len(d.Checks))
		for _, c := range d.Checks {
			if !Advisory(c.Name) {
				states = append(states, c.State)
			}
		}
		d.Checked, _ = Summarize(ctx, gh, repo, states)
	}
	return d, nil
}

const threadsQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 50) {
        nodes {
          isResolved isOutdated path line originalLine
          comments(first: 20) { nodes { author { login } body createdAt url } }
        }
      }
    }
  }
}`

// FetchThreads reads the unresolved review threads (the first 50) with gh api graphql.
func FetchThreads(ctx context.Context, gh Runner, repo string, number int) ([]ReviewThread, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("not a repository: %s", repo)
	}
	out, err := gh(ctx, "api", "graphql", "-f", "query="+threadsQuery, "-F", "owner="+owner, "-F", "name="+name, "-F", "number="+strconv.Itoa(number))
	if err != nil {
		return nil, Friendly(err)
	}
	var v struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved   bool   `json:"isResolved"`
							IsOutdated   bool   `json:"isOutdated"`
							Path         string `json:"path"`
							Line         *int   `json:"line"`
							OriginalLine *int   `json:"originalLine"`
							Comments     struct {
								Nodes []struct {
									Author    *login    `json:"author"`
									Body      string    `json:"body"`
									CreatedAt time.Time `json:"createdAt"`
									URL       string    `json:"url"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("review threads: %w", err)
	}
	if len(v.Errors) > 0 {
		return nil, fmt.Errorf("review threads: %s", v.Errors[0].Message)
	}
	res := []ReviewThread{}
	for _, n := range v.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if n.IsResolved {
			continue
		}
		rt := ReviewThread{Path: n.Path, Outdated: n.IsOutdated, Comments: []Comment{}}
		if n.Line != nil {
			rt.Line = *n.Line
		} else if n.OriginalLine != nil {
			rt.Line = *n.OriginalLine
		}
		for _, c := range n.Comments.Nodes {
			rt.Comments = append(rt.Comments, Comment{Author: c.Author.name(), Body: c.Body, At: ts(c.CreatedAt), URL: c.URL})
		}
		if len(rt.Comments) > 0 {
			rt.URL = rt.Comments[0].URL
		}
		res = append(res, rt)
	}
	return res, nil
}

// Friendly turns gh's failures the owner can fix into plain sentences.
func Friendly(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return errors.New("gh is not installed on this host")
	case strings.Contains(s, "gh auth login"):
		return errors.New("gh is not signed in on this host: run gh auth login")
	case strings.Contains(s, "Could not resolve to a PullRequest"):
		return errors.New("GitHub does not know this PR, or this host's gh cannot see it")
	}
	return err
}

// MergeBlock is why the host refuses to merge, or "" (V0.43).
func MergeBlock(d Detail) string {
	base := d.Base
	if base == "" {
		base = "the base branch"
	}
	switch {
	case d.State == "MERGED":
		return "The PR is already merged"
	case d.State != "OPEN":
		return "The PR is closed"
	case d.Draft || d.MergeState == "DRAFT":
		return "The PR is a draft"
	case d.Mergeable == "CONFLICTING" || d.MergeState == "DIRTY":
		return "Conflicts with " + base
	case d.MergeState == "BEHIND":
		return "Behind " + base + ": update the branch first"
	}
	fails := 0
	for _, c := range d.Checks {
		if c.State == "fail" {
			fails++
		}
	}
	switch {
	case d.MergeState == "BLOCKED" && fails == 1:
		return "Blocked: 1 failing check"
	case d.MergeState == "BLOCKED" && fails > 1:
		return fmt.Sprintf("Blocked: %d failing checks", fails)
	case d.Checked == core.ChecksPending || (d.Checked == "" && len(d.Checks) == 0):
		// V0.112: a merge waits for CI; no check reported is not green
		// (a detail that was not summed up, with no checks, neither).
		return waitCI(d)
	}
	for _, c := range d.Checks { // a detail that was not summed up
		if c.State == checkRunning {
			return waitCI(d)
		}
	}
	if d.MergeState == "BLOCKED" {
		switch {
		case d.ReviewDecision == "REVIEW_REQUIRED":
			return "Review required"
		case d.ReviewDecision == "CHANGES_REQUESTED":
			return "Changes requested"
		}
		return "Blocked by branch rules"
	}
	return ""
}

// waitCI is MergeBlock's refusal while CI has not passed (V0.112).
func waitCI(d Detail) string {
	on := "the head"
	if len(d.HeadSha) >= 7 {
		on = d.HeadSha[:7]
	}
	if len(d.Checks) == 0 {
		return "No check has reported on " + on + " yet: Merge waits for CI"
	}
	return "CI has not passed on " + on + " yet: Merge waits for the checks"
}

var methods = map[string]bool{"squash": true, "merge": true, "rebase": true}

// MergeChecked reads the PR again and merges it unless MergeBlock refuses
// or its head is not head, the commit the owner saw (V0.97).
func MergeChecked(ctx context.Context, gh Runner, url, method, head string) (string, error) {
	if method == "" {
		method = "squash"
	}
	if !methods[method] {
		return "", fmt.Errorf("unknown merge method %q", method)
	}
	if head == "" {
		return "", errors.New("not merged: no head commit was given")
	}
	for try := 0; ; try++ {
		d, err := FetchDetail(ctx, gh, url)
		if err != nil {
			return "", err
		}
		if !core.SameHead(d.HeadSha, head) {
			return "", errors.New(core.HeadMoved)
		}
		if why := MergeBlock(d); why != "" {
			return "", fmt.Errorf("not merged: %s", why)
		}
		out, err := Merge(ctx, gh, url, method, d.HeadSha)
		if err == nil || try >= len(mergeBackoff) || !strings.Contains(err.Error(), baseModified) {
			return out, Friendly(err)
		}
		select { // V0.205: another merge landed first; the same head may still merge
		case <-time.After(mergeBackoff[try]):
		case <-ctx.Done():
			return out, Friendly(err)
		}
	}
}

// baseModified is GitHub's refusal when the base moved during a merge.
const baseModified = "Base branch was modified"

// mergeBackoff is the wait before each further try of such a merge (V0.205):
// 3 tries in all.
var mergeBackoff = []time.Duration{2 * time.Second, 5 * time.Second}

// Run is a workflow run from gh run list.
type Run struct {
	ID         int64  `json:"databaseId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSha    string `json:"headSha"`
}

// FailedRuns keeps the completed runs of sha that failed, were cancelled or timed out.
func FailedRuns(runs []Run, sha string) []Run {
	var out []Run
	for _, r := range runs {
		if r.HeadSha != sha || !strings.EqualFold(r.Status, "completed") {
			continue
		}
		switch strings.ToLower(r.Conclusion) {
		case "failure", "cancelled", "timed_out", "startup_failure":
			out = append(out, r)
		}
	}
	return out
}

// RerunFailed reruns the failed jobs of each failed workflow run of the PR's head commit.
func RerunFailed(ctx context.Context, gh Runner, url string) (string, error) {
	repo, _, ok := core.ParsePR(url)
	canon, _ := core.CanonicalPR(url)
	if !ok {
		return "", fmt.Errorf("not a PR URL: %s", url)
	}
	out, err := gh(ctx, "pr", "view", "--json", "headRefOid", "--", canon)
	if err != nil {
		return "", Friendly(err)
	}
	var v struct {
		Sha string `json:"headRefOid"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.Sha == "" {
		return "", fmt.Errorf("gh pr view: no head commit")
	}
	out, err = gh(ctx, "run", "list", "--repo", repo, "--commit", v.Sha, "--json", "databaseId,name,status,conclusion,headSha", "--limit", "100")
	if err != nil {
		return "", Friendly(err)
	}
	var runs []Run
	if err := json.Unmarshal(out, &runs); err != nil {
		return "", fmt.Errorf("gh run list: %w", err)
	}
	failed := FailedRuns(runs, v.Sha)
	if len(failed) == 0 {
		return "", fmt.Errorf("no failed workflow runs on the head commit %s", short(v.Sha))
	}
	var names, errs []string
	for _, r := range failed {
		if _, err := gh(ctx, "run", "rerun", strconv.FormatInt(r.ID, 10), "--failed", "--repo", repo); err != nil {
			errs = append(errs, r.Name+": "+Friendly(err).Error())
			continue
		}
		names = append(names, r.Name)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("re-run failed: %s", strings.Join(errs, "; "))
	}
	msg := fmt.Sprintf("Re-ran %d failed workflow run%s of %s: %s.", len(names), plural(len(names)), short(v.Sha), strings.Join(names, ", "))
	if len(errs) > 0 {
		msg += " Not re-run: " + strings.Join(errs, "; ")
	}
	return msg, nil
}

func short(sha string) string { return sha[:min(7, len(sha))] }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Details caches PR details for TTL (default 30 s) per URL.
type Details struct {
	Gh  Runner
	TTL time.Duration
	Now func() time.Time

	mu sync.Mutex
	m  map[string]Detail
}

// Get returns the cached detail of url, or reads it with its review threads.
// A failure to read the threads keeps the rest (ThreadsError).
func (c *Details) Get(ctx context.Context, url string, fresh bool) (Detail, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	ttl := c.TTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	c.mu.Lock()
	d, ok := c.m[url]
	c.mu.Unlock()
	if ok && !fresh && now().Sub(d.FetchedAt) < ttl {
		return d, nil
	}
	d, err := FetchDetail(ctx, c.Gh, url)
	if err != nil {
		return Detail{}, err
	}
	if th, err := FetchThreads(ctx, c.Gh, d.Repo, d.Number); err != nil {
		d.ThreadsError = err.Error()
	} else {
		d.Threads = th
	}
	d.FetchedAt = now()
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]Detail{}
	}
	c.m[url] = d
	c.mu.Unlock()
	return d, nil
}

// Forget drops the cached detail of url (after a merge or a re-run).
func (c *Details) Forget(url string) {
	c.mu.Lock()
	delete(c.m, url)
	c.mu.Unlock()
}
