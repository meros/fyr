package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	if out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{"slot": "verify", "how": "the agent checks"}); isErr || !strings.Contains(out, "so it stands") {
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
	var mu sync.Mutex // probes run several at a time
	h.e.RunProbe = func(_ context.Context, dir, script string) (string, int, error) {
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, script)
		if !strings.HasSuffix(dir, "acme") {
			t.Errorf("probe ran in %s", dir)
		}
		if holds[strings.Fields(script)[0]] {
			return "", 0, nil
		}
		return "not yet", 1, nil
	}
	// GitHub has no deployment for this repo: the watch stays for the probes.
	PRWatch{E: h.e}.ObserveDeploy(th, firstChange(h, th).ID, core.DeployObs{None: true})
	if n := h.e.ProbeOnce(context.Background()); n != 2 || len(ran) != 2 || !slices.Contains(ran, "dev "+probeSHA) {
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

// V0.playbook-moments: with no model asked, a change that reaches a stage
// the owner checks puts its thread in Your move with a question; "Looks
// good" brings the Run card of the next stage's deploy command; an agent's
// finding on the owner's entry is a suggestion.
func TestStageMomentsOnTheHost(t *testing.T) {
	h, pid, th := playbookProject(t)
	for _, a := range []core.Args{
		{"slot": "stage", "name": "dev", "probe": "dev {sha}", "url": "https://dev.example.com", "ownerChecks": true},
		{"slot": "stage", "name": "prod", "deployCmd": "release {sha}", "how": "a release, by hand"},
		{"slot": "stage", "name": "dev", "allow": true},
	} {
		a["id"] = pid
		if r := h.cmd(core.KindProjectLearn, "", a); !r.OK {
			t.Fatalf("%+v", r)
		}
	}
	if out, isErr := callAs(t, h.e, th, "project_learn", map[string]any{"slot": "stage", "name": "prod", "how": "automatic"}); isErr || !strings.Contains(out, "suggestion") {
		t.Fatalf("%q", out)
	}
	if p := projectByID(h, pid); p.Stages()[1].Suggest == nil || p.Stages()[1].How != "a release, by hand" {
		t.Fatalf("%+v", p.Playbook)
	}
	url := "https://github.com/acme/app/pull/9"
	coretest.Patch(h.e, th, func(x *core.Thread, now time.Time) []core.Event {
		core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: "pass", Head: "h1"}, now)
		core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "MERGED", Checks: "pass", MergeSHA: probeSHA, MergedAt: &now}, now)
		return nil
	})
	h.e.RunProbe = func(context.Context, string, string) (string, int, error) { return "", 0, nil }
	h.e.ProbeOnce(context.Background())
	x, _ := h.e.Thread(th)
	if len(x.Decisions) != 1 || core.PendingRequest(&x) != nil {
		t.Fatalf("decisions %+v", x.Decisions)
	}
	if d := dtoOf(h.e, th); d.Attention.Section != core.NeedsYou {
		t.Fatalf("section %s", d.Attention.Section)
	}
	q := x.Decisions[0].Questions[0].Question
	if r := h.cmd("thread.answer", th, core.Args{"decisionId": x.Decisions[0].ID, "answers": map[string]any{q: core.StageLooksGood}}); !r.OK {
		t.Fatalf("answer: %+v", r)
	}
	h.e.ProbeOnce(context.Background())
	x, _ = h.e.Thread(th)
	a := core.PendingRequest(&x)
	if a == nil || a.Command != "release "+probeSHA || !strings.HasSuffix(a.Cwd, "acme") {
		t.Fatalf("run card: %+v", a)
	}
	h.e.ProbeOnce(context.Background())
	if x, _ := h.e.Thread(th); len(x.Decisions) != 0 || len(x.Offers) != 1 {
		t.Fatalf("a moment opened twice: %d decisions, %d offers", len(x.Decisions), len(x.Offers))
	}
}

// V0.playbook-moments: a merge whose watch ends with the probe never having
// seen it counts on the stage; the next merge the probe sees clears it.
func TestProbeMissCountsAndClears(t *testing.T) {
	h, pid, th := playbookProject(t)
	for _, a := range []core.Args{{"slot": "stage", "name": "dev", "probe": "dev {sha}"}, {"slot": "stage", "name": "dev", "allow": true}} {
		a["id"] = pid
		if r := h.cmd(core.KindProjectLearn, "", a); !r.OK {
			t.Fatalf("%+v", r)
		}
	}
	merge := func(n int, sha string) {
		url := "https://github.com/acme/app/pull/" + string(rune('0'+n))
		coretest.Patch(h.e, th, func(x *core.Thread, now time.Time) []core.Event {
			core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: n, State: "OPEN", Checks: "pass", Head: "h1"}, now)
			core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: n, State: "MERGED", Checks: "pass", MergeSHA: sha, MergedAt: &now}, now)
			return nil
		})
	}
	ok := false
	h.e.RunProbe = func(context.Context, string, string) (string, int, error) {
		if ok {
			return "", 0, nil
		}
		return "", 1, nil
	}
	missed := func() int { p := projectByID(h, pid); return p.Stages()[0].Missed }
	merge(1, probeSHA)
	h.e.ProbeOnce(context.Background())
	if missed() != 0 {
		t.Fatal("a probe that has not succeeded yet is a miss")
	}
	h.e.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	h.e.ProbeOnce(context.Background())
	for i := 0; i < 200 && missed() != 1; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if missed() != 1 {
		t.Fatalf("missed %d", missed())
	}
	ok = true
	merge(2, "fedcba9876543210fedcba9876543210fedcba98")
	h.e.ProbeOnce(context.Background())
	if missed() != 0 {
		t.Fatalf("a merge the probe saw did not clear the count: %d", missed())
	}
}

// V0.playbook-probe-health: a stage keeps what its probe last said, written
// when the answer changes; a probe stopped at the limit or one that cannot
// run is broken, and one stuck probe does not hold the others back.
func TestProbeAnswersAreKeptAndRunTogether(t *testing.T) {
	h, pid, th := playbookProject(t)
	for _, a := range []core.Args{
		{"slot": "stage", "name": "dev", "probe": "dev {sha} {repo}"}, {"slot": "stage", "name": "dev", "allow": true},
		{"slot": "stage", "name": "prod", "probe": "prod {sha}"}, {"slot": "stage", "name": "prod", "allow": true},
	} {
		a["id"] = pid
		if r := h.cmd(core.KindProjectLearn, "", a); !r.OK {
			t.Fatalf("%+v", r)
		}
	}
	url := "https://github.com/acme/app/pull/9"
	coretest.Patch(h.e, th, func(x *core.Thread, now time.Time) []core.Event {
		core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "OPEN", Checks: "pass", Head: "h1"}, now)
		core.PRObserved(x, core.PR{URL: url, Repo: "acme/app", Number: 9, State: "MERGED", Checks: "pass", MergeSHA: probeSHA, MergedAt: &now}, now)
		return nil
	})
	old := probeTimeout
	probeTimeout = 2 * time.Second
	t.Cleanup(func() { probeTimeout = old })
	off := time.Duration(0)
	step := func() { off += 2 * time.Minute; d := off; h.e.Now = func() time.Time { return time.Now().Add(d) } }
	last := func(name string) *core.ProbeResult {
		p := projectByID(h, pid)
		for _, e := range p.Stages() {
			if e.Name == name {
				return e.Last
			}
		}
		return nil
	}
	// dev sits until its limit; prod answers while it sits.
	prodRan := make(chan struct{}, 1)
	h.e.RunProbe = func(ctx context.Context, _, script string) (string, int, error) {
		if strings.HasPrefix(script, "dev ") {
			if script != "dev "+probeSHA+" acme/app" {
				t.Errorf("script %q", script)
			}
			select {
			case <-prodRan:
			case <-time.After(5 * time.Second):
				t.Error("prod's probe waited for dev's")
			}
			<-ctx.Done()
			return "applied: 899b950\nstopped after the time limit", -1, ctx.Err()
		}
		prodRan <- struct{}{}
		return "TIMEOUT after 0s: not applied on production.\n", 1, nil
	}
	h.e.ProbeOnce(context.Background())
	if l := last("dev"); l == nil || l.State != core.ProbeTimeout || l.Note != "applied: 899b950" {
		t.Fatalf("dev: %+v", l)
	}
	if l := last("prod"); l == nil || l.State != core.ProbeWaiting || l.Exit != 1 || !strings.HasPrefix(l.Note, "TIMEOUT after 0s") {
		t.Fatalf("prod: %+v", l)
	}
	p := projectByID(h, pid)
	if !p.Stages()[0].Broken() || p.Stages()[1].Broken() || !strings.Contains(core.PlaybookPrompt(&p), "30 s limit") {
		t.Fatalf("broken: %s", core.PlaybookPrompt(&p))
	}
	// The same answer writes nothing; another one does.
	at := last("prod").At
	h.e.RunProbe = func(_ context.Context, _, script string) (string, int, error) {
		if strings.HasPrefix(script, "dev ") {
			return "sh: 1: dev: not found", 127, nil
		}
		return "", 1, nil
	}
	step()
	h.e.ProbeOnce(context.Background())
	if l := last("prod"); !l.At.Equal(at) {
		t.Fatalf("the same answer was written again: %+v", l)
	}
	if l := last("dev"); l.State != core.ProbeError || l.Exit != 127 {
		t.Fatalf("dev: %+v", l)
	}
	h.e.RunProbe = func(context.Context, string, string) (string, int, error) { return "", 0, nil }
	step()
	h.e.ProbeOnce(context.Background())
	if l := last("dev"); l.State != core.ProbeReached {
		t.Fatalf("dev: %+v", l)
	}
	if c := firstChange(h, th); c.Phase != core.PhaseLive {
		t.Fatalf("phase %s", c.Phase)
	}
	// Another command starts without the old one's answer.
	if r := h.cmd(core.KindProjectLearn, "", core.Args{"id": pid, "slot": "stage", "name": "dev", "probe": "dev2 {sha}"}); !r.OK || last("dev") != nil {
		t.Fatalf("%+v %+v", r, last("dev"))
	}
}
