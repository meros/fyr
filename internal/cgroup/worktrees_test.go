package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sweepRepo is a repository with an origin, .env and node_modules ignored.
func sweepRepo(t *testing.T) (root, origin, repo string, wt func(name string) string, commit func(dir, file string)) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin, repo = filepath.Join(root, "origin.git"), filepath.Join(root, "repo")
	git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	git(t, root, "init", "-q", "-b", "main", repo)
	write(t, filepath.Join(repo, ".gitignore"), ".env\nnode_modules/\n")
	write(t, filepath.Join(repo, "f"), "1")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "1")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "-q", "origin", "main")
	wt = func(name string) string {
		p := filepath.Join(repo, ".claude", "worktrees", name)
		git(t, repo, "worktree", "add", "-q", p, "-b", name, "origin/main")
		return p
	}
	commit = func(dir, file string) {
		write(t, filepath.Join(dir, file), file)
		git(t, dir, "add", file)
		git(t, dir, "commit", "-qm", file)
	}
	return
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestWorktreeSweepRemovesOnlyIdleCleanKeptWorktreesAndPinsThem(t *testing.T) {
	_, _, repo, wt, commit := sweepRepo(t)
	pushed, merged, build := wt("pushed"), wt("merged"), wt("build")
	dirty, ahead, env, locked, busy, fresh, awake := wt("dirty"), wt("ahead"), wt("env"), wt("locked"), wt("busy"), wt("fresh"), wt("awake")
	for _, p := range []string{pushed, build, env, locked, busy, fresh, awake} {
		commit(p, filepath.Base(p))
		git(t, p, "push", "-q", "origin", filepath.Base(p))
	}
	// merged: the branch went from the remote (or history was rewritten);
	// its commit is the head of a GitHub PR.
	commit(merged, "merged")
	git(t, merged, "push", "-q", "origin", "HEAD:refs/pull/7/head")
	mergedHead := git(t, merged, "rev-parse", "HEAD")
	write(t, filepath.Join(dirty, "f"), "changed")
	commit(ahead, "ahead")
	write(t, filepath.Join(build, "node_modules", "x", "index.js"), "x")
	write(t, filepath.Join(env, ".env"), "SECRET=1")
	git(t, repo, "worktree", "lock", locked)

	now := time.Now().Add(48 * time.Hour)
	// fresh: its HEAD moved an hour before "now".
	gd := git(t, fresh, "rev-parse", "--path-format=absolute", "--git-dir")
	for _, f := range []string{gd, filepath.Join(gd, "HEAD"), filepath.Join(gd, "logs", "HEAD")} {
		os.Chtimes(f, now.Add(-time.Hour), now.Add(-time.Hour))
	}
	var lines []string
	kv := &memKV{}
	tp := &Temps{Dir: t.TempDir(), Now: func() time.Time { return now }, Auto: true, KV: kv,
		Threads: func() []TempThread {
			return []TempThread{
				{ID: "t_old", Cwd: repo, Asleep: true, Branches: []string{"pushed"}},
				{ID: "t_awake", Cwd: awake},
			}
		},
		Cwds:      func() []string { return []string{"/", filepath.Join(busy, "sub")} },
		OnRemoved: func(th, line string) { lines = append(lines, th+": "+line) },
	}
	tp.Sweep(true)

	for _, p := range []string{pushed, merged, build} {
		if exists(p) {
			t.Fatalf("%s stays", p)
		}
	}
	got := byPath(tp.View())
	want := map[string]string{dirty: "uncommitted", ahead: "not pushed", env: ".env", locked: "locked", busy: "in use"}
	for p, why := range want {
		x, ok := got[p]
		if !exists(p) || !ok || !strings.Contains(x.Why, why) {
			t.Fatalf("%s: listed %v %+v, want kept for %q", p, ok, x, why)
		}
	}
	if x := got[fresh]; !exists(fresh) || x.RemoveAt == nil || !x.RemoveAt.After(now) {
		t.Fatalf("fresh: %+v", x)
	}
	if _, ok := got[awake]; ok || !exists(awake) {
		t.Fatal("an awake thread's worktree is listed or removed")
	}
	// Pinned, branches kept, the owner's thread told.
	if h := git(t, repo, "rev-parse", "refs/fyr/worktrees/merged-"+mergedHead[:7]); h != mergedHead {
		t.Fatalf("pin %s, want %s", h, mergedHead)
	}
	for _, b := range []string{"pushed", "merged", "build"} {
		git(t, repo, "rev-parse", "--verify", "refs/heads/"+b)
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "t_old: Fyr removed the worktree "+pushed) {
		t.Fatalf("lines %q", lines)
	}
}

func TestWorktreeSweepRestoreMakesARemovedWorktreeAgain(t *testing.T) {
	_, _, repo, wt, commit := sweepRepo(t)
	onBranch, detached := wt("on-branch"), wt("detached")
	for _, p := range []string{onBranch, detached} {
		commit(p, filepath.Base(p))
		git(t, p, "push", "-q", "origin", filepath.Base(p))
	}
	head := git(t, detached, "rev-parse", "HEAD")
	tp := &Temps{Dir: t.TempDir(), Now: func() time.Time { return time.Now().Add(48 * time.Hour) }, Auto: true, NoFetch: true, KV: &memKV{},
		Threads: func() []TempThread { return []TempThread{{ID: "t", Cwd: repo, Asleep: true}} },
		Cwds:    func() []string { return nil }}
	tp.Sweep(true)
	if exists(onBranch) || exists(detached) {
		t.Fatal("not removed")
	}
	git(t, repo, "branch", "-D", "detached") // its branch went meanwhile: back at the pin

	if ok, err := tp.Restore(filepath.Join(onBranch, "sub")); !ok || err != nil {
		t.Fatalf("restore: %v %v", ok, err)
	}
	if b := git(t, onBranch, "branch", "--show-current"); b != "on-branch" {
		t.Fatalf("on branch %q", b)
	}
	if ok, err := tp.Restore(detached); !ok || err != nil {
		t.Fatalf("restore detached: %v %v", ok, err)
	}
	if h := git(t, detached, "rev-parse", "HEAD"); h != head {
		t.Fatalf("detached at %s, want %s", h, head)
	}
	if ok, _ := tp.Restore(onBranch); ok {
		t.Fatal("restored twice")
	}
	if ok, _ := tp.Restore(filepath.Join(repo, "elsewhere")); ok {
		t.Fatal("a folder Fyr never removed was made")
	}
}

func TestWorktreeSweepWithoutAutoOrCwdsRemovesNothing(t *testing.T) {
	_, _, repo, wt, commit := sweepRepo(t)
	p := wt("pushed")
	commit(p, "x")
	git(t, p, "push", "-q", "origin", "pushed")
	now := func() time.Time { return time.Now().Add(48 * time.Hour) }
	threads := func() []TempThread { return []TempThread{{ID: "t", Cwd: repo, Asleep: true}} }
	for _, tp := range []*Temps{
		{Dir: t.TempDir(), Now: now, Threads: threads, Cwds: func() []string { return nil }},
		{Dir: t.TempDir(), Now: now, Threads: threads, Auto: true},
	} {
		tp.Sweep(true)
		if !exists(p) {
			t.Fatal("removed")
		}
		if x := byPath(tp.View())[p]; !x.Safe || x.RemoveAt != nil {
			t.Fatalf("%+v", x)
		}
	}
}

// V0.change-worktree-cleanup: a worktree Fyr made for a change that landed
// or ended goes DoneGrace after the end and after its HEAD last moved, also
// while its thread is awake with a live CLI; not before, never with
// uncommitted work, and not while another awake thread is on its branch or
// a process works in it.
func TestWorktreeSweepRemovesFyrsWorktreeOfAnEndedChange(t *testing.T) {
	_, _, repo, wt, commit := sweepRepo(t)
	landed, recent, dirty, shared, busy, open := wt("landed"), wt("recent"), wt("dirty"), wt("shared"), wt("busy"), wt("open")
	for _, p := range []string{landed, recent, dirty, shared, busy, open} {
		commit(p, filepath.Base(p))
		git(t, p, "push", "-q", "origin", filepath.Base(p))
	}
	write(t, filepath.Join(dirty, "f"), "changed")
	now := time.Now().Add(3 * time.Hour) // every HEAD moved 3 h ago
	var lines []string
	tp := &Temps{Dir: t.TempDir(), Now: func() time.Time { return now }, Auto: true, NoFetch: true, KV: &memKV{},
		Threads: func() []TempThread {
			return []TempThread{
				{ID: "t_me", Cwd: repo, LiveCLI: true, Branches: []string{"open"}, Done: map[string]time.Time{
					landed: now.Add(-2 * time.Hour), recent: now.Add(-10 * time.Minute), dirty: now.Add(-2 * time.Hour),
					shared: now.Add(-2 * time.Hour), busy: now.Add(-2 * time.Hour),
				}},
				{ID: "t_other", Cwd: "/elsewhere", Branches: []string{"shared"}},
			}
		},
		Cwds:      func() []string { return []string{filepath.Join(busy, "sub")} },
		OnRemoved: func(th, line string) { lines = append(lines, th+": "+line) },
	}
	tp.SweepWorktrees()
	if exists(landed) {
		t.Fatal("the landed change's worktree stays")
	}
	for _, p := range []string{recent, dirty, shared, busy, open} {
		if !exists(p) {
			t.Fatalf("%s went", p)
		}
	}
	got := byPath(tp.View())
	if x := got[recent]; x.RemoveAt == nil || !x.RemoveAt.Equal(now.Add(50*time.Minute)) || x.Thread != "t_me" {
		t.Fatalf("recent: %+v", x)
	}
	if x := got[dirty]; !strings.Contains(x.Why, "uncommitted") {
		t.Fatalf("dirty: %+v", x)
	}
	if x := got[busy]; x.Why != "in use" {
		t.Fatalf("busy: %+v", x)
	}
	for _, p := range []string{shared, open} {
		if _, ok := got[p]; ok {
			t.Fatalf("%s is listed: an awake thread is on its branch", p)
		}
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "t_me: Fyr removed the worktree "+landed) {
		t.Fatalf("lines %q", lines)
	}
}
