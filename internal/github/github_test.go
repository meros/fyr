package github

import (
	"context"
	"errors"
	"slices"
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
		// V0.112: an empty list on an open PR is pending unless the repo has no CI (here the CI read fails)
		{`{"state":"OPEN","mergeable":"CONFLICTING","statusCheckRollup":[]}`, "pending", true, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"QUEUED","conclusion":""}]}`, "pending", false, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SKIPPED"},{"__typename":"StatusContext","state":"SUCCESS"}]}`, "pass", false, "OPEN"},
		{`{"state":"MERGED","statusCheckRollup":null}`, "none", false, "MERGED"},
		// V0.advisory-checks: a queued or failed advisory check neither holds nor fails the PR.
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","name":"swift build (advisory)","status":"QUEUED","conclusion":""}]}`, "pass", false, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","name":"swift build (Advisory)","status":"COMPLETED","conclusion":"CANCELLED"}]}`, "pass", false, "OPEN"},
		{`{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"},{"__typename":"StatusContext","context":"mac (advisory)","state":"PENDING"}]}`, "fail", false, "OPEN"},
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

// SPEC V0.112: an open PR with no check reported is pending on a repo with
// CI and none only on a repo where no PR had a check.
func TestFetchEmptyChecksByRepoCI(t *testing.T) {
	const view = `{"state":"OPEN","headRefOid":"029f9c7","statusCheckRollup":[]}`
	for ci, want := range map[string]core.Checks{"4": core.ChecksPending, "0": core.ChecksNone} {
		ciRepos.Clear()
		s := &scripted{out: map[string]string{"pr view": view, "api graphql": ci}}
		p, err := Fetch(context.Background(), s.run, "https://github.com/meros/fyr/pull/126")
		if err != nil || p.Checks != want {
			t.Errorf("CI count %s: %v %v, want %s", ci, p.Checks, err, want)
		}
		if got := s.calls[len(s.calls)-1]; strings.Join(got[:2], " ") != "api graphql" || !slices.Contains(got, "owner=meros") || !slices.Contains(got, "name=fyr") {
			t.Errorf("the CI read %v", got)
		}
	}
	// a check seen on any PR of the repo is enough; the repo is not read again
	ciRepos.Clear()
	seen := &scripted{out: map[string]string{"pr view": `{"state":"OPEN","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]}`}}
	Fetch(context.Background(), seen.run, "https://github.com/meros/fyr/pull/125")
	seen.out["pr view"] = view
	if p, _ := Fetch(context.Background(), seen.run, "https://github.com/meros/fyr/pull/126"); p.Checks != core.ChecksPending || seen.count("api graphql") != 0 {
		t.Errorf("after a check on #125: %v, %d reads", p.Checks, seen.count("api graphql"))
	}
	ciRepos.Clear()
}

// SPEC V0.136: the poller counts the head's finished checks and names the
// failed ones, from the rollup it already reads.
func TestFetchCountsChecks(t *testing.T) {
	p, err := Fetch(context.Background(), fakeGh(`{"state":"OPEN","statusCheckRollup":[
		{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"},
		{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"SUCCESS"},
		{"__typename":"CheckRun","name":"e2e","status":"IN_PROGRESS","conclusion":""},
		{"__typename":"StatusContext","context":"codecov","state":"ERROR"},
		{"__typename":"CheckRun","name":"docs","status":"COMPLETED","conclusion":"SKIPPED"}]}`), "https://github.com/a/b/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	if p.ChecksDone != 4 || p.ChecksTotal != 5 || !slices.Equal(p.FailedChecks, []string{"test", "codecov"}) {
		t.Fatalf("counts %d of %d, failed %v", p.ChecksDone, p.ChecksTotal, p.FailedChecks)
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

// V0.199: Refresh returns after the read is observed, so a merge ask is judged
// on it; without a running poller it gives up when its context ends.
func TestRefreshWaitsForTheRead(t *testing.T) {
	src := &laterSrc{observed: make(chan string, 4)}
	p := &Poller{Gh: fakeGh(`{"state":"OPEN"}`), Src: src, Interval: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)
	p.Refresh(ctx, "t1")
	if ctx.Err() != nil {
		t.Fatal("Refresh did not return before its context ended")
	}
	select {
	case id := <-src.observed:
		if id != "t1" {
			t.Fatal(id)
		}
	default:
		t.Fatal("Refresh returned before the read was observed")
	}
}

func TestRefreshGivesUpWithItsContext(t *testing.T) {
	p := &Poller{Gh: fakeGh(`{"state":"OPEN"}`), Src: &laterSrc{observed: make(chan string, 4)}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	p.Refresh(ctx, "t1") // nothing runs the poller: this must still return
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

// SPEC V0.98 (1): a merged PR into a branch that is not the repo's default
// one is stacked; the compare of the default branch with its merge commit
// says whether it is in the default branch yet.
func TestFetchReadsStackedPR(t *testing.T) {
	pr := `{"state":"MERGED","baseRefName":"feat/a","mergeCommit":{"oid":"m1"},"statusCheckRollup":[]}`
	run := func(status string) Runner {
		return func(_ context.Context, args ...string) ([]byte, error) {
			switch {
			case args[0] == "pr":
				return []byte(pr), nil
			case args[1] == "repos/st/ack":
				return []byte("main\n"), nil
			case args[1] == "repos/st/ack/compare/main...m1":
				return []byte(status + "\n"), nil
			}
			return nil, errors.New("unexpected " + strings.Join(args, " "))
		}
	}
	p, err := Fetch(context.Background(), run("diverged"), "https://github.com/st/ack/pull/2")
	if err != nil || p.Base != "feat/a" || !p.Stacked || p.InDefault == nil || *p.InDefault {
		t.Fatalf("not yet in main: %+v %v", p, err)
	}
	p, _ = Fetch(context.Background(), run("behind"), "https://github.com/st/ack/pull/2")
	if p.InDefault == nil || !*p.InDefault {
		t.Fatalf("in main: %+v", p)
	}
	p, _ = Fetch(context.Background(), run(""), "https://github.com/st/ack/pull/2")
	if !p.Stacked || p.InDefault != nil {
		t.Fatalf("an unread compare is known: %+v", p)
	}
	pr = `{"state":"MERGED","baseRefName":"main","mergeCommit":{"oid":"m1"},"statusCheckRollup":[]}`
	if p, _ = Fetch(context.Background(), run("diverged"), "https://github.com/st/ack/pull/2"); p.Stacked {
		t.Fatalf("a PR into the default branch is stacked: %+v", p)
	}
}

// SPEC V0.136: a PR GitHub reads as BLOCKED with green checks gets the
// base's active rules and the required checks that did not report, read
// once per repo and base; other PRs never read the rules.
func TestFetchNamesBlockingRules(t *testing.T) {
	baseRules.Clear()
	defer baseRules.Clear()
	const view = `{"state":"OPEN","headRefOid":"7333251aa","baseRefName":"main","mergeStateStatus":"BLOCKED","reviewDecision":"",
		"statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]}`
	s := &scripted{out: map[string]string{"pr view": view,
		"api repos/acme/app/rules/branches/main": `[{"type":"pull_request","parameters":{}},{"type":"required_signatures"},
			{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"test"},{"context":"build"}]}}]`}}
	p, err := Fetch(context.Background(), s.run, "https://github.com/acme/app/pull/8")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Rules, []string{"pull_request", "required_signatures", "required_status_checks"}) || !slices.Equal(p.MissingChecks, []string{"build"}) || p.Protected {
		t.Fatalf("rules %v missing %v protected %v", p.Rules, p.MissingChecks, p.Protected)
	}
	Fetch(context.Background(), s.run, "https://github.com/acme/app/pull/8")
	if n := s.count("api repos/acme/app/rules"); n != 1 {
		t.Errorf("the rules were read %d times", n)
	}
	// classic branch protection, when no ruleset applies
	c := &scripted{out: map[string]string{"pr view": strings.Replace(view, "acme", "solo", 1),
		"api repos/solo/app/rules/branches/main": `[]`,
		"api repos/solo/app/branches/main":       `{"protected":true,"protection":{"required_status_checks":{"contexts":["ci/build"]}}}`}}
	p, _ = Fetch(context.Background(), c.run, "https://github.com/solo/app/pull/8")
	if !p.Protected || !slices.Equal(p.MissingChecks, []string{"ci/build"}) {
		t.Fatalf("classic protection: %+v", p)
	}
	// CLEAN, or BLOCKED while CI runs: no rules read
	clean := &scripted{out: map[string]string{"pr view": strings.Replace(view, "BLOCKED", "CLEAN", 1)}}
	Fetch(context.Background(), clean.run, "https://github.com/other/app/pull/8")
	if clean.count("api repos/") != 0 {
		t.Errorf("a CLEAN PR read the rules: %v", clean.calls)
	}
}
