package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

// dtoOf is a thread as clients see it.
func dtoOf(e *Engine, id string) DTO {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.build(e.threads[id], e.Now())
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

func gitRemote(t *testing.T, dir, url string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", url).CombinedOutput(); err != nil {
		t.Skipf("git remote: %v %s", err, out)
	}
}

// SPEC 0.5 V0.28: Fyr makes no project by matching. A thread in a clone or
// in its worktree has "No project" and a suggestion named after the
// repository; project.createFrom makes it (edited) and files both threads.
func TestNoAutoProjectSuggestAndCreateFrom(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(h.dir, "lasso-app")
	gitInit(t, repo)
	gitRemote(t, repo, "git@github.com:meros/Lasso-App.git")
	wt := filepath.Join(h.dir, "lasso-app--fix")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "fix", wt).CombinedOutput(); err != nil {
		t.Skipf("worktree: %v %s", err, out)
	}
	plain := filepath.Join(h.dir, "notes")
	os.MkdirAll(plain, 0o755)
	r1 := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "a"})
	r2 := h.cmd("thread.create", "", core.Args{"cwd": wt, "text": "b"})
	r3 := h.cmd("thread.create", "", core.Args{"cwd": plain, "text": "c"})
	if n := len(h.e.Projects()); n != 0 {
		t.Fatalf("matching made %d projects", n)
	}
	want := &core.Suggestion{Name: "lasso-app", Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: repo}, {Kind: "repo", URL: "github.com/meros/lasso-app"}}}
	for _, id := range []string{r1.ThreadID, r2.ThreadID} {
		d := dtoOf(h.e, id)
		if d.ProjectID != "" || !reflect.DeepEqual(d.Suggest, want) || d.WorkspaceID == "" {
			t.Fatalf("%s: project %q suggest %+v ws %q", id, d.ProjectID, d.Suggest, d.WorkspaceID)
		}
	}
	if d := dtoOf(h.e, r3.ThreadID); d.Suggest != nil {
		t.Fatalf("a folder without git got a suggestion: %+v", d.Suggest)
	}
	if bad := h.e.Command(Cmd{CommandID: "cf0", Action: "project.createFrom", Args: core.Args{"threadId": r3.ThreadID}}); bad.OK || bad.Status != 400 {
		t.Fatalf("createFrom without a suggestion: %+v", bad)
	}
	r := h.e.Command(Cmd{CommandID: "cf1", Action: "project.createFrom", Args: core.Args{"threadId": r2.ThreadID}})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	pid := r.Result.(map[string]any)["id"].(string)
	p1, p2 := projectOf(h.e, r1.ThreadID), projectOf(h.e, r2.ThreadID)
	if p1.ID != pid || p2.ID != pid || p1.Name != "lasso-app" || !p1.Edited || p1.Auto || p1.Color != core.DefaultColor("lasso-app") {
		t.Fatalf("%+v %+v", p1, p2)
	}
	if d := dtoOf(h.e, r1.ThreadID); d.Suggest != nil || d.Project != "lasso-app" {
		t.Fatalf("a filed thread still suggests: %+v %q", d.Suggest, d.Project)
	}
	// A new thread in the clone now matches the project at once.
	r4 := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(repo), "text": "d"})
	if projectOf(h.e, r4.ThreadID).ID != pid {
		t.Fatal("a new thread in the clone did not match")
	}
	if again := h.e.Command(Cmd{CommandID: "cf2", Action: "project.createFrom", Args: core.Args{"threadId": r1.ThreadID}}); again.OK {
		t.Fatal("createFrom on a thread that has a project")
	}
}

// SPEC 0.5 V0.28: a repo source covers the same remote on every host: two
// hosts' clones of one repository belong to one project.
func TestSameRemoteTwoHosts(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := filepath.Join(h.dir, "a", "fyr")
	b := filepath.Join(h.dir, "b", "fyr-clone")
	gitInit(t, a)
	gitInit(t, b)
	gitRemote(t, a, "https://github.com/meros/fyr.git")
	gitRemote(t, b, "git@github.com:meros/fyr.git")
	h.e.HostID = "laptop"
	ra := h.cmd("thread.create", "", core.Args{"cwd": a, "text": "x"})
	r := h.e.Command(Cmd{CommandID: "cf", Action: "project.createFrom", Args: core.Args{"threadId": ra.ThreadID}})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	h.e.HostID = "server"
	rb := h.cmd("thread.create", "", core.Args{"cwd": b, "text": "y"})
	pa, pb := projectOf(h.e, ra.ThreadID), projectOf(h.e, rb.ThreadID)
	if pa.ID == "" || pa.ID != pb.ID {
		t.Fatalf("two hosts, two projects: %+v %+v", pa, pb)
	}
	h.e.mu.Lock()
	wb := h.e.workspaces[h.e.threads[rb.ThreadID].WorkspaceID]
	h.e.mu.Unlock()
	if wb.Host != "server" || wb.Provider != "clone" {
		t.Fatalf("workspace %+v", wb)
	}
}

// SPEC 0.5 V0.28: a scratch folder has no refs: it matches no project even
// when a project has its root as a folder source, and it suggests nothing.
func TestScratchMatchesNothing(t *testing.T) {
	h := newHarness(t, time.Minute)
	root := filepath.Join(h.dir, "scratch")
	one := filepath.Join(root, "2026-11-14-x")
	gitInit(t, one)
	gitRemote(t, one, "github.com/meros/x")
	h.e.ScratchRoots = []string{root}
	h.e.Command(Cmd{CommandID: "c", Action: "project.create", Args: core.Args{"name": "Scratch", "sources": []any{map[string]any{"kind": "folder", "path": root}}}})
	r := h.cmd("thread.create", "", core.Args{"cwd": one, "text": "x"})
	d := dtoOf(h.e, r.ThreadID)
	if d.ProjectID != "" || d.Suggest != nil {
		t.Fatalf("scratch: project %q suggest %+v", d.ProjectID, d.Suggest)
	}
}

// SPEC 0.5 V0.23: a seeded workspace root wins over a repository root, and
// project.update changes the identity; the beam amber is refused.
func TestProjectSeedAndUpdate(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "norban")
	gitInit(t, filepath.Join(ws, "web"))
	h.e.SeedProjects([]core.Project{{Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: ws}}, Name: "norban", Icon: core.Icon{Kind: "line", Value: "building"}}})
	r := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "x"})
	p := projectOf(h.e, r.ThreadID)
	if p.Name != "norban" || p.Icon.Value != "building" {
		t.Fatalf("%+v", p)
	}
	up := h.e.Command(Cmd{CommandID: "u1", Action: "project.update", Args: core.Args{"id": p.ID, "name": "Norban", "color": "violet", "icon": map[string]any{"kind": "emoji", "value": "🏠"}}})
	if !up.OK {
		t.Fatalf("%+v", up)
	}
	p = projectOf(h.e, r.ThreadID)
	if p.Name != "Norban" || p.Color != "violet" || p.Icon.Value != "🏠" {
		t.Fatalf("%+v", p)
	}
	if bad := h.e.Command(Cmd{CommandID: "u2", Action: "project.update", Args: core.Args{"id": p.ID, "color": "amber"}}); bad.OK || bad.Status != 400 {
		t.Fatalf("amber accepted: %+v", bad)
	}
	h.e.SeedProjects([]core.Project{{Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: ws}}, Name: "norban again"}})
	if projectOf(h.e, r.ThreadID).Name != "Norban" {
		t.Fatal("a second seed overwrote the user's edit")
	}
}

// SPEC 0.5 V0.28: a thread from before workspaces gets one at load, and,
// without a project, the project that matching finds; its workspace is
// stored and reused.
func TestWorkspaceBackfillAtLoad(t *testing.T) {
	h := newHarness(t, time.Minute)
	c := h.e.Command(Cmd{CommandID: "p", Action: "project.create", Args: core.Args{"name": "Here", "sources": []any{map[string]any{"kind": "folder", "path": h.dir}}}})
	pid := c.Result.(map[string]any)["id"].(string)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "x"})
	h.m.StopAll()
	// Make the thread look as an older build stored it.
	th, _ := h.e.Thread(r.ThreadID)
	old := th
	old.WorkspaceID, old.ProjectID, old.Project = "", "", ""
	h.e.st.Write(func(tx *store.Tx) error { return tx.PutThread(&old) })
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	d, _ := e2.Thread(r.ThreadID)
	if d.WorkspaceID != th.WorkspaceID || d.ProjectID != pid {
		t.Fatalf("after load: ws %q (was %q) project %q", d.WorkspaceID, th.WorkspaceID, d.ProjectID)
	}
	ws, _ := st.Workspaces()
	if len(ws) != 1 || ws[0].Path != h.dir || ws[0].Provider != "folder" {
		t.Fatalf("workspaces %+v", ws)
	}
}

// SPEC 0.5 V0.23: a project needs no git and no source; threads are filed
// into it by hand, and folder matching keeps the choice.
func TestProjectWithoutSourceAndManualFiling(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.e.Command(Cmd{CommandID: "p1", Action: "project.create", Args: core.Args{"name": "Hemmet"}})
	if !r.OK {
		t.Fatalf("a project without sources was refused: %+v", r)
	}
	pid := r.Result.(map[string]any)["id"].(string)
	th := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "recept"})
	if mv := h.cmd("thread.setProject", th.ThreadID, core.Args{"projectId": pid}); !mv.OK {
		t.Fatalf("%+v", mv)
	}
	if p := projectOf(h.e, th.ThreadID); p.Name != "Hemmet" {
		t.Fatalf("not filed: %+v", p)
	}
	h.cmd("thread.send", th.ThreadID, core.Args{"text": "again"})
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	e2.Load()
	if p := projectOf(e2, th.ThreadID); p.Name != "Hemmet" {
		t.Fatalf("folder matching moved the thread back: %+v", p)
	}
	if bad := h.e.Command(Cmd{CommandID: "x", Action: "thread.setProject", ThreadID: th.ThreadID, Args: core.Args{"projectId": "p_nope"}}); bad.OK {
		t.Fatal("an unknown project was accepted")
	}
}

// SPEC 0.5 V0.23, V0.28: seeding (now only an import) and Rematch move
// threads to the matching project; a hand-filed thread stays.
func TestSeedThenRematch(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "norban")
	os.MkdirAll(filepath.Join(ws, "web"), 0o755)
	r := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "x"})
	label := h.e.Command(Cmd{CommandID: "l", Action: "project.create", Args: core.Args{"name": "Hemmet"}})
	pid := label.Result.(map[string]any)["id"].(string)
	m := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "y"})
	h.cmd("thread.setProject", m.ThreadID, core.Args{"projectId": pid})

	h.e.SeedProjects([]core.Project{{Name: "norban", Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: ws}}}})
	h.e.Rematch()
	if p := projectOf(h.e, r.ThreadID); p.Name != "norban" {
		t.Fatalf("not moved to the seeded project: %+v", p)
	}
	if p := projectOf(h.e, m.ThreadID); p.Name != "Hemmet" {
		t.Fatalf("a hand-filed thread moved: %+v", p)
	}
	for _, p := range h.e.Projects() {
		if p.Name == "Hemmet" && p.Threads != 1 {
			t.Fatal("the label project lost its thread")
		}
	}
}

// SPEC 0.5 V0.28: a git repository in the home folder has no rule of its
// own: it makes no project, and a folder below it matches only a project
// that has the home folder.
func TestHomeGitRootMakesNoProject(t *testing.T) {
	h := newHarness(t, time.Minute)
	home := filepath.Join(h.dir, "home")
	gitInit(t, home)
	t.Setenv("HOME", home)
	folder := filepath.Join(home, "notes", "trip")
	os.MkdirAll(folder, 0o755)
	r := h.cmd("thread.create", "", core.Args{"cwd": folder, "text": "x"})
	if p := projectOf(h.e, r.ThreadID); p.ID != "" || len(h.e.Projects()) != 0 {
		t.Fatalf("a project was made: %+v", p)
	}
}

// SPEC 0.5 V0.23: a folder source must exist on the host; a source can be
// removed; a change to sources re-matches threads at once. (Found live: a
// project made with the typo /home/meris/… matched nothing and could not be
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

// SPEC 0.5 V0.28: at start, auto projects that hold threads become normal
// projects, empty unedited ones go, and filed threads stay where they are
// (a scratch thread in a seeded Scratch project keeps it; a thread in the
// home folder keeps "No project").
func TestAutoProjectMigration(t *testing.T) {
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
	put := func(p *core.Project) {
		h.e.st.Write(func(tx *store.Tx) error { return tx.PutProject(p) })
	}
	put(&core.Project{ID: "p_scratch", Name: "Scratch", Seeded: true, Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: scratch}}})
	put(&core.Project{ID: "p_app", Name: "app", Auto: true, Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: app}}})
	put(&core.Project{ID: "p_empty", Name: "empty", Auto: true, Sources: []core.Source{{Kind: "folder", Host: h.e.HostID, Path: filepath.Join(home, "gone")}}})
	put(&core.Project{ID: "p_kept", Name: "kept", Auto: true, Edited: true})
	h.m.StopAll()
	for id, pid := range map[string]string{a.ThreadID: "p_scratch", b.ThreadID: "p_app"} {
		th, _ := h.e.Thread(id)
		cp := th
		cp.ProjectID = pid
		h.e.st.Write(func(tx *store.Tx) error { return tx.PutThread(&cp) })
	}
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if p := projectOf(e2, a.ThreadID); p.ID != "p_scratch" {
		t.Fatalf("the scratch thread moved: %+v", p)
	}
	if p := projectOf(e2, b.ThreadID); p.ID != "p_app" || p.Auto {
		t.Fatalf("the auto project with a thread: %+v", p)
	}
	if p := projectOf(e2, c.ThreadID); p.ID != "" {
		t.Fatalf("a thread in the home folder got a project: %+v", p)
	}
	ids := map[string]bool{}
	for _, p := range e2.Projects() {
		ids[p.ID] = true
		if p.Auto {
			t.Fatalf("an auto flag stayed: %+v", p)
		}
	}
	if ids["p_empty"] || !ids["p_kept"] || !ids["p_app"] || !ids["p_scratch"] {
		t.Fatalf("projects after migration: %v", ids)
	}
	stored, _ := st.Projects()
	if len(stored) != 3 {
		t.Fatalf("stored projects: %d", len(stored))
	}
}

// SPEC 0.5 V0.23, V0.28: a thread in the home folder has no project, and none is made.
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
