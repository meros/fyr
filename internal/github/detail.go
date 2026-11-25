package github

// The PR tab's detail, the owner's merge and the re-run of failed checks
// (SPEC 0.5 V0.43).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
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

// Counts reports whether a check gates a merge (V0.advisory-checks): every
// check does but an advisory one, whose name ends in "(advisory)". A
// self-hosted runner that is asleep keeps such a check queued for hours, and
// GitHub ends no queued job by itself. Every place that decides whether CI
// passed for a merge reads the checks through it: the PR's checks (fromView,
// for the poll, the merge ask and auto-merge), the detail's summary and the
// host's last look before it merges (MergeBlock). The detail still lists an
// advisory check with its state.
func Counts(name string) bool {
	return !strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "(advisory)")
}

// gating is the detail's checks that gate a merge (Gating).
func (d Detail) gating() []Check { return Gating(d.Checks) }

// Gating is the checks of one head that gate a merge: every check but an
// advisory one (Counts), a cancelled run that a newer run of the same check
// replaced (V0.ci-superseded-checks), and one of a fallback pair that the
// other settled (localFallback). Every place that decides whether CI passed
// reads the checks through it: the poll and the merge ask (fromView), the
// detail's summary and the host's last look before it merges (MergeBlock).
func Gating(cs []Check) []Check {
	out := make([]Check, 0, len(cs))
	for i, c := range cs {
		if Counts(c.Name) && !superseded(cs, i) && !localFallback(cs, i) {
			out = append(out, c)
		}
	}
	return out
}

// superseded reports whether cs[i] is a cancelled run that a newer run of
// the same check (same name, same workflow) on the head replaced: marking a
// PR ready, a re-run or a second event starts the workflow again, and its
// concurrency group cancels the older run, which GitHub keeps in the
// rollup. A run with no start time is replaced by any run that was not
// cancelled.
func superseded(cs []Check, i int) bool {
	c := cs[i]
	if !strings.EqualFold(c.Conclusion, "CANCELLED") {
		return false
	}
	for j, o := range cs {
		if j == i || o.Name != c.Name || o.Workflow != c.Workflow {
			continue
		}
		if c.StartedAt != nil && o.StartedAt != nil {
			if o.StartedAt.After(*c.StartedAt) {
				return true
			}
		} else if !strings.EqualFold(o.Conclusion, "CANCELLED") {
			return true
		}
	}
	return false
}

// LocalPrefix names a commit status that stands in for a check of the same
// name (scripts/ci-local.sh posts local/test for the runner's test).
const LocalPrefix = "local/"

// localFallback reports whether cs[i] is one of a pair "X" (the runner's
// check) and "local/X" (the same gate run on a host, a fallback) that does
// not count because the other one settled it (V0.ci-superseded-checks):
// the runner's X passed, so local/X does not count, whatever it says (a red
// local gate is load on that host); or local/X passed while X has not
// finished (the runner is down, the job stays queued), so X does not count.
// A failed X always counts: a red runner check is fixed or re-run there,
// not outvoted by a host.
func localFallback(cs []Check, i int) bool {
	c := cs[i]
	if name, ok := strings.CutPrefix(c.Name, LocalPrefix); ok {
		for _, o := range cs {
			if o.Name == name && o.State == "pass" {
				return true
			}
		}
		return false
	}
	if c.State != checkRunning {
		return false
	}
	for _, o := range cs {
		if o.Name == LocalPrefix+c.Name && o.State == "pass" {
			return true
		}
	}
	return false
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
		gating := d.gating()
		states := make([]string, len(gating))
		for i, c := range gating {
			states[i] = c.State
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
	gating := d.gating()
	fails := 0
	for _, c := range gating {
		if c.State == "fail" {
			fails++
		}
	}
	switch {
	case d.MergeState == "BLOCKED" && fails == 1:
		return "Blocked: 1 failing check"
	case d.MergeState == "BLOCKED" && fails > 1:
		return fmt.Sprintf("Blocked: %d failing checks", fails)
	case d.Checked == core.ChecksPending || (d.Checked == "" && len(gating) == 0):
		// V0.112: a merge waits for CI; no check reported is not green
		// (a detail that was not summed up, with no checks, neither).
		return waitCI(d)
	}
	for _, c := range gating { // a detail that was not summed up
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
	if len(d.gating()) == 0 {
		return "No check has reported on " + on + " yet: Merge waits for CI"
	}
	return "CI has not passed on " + on + " yet: Merge waits for the checks"
}

var methods = map[string]bool{"squash": true, "merge": true, "rebase": true}

// MergeChecked reads the PR again and merges it unless MergeBlock refuses
// or its head is not head, the commit the owner saw (V0.97).
//
// V0.merge-github-trouble: when GitHub's own trouble (a 5xx, a lost
// connection) answers the merge or the read, the merge may still have
// happened. MergeChecked waits, reads the PR again, and takes a merged PR as
// the merge's success; an open one is merged again. Only when GitHub fails
// every try does it fail, with GitHubFailed. Each try is one read and at most
// one merge, after a wait of seconds: no tight loop on GitHub's API.
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
	var trouble error // GitHub's last passing failure, nil when none
	moved, failed := 0, 0
	wait := func(d time.Duration) bool {
		select {
		case <-time.After(d):
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		d, err := FetchDetail(ctx, gh, url)
		if err != nil {
			if !GitHubTrouble(err) {
				return "", err
			}
			trouble = err
			if failed >= len(troubleBackoff) || !wait(troubleBackoff[failed]) {
				return "", githubFailed(trouble, failed+1)
			}
			failed++
			continue
		}
		if trouble != nil && d.State == "MERGED" {
			// The merge GitHub answered with an error went through.
			return "Merged (GitHub answered with an error, but the PR is merged)", nil
		}
		if !core.SameHead(d.HeadSha, head) {
			return "", errors.New(core.HeadMoved)
		}
		if why := MergeBlock(d); why != "" {
			return "", fmt.Errorf("not merged: %s", why)
		}
		out, err := Merge(ctx, gh, url, method, d.HeadSha)
		switch {
		case err == nil:
			return out, nil
		case GitHubTrouble(err):
			trouble = err
			if failed >= len(troubleBackoff) || !wait(troubleBackoff[failed]) {
				return out, githubFailed(trouble, failed+1)
			}
			failed++
		case strings.Contains(err.Error(), baseModified) && moved < len(mergeBackoff):
			// V0.205: another merge landed first; the same head may still merge
			if !wait(mergeBackoff[moved]) {
				return out, Friendly(err)
			}
			moved++
		default:
			return out, Friendly(err)
		}
	}
}

// githubFailed is the refusal of a merge GitHub's trouble stopped tries
// times (V0.merge-github-trouble), with the first line of its last answer.
func githubFailed(err error, tries int) error {
	line := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	return fmt.Errorf("not merged: %s: GitHub answered with its own error on %d tries, and the PR is still open (%s)", core.GitHubFailed, tries, line)
}

// GitHubTrouble reports an error that is GitHub's own passing trouble
// (V0.merge-github-trouble): a 5xx, GraphQL's "Something went wrong", or a
// connection that failed or timed out. A refusal (a conflict, a rule, a 4xx),
// a rate limit or a failed login is not: trying again does not help those.
func GitHubTrouble(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if troubleStatus.MatchString(s) {
		return true
	}
	for _, w := range troubleWords {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// troubleStatus is a 5xx as gh writes it: "HTTP 502", "status code: 502".
var troubleStatus = regexp.MustCompile(`(?:http|status code:?)\s+5\d\d\b`)

// troubleWords name GitHub's passing trouble when no status is given.
var troubleWords = []string{
	"something went wrong while executing your query",
	"bad gateway", "service unavailable", "gateway timeout", "internal server error",
	"connection reset", "connection refused", "i/o timeout", "tls handshake timeout",
	"unexpected eof", "server closed", ": eof",
}

// troubleBackoff is the wait before each further read after GitHub's
// trouble (V0.merge-github-trouble): 4 tries over about 50 s, so a short
// outage passes and a long one fails within a minute.
var troubleBackoff = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}

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
