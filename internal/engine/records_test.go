package engine

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// fakeGit is the git observer's reader for tests: facts by path, and how a
// branch ended by path and branch.
type fakeGit struct {
	mu    sync.Mutex
	facts map[string]core.GitFacts
	ends  map[string][2]bool // path#branch: gone, merged
}

func (g *fakeGit) Facts(dir string) core.GitFacts {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.facts[dir]
}

func (g *fakeGit) End(dir, branch, _, _ string) (bool, bool, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.ends[dir+"#"+branch]
	if !ok {
		return false, false, true
	}
	return e[0], e[1], true
}

func (g *fakeGit) end(dir, branch string, gone, merged bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ends[dir+"#"+branch] = [2]bool{gone, merged}
}

func threadIn(t *testing.T, e *Engine, cwd string) string {
	t.Helper()
	r := e.Command(api.Cmd{CommandID: NewID("c"), Action: "thread.create", Args: core.Args{"cwd": cwd, "text": "hi"}})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	return r.ThreadID
}

// shownIn is a thread's changes as the client gets them.
func shownIn(e *Engine, id string) []core.Change {
	b, _ := json.Marshal(dtoOf(e, id))
	var w struct {
		Changes []core.Change `json:"changes"`
	}
	json.Unmarshal(b, &w)
	return w.Changes
}

// #35: two
// threads in one folder show one record of the branch, with both threads,
// stored once; a thread elsewhere does not. The live case: branch v0 of fyr
// was a change of two threads with one id. When the branch is merged into
// its base the record lands; when a branch is gone without a merge it is
// abandoned. Each thread gets its own notice.
func TestObservedBranchIsOneHostRecord(t *testing.T) {
	e, st, _ := bare(t)
	g := &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	e.Git = g
	dir, other := t.TempDir(), t.TempDir()
	a, b, c := threadIn(t, e, dir), threadIn(t, e, filepath.Join(dir, "web")), threadIn(t, e, other)
	v0 := core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "v0", Base: "origin/main", Ahead: 7, Unpushed: 7, Head: "h7"}
	e.observeGit(a, &v0)
	if len(e.records) != 1 {
		t.Fatalf("records: %+v", e.records)
	}
	var id string
	for _, th := range []string{a, b} {
		cs := shownIn(e, th)
		if len(cs) != 1 || cs[0].Branch != "v0" || !slices.Equal(cs[0].Threads, sorted(a, b)) {
			t.Fatalf("thread %s shows %+v", th, cs)
		}
		id = cs[0].ID
	}
	if cs := shownIn(e, c); len(cs) != 0 {
		t.Fatalf("a thread in another folder shows %+v", cs)
	}
	if rs, _ := st.Changes(); len(rs) != 1 || rs[0].ID != id {
		t.Fatalf("stored records: %+v", rs)
	}
	if th, _ := e.Thread(a); len(th.Changes) != 0 {
		t.Fatalf("the record is stored in the thread: %+v", th.Changes)
	}
	if evs, _ := e.Events(id, 0, 10); len(evs) != 1 || evs[0].Type != "change" {
		t.Fatalf("record events: %+v", evs)
	}

	// The branch merged into its base and was deleted; the folder is back on main.
	g.end(dir, "v0", true, true)
	e.observeGit(b, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "main", Base: "origin/main"})
	for _, th := range []string{a, b} {
		cs := shownIn(e, th)
		if len(cs) != 1 || cs[0].Phase != core.PhaseLanded || cs[0].PhaseBy != core.ByObserved {
			t.Fatalf("thread %s after the merge: %+v", th, cs)
		}
		x, _ := e.Thread(th)
		if !slices.ContainsFunc(x.Feed.Queue, func(n core.Notice) bool { return n.What == "Branch v0: merged." }) {
			t.Fatalf("thread %s was not told: %+v", th, x.Feed.Queue)
		}
	}

	// New work on a branch that is then deleted without a merge.
	e.observeGit(a, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"})
	g.end(dir, "spike", true, false)
	e.observeGit(a, &core.GitFacts{Repo: "github.com/meros/fyr", Path: dir, Branch: "main", Base: "origin/main"})
	cs := shownIn(e, a)
	if len(cs) != 2 || cs[1].Branch != "spike" || cs[1].Phase != core.PhaseAbandoned {
		t.Fatalf("after the branch went: %+v", cs)
	}

	// The records and references survive a restart.
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if cs := shownIn(e2, b); len(cs) != 2 || cs[0].ID != id {
		t.Fatalf("after a restart: %+v", cs)
	}
}

// #35, decision 0019: one thread with two code changes in two worktrees of
// the same repo holds two observed branches at once; the observer reads
// each change's workspace, and each change follows its own record. The
// thread's folder is not a checkout and yields nothing.
func TestTwoWorktreesInOneThread(t *testing.T) {
	e, _, _ := bare(t)
	g := &fakeGit{facts: map[string]core.GitFacts{}, ends: map[string][2]bool{}}
	e.Git = g
	root := t.TempDir()
	wa, wb := filepath.Join(root, "wt-a"), filepath.Join(root, "wt-b")
	g.facts[wa] = core.GitFacts{Repo: "github.com/o/r", Path: wa, Branch: "fix/a", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "a1"}
	g.facts[wb] = core.GitFacts{Repo: "github.com/o/r", Path: wb, Branch: "fix/b", Base: "origin/main", Ahead: 2, Head: "b2"}
	id := threadIn(t, e, root)
	for _, x := range []struct{ key, branch, path string }{{"a", "fix/a", wa}, {"b", "fix/b", wb}} {
		if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindChangeOpen, Thread: id, Args: core.ChangeOpenArgs{Open: core.ChangeOpen{Key: x.key, Kind: core.KindCode, Title: x.key, Branch: x.branch, Targets: []string{"repo:github.com/o/r"}}}}); err != nil {
			t.Fatal(err)
		}
		// #44 makes the worktree and sets the workspace; here the test does.
		e.mu.Lock()
		core.FindChange(e.threads[id], x.key).Workspace = &core.Workspace{Repo: "github.com/o/r", Path: x.path, Branch: x.branch}
		e.mu.Unlock()
	}
	// The run ends; the agent manager found no checkout in the folder.
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.mu.Lock()
		n := len(e.records)
		e.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("records: %d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	th, _ := e.Thread(id)
	a, b := core.FindChange(&th, "a"), core.FindChange(&th, "b")
	if a.Observed == "" || b.Observed == "" || a.Observed == b.Observed {
		t.Fatalf("followed: a %q, b %q", a.Observed, b.Observed)
	}
	if len(a.Revisions) != 1 || a.Revisions[0].ID != "a1" || len(b.Revisions) != 1 || b.Revisions[0].ID != "b2" {
		t.Fatalf("revisions: a %+v, b %+v", a.Revisions, b.Revisions)
	}
	if cs := shownIn(e, id); len(cs) != 2 {
		t.Fatalf("the thread shows %d changes, want its two: %+v", len(cs), cs)
	}
}

func sorted(ids ...string) []string {
	slices.Sort(ids)
	return ids
}
