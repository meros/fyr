package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/github"
)

// V0.pr-create-tool, V0.182: pr_create opens a PR without holding e.mu,
// resolves the change's worktree directory, links the PR to the change card,
// kicks OnPR, and refuses directories outside the thread's folder/worktrees.
func TestPRCreateTool(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))

	th, _ := h.e.Thread(id)
	wtDir := filepath.Join(t.TempDir(), "wt-feat")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed a change with a worktree outside t.Cwd to prove worktrees are allowed.
	if _, err := h.e.Apply(core.Input{
		Source: core.FromAgent,
		Kind:   core.KindChangeOpen,
		Thread: id,
		Args: core.ChangeOpenArgs{Open: core.ChangeOpen{
			Key:       "feat/pr-tool",
			Kind:      core.KindCode,
			Title:     "Add pr_create tool",
			Branch:    "feat/pr-tool",
			Workspace: &core.Workspace{Path: wtDir, Branch: "feat/pr-tool", MadeByFyr: true},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	var gotReq github.CreatePRReq
	var kicked string
	h.e.CreatePR = func(_ context.Context, req github.CreatePRReq) (string, error) {
		gotReq = req
		return "https://github.com/acme/app/pull/77", nil
	}
	h.e.OnPR = func(threadID string) { kicked = threadID }
	guardDeps(t, h.e)

	out, isErr := callAs(t, h.e, id, "pr_create", map[string]any{
		"change":    "feat/pr-tool",
		"title":     "Add pr_create tool",
		"body":      "In short, adds pr_create.",
		"base":      "main",
		"draft":     true,
		"labels":    []any{"area:core"},
		"reviewers": []any{"alice"},
	})
	if isErr {
		t.Fatalf("pr_create failed: %s", out)
	}
	if !strings.Contains(out, "https://github.com/acme/app/pull/77") || !strings.Contains(out, "change_request") {
		t.Fatalf("unexpected reply: %s", out)
	}
	if gotReq.Dir != wtDir || gotReq.Head != "feat/pr-tool" || gotReq.Base != "main" || !gotReq.Draft {
		t.Fatalf("CreatePR req = %+v", gotReq)
	}
	if kicked != id {
		t.Fatalf("OnPR kicked = %q, want %q", kicked, id)
	}
	c := changeOf(h, id, "feat/pr-tool")
	if c == nil || c.PRURL() != "https://github.com/acme/app/pull/77" {
		t.Fatalf("PR not linked to change: %+v", c)
	}

	// Refuse directories outside the thread's folder and worktrees.
	outside := t.TempDir()
	if out, isErr := callAs(t, h.e, id, "pr_create", map[string]any{
		"dir":   outside,
		"title": "Escape",
		"body":  "nope",
	}); !isErr || !strings.Contains(out, "inside the thread's folder") {
		t.Fatalf("expected outside dir refusal, got isErr=%v out=%q", isErr, out)
	}

	// Calling without change defaults to thread.Cwd and creates/links a PR change.
	h.e.CreatePR = func(_ context.Context, req github.CreatePRReq) (string, error) {
		gotReq = req
		return "https://github.com/acme/app/pull/78", nil
	}
	guardDeps(t, h.e)
	out, isErr = callAs(t, h.e, id, "pr_create", map[string]any{
		"title": "Second PR",
		"body":  "In short, second.",
	})
	if isErr || !strings.Contains(out, "https://github.com/acme/app/pull/78") {
		t.Fatalf("second pr_create failed: isErr=%v out=%q", isErr, out)
	}
	if gotReq.Dir != filepath.Clean(th.Cwd) {
		t.Fatalf("default dir = %q, want %q", gotReq.Dir, filepath.Clean(th.Cwd))
	}
	if c2 := changeOf(h, id, "https://github.com/acme/app/pull/78"); c2 == nil {
		t.Fatal("second PR change not recorded")
	}

	// Nil CreatePR returns a clear error.
	h.e.CreatePR = nil
	if out, isErr := callAs(t, h.e, id, "pr_create", map[string]any{
		"title": "No hook",
		"body":  "body",
	}); !isErr || !strings.Contains(out, "cannot open GitHub pull requests") {
		t.Fatalf("expected nil CreatePR error, got isErr=%v out=%q", isErr, out)
	}
}
