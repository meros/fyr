package core

import (
	"strings"
	"testing"
	"time"
)

// V0.prompt-fits-setup: a merge ask on a change with no pull request says
// why and what to do instead: a repository with no GitHub remote cannot be
// merged by Fyr; a GitHub one needs its PR first.
func TestMergeAskWithoutPR(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		repo string
		want []string
	}{
		{"/home/o/notes", []string{"has no GitHub remote", "/home/o/notes", "Fyr cannot merge it", "the owner lands the branch"}},
		{"gitlab.com/o/r", []string{"has no GitHub remote", "gitlab.com/o/r"}},
		{"github.com/o/r", []string{"has no GitHub pull request yet", "gh pr create"}},
		{"", []string{"has no GitHub pull request yet", "If this project has no GitHub remote"}},
	} {
		th, _, _, err := Reduce(&Thread{ID: "t1"}, &View{}, Input{Source: FromAgent, Kind: KindChangeOpen, Thread: "t1",
			Args: ChangeOpenArgs{Open: ChangeOpen{Key: "x", Kind: KindCode, Title: "X", Branch: "b"}}}, now)
		if err != nil {
			t.Fatal(err)
		}
		if c.repo != "" {
			th.Changes[0].Workspace = &Workspace{Repo: c.repo, Branch: "b"}
		}
		_, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
			Args: ChangeRequest{ID: "d1", Change: "x", Ask: AskMerge}}, now)
		if err == nil {
			t.Fatalf("%q: a merge ask without a PR was taken", c.repo)
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%q: %q lacks %q", c.repo, err, w)
			}
		}
		if strings.Contains(err.Error(), "args.pr") {
			t.Errorf("%q: still the args error: %q", c.repo, err)
		}
	}
}
