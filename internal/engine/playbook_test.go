package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

const probeSHA = "0123456789abcdef0123456789abcdef01234567"

func playbookProject(t *testing.T) (*harness, string, string) {
	t.Helper()
	h := setupHarness(t)
	dir := filepath.Join(h.dir, "acme")
	os.MkdirAll(dir, 0o755)
	pid := newProject(t, h, core.Args{"name": "Acme", "folder": dir})
	th := h.cmd("thread.create", "", core.Args{"cwd": dir, "text": "work"}).ThreadID
	return h, pid, th
}

// V0.project-playbook: project_learn writes the playbook of the thread's
// project and reads it back; a probe waits for the owner; the owner's entry
// is kept; ownerAsked makes the entry the owner's, its probe allowed.
func TestProjectLearnTool(t *testing.T) {
	h, pid, th := playbookProject(t)
	if out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{}); isErr || !strings.Contains(out, "is empty") {
		t.Fatalf("%q", out)
	}
	out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{"slot": "stage", "name": "dev", "how": "automatic on merge", "probe": "check {sha}", "url": "https://dev.example.com", "evidence": ".github/workflows/deploy.yml"})
	if isErr || !strings.Contains(out, "does not run its probe until the owner allows it") {
		t.Fatalf("%q", out)
	}
	p := projectByID(h, pid)
	if len(p.Playbook) != 1 || p.Playbook[0].By != core.ByAgent || p.Playbook[0].Thread != th || p.Playbook[0].Runs() {
		t.Fatalf("%+v", p.Playbook)
	}
	if r := h.cmd(core.KindProjectLearn, "", core.Args{"id": pid, "slot": "stage", "name": "dev", "allow": true}); !r.OK {
		t.Fatalf("allow: %+v", r)
	}
	if r := h.cmd(core.KindProjectLearn, "", core.Args{"id": pid, "slot": "verify", "how": "the owner checks on dev"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{"slot": "verify", "how": "the agent checks"}); !isErr || !strings.Contains(out, "the owner wrote the verify entry") {
		t.Fatalf("the owner's entry was replaced: %q", out)
	}
	if out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{"slot": "stage", "name": "prod", "how": "a release tag", "probe": "rel {sha}", "live": true, "ownerAsked": true}); !isErr {
		t.Fatalf("ownerAsked without the owner's message: %q", out)
	}
	if r := h.e.submit(core.Input{Source: core.FromOwner, Actor: "local:owner", Kind: "thread.send", Thread: th, Args: core.Args{"text": "prod is a release tag, check it with rel"}, CmdID: NewID("c_")}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{"slot": "stage", "name": "prod", "how": "a release tag", "probe": "rel {sha}", "live": true, "ownerAsked": true}); isErr || !strings.Contains(out, "Fyr runs its probe") {
		t.Fatalf("%q", out)
	}
	p = projectByID(h, pid)
	if st := p.ProbeStages(); len(st) != 2 || st[1].Name != "prod" || st[1].By != core.ByOwner {
		t.Fatalf("%+v", p.Playbook)
	}
	read, _ := callAs(t, h.e, th, "project_learn", map[string]any{})
	for _, want := range []string{"Stage dev: automatic on merge; at https://dev.example.com; Fyr checks it with a probe", "Stage prod, live", "Verify: the owner checks on dev", "Probe of dev: check {sha}"} {
		if !strings.Contains(read, want) {
			t.Fatalf("the playbook does not say %q:\n%s", want, read)
		}
	}
	pp := h.e.ProjectPrompt(th)
	if !strings.Contains(pp, "mcp__fyr__project_learn") || !strings.Contains(pp, `The playbook of the project "Acme"`) {
		t.Fatalf("prompt:\n%s", pp)
	}
	if other := h.create("elsewhere"); h.e.ProjectPrompt(other) != "" {
		t.Fatal("a thread without a project got a playbook line")
	}
}

// V0.project-playbook: after a merge Fyr runs the stages' probes itself, on
// a settled thread too, each stage until it holds the commit; the change
// moves through them and is live at the live stage.
func TestProbesMoveAMergedChange(t *testing.T) {
	h, pid, th := playbookProject(t)
	for _, a := range []core.Args{
		{"slot": "stage", "name": "dev", "probe": "dev {sha}", "url": "https://dev.example.com"},
		{"slot": "stage", "name": "prod", "probe": "prod {sha}"},
		{"slot": "stage", "name": "qa", "how": "by hand"}, // no probe: never run
	} {
		a["id"] = pid
		if r := h.cmd(core.KindProjectLearn, "", a); !r.OK {
			t.Fatalf("%+v", r)
		}
		if a.Str("probe") != "" {
			if r := h.cmd(core.KindProjectLearn, "", core.Args{"id": pid, "slot": "stage", "name": a.Str("name"), "allow": true}); !r.OK {
				t.Fatalf("%+v", r)
			}
		}
	}
	url := "https://github.com/acme/app/pull/9"
	coretest.Patch(h.e, th, func(x *core.Thread, now time.Time) []core.Event {
		core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: "pass", Head: "h1"}, now)
		core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "MERGED", Checks: "pass", MergeSHA: probeSHA, MergedAt: &now}, now)
		return nil
	})
	var ran []string
	holds := map[string]bool{}
	off := time.Duration(0)
	step := func() { off += 2 * time.Minute; d := off; h.e.Now = func() time.Time { return time.Now().Add(d) } }
	h.e.RunProbe = func(_ context.Context, dir, script string) (int, error) {
		ran = append(ran, script)
		if !strings.HasSuffix(dir, "acme") {
			t.Errorf("probe ran in %s", dir)
		}
		if holds[strings.Fields(script)[0]] {
			return 0, nil
		}
		return 1, nil
	}
	// GitHub has no deployment for this repo: the watch stays for the probes.
	PRWatch{E: h.e}.ObserveDeploy(th, firstChange(h, th).ID, core.DeployObs{None: true})
	if n := h.e.ProbeOnce(context.Background()); n != 2 || len(ran) != 2 || ran[0] != "dev "+probeSHA {
		t.Fatalf("ran %d: %v", n, ran)
	}
	if c := firstChange(h, th); c.Phase != core.PhaseLanded || len(c.Conditions) != 0 && core.StageReached(&c, core.PlaybookEntry{Slot: core.SlotStage, Name: "dev"}) {
		t.Fatalf("a probe that failed moved the change: %+v", c)
	}
	if r := h.cmd("thread.settle", th, nil); !r.OK {
		t.Fatalf("settle: %+v", r)
	}
	holds["dev"] = true
	step()
	h.e.ProbeOnce(context.Background())
	c := firstChange(h, th)
	if !core.StageReached(&c, core.PlaybookEntry{Slot: core.SlotStage, Name: "dev"}) || c.Phase != core.PhaseLanded {
		t.Fatalf("dev: %+v", c.Conditions)
	}
	ran = nil
	holds["prod"] = true
	step()
	h.e.ProbeOnce(context.Background())
	if len(ran) != 1 || !strings.HasPrefix(ran[0], "prod ") {
		t.Fatalf("a reached stage was probed again: %v", ran)
	}
	if c := firstChange(h, th); c.Phase != core.PhaseLive {
		t.Fatalf("phase %s", c.Phase)
	}
	ran = nil
	step()
	if n := h.e.ProbeOnce(context.Background()); n != 0 || len(ran) != 0 {
		t.Fatalf("a live change is probed: %v", ran)
	}
}

func firstChange(h *harness, th string) core.Change {
	h.t.Helper()
	x, _ := h.e.Thread(th)
	if len(x.Changes) == 0 {
		h.t.Fatal("no change")
	}
	return x.Changes[0]
}

// V0.project-playbook: the owner's "Learn from the repository" starts a
// thread in the project's folder with the brief.
func TestPlaybookRun(t *testing.T) {
	h, pid, _ := playbookProject(t)
	r := h.cmd(core.KindProjectPlaybookRun, "", core.Args{"id": pid})
	if !r.OK || r.ThreadID == "" {
		t.Fatalf("%+v", r)
	}
	x, _ := h.e.Thread(r.ThreadID)
	if x.ProjectID != pid || !strings.HasSuffix(x.Cwd, "acme") {
		t.Fatalf("%+v", x)
	}
	if r := h.cmd(core.KindProjectPlaybookRun, "", core.Args{"id": newProject(t, h, core.Args{"name": "No folder"})}); r.OK {
		t.Fatal("a project without a folder started a run")
	}
}
