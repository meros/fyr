package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
}

func projectOf(e *Engine, threadID string) core.Project {
	th, _ := e.Thread(threadID)
	for _, p := range e.Projects() {
		if p.ID == th.ProjectID {
			return p.Project
		}
	}
	return core.Project{}
}

// SPEC 0.5 V0.23: a thread in a worktree belongs to the main repository's
// project, made on first use with a stable colour and a monogram.
func TestProjectFromGitRoot(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(h.dir, "lasso-app")
	gitInit(t, repo)
	wt := filepath.Join(repo, ".claude", "worktrees", "fix")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "fix", wt).CombinedOutput(); err != nil {
		t.Skipf("worktree: %v %s", err, out)
	}
	r1 := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(repo), "text": "a"})
	r2 := h.cmd("thread.create", "", core.Args{"cwd": wt, "text": "b"})
	p1, p2 := projectOf(h.e, r1.ThreadID), projectOf(h.e, r2.ThreadID)
	if p1.ID == "" || p1.ID != p2.ID || p1.Folder(h.e.HostID) != repo || p1.Name != "lasso-app" {
		t.Fatalf("%+v %+v", p1, p2)
	}
	if p1.Color != core.DefaultColor("lasso-app") || p1.Icon.Kind != "monogram" {
		t.Fatalf("identity %+v", p1)
	}
	th, _ := h.e.Thread(r2.ThreadID)
	if th.Project != "lasso-app" {
		t.Fatalf("the thread's project name is %q", th.Project)
	}
}

// SPEC 0.5 V0.23: a seeded workspace root wins over a repository root, and
// project.update changes the identity; the beam amber is refused.
func TestProjectSeedAndUpdate(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "acme")
	gitInit(t, filepath.Join(ws, "web"))
	h.e.SeedProjects([]core.Project{{Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: ws}}, Name: "acme", Icon: core.Icon{Kind: "line", Value: "building"}}})
	r := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "x"})
	p := projectOf(h.e, r.ThreadID)
	if p.Name != "acme" || p.Icon.Value != "building" {
		t.Fatalf("%+v", p)
	}
	up := h.e.Command(Cmd{CommandID: "u1", Action: "project.update", Args: core.Args{"id": p.ID, "name": "Acme", "color": "violet", "icon": map[string]any{"kind": "emoji", "value": "🏠"}}})
	if !up.OK {
		t.Fatalf("%+v", up)
	}
	p = projectOf(h.e, r.ThreadID)
	if p.Name != "Acme" || p.Color != "violet" || p.Icon.Value != "🏠" {
		t.Fatalf("%+v", p)
	}
	if bad := h.e.Command(Cmd{CommandID: "u2", Action: "project.update", Args: core.Args{"id": p.ID, "color": "amber"}}); bad.OK || bad.Status != 400 {
		t.Fatalf("amber accepted: %+v", bad)
	}
	h.e.SeedProjects([]core.Project{{Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: ws}}, Name: "acme again"}})
	if projectOf(h.e, r.ThreadID).Name != "Acme" {
		t.Fatal("a second seed overwrote the user's edit")
	}
}

// SPEC 0.5 V0.23: projects persist; threads made before projects get one at load.
func TestProjectsPersistAndBackfill(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "x"})
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if p := projectOf(e2, r.ThreadID); p.ID == "" || p.Folder(e2.HostID) != h.dir {
		t.Fatalf("after load %+v", p)
	}
}

// SPEC 0.5 V0.23: a project needs no git and no source; threads are filed
// into it by hand, and folder matching keeps the choice.
func TestProjectWithoutSourceAndManualFiling(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.e.Command(Cmd{CommandID: "p1", Action: "project.create", Args: core.Args{"name": "Garden"}})
	if !r.OK {
		t.Fatalf("a project without sources was refused: %+v", r)
	}
	pid := r.Result.(map[string]any)["id"].(string)
	th := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "recept"})
	if mv := h.cmd("thread.setProject", th.ThreadID, core.Args{"projectId": pid}); !mv.OK {
		t.Fatalf("%+v", mv)
	}
	if p := projectOf(h.e, th.ThreadID); p.Name != "Garden" {
		t.Fatalf("not filed: %+v", p)
	}
	h.cmd("thread.send", th.ThreadID, core.Args{"text": "again"})
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	e2.Load()
	if p := projectOf(e2, th.ThreadID); p.Name != "Garden" {
		t.Fatalf("folder matching moved the thread back: %+v", p)
	}
	if bad := h.e.Command(Cmd{CommandID: "x", Action: "thread.setProject", ThreadID: th.ThreadID, Args: core.Args{"projectId": "p_nope"}}); bad.OK {
		t.Fatal("an unknown project was accepted")
	}
}

// SPEC 0.5 V0.23: seeding after threads exist re-matches them; empty auto
// projects go; a hand-filed thread stays. (Found live: the first start
// filed threads before the T3 seed.)
func TestSeedThenRematch(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "acme")
	os.MkdirAll(filepath.Join(ws, "web"), 0o755)
	r := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "x"})
	auto := projectOf(h.e, r.ThreadID)
	label := h.e.Command(Cmd{CommandID: "l", Action: "project.create", Args: core.Args{"name": "Garden"}})
	pid := label.Result.(map[string]any)["id"].(string)
	m := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "y"})
	h.cmd("thread.setProject", m.ThreadID, core.Args{"projectId": pid})

	h.e.SeedProjects([]core.Project{{Name: "acme", Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: ws}}}})
	h.e.Rematch()
	if p := projectOf(h.e, r.ThreadID); p.Name != "acme" {
		t.Fatalf("not moved to the seeded project: %+v", p)
	}
	if p := projectOf(h.e, m.ThreadID); p.Name != "Garden" {
		t.Fatalf("a hand-filed thread moved: %+v", p)
	}
	for _, p := range h.e.Projects() {
		if p.ID == auto.ID {
			t.Fatalf("the empty auto project stayed: %+v", p)
		}
		if p.Name == "Garden" && p.Threads != 1 {
			t.Fatal("the label project lost its thread")
		}
	}
}

// SPEC 0.5 V0.23: a git repository in the home folder is not a project root.
func TestHomeGitRootIgnored(t *testing.T) {
	h := newHarness(t, time.Minute)
	home := filepath.Join(h.dir, "home")
	gitInit(t, home)
	t.Setenv("HOME", home)
	folder := filepath.Join(home, "notes", "trip")
	os.MkdirAll(folder, 0o755)
	r := h.cmd("thread.create", "", core.Args{"cwd": folder, "text": "x"})
	if p := projectOf(h.e, r.ThreadID); p.Folder(h.e.HostID) != folder || p.Name != "trip" {
		t.Fatalf("home repo swallowed the folder: %+v", p)
	}
}

// SPEC 0.5 V0.23: a folder source must exist on the host; a source can be
// removed; a change to sources re-matches threads at once. (Found live: a
// project made with the typo /home/owner/… matched nothing and could not be
// corrected.)
func TestFolderSourceChecksAndRematch(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(h.dir, "src", "fyr")
	os.MkdirAll(repo, 0o755)
	h.e.SeedProjects([]core.Project{{Name: "home", Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: h.dir}}}})
	r := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "x"})
	if p := projectOf(h.e, r.ThreadID); p.Name != "home" {
		t.Fatalf("setup: %+v", p)
	}
	typo := core.Args{"name": "Fyr", "sources": []any{map[string]any{"kind": "folder", "host": h.e.HostID, "path": filepath.Join(h.dir, "nope", "fyr")}}}
	if bad := h.e.Command(Cmd{CommandID: "c1", Action: "project.create", Args: typo}); bad.OK || bad.Status != 400 {
		t.Fatalf("a missing folder was accepted: %+v", bad)
	}
	c := h.e.Command(Cmd{CommandID: "c2", Action: "project.create", Args: core.Args{"name": "Fyr"}})
	pid := c.Result.(map[string]any)["id"].(string)
	if bad := h.e.Command(Cmd{CommandID: "a1", Action: "project.addSource", Args: core.Args{"id": pid, "source": map[string]any{"kind": "folder", "host": h.e.HostID, "path": "relative/fyr"}}}); bad.OK || bad.Status != 400 {
		t.Fatalf("a relative folder was accepted: %+v", bad)
	}
	if a := h.e.Command(Cmd{CommandID: "a2", Action: "project.addSource", Args: core.Args{"id": pid, "source": map[string]any{"kind": "folder", "host": h.e.HostID, "path": repo + "/"}}}); !a.OK {
		t.Fatalf("%+v", a)
	}
	if p := projectOf(h.e, r.ThreadID); p.Name != "Fyr" {
		t.Fatalf("not re-matched after addSource: %+v", p)
	}
	if rm := h.e.Command(Cmd{CommandID: "r1", Action: "project.removeSource", Args: core.Args{"id": pid, "index": 0}}); !rm.OK {
		t.Fatalf("%+v", rm)
	}
	if p := projectOf(h.e, r.ThreadID); p.Name != "home" {
		t.Fatalf("not re-matched after removeSource: %+v", p)
	}
	if bad := h.e.Command(Cmd{CommandID: "r2", Action: "project.removeSource", Args: core.Args{"id": pid, "index": 3}}); bad.OK || bad.Status != 400 {
		t.Fatalf("a bad index was accepted: %+v", bad)
	}
}

// SPEC 0.5 V0.23: projects made by matching before the auto flag existed
// (no auto, no edit, no seed) are auto projects: they stop shadowing seeded
// ones and go when empty. (Found live: per-scratch-folder projects and a
// "meros" home project kept their threads after the re-match.)
func TestLegacyAutoProjectsRetired(t *testing.T) {
	h := newHarness(t, time.Minute)
	home := filepath.Join(h.dir, "home")
	t.Setenv("HOME", home)
	scratch := filepath.Join(home, ".t3", "scratch")
	one := filepath.Join(scratch, "2026-11-13-plan")
	app := filepath.Join(home, "src", "app")
	os.MkdirAll(one, 0o755)
	os.MkdirAll(app, 0o755)
	a := h.cmd("thread.create", "", core.Args{"cwd": one, "text": "x"})
	b := h.cmd("thread.create", "", core.Args{"cwd": app, "text": "y"})
	c := h.cmd("thread.create", "", core.Args{"cwd": home, "text": "z"})
	// Make every project legacy, and add a legacy home project that holds b and c.
	h.e.mu.Lock()
	legacyHome := &core.Project{ID: "p_home", Name: "home", Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: home}}}
	h.e.projects[legacyHome.ID] = legacyHome
	for _, p := range h.e.projects {
		p.Auto = false
	}
	h.e.threads[b.ThreadID].ProjectID = "p_home"
	h.e.threads[c.ThreadID].ProjectID = "p_home"
	h.e.mu.Unlock()

	h.e.SeedProjects([]core.Project{{Name: "Scratch", Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: scratch}}}})
	h.e.Rematch()
	if p := projectOf(h.e, a.ThreadID); p.Name != "Scratch" {
		t.Fatalf("a legacy folder project shadowed the seeded one: %+v", p)
	}
	if p := projectOf(h.e, b.ThreadID); p.Name != "app" {
		t.Fatalf("the home project kept a thread from a folder below it: %+v", p)
	}
	if id := h.e.threads[c.ThreadID].ProjectID; id != "" {
		t.Fatalf("a thread in the home folder got a project: %s", id)
	}
	for _, p := range h.e.Projects() {
		if f := p.Folder(h.e.HostID); f == home || f == one {
			t.Fatalf("a legacy project stayed: %+v", p)
		}
	}
}

// SPEC 0.5 V0.23: the home folder and / are never an auto project.
func TestHomeFolderThreadHasNoProject(t *testing.T) {
	h := newHarness(t, time.Minute)
	home := filepath.Join(h.dir, "home")
	os.MkdirAll(home, 0o755)
	t.Setenv("HOME", home)
	r := h.cmd("thread.create", "", core.Args{"cwd": home + "/", "text": "x"})
	if id := h.e.threads[r.ThreadID].ProjectID; id != "" {
		t.Fatalf("a thread in the home folder got a project: %+v", h.e.projects[id])
	}
	if n := len(h.e.Projects()); n != 0 {
		t.Fatalf("a project was made for the home folder: %d", n)
	}
}
