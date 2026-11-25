package core

import (
	"strings"
	"testing"
	"time"
)

func splitQueued(th *Thread) []Notice {
	var out []Notice
	for _, q := range th.Feed.Queue {
		if q.Key == "split" {
			out = append(out, q)
		}
	}
	return out
}

func splitOpen(t *testing.T, th *Thread, o ChangeOpen, at time.Time) {
	t.Helper()
	o.Kind = KindCode
	if o.Branch == "" {
		o.Branch = o.Key
	}
	if o.Title == "" {
		o.Title = o.Key
	}
	step(t, th, &View{}, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{Open: o}}, at)
}

// V0.thread-focus: the third separate piece of work tells the agent once to
// split; a follow-up second change says nothing.
func TestSplitNudgeAtThirdPiece(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 9, 0, 0, 0, time.Local)
	th := &Thread{ID: "t"}
	splitOpen(t, th, ChangeOpen{Key: "a", Title: "Fix geocoding"}, t0)
	splitOpen(t, th, ChangeOpen{Key: "b", Title: "Follow-up"}, t0.Add(time.Minute))
	if len(splitQueued(th)) != 0 || th.SplitNudged {
		t.Fatalf("nudged at two pieces: %+v", th.Feed.Queue)
	}
	splitOpen(t, th, ChangeOpen{Key: "c", Title: "Backoffice page"}, t0.Add(2*time.Minute))
	ns := splitQueued(th)
	if len(ns) != 1 || !th.SplitNudged {
		t.Fatalf("want one nudge, got %+v", th.Feed.Queue)
	}
	for _, w := range []string{"3 separate pieces of work", `"Fix geocoding"`, `"Backoffice page"`, "mcp__fyr__thread_new", "full brief", "after (a stack) or the same group"} {
		if !strings.Contains(ns[0].What, w) {
			t.Errorf("nudge lacks %q: %s", w, ns[0].What)
		}
	}
	th.Feed.Queue = nil
	splitOpen(t, th, ChangeOpen{Key: "d"}, t0.Add(3*time.Minute))
	if len(splitQueued(th)) != 0 {
		t.Fatalf("told twice: %+v", th.Feed.Queue)
	}
}

// A stack (after) and a group are one piece of work: no nudge however many
// changes they hold.
func TestSplitNudgeStackAndGroupAreOnePiece(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 9, 0, 0, 0, time.Local)
	th := &Thread{ID: "t"}
	splitOpen(t, th, ChangeOpen{Key: "spec"}, t0)
	splitOpen(t, th, ChangeOpen{Key: "db", After: []string{"spec"}}, t0)
	splitOpen(t, th, ChangeOpen{Key: "job", After: []string{"db"}}, t0)
	splitOpen(t, th, ChangeOpen{Key: "app", Group: "feature", Targets: []string{"repo:github.com/acme/app"}}, t0)
	splitOpen(t, th, ChangeOpen{Key: "hosting", Group: "feature", Targets: []string{"repo:github.com/acme/hosting"}}, t0)
	if len(splitPieces(th)) != 2 || th.SplitNudged || len(splitQueued(th)) != 0 {
		t.Fatalf("pieces %d, queue %+v", len(splitPieces(th)), th.Feed.Queue)
	}
}

// A second repo whose change is not tied to the others nudges at once; an
// abandoned change does not count.
func TestSplitNudgeSecondRepo(t *testing.T) {
	t0 := time.Date(2026, 11, 25, 9, 0, 0, 0, time.Local)
	th := &Thread{ID: "t"}
	splitOpen(t, th, ChangeOpen{Key: "a", Targets: []string{"repo:github.com/acme/app"}}, t0)
	splitOpen(t, th, ChangeOpen{Key: "a2", After: []string{"a"}, Targets: []string{"repo:github.com/Acme/App"}}, t0)
	if th.SplitNudged {
		t.Fatal("nudged in the same repo")
	}
	splitOpen(t, th, ChangeOpen{Key: "b", Targets: []string{"repo:github.com/acme/hosting"}}, t0)
	ns := splitQueued(th)
	if len(ns) != 1 || !strings.Contains(ns[0].What, "2 repos (acme/app, acme/hosting)") {
		t.Fatalf("queue %+v", th.Feed.Queue)
	}
}

// Changes Fyr made from a PR it observed count like opened ones, and an
// abandoned change is no piece of work.
func TestSplitPiecesSkipAbandoned(t *testing.T) {
	th := &Thread{ID: "t", Changes: []Change{
		{ID: "1", Kind: KindCode, Phase: PhaseLanded},
		{ID: "2", Kind: KindCode, Phase: PhaseAbandoned},
		{ID: "3", Kind: KindCode, Phase: PhaseReview, PR: &PR{Repo: "acme/x"}},
	}}
	if p := splitPieces(th); len(p) != 2 || changeRepo(p[1][0]) != "acme/x" {
		t.Fatalf("pieces %+v", p)
	}
	if r := changeRepo(&Change{Workspace: &Workspace{Repo: "git@github.com:Acme/Y.git"}}); r != "acme/y" {
		t.Fatalf("repo %q", r)
	}
}
