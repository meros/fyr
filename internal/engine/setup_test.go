package engine

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/projicon"
)

func logoPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// setupHarness is a harness with an icon store, a fetch that may reach the
// test's own server, and the placeholder system agent.
func setupHarness(t *testing.T) *harness {
	h := newHarness(t, time.Minute)
	h.e.Icons = &projicon.Store{Dir: filepath.Join(h.dir, "project-icons")}
	h.e.FetchIcon = projicon.Fetcher{AllowLoopback: true}.Fetch
	h.e.SystemAgentResolve = func(job string) (string, string, error) { return "", "claude-haiku-5-5", nil }
	guardDeps(t, h.e)
	return h
}

func projectByID(h *harness, id string) core.Project {
	for _, p := range h.e.Projects() {
		if p.ID == id {
			return p.Project
		}
	}
	h.t.Fatalf("no project %s", id)
	return core.Project{}
}

func newProject(t *testing.T, h *harness, args core.Args) string {
	t.Helper()
	r := h.cmd("project.create", "", args)
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	return r.Result.(map[string]any)["id"].(string)
}

// SPEC 0.5 V0.116: project_setup reads the icon from a file in the project,
// stores it square at 64 and 128 px, takes the colour from it, and sets the
// name; each value is the agent's.
func TestProjectSetupToolFromFile(t *testing.T) {
	h := setupHarness(t)
	dir := filepath.Join(h.dir, "fyr")
	os.MkdirAll(filepath.Join(dir, "public"), 0o755)
	logo := filepath.Join(dir, "public", "logo.png")
	os.WriteFile(logo, logoPNG(t, color.NRGBA{30, 120, 115, 255}), 0o644)
	pid := newProject(t, h, core.Args{"folder": dir})
	th := h.cmd("thread.create", "", core.Args{"cwd": dir, "text": "set up"}).ThreadID

	out, isErr := callAs(t, h.e, th, "project_setup", map[string]any{"icon": logo, "name": "Fyr"})
	if isErr {
		t.Fatalf("%s", out)
	}
	p := projectByID(h, pid)
	if p.Image == "" || p.Name != "Fyr" || p.Color != "teal" || p.SetBy("image") != core.ByAgent || p.SetBy("color") != core.ByAgent || p.SetBy("name") != core.ByAgent {
		t.Fatalf("%s\n%+v", out, p)
	}
	for _, n := range projicon.Sizes {
		if _, err := os.Stat(h.e.ProjectIconPath(pid, n)); err != nil {
			t.Fatalf("%d px: %v", n, err)
		}
	}
	if !strings.Contains(out, "icon") || !strings.Contains(out, "teal") {
		t.Fatalf("the reply does not say what it set: %q", out)
	}
}

// SPEC 0.5 V0.116: the default project is the calling thread's; an icon may
// come from a public URL; a file that is not an image is refused.
func TestProjectSetupToolFromURL(t *testing.T) {
	h := setupHarness(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.Write([]byte("<html>nope"))
			return
		}
		w.Write(logoPNG(t, color.NRGBA{190, 40, 90, 255}))
	}))
	defer srv.Close()
	dir := filepath.Join(h.dir, "acme")
	os.MkdirAll(dir, 0o755)
	pid := newProject(t, h, core.Args{"name": "Acme", "folder": dir})
	th := h.cmd("thread.create", "", core.Args{"cwd": dir, "text": "set up"}).ThreadID
	if out, isErr := callAs(t, h.e, th, "project_setup", map[string]any{"icon": srv.URL + "/bad"}); !isErr || !strings.Contains(out, "image") {
		t.Fatalf("a page was taken as an icon: %q", out)
	}
	out, isErr := callAs(t, h.e, th, "project_setup", map[string]any{"icon": srv.URL + "/logo.png", "name": "Something else"})
	if isErr {
		t.Fatal(out)
	}
	p := projectByID(h, pid)
	if p.Image == "" || p.Name != "Acme" || p.Color != "rose" {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(out, "owner") || !strings.Contains(out, "name") {
		t.Fatalf("the reply does not say the owner's name was kept: %q", out)
	}
	// The real fetch never reaches the local network.
	h.e.FetchIcon = nil
	if out, isErr := callAs(t, h.e, th, "project_setup", map[string]any{"project": pid, "icon": srv.URL + "/logo.png"}); !isErr || !strings.Contains(out, "local network") {
		t.Fatalf("fetched a loopback URL: %q", out)
	}
}

// SPEC 0.5 V0.116: the owner's upload is the owner's icon, the agent keeps
// it; the owner's remove goes back to the glyph and deletes the files, and
// the agent does not put one back.
func TestProjectImageOwner(t *testing.T) {
	h := setupHarness(t)
	dir := filepath.Join(h.dir, "app")
	os.MkdirAll(dir, 0o755)
	pid := newProject(t, h, core.Args{"folder": dir})
	if r := h.e.UploadProjectImage(pid, []byte("<svg/>")); r.OK || r.Status != 400 {
		t.Fatalf("an svg was taken: %+v", r)
	}
	if r := h.e.UploadProjectImage(pid, logoPNG(t, color.NRGBA{40, 100, 190, 255})); !r.OK {
		t.Fatalf("%+v", r)
	}
	p := projectByID(h, pid)
	if p.Image == "" || p.SetBy("image") != core.ByOwner {
		t.Fatalf("%+v", p)
	}
	th := h.cmd("thread.create", "", core.Args{"cwd": dir, "text": "x"}).ThreadID
	logo := filepath.Join(dir, "logo.png")
	os.WriteFile(logo, logoPNG(t, color.NRGBA{200, 30, 60, 255}), 0o644)
	out, _ := callAs(t, h.e, th, "project_setup", map[string]any{"icon": logo})
	if q := projectByID(h, pid); q.Image != p.Image || !strings.Contains(out, "owner") {
		t.Fatalf("the agent replaced the owner's icon: %q %+v", out, q)
	}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "image": ""}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(h.e.ProjectIconPath(pid, 64)); !os.IsNotExist(err) {
		t.Fatalf("remove left the file: %v", err)
	}
	callAs(t, h.e, th, "project_setup", map[string]any{"icon": logo})
	if q := projectByID(h, pid); q.Image != "" {
		t.Fatal("the agent put back an icon the owner removed")
	}
	if r := h.e.UploadProjectImage("p_nope", logoPNG(t, color.NRGBA{1, 2, 3, 255})); r.Status != 404 {
		t.Fatalf("%+v", r)
	}
}

// SPEC 0.5 V0.116: "Set up automatically" starts a thread in the project's
// folder, filed in it, on the system agent's model, with the brief; the
// project records the run. A project without a folder cannot be set up;
// a system agent that cannot run is recorded and shown.
func TestProjectSetupRun(t *testing.T) {
	h := setupHarness(t)
	dir := filepath.Join(h.dir, "fyr")
	os.MkdirAll(dir, 0o755)
	pid := newProject(t, h, core.Args{"folder": dir})
	var asked string
	h.e.SystemAgentResolve = func(job string) (string, string, error) { asked = job; return "", "claude-haiku-5-5", nil }
	guardDeps(t, h.e)
	r := h.cmd("project.setupRun", "", core.Args{"id": pid})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	if asked != core.JobSetup {
		t.Fatalf("asked the system agent for %q", asked)
	}
	tid, _ := r.Result.(map[string]any)["thread"].(string)
	th, ok := h.e.Thread(tid)
	if !ok || th.Cwd != dir || th.ProjectID != pid || th.Model != "claude-haiku-5-5" || !strings.HasPrefix(th.Title, "Set up ") {
		t.Fatalf("%+v", th)
	}
	if p := projectByID(h, pid); p.Setup == nil || p.Setup.Thread != tid {
		t.Fatalf("%+v", p.Setup)
	}
	brief := ""
	eventually(t, "the brief is sent", func() bool {
		var b strings.Builder
		for _, ev := range h.events(tid) {
			b.Write(ev.Data)
		}
		brief = b.String()
		return strings.Contains(brief, "project_setup")
	})
	for _, want := range []string{"project_setup", pid, "favicon", "licen"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief lacks %q: %s", want, brief)
		}
	}

	none := newProject(t, h, core.Args{"name": "Household"})
	if r := h.cmd("project.setupRun", "", core.Args{"id": none}); r.OK || r.Status != 400 {
		t.Fatalf("set up a project without a folder: %+v", r)
	}
	h.e.SystemAgentResolve = func(string) (string, string, error) {
		return "", "", errors.New("no Haiku on this provider; Settings → System agents")
	}
	guardDeps(t, h.e)
	r = h.cmd("project.setupRun", "", core.Args{"id": pid})
	if r.OK || !strings.Contains(r.Error, "Haiku") {
		t.Fatalf("%+v", r)
	}
	if p := projectByID(h, pid); p.Setup == nil || !strings.Contains(p.Setup.Error, "Haiku") {
		t.Fatalf("the failure is not on the project: %+v", p.Setup)
	}
}

// SPEC 0.5 V0.116: with AutoSetup a project made with a folder gets one
// setup run; one without a folder does not.
func TestAutoSetupOnCreate(t *testing.T) {
	h := setupHarness(t)
	h.e.AutoSetup = true
	dir := filepath.Join(h.dir, "fyr")
	os.MkdirAll(dir, 0o755)
	pid := newProject(t, h, core.Args{"folder": dir})
	p := projectByID(h, pid)
	if p.Setup == nil || p.Setup.Thread == "" {
		t.Fatalf("no setup run: %+v", p.Setup)
	}
	none := newProject(t, h, core.Args{"name": "Household"})
	if q := projectByID(h, none); q.Setup != nil {
		t.Fatalf("%+v", q.Setup)
	}
}

// SPEC 0.5 V0.116: once for each existing project: every project with a
// folder, no image and no setup run gets one; a second pass starts none.
func TestSetupExisting(t *testing.T) {
	h := setupHarness(t)
	a, b := filepath.Join(h.dir, "a"), filepath.Join(h.dir, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	pa := newProject(t, h, core.Args{"folder": a})
	pb := newProject(t, h, core.Args{"folder": b})
	newProject(t, h, core.Args{"name": "No folder"})
	if r := h.e.UploadProjectImage(pb, logoPNG(t, color.NRGBA{40, 100, 190, 255})); !r.OK {
		t.Fatal(r)
	}
	if n := h.e.SetupExisting(0); n != 1 {
		t.Fatalf("started %d runs", n)
	}
	if projectByID(h, pa).Setup == nil || projectByID(h, pb).Setup != nil {
		t.Fatal("the wrong project was set up")
	}
	if n := h.e.SetupExisting(0); n != 0 {
		t.Fatalf("a second pass started %d", n)
	}
}

// SPEC 0.5 V0.116: deleting a project deletes its icon files.
func TestDeleteProjectRemovesIcon(t *testing.T) {
	h := setupHarness(t)
	dir := filepath.Join(h.dir, "x")
	os.MkdirAll(dir, 0o755)
	pid := newProject(t, h, core.Args{"folder": dir})
	h.e.UploadProjectImage(pid, logoPNG(t, color.NRGBA{40, 100, 190, 255}))
	if r := h.cmd("project.delete", "", core.Args{"id": pid}); !r.OK {
		t.Fatal(r)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "project-icons", pid)); !os.IsNotExist(err) {
		t.Fatalf("%v", err)
	}
}

// SPEC V0.117: a setup run that could not start (the system agent failed) is
// not done once: the next pass after the fix starts it, and the error goes.
func TestSetupThatCouldNotStartRunsAgain(t *testing.T) {
	h := setupHarness(t)
	dir := filepath.Join(h.dir, "a")
	os.MkdirAll(dir, 0o755)
	h.e.SystemAgentResolve = func(string) (string, string, error) {
		return "", "", errors.New("Claude has no Haiku model: pick a model in Settings → System agents")
	}
	guardDeps(t, h.e)
	pid := newProject(t, h, core.Args{"folder": dir})
	if n := h.e.SetupExisting(0); n != 0 {
		t.Fatalf("started %d with a failing system agent", n)
	}
	if s := projectByID(h, pid).Setup; s == nil || s.Error == "" || s.Thread != "" {
		t.Fatalf("the failure is not shown: %+v", s)
	}
	h.e.SystemAgentResolve = func(string) (string, string, error) { return "", "claude-haiku-5-5", nil }
	guardDeps(t, h.e)
	if n := h.e.SetupExisting(0); n != 1 {
		t.Fatalf("after the fix a pass started %d", n)
	}
	if s := projectByID(h, pid).Setup; s == nil || s.Thread == "" || s.Error != "" {
		t.Fatalf("after the retry: %+v", s)
	}
	if n := h.e.SetupExisting(0); n != 0 {
		t.Fatalf("a run that started ran again: %d", n)
	}
}
