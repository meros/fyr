package cgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func byPath(ts []Temp) map[string]Temp {
	m := map[string]Temp{}
	for _, x := range ts {
		m[x.Path] = x
	}
	return m
}

func TestV176CLayoutProfilesWhoseChromeIsGoneAreRemovedOthersStay(t *testing.T) {
	tmp := t.TempDir()
	now := time.Now()
	old, busy, fresh := filepath.Join(tmp, "fyr-layout-old"), filepath.Join(tmp, "fyr-layout-busy"), filepath.Join(tmp, "fyr-layout-new")
	for _, d := range []string{old, busy, fresh} {
		write(t, filepath.Join(d, "Default", "x"), strings.Repeat("x", 1000))
	}
	for _, d := range []string{old, busy} {
		os.Chtimes(d, now.Add(-time.Hour), now.Add(-time.Hour))
	}
	tp := &Temps{Dir: tmp, Now: func() time.Time { return now },
		Cmdlines: func() []string { return []string{"chromium --user-data-dir=" + busy} }}
	tp.Sweep(false)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("a profile whose Chrome is gone stays")
	}
	for _, d := range []string{busy, fresh} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("%s removed", d)
		}
	}
}

func TestV176CScratchOfAnAsleepThreadWithoutCLIGoesAndAnAwakeOneStays(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "claude-1000", "-home-x-proj", "sess-a")
	b := filepath.Join(tmp, "claude-1000", "-home-x-proj", "sess-b")
	c := filepath.Join(tmp, "claude-1000", "-home-x-proj", "sess-c")
	for _, d := range []string{a, b, c} {
		write(t, filepath.Join(d, "scratchpad", "f"), "x")
	}
	tp := &Temps{Dir: tmp, Now: time.Now, Threads: func() []TempThread {
		return []TempThread{
			{ID: "t_a", SessionID: "sess-a", Asleep: true},
			{ID: "t_b", SessionID: "sess-b"},
			{ID: "t_c", SessionID: "sess-c", Asleep: true, LiveCLI: true},
		}
	}}
	tp.Sweep(false)
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("a settled thread's scratchpad stays")
	}
	for _, d := range []string{b, c} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("%s removed", d)
		}
	}
}

func TestV176CWorktreesOfSettledThreadsAreListedOnlyAPushedCleanOneCanGo(t *testing.T) {
	root := t.TempDir()
	origin, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "repo")
	git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	git(t, root, "init", "-q", "-b", "main", repo)
	write(t, filepath.Join(repo, "f"), "1")
	git(t, repo, "add", "f")
	git(t, repo, "commit", "-qm", "1")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "-q", "origin", "main")
	wt := func(name string) string {
		p := filepath.Join(repo, ".claude", "worktrees", name)
		git(t, repo, "worktree", "add", "-q", p, "-b", name, "origin/main")
		return p
	}
	pushed, dirty, ahead := wt("pushed"), wt("dirty"), wt("ahead")
	write(t, filepath.Join(pushed, "g"), "2")
	git(t, pushed, "add", "g")
	git(t, pushed, "commit", "-qm", "2")
	git(t, pushed, "push", "-q", "origin", "pushed")
	write(t, filepath.Join(dirty, "f"), "dirty")
	write(t, filepath.Join(ahead, "h"), "3")
	git(t, ahead, "add", "h")
	git(t, ahead, "commit", "-qm", "3")
	other := wt("other") // no settled thread's

	tp := &Temps{Dir: t.TempDir(), Now: time.Now, Threads: func() []TempThread {
		return []TempThread{{ID: "t_a", Cwd: repo, Asleep: true, Branches: []string{"pushed", "dirty", "ahead"}}}
	}}
	tp.Sweep(true)
	got := byPath(tp.View())
	if x := got[pushed]; x.Kind != "worktree" || !x.Safe || x.Thread != "t_a" {
		t.Fatalf("pushed: %+v (all %+v)", x, got)
	}
	if x := got[dirty]; x.Safe || !strings.Contains(x.Why, "uncommitted") {
		t.Fatalf("dirty: %+v", x)
	}
	if x := got[ahead]; x.Safe || !strings.Contains(x.Why, "not pushed") {
		t.Fatalf("ahead: %+v", x)
	}
	if _, ok := got[other]; ok {
		t.Fatal("a worktree of no settled thread listed")
	}
	// Never removed by a sweep; the owner's Remove only when it is still safe.
	for _, p := range []string{pushed, dirty, ahead} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed by the sweep", p)
		}
	}
	if err := tp.Remove(dirty); err == nil {
		t.Fatal("a dirty worktree removed")
	}
	if err := tp.Remove(other); err == nil {
		t.Fatal("an unlisted path removed")
	}
	if err := tp.Remove(pushed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pushed); !os.IsNotExist(err) {
		t.Fatal("the pushed worktree stays")
	}
	if _, ok := byPath(tp.View())[pushed]; ok {
		t.Fatal("still listed")
	}
}
