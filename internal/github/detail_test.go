package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// scripted answers gh calls by their first arguments and records them.
type scripted struct {
	calls [][]string
	out   map[string]string // key: the first three args joined by space
	err   map[string]error
}

func (s *scripted) run(_ context.Context, args ...string) ([]byte, error) {
	s.calls = append(s.calls, args)
	k := strings.Join(args[:min(3, len(args))], " ")
	for p, e := range s.err {
		if strings.HasPrefix(k, p) {
			return nil, e
		}
	}
	for p, o := range s.out {
		if strings.HasPrefix(k, p) {
			return []byte(o), nil
		}
	}
	return []byte("{}"), nil
}

func (s *scripted) count(prefix string) int {
	n := 0
	for _, c := range s.calls {
		if strings.HasPrefix(strings.Join(c, " "), prefix) {
			n++
		}
	}
	return n
}

const viewJSON = `{
 "number": 88, "url": "https://github.com/acme/app/pull/88", "title": "Listing cards v2", "body": "Adds **cards**.",
 "state": "OPEN", "isDraft": false, "author": {"login": "octo"}, "headRefName": "fix/cards", "baseRefName": "main",
 "headRefOid": "abc1234def", "additions": 120, "deletions": 18, "changedFiles": 4,
 "labels": [{"name": "bug", "color": "d73a4a"}],
 "mergeable": "MERGEABLE", "mergeStateStatus": "BLOCKED", "reviewDecision": "REVIEW_REQUIRED",
 "latestReviews": [{"author": {"login": "anna"}, "state": "CHANGES_REQUESTED", "submittedAt": "2026-11-14T08:00:00Z"}],
 "statusCheckRollup": [
  {"__typename": "CheckRun", "name": "lint", "workflowName": "CI", "status": "COMPLETED", "conclusion": "FAILURE",
   "startedAt": "2026-11-14T08:00:00Z", "completedAt": "2026-11-14T08:01:30Z", "detailsUrl": "https://github.com/acme/app/actions/runs/1/job/11"},
  {"__typename": "CheckRun", "name": "e2e", "workflowName": "CI", "status": "IN_PROGRESS", "conclusion": "",
   "startedAt": "2026-11-14T08:00:00Z", "completedAt": "0001-01-01T00:00:00Z", "detailsUrl": "https://github.com/acme/app/actions/runs/1/job/12"},
  {"__typename": "CheckRun", "name": "docs", "workflowName": "CI", "status": "COMPLETED", "conclusion": "SKIPPED"},
  {"__typename": "StatusContext", "context": "vercel", "state": "SUCCESS", "targetUrl": "https://vercel.com/x", "startedAt": "2026-11-14T08:00:00Z"},
  {"__typename": "StatusContext", "context": "codecov", "state": "ERROR", "targetUrl": "https://codecov.io/x"},
  {"__typename": "StatusContext", "context": "deploy", "state": "PENDING"}
 ]
}`

// SPEC 0.5 V0.43: the PR detail maps gh pr view, CheckRuns and commit statuses alike.
func TestFetchDetailMapsView(t *testing.T) {
	s := &scripted{out: map[string]string{"pr view": viewJSON}}
	d, err := FetchDetail(context.Background(), s.run, "https://github.com/acme/app/pull/88")
	if err != nil {
		t.Fatal(err)
	}
	if d.Repo != "acme/app" || d.Number != 88 || d.Author != "octo" || d.Head != "fix/cards" || d.Base != "main" || d.HeadSha != "abc1234def" {
		t.Fatalf("header %+v", d)
	}
	if d.Additions != 120 || d.Deletions != 18 || d.ChangedFiles != 4 || len(d.Labels) != 1 || d.Labels[0].Name != "bug" {
		t.Fatalf("size/labels %+v", d)
	}
	if d.MergeState != "BLOCKED" || d.Mergeable != "MERGEABLE" || d.ReviewDecision != "REVIEW_REQUIRED" {
		t.Fatalf("merge %+v", d)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].Author != "anna" || d.Reviews[0].State != "CHANGES_REQUESTED" {
		t.Fatalf("reviews %+v", d.Reviews)
	}
	want := map[string]string{"lint": "fail", "e2e": "running", "docs": "skipped", "vercel": "pass", "codecov": "fail", "deploy": "running"}
	if len(d.Checks) != len(want) {
		t.Fatalf("checks %+v", d.Checks)
	}
	for _, c := range d.Checks {
		if want[c.Name] != c.State {
			t.Errorf("check %s: state %q, want %q", c.Name, c.State, want[c.Name])
		}
	}
	lint := d.Checks[0]
	if lint.Workflow != "CI" || lint.URL != "https://github.com/acme/app/actions/runs/1/job/11" || lint.CompletedAt == nil || lint.CompletedAt.Sub(*lint.StartedAt) != 90*time.Second {
		t.Fatalf("lint %+v", lint)
	}
	if d.Checks[1].CompletedAt != nil {
		t.Fatal("a zero completedAt must be absent")
	}
	if d.Checks[3].URL != "https://vercel.com/x" {
		t.Fatalf("status context url %+v", d.Checks[3])
	}
}

const threadsJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
 {"isResolved": false, "isOutdated": false, "path": "web/src/Card.tsx", "line": 42, "originalLine": 40,
  "comments": {"nodes": [{"author": {"login": "anna"}, "body": "Use the token here.", "createdAt": "2026-11-14T08:00:00Z", "url": "https://github.com/acme/app/pull/88#discussion_r1"},
                         {"author": {"login": "octo"}, "body": "Will do.", "createdAt": "2026-11-14T08:05:00Z", "url": "https://github.com/acme/app/pull/88#discussion_r2"}]}},
 {"isResolved": true, "isOutdated": false, "path": "a.go", "line": 1, "comments": {"nodes": [{"author": {"login": "x"}, "body": "done", "createdAt": "2026-11-14T08:00:00Z"}]}},
 {"isResolved": false, "isOutdated": true, "path": "web/src/Grid.tsx", "line": null, "originalLine": 7,
  "comments": {"nodes": [{"author": null, "body": "Ghost comment", "createdAt": "2026-11-14T08:00:00Z", "url": "u3"}]}}
]}}}}}`

// SPEC 0.5 V0.43: only unresolved review threads; an outdated thread keeps its original line.
func TestFetchThreadsUnresolved(t *testing.T) {
	s := &scripted{out: map[string]string{"api graphql": threadsJSON}}
	ts, err := FetchThreads(context.Background(), s.run, "acme/app", 88)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 {
		t.Fatalf("threads %+v", ts)
	}
	if ts[0].Path != "web/src/Card.tsx" || ts[0].Line != 42 || ts[0].Outdated || len(ts[0].Comments) != 2 || ts[0].Comments[0].Author != "anna" || ts[0].URL != "https://github.com/acme/app/pull/88#discussion_r1" {
		t.Fatalf("first %+v", ts[0])
	}
	if ts[1].Line != 7 || !ts[1].Outdated || ts[1].Comments[0].Author != "ghost" {
		t.Fatalf("second %+v", ts[1])
	}
	q := strings.Join(s.calls[0], " ")
	if !strings.Contains(q, "owner=acme") || !strings.Contains(q, "name=app") || !strings.Contains(q, "number=88") || !strings.Contains(q, "reviewThreads(first: 50)") {
		t.Fatalf("query %s", q)
	}
}

// SPEC 0.5 V0.43: the detail is cached 30 s; fresh fetches again; threads failing keeps the rest.
func TestDetailsCache(t *testing.T) {
	s := &scripted{out: map[string]string{"pr view": viewJSON}, err: map[string]error{"api graphql": errors.New("HTTP 502")}}
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	c := &Details{Gh: s.run, Now: func() time.Time { return now }}
	url := "https://github.com/acme/app/pull/88"
	d, err := c.Get(context.Background(), url, false)
	if err != nil || d.ThreadsError == "" || len(d.Checks) != 6 || !d.FetchedAt.Equal(now) {
		t.Fatalf("first %v %+v", err, d)
	}
	c.Get(context.Background(), url, false)
	if s.count("pr view") != 1 {
		t.Fatalf("cached call ran gh again: %d", s.count("pr view"))
	}
	now = now.Add(31 * time.Second)
	c.Get(context.Background(), url, false)
	c.Get(context.Background(), url, true)
	if s.count("pr view") != 3 {
		t.Fatalf("expired and fresh: %d gh calls, want 3", s.count("pr view"))
	}
	c.Forget(url)
	c.Get(context.Background(), url, false)
	if s.count("pr view") != 4 {
		t.Fatal("Forget did not drop the cache")
	}
	bad := &Details{Gh: (&scripted{err: map[string]error{"pr view": errors.New("gh pr: exit status 4: To get started with GitHub CLI, please run:  gh auth login")}}).run}
	if _, err := bad.Get(context.Background(), url, false); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("auth error %v", err)
	}
}

// SPEC 0.5 V0.43: the reasons the host refuses a merge.
func TestMergeBlock(t *testing.T) {
	cases := []struct {
		d    Detail
		want string
	}{
		{Detail{State: "OPEN", MergeState: "CLEAN"}, ""},
		{Detail{State: "OPEN", MergeState: "UNSTABLE"}, ""},
		{Detail{State: "OPEN", MergeState: "UNKNOWN"}, ""},
		{Detail{State: "MERGED"}, "already merged"},
		{Detail{State: "CLOSED"}, "closed"},
		{Detail{State: "OPEN", Draft: true}, "draft"},
		{Detail{State: "OPEN", Mergeable: "CONFLICTING", Base: "main"}, "Conflicts with main"},
		{Detail{State: "OPEN", MergeState: "DIRTY", Base: "main"}, "Conflicts with main"},
		{Detail{State: "OPEN", MergeState: "BEHIND", Base: "main"}, "Behind main"},
		{Detail{State: "OPEN", MergeState: "BLOCKED", Checks: []Check{{State: "fail"}}}, "Blocked: 1 failing check"},
		{Detail{State: "OPEN", MergeState: "BLOCKED", ReviewDecision: "REVIEW_REQUIRED"}, "Review required"},
		{Detail{State: "OPEN", MergeState: "BLOCKED"}, "Blocked"},
	}
	for i, c := range cases {
		got := MergeBlock(c.d)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("case %d: %q, want %q", i, got, c.want)
		}
	}
}

// SPEC 0.5 V0.43: the owner's merge reads the PR first and never runs gh pr merge when it is blocked.
func TestMergeCheckedRefuses(t *testing.T) {
	s := &scripted{out: map[string]string{"pr view": viewJSON}}
	if _, err := MergeChecked(context.Background(), s.run, "https://github.com/acme/app/pull/88", "squash"); err == nil || !strings.Contains(err.Error(), "failing check") {
		t.Fatalf("blocked merge: %v", err)
	}
	if s.count("pr merge") != 0 {
		t.Fatal("gh pr merge ran on a blocked PR")
	}
	ok := &scripted{out: map[string]string{"pr view": `{"state":"OPEN","mergeStateStatus":"CLEAN","mergeable":"MERGEABLE"}`, "pr merge": "✓ Squashed and merged"}}
	out, err := MergeChecked(context.Background(), ok.run, "https://github.com/acme/app/pull/88", "rebase")
	if err != nil || !strings.Contains(out, "merged") || ok.count("pr merge --rebase -- https://github.com/acme/app/pull/88") != 1 {
		t.Fatalf("clean merge %q %v %v", out, err, ok.calls)
	}
	if _, err := MergeChecked(context.Background(), ok.run, "https://github.com/acme/app/pull/88", "--admin"); err == nil {
		t.Fatal("an unknown method was accepted")
	}
}

// SPEC 0.5 V0.43: re-run selects only failed runs of the head commit.
func TestFailedRunsSelection(t *testing.T) {
	runs := []Run{
		{ID: 1, Name: "CI", Status: "completed", Conclusion: "failure", HeadSha: "abc"},
		{ID: 2, Name: "e2e", Status: "completed", Conclusion: "timed_out", HeadSha: "abc"},
		{ID: 3, Name: "lint", Status: "completed", Conclusion: "success", HeadSha: "abc"},
		{ID: 4, Name: "CI", Status: "in_progress", Conclusion: "", HeadSha: "abc"},
		{ID: 5, Name: "CI", Status: "completed", Conclusion: "failure", HeadSha: "old"},
		{ID: 6, Name: "deploy", Status: "completed", Conclusion: "cancelled", HeadSha: "abc"},
		{ID: 7, Name: "docs", Status: "completed", Conclusion: "skipped", HeadSha: "abc"},
	}
	got := FailedRuns(runs, "abc")
	ids := []int64{}
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 6 {
		t.Fatalf("selected %v", ids)
	}
}

// SPEC 0.5 V0.43: re-run calls gh run rerun --failed once per failed run.
func TestRerunFailedRunsGh(t *testing.T) {
	s := &scripted{out: map[string]string{
		"pr view":  `{"headRefOid":"abc"}`,
		"run list": `[{"databaseId":1,"name":"CI","status":"completed","conclusion":"failure","headSha":"abc"},{"databaseId":3,"name":"lint","status":"completed","conclusion":"success","headSha":"abc"}]`,
	}}
	out, err := RerunFailed(context.Background(), s.run, "https://github.com/acme/app/pull/88")
	if err != nil || !strings.Contains(out, "CI") {
		t.Fatalf("%q %v", out, err)
	}
	if s.count("run rerun 1 --failed --repo acme/app") != 1 || s.count("run rerun 3") != 0 {
		t.Fatalf("calls %v", s.calls)
	}
	if s.count("run list --repo acme/app --commit abc") != 1 {
		t.Fatalf("run list not scoped to the head commit: %v", s.calls)
	}
	none := &scripted{out: map[string]string{"pr view": `{"headRefOid":"abc"}`, "run list": `[]`}}
	if _, err := RerunFailed(context.Background(), none.run, "https://github.com/acme/app/pull/88"); err == nil {
		t.Fatal("no failed runs must be an error")
	}
}
