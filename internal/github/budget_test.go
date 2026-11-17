package github

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// counting answers every call with ok and counts what reached GitHub.
type counting struct {
	calls []string
	out   func(args []string) ([]byte, error)
}

func (c *counting) run(_ context.Context, args ...string) ([]byte, error) {
	c.calls = append(c.calls, strings.Join(args, " "))
	if c.out != nil {
		return c.out(args)
	}
	return []byte("[]"), nil
}

var at0 = time.Date(2026, 11, 17, 20, 0, 0, 0, time.UTC)

// V0.178: while the account is low, polls wait for the reset and say so
// once; the updater and merges still run.
func TestBudgetDefersPollsBeforeTheUpdater(t *testing.T) {
	now := at0
	b := &Budget{Now: func() time.Time { return now }}
	b.SetRate(Rate{Limit: 5000, Remaining: 120, Reset: at0.Add(30 * time.Minute), GraphQL: 4000})
	c := &counting{}
	poll, act := b.Runner(c.run, Poll), b.Runner(c.run, Act)
	for i := 0; i < 5; i++ {
		if _, err := poll(context.Background(), "api", "repos/o/r/deployments?sha=m"); !errors.Is(err, ErrBudget) {
			t.Fatalf("poll %d: %v", i, err)
		}
	}
	if _, err := act(context.Background(), "api", "repos/meros/fyr/git/ref/tags/deployable"); err != nil {
		t.Fatal(err)
	}
	if len(c.calls) != 1 || !strings.Contains(c.calls[0], "deployable") {
		t.Fatalf("calls %v", c.calls)
	}
	p := b.Paused()
	if p == nil || p.Resource != "core" || !p.Until.Equal(at0.Add(30*time.Minute)) || p.Deferred != 5 || p.Remaining != 120 {
		t.Fatalf("pause %+v", p)
	}
	// GraphQL has its own limit: a PR read still runs.
	if _, err := poll(context.Background(), "pr", "view", "--", "https://github.com/o/r/pull/1"); err != nil {
		t.Fatal(err)
	}
	now = at0.Add(31 * time.Minute)
	if b.Paused() != nil {
		t.Fatal("still paused after the reset")
	}
	if _, err := poll(context.Background(), "api", "repos/o/r/deployments?sha=m"); err != nil {
		t.Fatal(err)
	}
}

// V0.178: a refusal for the rate limit stops the polls until the reset:
// one failed call, not a storm.
func TestBudgetRateLimitedStopsTheStorm(t *testing.T) {
	now := at0
	b := &Budget{Now: func() time.Time { return now }}
	b.SetRate(Rate{Limit: 5000, Remaining: 4000, Reset: at0.Add(40 * time.Minute), GraphQL: 4000})
	c := &counting{out: func([]string) ([]byte, error) {
		return []byte(`{"message":"API rate limit exceeded for user ID 1."}`), errors.New("gh api: exit status 1: API rate limit exceeded for user ID 1. (HTTP 403)")
	}}
	poll := b.Runner(c.run, Poll)
	for i := 0; i < 10; i++ {
		poll(context.Background(), "api", "repos/o/r/commits/abc1234/check-runs")
	}
	if len(c.calls) != 1 {
		t.Fatalf("calls %d", len(c.calls))
	}
	if p := b.Paused(); p == nil || !p.Until.Equal(at0.Add(40*time.Minute)) {
		t.Fatalf("pause %+v", p)
	}
	// A secondary limit names no reset: polls wait five minutes.
	b2 := &Budget{Now: func() time.Time { return now }}
	c2 := &counting{out: func([]string) ([]byte, error) {
		return nil, errors.New("gh: You have exceeded a secondary rate limit. (HTTP 403)")
	}}
	p2 := b2.Runner(c2.run, Poll)
	p2(context.Background(), "api", "repos/o/r/x")
	p2(context.Background(), "api", "repos/o/r/x")
	if len(c2.calls) != 1 || b2.Paused() == nil || !b2.Paused().Until.Equal(at0.Add(5*time.Minute)) {
		t.Fatalf("secondary: %d calls, %+v", len(c2.calls), b2.Paused())
	}
}

// V0.178: poll REST calls keep a pace (30 a minute by default).
func TestBudgetPacesPolls(t *testing.T) {
	now := at0
	b := &Budget{Now: func() time.Time { return now }, PollsPerMinute: 10}
	c := &counting{}
	poll := b.Runner(c.run, Poll)
	ok := 0
	for i := 0; i < 25; i++ {
		if _, err := poll(context.Background(), "api", "repos/o/r/x"); err == nil {
			ok++
		}
	}
	if ok != 10 {
		t.Fatalf("burst: %d", ok)
	}
	now = now.Add(30 * time.Second)
	ok = 0
	for i := 0; i < 25; i++ {
		if _, err := poll(context.Background(), "api", "repos/o/r/x"); err == nil {
			ok++
		}
	}
	if ok != 5 || b.Paused() != nil {
		t.Fatalf("after 30 s: %d, pause %+v", ok, b.Paused())
	}
}

// V0.178: the rate headers of gh api -i keep the budget current between
// reads of rate_limit.
func TestBudgetReadsRateHeaders(t *testing.T) {
	now := at0
	b := &Budget{Now: func() time.Time { return now }}
	reset := at0.Add(20 * time.Minute)
	c := &counting{out: func([]string) ([]byte, error) {
		return []byte("HTTP/2.0 200 OK\r\nX-Ratelimit-Remaining: 99\r\nX-Ratelimit-Reset: " + itoa(reset.Unix()) + "\r\nX-Ratelimit-Resource: core\r\n\r\n[]"), nil
	}}
	poll := b.Runner(c.run, Poll)
	poll(context.Background(), "api", "-i", "repos/o/r/x")
	if _, err := poll(context.Background(), "api", "-i", "repos/o/r/x"); !errors.Is(err, ErrBudget) {
		t.Fatalf("not held: %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// V0.178: three PRs of one repo are one GraphQL call; a PR the batch did
// not return is read alone.
func TestPollerBatchesAReposPRs(t *testing.T) {
	batch := `{"data":{"repository":{
	  "p0":{"state":"OPEN","mergeable":"MERGEABLE","reviewDecision":null,"title":"one","headRefName":"a","headRefOid":"h1","baseRefName":"main","isDraft":false,"mergeStateStatus":"CLEAN","mergedAt":null,"mergeCommit":null,"autoMergeRequest":null,
	    "commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[{"__typename":"CheckRun","name":"go","status":"COMPLETED","conclusion":"SUCCESS"}]}}}}]}},
	  "p1":{"state":"MERGED","mergeable":"UNKNOWN","reviewDecision":"APPROVED","title":"two","headRefName":"b","headRefOid":"h2","baseRefName":"main","isDraft":false,"mergeStateStatus":"UNKNOWN","mergedAt":"2026-11-17T19:00:00Z","mergeCommit":{"oid":"m2"},"autoMergeRequest":null,
	    "commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[{"__typename":"StatusContext","context":"ci","state":"PENDING"}]}}}}]}},
	  "p2":null}},"errors":[{"message":"Could not resolve to a PullRequest with the number of 3."}]}`
	c := &counting{out: func(args []string) ([]byte, error) {
		j := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(j, "api graphql") && strings.Contains(j, "p2: pullRequest(number: 3)"):
			return []byte(batch), errors.New("exit status 1")
		case strings.HasPrefix(j, "pr view"):
			return []byte("GraphQL: Could not resolve to a PullRequest with the number of 3."), errors.New("gh pr view: Could not resolve to a PullRequest")
		case strings.Contains(j, "repos/o/r") && strings.Contains(j, "default_branch"):
			return []byte("main\n"), nil
		}
		return nil, errors.New("unexpected " + j)
	}}
	src := &prSrc{ts: []PRThread{{ID: "t1", URL: "https://github.com/o/r/pull/1"}, {ID: "t2", URL: "https://github.com/o/r/pull/2"}, {ID: "t3", URL: "https://github.com/o/r/pull/3"}}}
	p := &Poller{Gh: c.run, Src: src}
	p.pollAll(context.Background())
	gql, views := 0, 0
	for _, x := range c.calls {
		if strings.HasPrefix(x, "api graphql") {
			gql++
		}
		if strings.HasPrefix(x, "pr view") {
			views++
		}
	}
	if gql != 1 || views != 1 {
		t.Fatalf("graphql %d, pr view %d: %v", gql, views, c.calls)
	}
	one, two := src.got["t1"], src.got["t2"]
	if one.Title != "one" || one.Checks != core.ChecksPass || one.Head != "h1" || one.State != "OPEN" {
		t.Fatalf("one %+v", one)
	}
	if two.State != "MERGED" || two.MergeSHA != "m2" || two.MergedAt == nil || two.Checks != core.ChecksPending || two.Review != "APPROVED" {
		t.Fatalf("two %+v", two)
	}
	if _, ok := src.got["t3"]; ok {
		t.Fatal("a missing PR is ended only on the second answer (V0.72)")
	}
}

type prSrc struct {
	ts  []PRThread
	got map[string]core.PR
}

func (s *prSrc) Snapshot() []PRThread { return s.ts }
func (s *prSrc) Observe(id string, pr core.PR) {
	if s.got == nil {
		s.got = map[string]core.PR{}
	}
	s.got[id] = pr
}
