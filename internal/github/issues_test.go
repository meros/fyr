package github

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.thread-issues: the batched PR read carries the issues the PR closes.
func TestBatchViewClosingIssues(t *testing.T) {
	const pr = `{"state":"OPEN","headRefOid":"h1","closingIssuesReferences":{"nodes":[
	  {"url":"https://github.com/acme/app/issues/4","number":4,"title":"Crash","state":"OPEN"},
	  {"url":"https://github.com/acme/app/pull/5","number":5,"title":"not an issue","state":"OPEN"}]},
	  "commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[{"__typename":"StatusContext","context":"ci","state":"SUCCESS"}]}}}}]}}`
	var b batchPR
	if err := json.Unmarshal([]byte(pr), &b); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(b.view())
	p, _, err := fromView(context.Background(), fakeGh(""), "https://github.com/acme/app/pull/1", "acme/app", 1, out, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Closes) != 1 || p.Closes[0].URL != "https://github.com/acme/app/issues/4" || p.Closes[0].Title != "Crash" || p.Closes[0].State != "OPEN" || p.Closes[0].CheckedAt == nil {
		t.Fatalf("closes %+v", p.Closes)
	}
	if !strings.Contains(prFields, "closingIssuesReferences") {
		t.Fatal("the batched query does not read closing issues")
	}
}

type issueSrc struct {
	ts  []IssueThread
	got []core.Issue
}

func (s *issueSrc) Issues() []IssueThread { return s.ts }
func (s *issueSrc) ObserveIssue(_ string, is core.Issue) {
	s.got = append(s.got, is)
}

// V0.thread-issues: the issues of one repo are read in one query, reported
// only when they changed, and read again only after IssueWatchEvery.
func TestPollIssues(t *testing.T) {
	calls := 0
	var query string
	gh := func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		query = strings.Join(args, " ")
		return []byte(`{"data":{"repository":{
		  "i0":{"url":"https://github.com/acme/app/issues/4","number":4,"title":"Crash","state":"CLOSED"},
		  "i1":{"url":"https://github.com/acme/app/issues/6","number":6,"title":"Leak","state":"OPEN"},
		  "i2":null}}}`), nil
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	p := &Poller{Gh: gh, Now: func() time.Time { return now }}
	src := &issueSrc{ts: []IssueThread{
		{ID: "t1", URL: "https://github.com/acme/app/issues/4", State: "OPEN"},
		{ID: "t1", URL: "https://github.com/acme/app/issues/6", Title: "Leak", State: "OPEN"},
		{ID: "t2", URL: "https://github.com/acme/app/issues/99"},
	}}
	p.pollIssues(context.Background(), src)
	if calls != 1 || !strings.Contains(query, "i0: issue(number: 4)") || !strings.Contains(query, "i2: issue(number: 99)") {
		t.Fatalf("%d calls, query %s", calls, query)
	}
	if len(src.got) != 1 || src.got[0].URL != "https://github.com/acme/app/issues/4" || src.got[0].State != "CLOSED" {
		t.Fatalf("observed %+v", src.got)
	}
	now = now.Add(core.IssueWatchEvery / 2)
	p.pollIssues(context.Background(), src)
	if calls != 1 {
		t.Fatal("read again before IssueWatchEvery")
	}
	now = now.Add(core.IssueWatchEvery)
	p.pollIssues(context.Background(), src)
	if calls != 2 {
		t.Fatalf("not read again after IssueWatchEvery: %d", calls)
	}
}
