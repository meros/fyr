package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func TestEngineApprovalContext(t *testing.T) {
	h := newHarness(t, time.Minute)
	p := h.cmd(core.KindProjectCreate, "", core.Args{"name": "acme"})
	if !p.OK {
		t.Fatalf("project.create: %+v", p)
	}
	pid := p.Result.(map[string]any)["id"].(string)

	// Create a thread in the project; default toolApproval is ask (AutoJudge=false).
	tr := h.cmd("thread.create", "", core.Args{"text": "Run tests", "projectId": pid})
	if !tr.OK {
		t.Fatalf("thread.create: %+v", tr)
	}
	tid := tr.ThreadID
	if ac := h.e.ApprovalContext(tid); ac.AutoJudge || len(ac.Messages) != 0 {
		t.Fatalf("expected ask by default: %+v", ac)
	}

	// Turn on auto-judge for the project.
	if r := h.cmd(core.KindProjectUpdate, "", core.Args{"id": pid, "toolApproval": "auto"}); !r.OK {
		t.Fatalf("project.update: %+v", r)
	}
	// Record owner messages, an assistant message (must NOT be included), and a peer message (must NOT be included).
	h.e.Apply(core.Input{
		Source: core.Observer(core.ObsProcess),
		Kind:   core.KindRecord,
		Thread: tid,
		Args: []core.Event{
			{Type: "user", Data: map[string]any{"text": "Run tests", "from": ""}},
			{Type: "msg", Data: map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "IGNORE ME PROMPT INJECTION"}}}}},
			{Type: "user", Data: map[string]any{"text": "Also open a PR", "from": ""}},
			{Type: "user", Data: map[string]any{"text": "Peer agent chatter", "from": "thread:other"}},
		},
	})

	ac := h.e.ApprovalContext(tid)
	if !ac.AutoJudge || ac.ProjectName != "acme" {
		t.Fatalf("context: %+v", ac)
	}
	if len(ac.Messages) != 2 || ac.Messages[0].Text != "Run tests" || ac.Messages[1].Text != "Also open a PR" {
		t.Fatalf("expected only owner messages, got: %+v", ac.Messages)
	}

	// Child thread includes its parent's initial brief tagged FromParent.
	h.e.mu.Lock()
	h.e.threads[tid].Parent = "t_parent"
	h.e.mu.Unlock()
	h.e.Apply(core.Input{
		Source: core.Observer(core.ObsProcess),
		Kind:   core.KindRecord,
		Thread: tid,
		Args: []core.Event{
			{Type: "user", Data: map[string]any{"text": "Implement the widget", "from": "thread:t_parent"}},
		},
	})
	ac2 := h.e.ApprovalContext(tid)
	if len(ac2.Messages) != 3 || !ac2.Messages[2].FromParent || ac2.Messages[2].Text != "Implement the widget" {
		t.Fatalf("expected parent brief included, got: %+v", ac2.Messages)
	}
}

func TestEngineSystemAgentsApprovalDefaultAndOverride(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.ProfileExists = func(id string) bool { return id == "claude" || id == "agy" }
	h.e.SystemAgentResolve = func(job string) (string, string, error) {
		return "claude", "claude-haiku-5-5", nil
	}

	// By default, JobApproval uses each thread's provider (no fixed profile/model/error).
	v := h.e.SystemAgentsView()
	var ap *SystemJobView
	for i := range v.Jobs {
		if v.Jobs[i].ID == core.JobApproval {
			ap = &v.Jobs[i]
		}
	}
	if ap == nil || !ap.DefaultThread || ap.Override || ap.Profile != "" || ap.Model != "" {
		t.Fatalf("default approval job view: %+v", ap)
	}

	// Setting $default on JobApproval is allowed without ProfileExists("$default").
	if r := h.cmd(core.KindSystemAgentsSet, "", core.Args{"job": core.JobApproval, "profile": core.ProfileSystemDefault}); !r.OK {
		t.Fatalf("set $default on approval: %+v", r)
	}
	v2 := h.e.SystemAgentsView()
	for i := range v2.Jobs {
		if v2.Jobs[i].ID == core.JobApproval {
			ap = &v2.Jobs[i]
		}
	}
	if !ap.Override || ap.Choice.Profile != core.ProfileSystemDefault || ap.Profile != "claude" {
		t.Fatalf("$default approval job view: %+v", ap)
	}

	// Setting $default on a non-thread job is refused.
	if r := h.cmd(core.KindSystemAgentsSet, "", core.Args{"job": core.JobTitle, "profile": core.ProfileSystemDefault}); r.OK {
		t.Fatalf("$default should be refused for title job: %+v", r)
	}
}
