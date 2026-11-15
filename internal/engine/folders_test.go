package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func projectDTO(e *Engine, id string) ProjectDTO {
	for _, p := range e.Projects() {
		if p.ID == id {
			return p
		}
	}
	return ProjectDTO{}
}

// SPEC 0.5 V0.55: a project has one folder; a new thread in the project
// without a cwd starts there and is filed by hand; project.update changes
// the folder; the commands of several folders are gone.
func TestProjectFolderAndNewThread(t *testing.T) {
	h := newHarness(t, time.Minute)
	a, b := filepath.Join(h.dir, "a"), filepath.Join(h.dir, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	r := h.cmd("project.create", "", core.Args{"name": "P", "folder": a + "/"})
	pid := r.Result.(map[string]any)["id"].(string)
	if f := projectDTO(h.e, pid).Folder; f != a {
		t.Fatalf("folder %q", f)
	}
	n := h.cmd("thread.create", "", core.Args{"projectId": pid, "text": "hej"})
	th := dtoOf(h.e, n.ThreadID)
	if !n.OK || th.Cwd != a || th.ProjectID != pid || !th.ProjectManual || th.Project != "P" {
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
	if up := h.cmd("project.update", "", core.Args{"id": pid, "folder": b}); !up.OK || projectDTO(h.e, pid).Folder != b {
		t.Fatalf("change folder: %+v %q", up, projectDTO(h.e, pid).Folder)
	}
	n2 := h.cmd("thread.create", "", core.Args{"projectId": pid, "text": "där"})
	if t2, _ := h.e.Thread(n2.ThreadID); t2.Cwd != b {
		t.Fatalf("after change: %q", t2.Cwd)
	}
	for _, act := range []string{"project.setMain", "project.addSource", "project.removeSource", "project.createFrom"} {
		if bad := h.cmd(act, "", core.Args{"id": pid, "index": 0, "threadId": n.ThreadID}); bad.OK || bad.Status != 400 {
			t.Fatalf("%s: %+v", act, bad)
		}
	}
	if up := h.cmd("project.update", "", core.Args{"id": pid, "folder": ""}); !up.OK || projectDTO(h.e, pid).Folder != "" {
		t.Fatalf("clear folder: %+v", up)
	}
	// The wire form carries folder and none of the old fields.
	raw, _ := json.Marshal(projectDTO(h.e, pid))
	var keys map[string]any
	json.Unmarshal(raw, &keys)
	for _, k := range []string{"sources", "repos", "cwd", "seeded"} {
		if _, ok := keys[k]; ok {
			t.Fatalf("project JSON still has %q: %s", k, raw)
		}
	}
}

// SPEC 0.5 V0.42: no project and no folder: a fresh scratch folder named
// after the date and the first words; it offers no new project. A project
// with no folder also gets a scratch folder and keeps the project.
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
	if d.ProjectID != "" || d.NewProject != nil {
		t.Fatalf("scratch thread: %+v %+v", d.ProjectID, d.NewProject)
	}
	// Same second: a second folder, not the same one.
	r2 := h.cmd("thread.create", "", core.Args{"text": "Fixa köket: mät skåpen! och mer text som är lång nog att kapas här"})
	if t2, _ := h.e.Thread(r2.ThreadID); t2.Cwd == th.Cwd {
		t.Fatal("two threads share a scratch folder")
	}
	lbl := h.cmd("project.create", "", core.Args{"name": "Garden"})
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

// SPEC 0.5 V0.55: no repo matching: a clone of the project's repository in
// another folder does not land in the project.
func TestNoRepoMatching(t *testing.T) {
	h := newHarness(t, time.Minute)
	main := filepath.Join(h.dir, "fyr")
	gitInit(t, main)
	gitRemote(t, main, "git@github.com:meros/fyr.git")
	clone := filepath.Join(h.dir, "fyr-projcut")
	gitInit(t, clone)
	gitRemote(t, clone, "git@github.com:meros/fyr.git")
	r := h.cmd("project.create", "", core.Args{"name": "Fyr", "folder": main})
	pid := r.Result.(map[string]any)["id"].(string)
	in := h.cmd("thread.create", "", core.Args{"cwd": filepath.Join(main), "text": "x"})
	out := h.cmd("thread.create", "", core.Args{"cwd": clone, "text": "y"})
	if th, _ := h.e.Thread(in.ThreadID); th.ProjectID != pid {
		t.Fatalf("a thread in the folder did not land: %q", th.ProjectID)
	}
	if th, _ := h.e.Thread(out.ThreadID); th.ProjectID != "" {
		t.Fatalf("a clone elsewhere matched by repo: %q", th.ProjectID)
	}
	if d := dtoOf(h.e, out.ThreadID); d.NewProject == nil || d.NewProject.Folder != clone || d.NewProject.Name != "fyr-projcut" {
		t.Fatalf("newProject %+v", d.NewProject)
	}
}
