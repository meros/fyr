package github

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func fakeGh(out string) Runner {
	return func(_ context.Context, args ...string) ([]byte, error) { return []byte(out), nil }
}

// SPEC 0.5 V0.8, 12.5: PR snapshot from gh pr view.
func TestFetchParsesChecks(t *testing.T) {
	cases := []struct {
		json, checks string
		conflict     bool
		state        string
	}{
		{`{"state":"OPEN","mergeable":"MERGEABLE","reviewDecision":"","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]}`, "pass", false, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]}`, "fail", false, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""}]}`, "pending", false, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"StatusContext","state":"ERROR"}]}`, "fail", false, "OPEN"},
		{`{"state":"OPEN","mergeable":"CONFLICTING","statusCheckRollup":[]}`, "none", true, "OPEN"},
		{`{"state":"MERGED","statusCheckRollup":null}`, "none", false, "MERGED"},
	}
	for i, c := range cases {
		p, err := Fetch(context.Background(), fakeGh(c.json), "https://github.com/a/b/pull/1")
		if err != nil {
			t.Fatal(err)
		}
		if string(p.Checks) != c.checks || p.Conflict != c.conflict || p.State != c.state || p.Repo != "a/b" || p.Number != 1 {
			t.Errorf("case %d: %+v", i, p)
		}
	}
}

// SPEC 5.10: Fyr merges with the method the agent asked for.
func TestMergeRunsGh(t *testing.T) {
	var got []string
	r := func(_ context.Context, args ...string) ([]byte, error) {
		got = args
		return []byte("✓ Squashed and merged"), nil
	}
	out, err := Merge(context.Background(), r, "https://github.com/a/b/pull/9", "squash", "abc1234")
	if err != nil || !strings.Contains(out, "merged") {
		t.Fatal(out, err)
	}
	if strings.Join(got, " ") != "pr merge --squash --match-head-commit abc1234 -- https://github.com/a/b/pull/9" {
		t.Fatalf("args %v", got)
	}
	if _, err := Merge(context.Background(), r, "https://evil/x", "squash", "abc1234"); err == nil {
		t.Fatal("non-PR URL accepted")
	}
}

// review 2026-11-15: gh gets the canonical PR URL after "--", so nothing
// in a stored URL can be read as a gh flag.
func TestGhPRArgsEndFlags(t *testing.T) {
	s := &scripted{out: map[string]string{"pr view": `{"state":"OPEN","headRefOid":"abc"}`}}
	url := "https://github.com/a/b/pull/9/files#x"
	if _, err := Fetch(context.Background(), s.run, url); err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(context.Background(), s.run, url, "squash", "abc1234"); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchDetail(context.Background(), s.run, url); err != nil {
		t.Fatal(err)
	}
	RerunFailed(context.Background(), s.run, url)
	if len(s.calls) < 4 {
		t.Fatalf("calls %v", s.calls)
	}
	for _, c := range s.calls {
		if c[0] != "pr" {
			continue
		}
		n := len(c)
		if n < 2 || c[n-2] != "--" || c[n-1] != "https://github.com/a/b/pull/9" {
			t.Errorf("gh %v: want ... -- https://github.com/a/b/pull/9", c)
		}
	}
	if _, err := Merge(context.Background(), s.run, "--subject=x https://github.com/a/b/pull/9", "squash", "abc1234"); err == nil {
		t.Fatal("URL with a flag in front accepted")
	}
}

// review 2026-11-15: a Kick before Run starts is kept, not dropped (and
// Kick never reads a channel field Run is still writing).
type laterSrc struct {
	mu       sync.Mutex
	calls    int
	observed chan string
}

func (l *laterSrc) Snapshot() []PRThread {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls == 1 { // Run's first pollAll sees no PR yet
		return nil
	}
	return []PRThread{{ID: "t1", URL: "https://github.com/a/b/pull/9"}}
}
func (l *laterSrc) Observe(id string, _ core.PR) { l.observed <- id }

func TestKickBeforeRunIsKept(t *testing.T) {
	src := &laterSrc{observed: make(chan string, 4)}
	p := &Poller{Gh: fakeGh(`{"state":"OPEN"}`), Src: src, Interval: time.Hour}
	p.Kick("t1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	select {
	case id := <-src.observed:
		if id != "t1" {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the kick before Run was dropped")
	}
}

// V0.72 (1): GitHub's "does not exist" answer is an observation, reported
// once a second answer a poll later confirms it; a transient error (network,
// rate limit, 5xx, auth) reports nothing.
type observeSrc struct{ got []core.PR }

func (o *observeSrc) Snapshot() []PRThread         { return nil }
func (o *observeSrc) Observe(_ string, pr core.PR) { o.got = append(o.got, pr) }

func TestPollReportsAMissingPR(t *testing.T) {
	url := "https://github.com/acme/app/pull/88"
	gone := func(_ context.Context, args ...string) ([]byte, error) {
		return nil, errors.New("gh pr view: exit status 1: GraphQL: Could not resolve to a Repository with the name 'acme/app'. (repository)")
	}
	at := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	src := &observeSrc{}
	p := &Poller{Gh: gone, Src: src, Now: func() time.Time { return at }}
	th := PRThread{ID: "t1", URL: url}
	p.poll(context.Background(), th)
	at = at.Add(10 * time.Second) // a kick right after: not yet a second answer
	p.poll(context.Background(), th)
	if len(src.got) != 0 {
		t.Fatalf("reported after one answer: %+v", src.got)
	}
	at = at.Add(2 * time.Minute)
	p.poll(context.Background(), th)
	if len(src.got) != 1 || src.got[0].State != core.PRMissing || src.got[0].URL != url || src.got[0].Number != 88 || src.got[0].CheckedAt == nil {
		t.Fatalf("reported %+v", src.got)
	}
	for _, msg := range []string{
		"gh pr view: exit status 1: GraphQL: Could not resolve to a PullRequest with the number of 88. (repository.pullRequest)",
		"gh api: exit status 1: HTTP 404: Not Found (https://api.github.com/repos/acme/app/pulls/88)",
	} {
		if !Missing(errors.New(msg)) {
			t.Errorf("not read as missing: %s", msg)
		}
	}
	for _, msg := range []string{
		"gh pr view: exit status 1: error connecting to api.github.com",
		"gh pr view: exit status 1: GraphQL: API rate limit exceeded for user ID 1.",
		"gh pr view: exit status 1: HTTP 502: Bad Gateway",
		"gh pr view: exit status 4: HTTP 401: Bad credentials",
		"gh pr view: context deadline exceeded",
	} {
		if Missing(errors.New(msg)) {
			t.Errorf("transient read as missing: %s", msg)
		}
	}
	flaky := &observeSrc{}
	p = &Poller{Gh: func(context.Context, ...string) ([]byte, error) { return nil, errors.New("HTTP 502: Bad Gateway") }, Src: flaky, Now: func() time.Time { return at }}
	for range 3 {
		at = at.Add(2 * time.Minute)
		p.poll(context.Background(), th)
	}
	if len(flaky.got) != 0 {
		t.Fatalf("a transient error was reported: %+v", flaky.got)
	}
	// An answer in between starts the count again.
	src = &observeSrc{}
	n := 0
	p = &Poller{Gh: func(ctx context.Context, args ...string) ([]byte, error) {
		n++
		if n == 2 {
			return []byte(`{"state":"OPEN"}`), nil
		}
		return gone(ctx, args...)
	}, Src: src, Now: func() time.Time { return at }}
	for range 3 {
		at = at.Add(2 * time.Minute)
		p.poll(context.Background(), th)
	}
	if len(src.got) != 1 || src.got[0].State != "OPEN" {
		t.Fatalf("an answer in between did not reset the count: %+v", src.got)
	}
}
