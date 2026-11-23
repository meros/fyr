package engine

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
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
func dtoOf(e *Engine, id string) api.Thread {
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

// SPEC 0.5 V0.28, V0.55: Fyr makes no project by matching. A thread in a
// folder that no project has gets "No project" and newProject {name,
// folder}; project.create {folder} makes the project and every unfiled
// thread in that folder or below joins it.
func TestNoAutoProjectAndCreate(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(h.dir, "lasso-app")
	gitInit(t, repo)
	sub := filepath.Join(repo, "web")
	os.MkdirAll(sub, 0o755)
	r1 := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "a"})
	r2 := h.cmd("thread.create", "", core.Args{"cwd": sub, "text": "b"})
	if n := len(h.e.Projects()); n != 0 {
		t.Fatalf("matching made %d projects", n)
	}
	if d := dtoOf(h.e, r1.ThreadID); d.ProjectID != "" || d.NewProject == nil || *d.NewProject != (api.NewProject{Name: "lasso-app", Folder: repo}) {
		t.Fatalf("project %q newProject %+v", d.ProjectID, d.NewProject)
	}
	r := h.cmd("project.create", "", core.Args{"name": "Lasso", "folder": repo})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	pid := r.Result.(map[string]any)["id"].(string)
	p1, p2 := projectOf(h.e, r1.ThreadID), projectOf(h.e, r2.ThreadID)
	if p1.ID != pid || p2.ID != pid || !p1.Edited || p1.Color != core.DefaultColor("Lasso") {
		t.Fatalf("%+v %+v", p1, p2)
	}
	if th, _ := h.e.Thread(r2.ThreadID); th.ProjectManual || dtoOf(h.e, r2.ThreadID).Project != "Lasso" {
		t.Fatalf("matched thread: %+v", th)
	}
	if d := dtoOf(h.e, r1.ThreadID); d.NewProject != nil {
		t.Fatalf("newProject repeats a project's folder: %+v", d.NewProject)
	}
	r4 := h.cmd("thread.create", "", core.Args{"cwd": sub, "text": "d"})
	if projectOf(h.e, r4.ThreadID).ID != pid {
		t.Fatal("a new thread below the folder did not land")
	}
	// A name is taken from the folder when none is given.
	other := filepath.Join(h.dir, "notes")
	os.MkdirAll(other, 0o755)
	if r := h.cmd("project.create", "", core.Args{"folder": other}); !r.OK || projectDTO(h.e, r.Result.(map[string]any)["id"].(string)).Name != "notes" {
		t.Fatalf("name from folder: %+v", r)
	}
}

// SPEC 0.5 V0.23: project.update changes the identity; the beam amber is
// refused.
func TestProjectUpdate(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "acme")
	os.MkdirAll(filepath.Join(ws, "web"), 0o755)
	h.cmd("project.create", "", core.Args{"name": "acme", "folder": ws})
	r := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(ws, "web"), "text": "x"})
	p := projectOf(h.e, r.ThreadID)
	if p.Name != "acme" {
		t.Fatalf("%+v", p)
	}
	up := h.e.Command(api.Cmd{CommandID: "u1", Action: "project.update", Args: core.Args{"id": p.ID, "name": "Acme", "color": "violet", "icon": map[string]any{"kind": "emoji", "value": "🏠"}}})
	if !up.OK {
		t.Fatalf("%+v", up)
	}
	p = projectOf(h.e, r.ThreadID)
	if p.Name != "Acme" || p.Color != "violet" || p.Icon.Value != "🏠" || p.Folder != ws {
		t.Fatalf("%+v", p)
	}
	if d := dtoOf(h.e, r.ThreadID); d.Project != "Acme" {
		t.Fatalf("the thread kept the old name: %q", d.Project)
	}
	if bad := h.e.Command(api.Cmd{CommandID: "u2", Action: "project.update", Args: core.Args{"id": p.ID, "color": "amber"}}); bad.OK || bad.Status != 400 {
		t.Fatalf("amber accepted: %+v", bad)
	}
}

// SPEC 0.5 V0.55: at start a thread not filed by hand lands in the project
// whose folder covers it, and a thread whose project is gone loses it.
func TestMatchAtLoad(t *testing.T) {
	h := newHarness(t, time.Minute)
	c := h.cmd("project.create", "", core.Args{"name": "Here", "folder": h.dir})
	pid := c.Result.(map[string]any)["id"].(string)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "x"})
	g := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "y"})
	h.m.StopAll()
	th, _ := h.e.Thread(r.ThreadID)
	old := th
	old.ProjectID = ""
	h.e.st.Write(func(tx *store.Tx) error { return tx.PutThread(&old) })
	tg, _ := h.e.Thread(g.ThreadID)
	tg.ProjectID, tg.ProjectManual = "p_gone", true
	h.e.st.Write(func(tx *store.Tx) error { return tx.PutThread(&tg) })
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if d := dtoOf(e2, r.ThreadID); d.ProjectID != pid || d.Project != "Here" {
		t.Fatalf("after load: project %q %q", d.ProjectID, d.Project)
	}
	if d, _ := e2.Thread(g.ThreadID); d.ProjectID != pid || d.ProjectManual {
		t.Fatalf("a thread whose project is gone: %q %v", d.ProjectID, d.ProjectManual)
	}
}

// SPEC 0.5 V0.23, V0.55: a project needs no folder; threads are filed into
// it by hand, and folder matching keeps the choice.
func TestProjectWithoutFolderAndManualFiling(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.cmd("project.create", "", core.Args{"name": "Here", "folder": h.dir})
	r := h.e.Command(api.Cmd{CommandID: "p1", Action: "project.create", Args: core.Args{"name": "Garden"}})
	if !r.OK {
		t.Fatalf("a project without a folder was refused: %+v", r)
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
	h.cmd("project.update", "", core.Args{"id": pid}) // every project Input files threads again
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	e2.Load()
	if p := projectOf(e2, th.ThreadID); p.Name != "Garden" {
		t.Fatalf("folder matching moved the thread back: %+v", p)
	}
	if bad := h.e.Command(api.Cmd{CommandID: "x", Action: "thread.setProject", ThreadID: th.ThreadID, Args: core.Args{"projectId": "p_nope"}}); bad.OK {
		t.Fatal("an unknown project was accepted")
	}
}

// SPEC 0.5 V0.28: a git repository in the home folder has no rule of its
// own: it makes no project.
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

// SPEC 0.5 V0.23, V0.55: a folder must exist on the host and be a full
// path; a change of folder matches threads again at once, the longest folder
// wins, a hand-filed thread stays, and a thread that nothing covers keeps
// its project. (Found
// live: a project made with the typo /home/owner/… matched nothing.)
func TestFolderChecksAndRematch(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(h.dir, "src", "fyr")
	os.MkdirAll(repo, 0o755)
	h.cmd("project.create", "", core.Args{"name": "home", "folder": h.dir})
	r := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "x"})
	m := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "y"})
	if p := projectOf(h.e, r.ThreadID); p.Name != "home" {
		t.Fatalf("setup: %+v", p)
	}
	h.cmd("thread.setProject", m.ThreadID, core.Args{"projectId": ""})
	typo := core.Args{"name": "Fyr", "folder": filepath.Join(h.dir, "nope", "fyr")}
	if bad := h.e.Command(api.Cmd{CommandID: "c1", Action: "project.create", Args: typo}); bad.OK || bad.Status != 400 || !strings.Contains(bad.Error, "nope") {
		t.Fatalf("a missing folder was accepted: %+v", bad)
	}
	c := h.e.Command(api.Cmd{CommandID: "c2", Action: "project.create", Args: core.Args{"name": "Fyr"}})
	pid := c.Result.(map[string]any)["id"].(string)
	if bad := h.e.Command(api.Cmd{CommandID: "a1", Action: "project.update", Args: core.Args{"id": pid, "folder": "relative/fyr"}}); bad.OK || bad.Status != 400 {
		t.Fatalf("a relative folder was accepted: %+v", bad)
	}
	if a := h.e.Command(api.Cmd{CommandID: "a2", Action: "project.update", Args: core.Args{"id": pid, "folder": repo + "/"}}); !a.OK {
		t.Fatalf("%+v", a)
	}
	if p := projectOf(h.e, r.ThreadID); p.Name != "Fyr" {
		t.Fatalf("not matched again after the folder change: %+v", p)
	}
	if p := projectOf(h.e, m.ThreadID); p.ID != "" {
		t.Fatalf("a thread unfiled by hand was matched: %+v", p)
	}
	if rm := h.e.Command(api.Cmd{CommandID: "r1", Action: "project.update", Args: core.Args{"id": pid, "folder": ""}}); !rm.OK {
		t.Fatalf("%+v", rm)
	}
	if p := projectOf(h.e, r.ThreadID); p.Name != "home" {
		t.Fatalf("after clearing the folder the next folder up did not win: %+v", p)
	}
	// Nothing covers the thread now: it keeps its project.
	if rm := h.e.Command(api.Cmd{CommandID: "r2", Action: "project.update", Args: core.Args{"id": projectOf(h.e, r.ThreadID).ID, "folder": ""}}); !rm.OK {
		t.Fatalf("%+v", rm)
	}
	if p := projectOf(h.e, r.ThreadID); p.Name != "home" {
		t.Fatalf("matching took the project away: %+v", p)
	}
}

// SPEC 0.5 V0.28: at start, auto projects that hold threads become normal
// projects, empty unedited ones go, and filed threads stay where they are.
func TestAutoProjectMigration(t *testing.T) {
	h := newHarness(t, time.Minute)
	home := filepath.Join(h.dir, "home")
	t.Setenv("HOME", home)
	app := filepath.Join(home, "src", "app")
	os.MkdirAll(app, 0o755)
	b := h.cmd("thread.create", "", core.Args{"cwd": app, "text": "y"})
	c := h.cmd("thread.create", "", core.Args{"cwd": home, "text": "z"})
	// V0.flaky-auto-migration: both runs end before the store is written
	// behind the engine's back; a run that ends later writes its thread again,
	// without the project set below.
	h.wait(b.ThreadID, "done", state(core.Done))
	h.wait(c.ThreadID, "done", state(core.Done))
	put := func(p *core.Project) {
		h.e.st.Write(func(tx *store.Tx) error { return tx.PutProject(p) })
	}
	put(&core.Project{ID: "p_app", Name: "app", Auto: true, Folder: app})
	put(&core.Project{ID: "p_empty", Name: "empty", Auto: true, Folder: filepath.Join(home, "gone")})
	put(&core.Project{ID: "p_kept", Name: "kept", Auto: true, Edited: true})
	h.m.StopAll()
	th, _ := h.e.Thread(b.ThreadID)
	th.ProjectID = "p_app"
	h.e.st.Write(func(tx *store.Tx) error { return tx.PutThread(&th) })
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
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
	if ids["p_empty"] || !ids["p_kept"] || !ids["p_app"] {
		t.Fatalf("projects after migration: %v", ids)
	}
}

// SPEC 0.5 V0.55: a project stored with sources keeps one folder (the main
// one on this host, else the first here, else the first anywhere); the rest
// is dropped, and threads keep their projects.
func TestFolderMigration(t *testing.T) {
	h := newHarness(t, time.Minute)
	host := h.e.HostID
	a, b := filepath.Join(h.dir, "a"), filepath.Join(h.dir, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	th := h.cmd("thread.create", "", core.Args{"cwd": b, "text": "x"})
	h.m.StopAll()
	h.e.st.Close()
	// Close does not wait for a write the engine's goroutines have under way
	// (the agent the thread started): without a busy timeout the INSERT
	// below meets its lock and fails at once with SQLITE_BUSY.
	db, err := sql.Open("sqlite", "file:"+h.db+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{
		"p_main":  `{"id":"p_main","name":"Main","color":"teal","icon":{"kind":"monogram"},"sources":[{"kind":"repo","url":"github.com/me/x"},{"kind":"folder","host":"other","path":"/elsewhere","main":true},{"kind":"folder","host":"` + host + `","path":"` + a + `"},{"kind":"folder","host":"` + host + `","path":"` + b + `","main":true}]}`,
		"p_first": `{"id":"p_first","name":"First","color":"teal","icon":{"kind":"monogram"},"sources":[{"kind":"folder","host":"other","path":"/elsewhere"},{"kind":"folder","host":"` + host + `","path":"` + a + `/"},{"kind":"folder","host":"` + host + `","path":"` + b + `"}]}`,
		"p_away":  `{"id":"p_away","name":"Away","color":"teal","icon":{"kind":"monogram"},"sources":[{"kind":"repo","url":"github.com/me/y"},{"kind":"folder","host":"other","path":"/srv/y"}]}`,
		"p_repo":  `{"id":"p_repo","name":"Repo","color":"teal","icon":{"kind":"monogram"},"sources":[{"kind":"repo","url":"github.com/me/z"}]}`,
	}
	for id, data := range rows {
		if _, err := db.Exec(`INSERT INTO projects(id,data) VALUES(?,?)`, id, data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE threads SET data=json_set(data,'$.projectId','p_first','$.projectManual',json('true'),'$.workspaceId','w_x')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"p_main": b, "p_first": a, "p_away": "/srv/y", "p_repo": ""}
	for _, p := range e2.Projects() {
		if p.Folder != want[p.ID] {
			t.Errorf("%s: folder %q, want %q", p.ID, p.Folder, want[p.ID])
		}
	}
	if p := projectOf(e2, th.ThreadID); p.ID != "p_first" {
		t.Fatalf("the thread lost its project: %+v", p)
	}
	old, _ := st.OldProjectSources()
	if len(old) != 0 {
		t.Fatalf("sources still stored: %v", old)
	}
	ps, _ := st.Projects()
	if len(ps) != 4 {
		t.Fatalf("stored projects: %d", len(ps))
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
	if d := dtoOf(h.e, r.ThreadID); d.NewProject != nil {
		t.Fatalf("the home folder offers a project: %+v", d.NewProject)
	}
}

// SPEC 0.5 V0.30: project.create with threadId makes the project and files
// that thread by hand; other unfiled threads in the same folder join it by
// matching. An unknown thread is a 404 and makes nothing.
func TestCreateAndFile(t *testing.T) {
	h := newHarness(t, time.Minute)
	dir := filepath.Join(h.dir, "Garden")
	os.MkdirAll(dir, 0o755)
	a := h.cmd("thread.create", "", core.Args{"cwd": dir, "text": "a"})
	b := h.cmd("thread.create", "", core.Args{"cwd": dir, "text": "b"})
	if bad := h.cmd("project.create", "", core.Args{"name": "X", "threadId": "t_nope"}); bad.OK || bad.Status != 404 || len(h.e.Projects()) != 0 {
		t.Fatalf("unknown thread: %+v, %d projects", bad, len(h.e.Projects()))
	}
	r := h.cmd("project.create", "", core.Args{"name": "Home", "threadId": a.ThreadID, "folder": dir})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	pid := r.Result.(map[string]any)["id"].(string)
	ta := dtoOf(h.e, a.ThreadID)
	if ta.ProjectID != pid || !ta.ProjectManual || ta.Project != "Home" {
		t.Fatalf("filed: %+v %v %q", ta.ProjectID, ta.ProjectManual, ta.Project)
	}
	if tb, _ := h.e.Thread(b.ThreadID); tb.ProjectID != pid || tb.ProjectManual {
		t.Fatalf("the other thread did not join by matching: %q %v", tb.ProjectID, tb.ProjectManual)
	}
	// A label made from a thread files it too.
	c := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "c"})
	r2 := h.cmd("project.create", "", core.Args{"name": "Kök", "threadId": c.ThreadID})
	if tc := dtoOf(h.e, c.ThreadID); !r2.OK || tc.Project != "Kök" || !tc.ProjectManual {
		t.Fatalf("label: %+v %q", r2, tc.Project)
	}
}

// SPEC 0.5 V0.30: thread.setProject with an empty projectId unfiles the
// thread; it counts as filed by hand, so matching leaves it unfiled.
func TestUnfile(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(h.dir, "lasso-app")
	os.MkdirAll(repo, 0o755)
	th := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "a"})
	pr := h.cmd("project.create", "", core.Args{"name": "Lasso", "folder": repo})
	if !pr.OK {
		t.Fatalf("%+v", pr)
	}
	if r := h.cmd("thread.setProject", th.ThreadID, core.Args{"projectId": ""}); !r.OK {
		t.Fatalf("unfile: %+v", r)
	}
	h.cmd("project.update", "", core.Args{"id": pr.Result.(map[string]any)["id"]}) // every project Input files threads again
	h.cmd("thread.send", th.ThreadID, core.Args{"text": "b"})
	got := dtoOf(h.e, th.ThreadID)
	if got.ProjectID != "" || got.Project != "" || !got.ProjectManual {
		t.Fatalf("after unfile: %q %q %v", got.ProjectID, got.Project, got.ProjectManual)
	}
}

// SPEC 0.5 V0.58: project.delete removes the project; its threads lose it and
// the filed-by-hand mark, then matching runs, so a thread inside another
// project's folder lands there. The stream says {id, deleted: true}.
func TestProjectDelete(t *testing.T) {
	h := newHarness(t, time.Minute)
	outer := filepath.Join(h.dir, "work")
	inner := filepath.Join(outer, "app")
	os.MkdirAll(inner, 0o755)
	if r := h.cmd("project.create", "", core.Args{"name": "Work", "folder": outer}); !r.OK {
		t.Fatalf("%+v", r)
	}
	r := h.cmd("project.create", "", core.Args{"name": "App", "folder": inner})
	pid := r.Result.(map[string]any)["id"].(string)
	a := h.cmd("thread.create", "", core.Args{"cwd": inner, "text": "a"})
	b := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "b"})
	if r := h.cmd("thread.setProject", b.ThreadID, core.Args{"projectId": pid}); !r.OK {
		t.Fatalf("file b: %+v", r)
	}
	if p := projectOf(h.e, a.ThreadID); p.ID != pid {
		t.Fatalf("a is in %q, want App", p.Name)
	}
	if bad := h.cmd("project.delete", "", core.Args{"id": "p_nope"}); bad.OK || bad.Status != 404 {
		t.Fatalf("unknown id: %+v", bad)
	}
	ch := h.e.Hub.Subscribe()
	defer h.e.Hub.Unsubscribe(ch)
	if r := h.cmd("project.delete", "", core.Args{"id": pid}); !r.OK {
		t.Fatalf("delete: %+v", r)
	}
	for _, p := range h.e.Projects() {
		if p.ID == pid {
			t.Fatal("the project is still listed")
		}
	}
	if p := projectOf(h.e, a.ThreadID); p.Name != "Work" {
		t.Fatalf("a matched to %q, want Work", p.Name)
	}
	tb := dtoOf(h.e, b.ThreadID)
	if tb.ProjectID != "" || tb.ProjectManual || tb.Project != "" {
		t.Fatalf("b after delete: %q %v %q", tb.ProjectID, tb.ProjectManual, tb.Project)
	}
	gone := false
	for _, m := range drain(ch, "project") {
		if m["id"] == pid && m["deleted"] == true {
			gone = true
		}
	}
	if !gone {
		t.Fatal("no project {id, deleted: true} on the stream")
	}
	// The project stays gone after a restart.
	h.m.StopAll()
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	for _, p := range h2.e.Projects() {
		if p.ID == pid {
			t.Fatal("the project came back after a restart")
		}
	}
}

// #42: a project carries rev, a stamp of when its DTO was built, so a client
// keeps the newer of a snapshot and a stream frame that crossed it.
func TestProjectRev(t *testing.T) {
	h := newHarness(t, time.Minute)
	c := h.cmd("project.create", "", core.Args{"name": "App"})
	pid := c.Result.(map[string]any)["id"].(string)
	revOf := func() int64 {
		for _, p := range h.e.Projects() {
			if p.ID == pid {
				return p.Rev
			}
		}
		t.Fatal("no project")
		return 0
	}
	before := revOf()
	ch := h.e.Hub.Subscribe()
	defer h.e.Hub.Unsubscribe(ch)
	if r := h.cmd("project.update", "", core.Args{"id": pid, "color": "violet"}); !r.OK {
		t.Fatalf("update: %+v", r)
	}
	var sent int64
	for len(ch) > 0 {
		fr := <-ch
		if fr.Event != "project" {
			continue
		}
		var p struct {
			ID  string `json:"id"`
			Rev int64  `json:"rev"`
		}
		if err := json.Unmarshal(fr.Data, &p); err != nil {
			t.Fatal(err)
		}
		if p.ID == pid {
			sent = p.Rev
		}
	}
	if before <= 0 || sent <= before {
		t.Fatalf("the update's frame has rev %d, the snapshot before it %d", sent, before)
	}
	if after := revOf(); after <= sent {
		t.Fatalf("a snapshot after the frame has rev %d <= %d", after, sent)
	}
}

// #48: a project Input goes down the one path: one receipt per commandId,
// an event on the project, and the thread it files reduced in the same step
// as its own Input.
func TestProjectInputOnPath(t *testing.T) {
	h := newHarness(t, time.Minute)
	th := h.create("recept")
	var ids []any
	for range 2 {
		r := h.cmdID("p1", "project.create", "", core.Args{"name": "Kök", "threadId": th})
		if !r.OK {
			t.Fatalf("%+v", r)
		}
		ids = append(ids, r.Result.(map[string]any)["id"])
	}
	if ids[0] != ids[1] || len(h.e.Projects()) != 1 {
		t.Fatalf("one command made %d projects: %v", len(h.e.Projects()), ids)
	}
	pid, _ := ids[0].(string)
	if !hasEvent(h.events(pid), "command", `"project.create"`) {
		t.Fatal("no event on the project")
	}
	if !hasEvent(h.events(th), "command", `"thread.setProject"`) {
		t.Fatal("the filed thread's Input left no event")
	}
	if r := h.cmd("project.update", "", core.Args{"id": "p_nope", "name": "x"}); r.OK || r.Status != 404 {
		t.Fatalf("an unknown project: %+v", r)
	}
	if r := h.cmd("project.create", "", core.Args{"name": "x", "threadId": "t_nope"}); r.OK || r.Status != 404 || len(h.e.Projects()) != 1 {
		t.Fatalf("an unknown thread: %+v, %d projects", r, len(h.e.Projects()))
	}
}

// Decision 0034: a project says its folder's origin, read without running git.
func TestProjectRepo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n\turl = no\n[remote \"upstream\"]\n\turl = git@github.com:x/other.git\n[remote \"origin\"]\n\turl = git@github.com:x/fyr.git\n"), 0o644)
	if got, want := originOf(filepath.Join(dir, ".git", "config")), "github.com/x/fyr"; got != want {
		t.Fatalf("origin %q, want %q", got, want)
	}
	if got := originOf(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("no config: %q", got)
	}
}

// V0.project-archive: archiving a project shelves its idle threads in the
// same write and is refused, with the thread named, while one of them asks;
// a new message brings only that thread back; unarchive brings the rest.
func TestProjectArchive(t *testing.T) {
	h := newHarness(t, time.Minute)
	sub := filepath.Join(h.dir, "asking")
	os.MkdirAll(sub, 0o755)
	pid := h.cmd("project.create", "", core.Args{"name": "Done work", "folder": h.dir}).Result.(map[string]any)["id"].(string)
	qid := h.cmd("project.create", "", core.Args{"name": "Asking", "folder": sub}).Result.(map[string]any)["id"].(string)
	a, b := h.create("first"), h.create("second")
	q := h.cmd("thread.create", "", core.Args{"cwd": sub, "text": "third"}).ThreadID
	for _, id := range []string{a, b, q} {
		h.wait(id, "idle", func(t core.Thread, _ core.Attention) bool {
			return !t.Running && t.Starting == nil && len(t.Outbox) == 0
		})
	}
	if _, err := h.e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindAsk, Thread: q, Args: core.AskArgs{ID: "d_1", Questions: []core.Question{{Question: "Keep it?"}}}}); err != nil {
		t.Fatal(err)
	}
	r := h.cmd(core.KindProjectArchive, "", core.Args{"id": qid})
	if r.OK || r.Status != 409 || !strings.Contains(r.Error, "has an open question") {
		t.Fatalf("archive with an open question: %+v", r)
	}
	if th, _ := h.e.Thread(q); projectDTO(h.e, qid).ArchivedAt != nil || th.ShelvedAt != nil {
		t.Fatal("a refused archive stamped the project or shelved its thread")
	}
	if r := h.cmd(core.KindProjectArchive, "", core.Args{"id": pid}); !r.OK {
		t.Fatalf("archive: %+v", r)
	}
	for _, id := range []string{a, b} {
		if th, _ := h.e.Thread(id); th.ShelvedAt == nil {
			b, _ := json.Marshal(th)
			t.Fatalf("%s not shelved: %s", id, b)
		}
	}
	if th, _ := h.e.Thread(q); th.ShelvedAt != nil {
		t.Fatal("another project's thread was shelved")
	}
	if r := h.cmd("thread.send", a, core.Args{"text": "one more thing"}); !r.OK {
		t.Fatalf("send: %+v", r)
	}
	if th, _ := h.e.Thread(a); th.ShelvedAt != nil {
		t.Fatal("a message did not bring the thread back")
	}
	if th, _ := h.e.Thread(b); th.ShelvedAt == nil || projectDTO(h.e, pid).ArchivedAt == nil {
		t.Fatal("a message in one thread brought back more than that thread")
	}
	if r := h.cmd(core.KindProjectUnarchive, "", core.Args{"id": pid}); !r.OK {
		t.Fatalf("unarchive: %+v", r)
	}
	if th, _ := h.e.Thread(b); th.ShelvedAt != nil || projectDTO(h.e, pid).ArchivedAt != nil {
		t.Fatal("unarchive left the project or a thread put away")
	}
}
