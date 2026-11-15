package github

import (
	"context"
	"strings"
	"testing"
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
		if p.Checks != c.checks || p.Conflict != c.conflict || p.State != c.state || p.Repo != "a/b" || p.Number != 1 {
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
	out, err := Merge(context.Background(), r, "https://github.com/a/b/pull/9", "squash")
	if err != nil || !strings.Contains(out, "merged") {
		t.Fatal(out, err)
	}
	if strings.Join(got, " ") != "pr merge --squash -- https://github.com/a/b/pull/9" {
		t.Fatalf("args %v", got)
	}
	if _, err := Merge(context.Background(), r, "https://evil/x", "squash"); err == nil {
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
	if _, err := Merge(context.Background(), s.run, url, "squash"); err != nil {
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
	if _, err := Merge(context.Background(), s.run, "--subject=x https://github.com/a/b/pull/9", "squash"); err == nil {
		t.Fatal("URL with a flag in front accepted")
	}
}

