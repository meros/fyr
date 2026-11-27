package core

import (
	"testing"
	"time"
)

// V0.thread-issues: issue URLs parse to one canonical form; a PR URL is not one.
func TestParseIssue(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/app/issues/12":                    "https://github.com/acme/app/issues/12",
		"https://github.com/acme/app/issues/12#issuecomment-1":     "https://github.com/acme/app/issues/12",
		" https://github.com/acme/app.js/issues/3 ":                "https://github.com/acme/app.js/issues/3",
		"https://github.com/acme/app/pull/12":                      "",
		"https://github.com/acme/app/issues/0":                     "",
		"see https://github.com/acme/app/issues/12":                "",
		"https://github.com/acme/app/issues/12/../../pull/1":       "",
		"https://example.com/acme/app/issues/12":                   "",
		"https://github.com/acme/app/issues/12?foo=bar#issue-3456": "https://github.com/acme/app/issues/12",
	} {
		got, _ := CanonicalIssue(in)
		if got != want {
			t.Errorf("CanonicalIssue(%q) = %q, want %q", in, got, want)
		}
	}
	got := IssueURLsIn("fix https://github.com/acme/app/issues/4 and https://github.com/acme/app/issues/4, see https://github.com/acme/app/pull/5 and https://github.com/acme/lib/issues/9#x.")
	if len(got) != 2 || got[0] != "https://github.com/acme/app/issues/4" || got[1] != "https://github.com/acme/lib/issues/9" {
		t.Fatalf("IssueURLsIn %v", got)
	}
}

func issueRed(t *testing.T, th *Thread, in Input, now time.Time) *Thread {
	t.Helper()
	th, _, _, err := Reduce(th, &View{}, in, now)
	if err != nil {
		t.Fatalf("%s: %v", in.Kind, err)
	}
	return th
}

// V0.thread-issues: each way an issue joins a thread, and the poller's read.
func TestThreadLinksIssues(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1", Cwd: "/w", Profile: "claude"}

	// The owner's message gives its URL.
	th = issueRed(t, th, Input{Source: FromOwner, Kind: "thread.send", Thread: "t1", Args: Args{"text": "fix https://github.com/acme/app/issues/4 please"}}, now)
	if len(th.Issues) != 1 || th.Issues[0].Via != IssueViaOwner || th.Issues[0].Repo != "acme/app" || th.Issues[0].Number != 4 {
		t.Fatalf("owner link %+v", th.Issues)
	}
	// Another agent's message does not link.
	th.Decisions = nil
	th = issueRed(t, th, Input{Source: FromAgent, From: "t2", Kind: "thread.send", Thread: "t1", Args: Args{"text": "see https://github.com/acme/app/issues/8"}}, now)
	if len(th.Issues) != 1 {
		t.Fatalf("an agent's message linked %+v", th.Issues)
	}

	// gh issue create.
	th = issueRed(t, th, Input{Source: Observer(ObsProcess), Kind: KindDigest, Thread: "t1", Args: DigestArgs{Issues: []string{"https://github.com/acme/app/issues/5"}}}, now)
	if is := th.FindIssue("https://github.com/acme/app/issues/5"); is == nil || is.Via != IssueViaCreated || is.State != "OPEN" {
		t.Fatalf("created %+v", th.Issues)
	}

	// A PR of the thread closes an issue it already links, and a new one.
	at := now.Add(time.Minute)
	pr := PR{URL: "https://github.com/acme/app/pull/9", Repo: "acme/app", Number: 9, State: "OPEN", Checks: ChecksPending, Branch: "fix", CheckedAt: &at,
		Closes: []Issue{{URL: "https://github.com/acme/app/issues/4", Title: "Crash", State: "OPEN"}, {URL: "https://github.com/acme/app/issues/6", Title: "Leak", State: "OPEN"}}}
	th = issueRed(t, th, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1", Args: pr}, now)
	four, six := th.FindIssue("https://github.com/acme/app/issues/4"), th.FindIssue("https://github.com/acme/app/issues/6")
	if four == nil || four.Via != IssueViaOwner || four.Title != "Crash" || four.PR != pr.URL {
		t.Fatalf("issue 4 %+v", four)
	}
	if six == nil || six.Via != IssueViaPR || six.PR != pr.URL || six.CheckedAt == nil {
		t.Fatalf("issue 6 %+v", six)
	}
	if c := changeByPR(th, pr.URL); c == nil || c.PR.Closes != nil {
		t.Fatalf("the PR snapshot keeps its closing issues: %+v", c)
	}

	// The poller reads it closed; a closed issue is no longer watched.
	th = issueRed(t, th, Input{Source: Observer(ObsGitHub), Kind: KindIssueObserved, Thread: "t1", Args: Issue{URL: "https://github.com/acme/app/issues/6", Title: "Leak", State: "CLOSED"}}, now)
	if six := th.FindIssue("https://github.com/acme/app/issues/6"); six.State != "CLOSED" || six.Via != IssueViaPR {
		t.Fatalf("observed %+v", six)
	}
	for _, is := range WatchedIssues(th) {
		if is.State == "CLOSED" {
			t.Fatalf("a closed issue is watched: %+v", is)
		}
	}
	// A read of an issue the thread no longer links adds nothing.
	th = issueRed(t, th, Input{Source: Observer(ObsGitHub), Kind: KindIssueObserved, Thread: "t1", Args: Issue{URL: "https://github.com/acme/app/issues/77", State: "OPEN"}}, now)
	if th.FindIssue("https://github.com/acme/app/issues/77") != nil {
		t.Fatal("an observation linked an issue")
	}

	// The agent links and unlinks; a bad URL is refused.
	link := func(u string, unlink bool) Input {
		return Input{Source: FromAgent, Kind: KindIssueLink, Thread: "t1", Args: IssueLinkArgs{URL: u, Unlink: unlink}}
	}
	th = issueRed(t, th, link("https://github.com/acme/lib/issues/2", false), now)
	if is := th.FindIssue("https://github.com/ACME/lib/issues/2"); is == nil || is.Via != IssueViaAgent {
		t.Fatalf("agent link %+v", th.Issues)
	}
	th = issueRed(t, th, link("https://github.com/acme/lib/issues/2", true), now)
	if th.FindIssue("https://github.com/acme/lib/issues/2") != nil {
		t.Fatal("unlink kept it")
	}
	if _, _, _, err := Reduce(th, &View{}, link("https://github.com/acme/lib/pull/2", false), now); err == nil {
		t.Fatal("a PR URL was linked as an issue")
	}
	if _, _, _, err := Reduce(th, &View{}, Input{Source: FromOwner, Kind: KindIssueLink, Thread: "t1", Args: IssueLinkArgs{URL: "https://github.com/acme/lib/issues/3"}}, now); err == nil {
		t.Fatal("issue.link is the agent's")
	}

	// A settled thread watches nothing.
	th.Settle.At = &now
	if len(WatchedIssues(th)) != 0 {
		t.Fatal("a settled thread's issues are watched")
	}
}

// V0.thread-issues: a thread links at most MaxIssues issues.
func TestLinkIssueCap(t *testing.T) {
	th := &Thread{}
	now := time.Now()
	for i := 1; i <= MaxIssues+3; i++ {
		LinkIssue(th, Issue{URL: "https://github.com/acme/app/issues/" + itoa(i), Via: IssueViaAgent}, now)
	}
	if len(th.Issues) != MaxIssues {
		t.Fatalf("%d issues", len(th.Issues))
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
