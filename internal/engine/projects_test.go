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
