package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func folderArg(path string) map[string]any { return map[string]any{"kind": "folder", "path": path} }

func projectDTO(e *Engine, id string) ProjectDTO {
	for _, p := range e.Projects() {
		if p.ID == id {
			return p
		}
	}
	return ProjectDTO{}
}

// SPEC 0.5 V0.42: the first folder here is main; project.setMain moves it;
// a new thread in the project without a cwd starts in the main folder and is
// filed there by hand.
func TestMainFolderAndNewThread(t *testing.T) {
	h := newHarness(t, time.Minute)
	a, b := filepath.Join(h.dir, "a"), filepath.Join(h.dir, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	r := h.cmd("project.create", "", core.Args{"name": "P", "sources": []any{
		map[string]any{"kind": "repo", "url": "github.com/me/p"}, folderArg(a), folderArg(b)}})
	pid := r.Result.(map[string]any)["id"].(string)
	if f := projectDTO(h.e, pid).Folder(h.e.HostID); f != a {
		t.Fatalf("default main %q", f)
	}
	for _, i := range []any{0, 3, -1, 1.5} {
		if bad := h.cmd("project.setMain", "", core.Args{"id": pid, "index": i}); bad.OK || bad.Status != 400 {
			t.Fatalf("setMain %v: %+v", i, bad)
		}
	}
	if ok := h.cmd("project.setMain", "", core.Args{"id": pid, "index": 2}); !ok.OK {
		t.Fatalf("%+v", ok)
	}
	p := projectDTO(h.e, pid)
	if p.Folder(h.e.HostID) != b || !p.Sources[2].Main || p.Sources[1].Main {
		t.Fatalf("main moved: %+v", p.Sources)
	}
	n := h.cmd("thread.create", "", core.Args{"projectId": pid, "text": "hej"})
	th, _ := h.e.Thread(n.ThreadID)
	if !n.OK || th.Cwd != b || th.ProjectID != pid || !th.ProjectManual || th.Project != "P" {
		t.Fatalf("new thread: %+v cwd %q project %q manual %v", n, th.Cwd, th.ProjectID, th.ProjectManual)
	}
	// An explicit folder wins; the thread is still filed in the project.
	m := h.cmd("thread.create", "", core.Args{"projectId": pid, "cwd": h.dir, "text": "x"})
	if tm, _ := h.e.Thread(m.ThreadID); tm.Cwd != h.dir || tm.ProjectID != pid {
		t.Fatalf("explicit cwd: %q %q", tm.Cwd, tm.ProjectID)
	}
	if bad := h.cmd("thread.create", "", core.Args{"projectId": "p_nope", "text": "x"}); bad.OK || bad.Status != 400 {
		t.Fatalf("unknown project: %+v", bad)
	}
}

// SPEC 0.5 V0.42: no project and no folder: a fresh scratch folder named
// after the date and the first words; it matches no project. A project with
// no folder here also gets a scratch folder and keeps the project.
func TestScratchThread(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.ScratchDir = filepath.Join(h.dir, "fyr-scratch")
	h.e.ScratchRoots = []string{}
	h.e.Now = func() time.Time { return time.Date(2026, 11, 14, 9, 30, 5, 0, time.Local) }
	r := h.cmd("thread.create", "", core.Args{"text": "Fixa köket: mät skåpen! och mer text som är lång nog att kapas här"})
	th, _ := h.e.Thread(r.ThreadID)
	if !r.OK || filepath.Dir(th.Cwd) != h.e.ScratchDir || !strings.HasPrefix(filepath.Base(th.Cwd), "20261114-093005-fixa-k-ket") || len(filepath.Base(th.Cwd)) > 16+40 {
		t.Fatalf("scratch cwd %q %+v", th.Cwd, r)
	}
	if fi, err := os.Stat(th.Cwd); err != nil || !fi.IsDir() {
		t.Fatal("the scratch folder was not made")
	}
	d := dtoOf(h.e, r.ThreadID)
	if d.ProjectID != "" || d.Suggest != nil || d.NewProject != nil {
		t.Fatalf("scratch thread: %+v %+v", d.ProjectID, d.NewProject)
	}
	// Same second: a second folder, not the same one.
	r2 := h.cmd("thread.create", "", core.Args{"text": "Fixa köket: mät skåpen! och mer text som är lång nog att kapas här"})
	if t2, _ := h.e.Thread(r2.ThreadID); t2.Cwd == th.Cwd {
		t.Fatal("two threads share a scratch folder")
	}
	lbl := h.cmd("project.create", "", core.Args{"name": "Hemmet"})
	pid := lbl.Result.(map[string]any)["id"].(string)
	r3 := h.cmd("thread.create", "", core.Args{"text": "", "projectId": pid, "attachments": []any{}})
	if r3.OK {
		t.Fatal("an empty thread started")
	}
	if ents, _ := os.ReadDir(h.e.ScratchDir); len(ents) != 2 {
		t.Fatalf("a failed create left a scratch folder: %d", len(ents))
	}
	r4 := h.cmd("thread.create", "", core.Args{"text": "Handla", "projectId": pid})
	if t4, _ := h.e.Thread(r4.ThreadID); filepath.Dir(t4.Cwd) != h.e.ScratchDir || t4.ProjectID != pid {
		t.Fatalf("label project: %q %q", t4.Cwd, t4.ProjectID)
	}
	if got := scratchSlug("  !!  "); got != "thread" {
		t.Fatalf("empty slug %q", got)
	}
}

// SPEC 0.5 V0.42: a project's repos come from its folders' origin remotes
// and its repo sources, deduplicated; a clone elsewhere of a folder's repo
// matches the project.
func TestDerivedRepos(t *testing.T) {
	h := newHarness(t, time.Minute)
	main := filepath.Join(h.dir, "fyr")
	gitInit(t, main)
	gitRemote(t, main, "git@github.com:meros/Fyr.git")
	clone := filepath.Join(h.dir, "elsewhere", "fyr-copy")
	gitInit(t, clone)
	gitRemote(t, clone, "https://github.com/meros/fyr")
	plain := filepath.Join(h.dir, "notes")
	os.MkdirAll(plain, 0o755)
	early := h.cmd("thread.create", "", core.Args{"cwd": clone, "text": "x"})
	r := h.cmd("project.create", "", core.Args{"name": "Fyr", "sources": []any{folderArg(main), folderArg(plain)}})
	pid := r.Result.(map[string]any)["id"].(string)
	if got := projectDTO(h.e, pid).Repos; !reflect.DeepEqual(got, []string{"github.com/meros/fyr"}) {
		t.Fatalf("derived repos %v", got)
	}
	if th, _ := h.e.Thread(early.ThreadID); th.ProjectID != pid {
		t.Fatalf("an earlier thread in a clone did not join: %q", th.ProjectID)
	}
	late := h.cmd("thread.create", "", core.Args{"cwd": clone, "text": "y"})
	if th, _ := h.e.Thread(late.ThreadID); th.ProjectID != pid {
		t.Fatalf("a new thread in a clone did not match: %q", th.ProjectID)
	}
	// Hand-made repo sources stay and are deduplicated.
	h.cmd("project.addSource", "", core.Args{"id": pid, "source": map[string]any{"kind": "repo", "url": "https://github.com/meros/fyr.git"}})
	h.cmd("project.addSource", "", core.Args{"id": pid, "source": map[string]any{"kind": "repo", "url": "github.com/meros/other"}})
	if got := projectDTO(h.e, pid).Repos; !reflect.DeepEqual(got, []string{"github.com/meros/fyr", "github.com/meros/other"}) {
		t.Fatalf("repos %v", got)
	}
	// A project without folders has an empty list, not null.
	r2 := h.cmd("project.create", "", core.Args{"name": "Label"})
	if got := projectDTO(h.e, r2.Result.(map[string]any)["id"].(string)).Repos; got == nil || len(got) != 0 {
		t.Fatalf("label repos %v", got)
	}
}
