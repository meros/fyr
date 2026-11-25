package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// The errors GitHub gave on PR merges Fyr ran (V0.merge-github-trouble).
const (
	gql500  = "gh pr merge: exit status 1: GraphQL: Something went wrong while executing your query on 2026-11-25T15:46:20Z. Please include `E0F6:2CE761` when reporting this issue."
	http502 = "gh pr merge: exit status 1: non-200 OK status code: 502 Bad Gateway body: \"<html>\\r\\n<head><title>502 Bad Gateway</title></head>\""
)

// troubled answers gh: the PR reads open (or merged once merged is set), the
// first fails reads and merges answer with err, and a merge that fails can
// still have merged the PR when landsAnyway is set.
type troubled struct {
	err         string
	failMerges  int
	failReads   int
	landsAnyway bool
	merged      bool
	merges      int
	reads       int
}

func (g *troubled) run(_ context.Context, args ...string) ([]byte, error) {
	if args[1] == "merge" {
		g.merges++
		if g.merges <= g.failMerges {
			g.merged = g.merged || g.landsAnyway
			return nil, errors.New(g.err)
		}
		g.merged = true
		return []byte("merged"), nil
	}
	g.reads++
	if g.reads <= g.failReads {
		return nil, errors.New(g.err)
	}
	state := "OPEN"
	if g.merged {
		state = "MERGED"
	}
	return []byte(`{"state":"` + state + `","mergeStateStatus":"CLEAN","mergeable":"MERGEABLE","headRefOid":"` + headA + `",` + greenRollup + `}`), nil
}

func noTroubleWaits(t *testing.T) {
	old := troubleBackoff
	troubleBackoff = []time.Duration{0, 0, 0}
	t.Cleanup(func() { troubleBackoff = old })
}

const troubleURL = "https://github.com/acme/app/pull/88"

// SPEC V0.merge-github-trouble (1): a merge GitHub answers with a 5xx but
// made anyway is a success, found by reading the PR again; it is not merged twice.
func TestMergeGitHubTroubleMergedAnywayIsSuccess(t *testing.T) {
	noTroubleWaits(t)
	g := &troubled{err: http502, failMerges: 1, landsAnyway: true}
	out, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA)
	if err != nil || !strings.Contains(out, "merged") || g.merges != 1 {
		t.Fatalf("merged anyway: %q %v after %d merges", out, err, g.merges)
	}
}

// SPEC V0.merge-github-trouble (2): a merge GitHub's trouble stopped, with the
// PR still open, is tried again after a wait and merges.
func TestMergeGitHubTroubleRetriesAndMerges(t *testing.T) {
	noTroubleWaits(t)
	g := &troubled{err: gql500, failMerges: 2}
	out, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA)
	if err != nil || out != "merged" || g.merges != 3 {
		t.Fatalf("recovers: %q %v after %d merges", out, err, g.merges)
	}
	// A read GitHub fails is read again too.
	g = &troubled{err: "HTTP 503: Service Unavailable", failReads: 2}
	if out, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA); err != nil || out != "merged" || g.merges != 1 {
		t.Fatalf("read recovers: %q %v after %d merges", out, err, g.merges)
	}
}

// SPEC V0.merge-github-trouble (3): when GitHub fails every try, the merge fails
// after a bounded number of tries, saying GitHub failed, and the agent's next
// step is to wait and ask again, not to fix the PR.
func TestMergeGitHubTroubleGivesUpSayingGitHub(t *testing.T) {
	noTroubleWaits(t)
	g := &troubled{err: gql500, failMerges: 99}
	_, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA)
	if err == nil || !strings.Contains(err.Error(), core.GitHubFailed) || g.merges != len(troubleBackoff)+1 {
		t.Fatalf("gives up: %v after %d merges", err, g.merges)
	}
	if !strings.Contains(err.Error(), "Something went wrong") {
		t.Fatalf("GitHub's words are lost: %v", err)
	}
	g = &troubled{err: "Post \"https://api.github.com/graphql\": dial tcp: i/o timeout", failReads: 99}
	if _, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA); err == nil || !strings.Contains(err.Error(), core.GitHubFailed) || g.merges != 0 || g.reads != len(troubleBackoff)+1 {
		t.Fatalf("reads give up: %v after %d reads, %d merges", err, g.reads, g.merges)
	}
	// A cancelled context stops the waits.
	old := troubleBackoff
	troubleBackoff = []time.Duration{time.Hour}
	defer func() { troubleBackoff = old }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g = &troubled{err: http502, failMerges: 99}
	if _, err := MergeChecked(ctx, g.run, troubleURL, "squash", headA); err == nil || g.merges != 1 {
		t.Fatalf("cancelled: %v after %d merges", err, g.merges)
	}
}

// SPEC V0.merge-github-trouble (4): a refusal is not GitHub's trouble; it is
// not tried again, and a PR merged by someone else before any trouble is still
// "already merged".
func TestMergeGitHubTroubleOnlyForTrouble(t *testing.T) {
	noTroubleWaits(t)
	for _, refusal := range []string{
		"gh pr merge: exit status 1: Pull Request has merge conflicts",
		"GraphQL: Base branch policy prohibits the merge",
		"HTTP 403: API rate limit exceeded",
		"HTTP 404: Not Found",
		"gh auth login",
	} {
		if GitHubTrouble(errors.New(refusal)) {
			t.Errorf("%q is read as GitHub's trouble", refusal)
		}
		g := &troubled{err: refusal, failMerges: 99}
		if _, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA); err == nil || g.merges != 1 || strings.Contains(err.Error(), core.GitHubFailed) {
			t.Errorf("%q: %v after %d merges", refusal, err, g.merges)
		}
	}
	for _, trouble := range []string{gql500, http502, "HTTP 500", "HTTP 504: Gateway Timeout", "read tcp 10.0.0.2:443: connection reset by peer", "Post \"https://api.github.com/graphql\": EOF", "net/http: TLS handshake timeout"} {
		if !GitHubTrouble(errors.New(trouble)) {
			t.Errorf("%q is not read as GitHub's trouble", trouble)
		}
	}
	g := &troubled{merged: true}
	if _, err := MergeChecked(context.Background(), g.run, troubleURL, "squash", headA); err == nil || !strings.Contains(err.Error(), "already merged") {
		t.Fatalf("merged before: %v", err)
	}
}
