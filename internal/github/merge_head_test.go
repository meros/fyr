package github

import (
	"context"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
)

const headA = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"

// SPEC V0.97: a merge bound to a head the PR no longer has is refused, and gh is never asked to merge.
func TestMergeCheckedRefusesStaleHead(t *testing.T) {
	s := &scripted{out: map[string]string{"pr view": `{"state":"OPEN","mergeStateStatus":"CLEAN","mergeable":"MERGEABLE","headRefOid":"ffff0000ffff0000ffff0000ffff0000ffff0000"}`, "pr merge": "merged"}}
	_, err := MergeChecked(context.Background(), s.run, "https://github.com/acme/app/pull/88", "squash", headA)
	if err == nil || err.Error() != core.HeadMoved {
		t.Fatalf("stale head: %v, want %q", err, core.HeadMoved)
	}
	if s.count("pr merge") != 0 {
		t.Fatal("gh merged with a stale head")
	}
	if _, err := MergeChecked(context.Background(), s.run, "https://github.com/acme/app/pull/88", "squash", ""); err == nil || s.count("pr merge") != 0 {
		t.Fatalf("a merge with no head: %v", err)
	}
}

// SPEC V0.97: a merge bound to the current head passes it to gh as --match-head-commit, so GitHub refuses a head that moves in between.
func TestMergeCheckedMatchesHead(t *testing.T) {
	s := &scripted{out: map[string]string{"pr view": `{"state":"OPEN","mergeStateStatus":"CLEAN","mergeable":"MERGEABLE","headRefOid":"` + headA + `"}`, "pr merge": "Squashed and merged"}}
	out, err := MergeChecked(context.Background(), s.run, "https://github.com/acme/app/pull/88", "rebase", headA[:7])
	if err != nil || !strings.Contains(out, "merged") {
		t.Fatalf("merge: %q %v", out, err)
	}
	if s.count("pr merge --rebase --match-head-commit "+headA+" -- https://github.com/acme/app/pull/88") != 1 {
		t.Fatalf("gh calls %v", s.calls)
	}
}

// SPEC V0.97: the poller reads draft, merge state and auto-merge.
func TestFetchReadsReadyFields(t *testing.T) {
	var asked string
	gh := func(_ context.Context, args ...string) ([]byte, error) {
		asked = strings.Join(args, " ")
		return []byte(`{"state":"OPEN","isDraft":true,"mergeStateStatus":"BLOCKED","autoMergeRequest":{"enabledAt":"2026-11-16T10:00:00Z"},"statusCheckRollup":[]}`), nil
	}
	p, err := Fetch(context.Background(), gh, "https://github.com/a/b/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"isDraft", "mergeStateStatus", "autoMergeRequest"} {
		if !strings.Contains(asked, f) {
			t.Errorf("gh pr view does not ask for %s: %s", f, asked)
		}
	}
	if !p.Draft || p.MergeState != "BLOCKED" || !p.AutoMerge {
		t.Fatalf("PR %+v", p)
	}
	p, _ = Fetch(context.Background(), fakeGh(`{"state":"OPEN","autoMergeRequest":null}`), "https://github.com/a/b/pull/1")
	if p.AutoMerge || p.Draft {
		t.Fatalf("no auto-merge: %+v", p)
	}
}
