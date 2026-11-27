package core

// V0.thread-issues: a thread links the GitHub issues its work is about, the
// way it links its PRs. An issue joins the thread when a PR of the thread
// closes it (GitHub's closing references, read in the PR poll), when the
// agent files it with gh issue create, when the owner's message gives its
// URL, or when the agent links it with issue_link. The poller reads the
// title and state of the open ones.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Issue is one linked GitHub issue of a thread.
type Issue struct {
	URL    string `json:"url"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title,omitempty"`
	// State is OPEN or CLOSED; "" until GitHub was read.
	State string `json:"state,omitempty"`
	// Via is how it joined: IssueViaPR, IssueViaCreated, IssueViaOwner or
	// IssueViaAgent. The first way it joined is kept.
	Via string `json:"via"`
	// PR is the thread's PR that closes it (Via IssueViaPR).
	PR        string     `json:"pr,omitempty"`
	At        time.Time  `json:"at"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
}

const (
	IssueViaPR      = "pr"      // a PR of the thread closes it
	IssueViaCreated = "created" // the agent filed it (gh issue create)
	IssueViaOwner   = "owner"   // the owner's message gave its URL
	IssueViaAgent   = "agent"   // the agent linked it (issue_link)
)

// MaxIssues is the most issues a thread links; one past it is not linked.
const MaxIssues = 20

// issuePageRe is a whole value that is an issue URL, with an optional
// fragment or query (a comment's anchor).
var issuePageRe = regexp.MustCompile(`^https://github\.com/([\w.-]+/[\w.-]+)/issues/(\d+)(?:[?#]\S*)?$`)

// issueInTextRe finds issue URLs inside text (the owner's message).
var issueInTextRe = regexp.MustCompile(`https://github\.com/[\w.-]+/[\w.-]+/issues/\d+\b`)

// ParseIssue splits a GitHub issue URL into "owner/repo" and the number.
func ParseIssue(url string) (repo string, number int, ok bool) {
	m := issuePageRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return m[1], n, true
}

// CanonicalIssue is the issue URL in one form:
// https://github.com/owner/repo/issues/N.
func CanonicalIssue(url string) (string, bool) {
	repo, n, ok := ParseIssue(url)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", repo, n), true
}

// IssueURLsIn are the unique issue URLs in text, canonical, in order.
func IssueURLsIn(text string) []string {
	var out []string
	for _, m := range issueInTextRe.FindAllString(text, -1) {
		if u, ok := CanonicalIssue(m); ok && !containsString(out, u) {
			out = append(out, u)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// issueKey compares issue URLs without case (GitHub's owner and repo names
// are not case sensitive).
func issueKey(url string) string {
	if u, ok := CanonicalIssue(url); ok {
		return strings.ToLower(u)
	}
	return strings.ToLower(url)
}

// FindIssue is the thread's linked issue with url, or nil.
func (t *Thread) FindIssue(url string) *Issue {
	k := issueKey(url)
	for i := range t.Issues {
		if issueKey(t.Issues[i].URL) == k {
			return &t.Issues[i]
		}
	}
	return nil
}

// LinkIssue links is to t, or updates the linked one: a known title and
// state replace the stored ones, and the first way it joined is kept. It
// reports whether anything changed; an issue past MaxIssues is not linked.
func LinkIssue(t *Thread, is Issue, now time.Time) bool {
	u, ok := CanonicalIssue(is.URL)
	if !ok {
		return false
	}
	is.URL = u
	if is.Repo == "" || is.Number == 0 {
		is.Repo, is.Number, _ = ParseIssue(u)
	}
	if x := t.FindIssue(u); x != nil {
		changed := false
		if is.Title != "" && is.Title != x.Title {
			x.Title, changed = is.Title, true
		}
		if is.State != "" && is.State != x.State {
			x.State, changed = is.State, true
		}
		if is.CheckedAt != nil {
			x.CheckedAt = is.CheckedAt
		}
		if x.PR == "" && is.PR != "" {
			x.PR, changed = is.PR, true
		}
		return changed
	}
	if len(t.Issues) >= MaxIssues {
		return false
	}
	if is.At.IsZero() {
		is.At = now
	}
	t.Issues = append(t.Issues, is)
	return true
}

// UnlinkIssue removes the linked issue with url; it reports whether one was.
func UnlinkIssue(t *Thread, url string) bool {
	k := issueKey(url)
	for i := range t.Issues {
		if issueKey(t.Issues[i].URL) == k {
			t.Issues = append(t.Issues[:i], t.Issues[i+1:]...)
			return true
		}
	}
	return false
}

// IssueWatchEvery is how often the poller reads a linked issue it has read
// before; one it has not read is read at the next poll.
const IssueWatchEvery = 10 * time.Minute

// WatchedIssues are the linked issues of t the poller reads: those not
// closed, of a thread that is not settled.
func WatchedIssues(t *Thread) []Issue {
	if t.Settle.At != nil {
		return nil
	}
	var out []Issue
	for _, is := range t.Issues {
		if is.State != "CLOSED" {
			out = append(out, is)
		}
	}
	return out
}

// reduceIssueObserved takes the poller's read of a linked issue.
func reduceIssueObserved(r Rec, in Input, now time.Time) (Result, error) {
	is, err := argsOf[Issue](in)
	if err != nil {
		return Result{}, err
	}
	if r.T.FindIssue(is.URL) == nil {
		return Result{}, nil // unlinked since the poll began
	}
	is.Via = ""
	LinkIssue(r.T, is, now)
	return Result{}, nil
}

// IssueLinkArgs are the agent's issue_link.
type IssueLinkArgs struct {
	URL    string
	Unlink bool
}

// reduceIssueLink is the agent's issue_link: it links the issue, or with
// Unlink removes it.
func reduceIssueLink(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[IssueLinkArgs](in)
	if err != nil {
		return Result{}, err
	}
	u, ok := CanonicalIssue(a.URL)
	if !ok {
		return Result{}, &Rejection{Code: RejectBadArgs, Reason: "not a GitHub issue URL (https://github.com/owner/repo/issues/N)"}
	}
	if a.Unlink {
		if !UnlinkIssue(r.T, u) {
			return Result{}, nil
		}
		return Result{Events: []Event{{"issue.unlinked", map[string]any{"url": u}}}}, nil
	}
	if r.T.FindIssue(u) != nil {
		return Result{}, nil
	}
	if !LinkIssue(r.T, Issue{URL: u, Via: IssueViaAgent}, now) {
		return Result{}, &Rejection{Code: RejectBadArgs, Reason: fmt.Sprintf("a thread links at most %d issues", MaxIssues)}
	}
	return Result{Events: []Event{{"issue.linked", map[string]any{"url": u, "via": IssueViaAgent}}}}, nil
}

// linkIssuesOf links the issues a PR read names as closed by it.
func linkIssuesOf(t *Thread, p PR, now time.Time) {
	if c := changeByPR(t, p.URL); c != nil && c.Dismissed {
		return
	}
	for _, is := range p.Closes {
		is.Via, is.PR = IssueViaPR, p.URL
		if is.CheckedAt == nil {
			is.CheckedAt = p.CheckedAt
		}
		LinkIssue(t, is, now)
	}
}

func init() {
	RegisterKind(&InputKind{Name: KindIssueObserved, From: observed, Untouched: true, Reduce: reduceIssueObserved})
	RegisterKind(&InputKind{Name: KindIssueLink, From: agentOnly, Reduce: reduceIssueLink})
}
