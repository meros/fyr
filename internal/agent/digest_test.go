package agent

import "testing"

type M = map[string]any

func asst(parent any, blocks ...M) M {
	bs := []any{}
	for _, b := range blocks {
		bs = append(bs, b)
	}
	return M{"type": "assistant", "parent_tool_use_id": parent, "message": M{"content": bs}}
}

func result(id string, content any) M {
	return M{"type": "user", "parent_tool_use_id": nil, "message": M{"content": []any{M{"type": "tool_result", "tool_use_id": id, "content": content}}}}
}

// SPEC 0.5 V0.3, 13.5: the row shows the last tool; subagents are counted.
func TestDigestActivityAndSubagents(t *testing.T) {
	d := newDigest()
	o := d.feed(asst(nil, M{"type": "tool_use", "id": "a", "name": "Bash", "input": M{"command": "yarn test --watch=false\nmore"}}))
	if o.Activity != "Running yarn test --watch=false" {
		t.Fatalf("activity %q", o.Activity)
	}
	o = d.feed(asst(nil, M{"type": "tool_use", "id": "b", "name": "Task", "input": M{"description": "explore gestures"}}))
	if o.Subagents != 1 || o.Activity != "explore gestures" {
		t.Fatalf("%+v", o)
	}
	o = d.feed(asst("b", M{"type": "tool_use", "id": "c", "name": "Read", "input": M{"file_path": "/x"}}))
	if o.Subagents != 0 || o.Activity != "" {
		t.Fatalf("subagent message changed the row: %+v", o)
	}
	o = d.feed(asst(nil, M{"type": "text", "text": "\n\nAll tests pass.\nDetails follow."}))
	if o.Summary != "All tests pass." {
		t.Fatalf("summary %q", o.Summary)
	}
}

// SPEC 0.5 V0.8: a PR URL printed by gh pr create links the PR.
func TestDigestPRFromGhPrCreate(t *testing.T) {
	d := newDigest()
	d.feed(asst(nil, M{"type": "tool_use", "id": "p", "name": "Bash", "input": M{"command": "gh pr create --fill"}}))
	o := d.feed(result("p", "Creating pull request\nhttps://github.com/acme/app/pull/42\n"))
	if o.PRURL != "https://github.com/acme/app/pull/42" {
		t.Fatalf("pr %q", o.PRURL)
	}
	d.feed(asst(nil, M{"type": "tool_use", "id": "q", "name": "Bash", "input": M{"command": "gh pr view 7"}}))
	if o := d.feed(result("q", []any{M{"type": "text", "text": "https://github.com/acme/app/pull/7"}})); o.PRURL != "" {
		t.Fatalf("gh pr view must not link: %q", o.PRURL)
	}
}

func TestParsePR(t *testing.T) {
	repo, n, ok := ParsePR("https://github.com/acme/web/pull/12345")
	if !ok || repo != "acme/web" || n != 12345 {
		t.Fatal(repo, n, ok)
	}
	if _, _, ok := ParsePR("https://github.com/a/b/issues/1"); ok {
		t.Fatal("issue parsed as PR")
	}
}

// SPEC 0.5 V0.16: the CLI's task messages, in the shapes CLI 2.1.325 sends.
func TestTaskFromSystem(t *testing.T) {
	cases := []struct {
		msg          M
		status, act  string
		event        string
		uses, tokens int
	}{
		{M{"type": "system", "subtype": "task_started", "task_id": "a1", "description": "Design mobile", "task_type": "local_agent"}, "running", "", "started", 0, 0},
		{M{"type": "system", "subtype": "task_progress", "task_id": "a1", "description": "Running Create output directories", "last_tool_name": "Bash",
			"usage": M{"total_tokens": 143218.0, "tool_uses": 14.0}}, "", "Running Create output directories", "", 14, 143218},
		{M{"type": "system", "subtype": "task_updated", "task_id": "a1", "patch": M{"status": "completed"}}, "completed", "", "ended", 0, 0},
		{M{"type": "system", "subtype": "task_notification", "task_id": "a1", "status": "stopped", "summary": "did not finish"}, "stopped", "", "ended", 0, 0},
	}
	for i, c := range cases {
		k, ev, ok := taskFromSystem(c.msg)
		if !ok || k.ID != "a1" || k.Status != c.status || k.Activity != c.act || ev != c.event || k.ToolUses != c.uses || k.Tokens != c.tokens {
			t.Errorf("case %d: %+v %q %v", i, k, ev, ok)
		}
	}
	if _, _, ok := taskFromSystem(M{"type": "system", "subtype": "thinking_tokens"}); ok {
		t.Error("thinking_tokens is not a task")
	}
	bg := taskList(M{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{M{"task_id": "b1", "description": "watch CI", "task_type": "local_bash"}}})
	if len(bg) != 1 || bg[0].ID != "b1" || bg[0].Status != "running" {
		t.Errorf("background list %+v", bg)
	}
}

// SPEC 0.5 V0.21: the live activity line is human-readable.
func TestActivityLabels(t *testing.T) {
	cases := []struct {
		name string
		in   M
		want string
	}{
		{"Bash", M{"command": "go test ./...", "description": "Run the Go tests"}, "Run the Go tests"},
		{"Bash", M{"command": "make build\nmore"}, "Running make build"},
		{"Read", M{"file_path": "/home/u/src/fyr/web/src/app.tsx"}, "Reading app.tsx"},
		{"Edit", M{"file_path": "/x/thread.tsx"}, "Editing thread.tsx"},
		{"Write", M{"file_path": "/x/new.go"}, "Writing new.go"},
		{"Grep", M{"pattern": "presence"}, "Searching for presence"},
		{"Glob", M{"pattern": "**/*.go"}, "Listing **/*.go"},
		{"WebSearch", M{"query": "tailscale serve"}, "Searching the web for tailscale serve"},
		{"WebFetch", M{"url": "https://pkg.go.dev/net/http"}, "Reading pkg.go.dev"},
		{"Agent", M{"description": "Design mobile"}, "Design mobile"},
		{"mcp__fyr__show", M{"title": "Chart"}, "Showing Chart"},
		{"mcp__github__get_pr", M{}, "GitHub get pr"},
		{"ToolSearch", M{"query": "select:x"}, ""},
		{"TodoWrite", M{}, ""},
	}
	for _, c := range cases {
		if got := activityLabel(c.name, c.in); got != c.want {
			t.Errorf("%s %v: got %q want %q", c.name, c.in, got, c.want)
		}
	}
}

// SPEC 0.5 V0.37: only a process started with the live view registers the
// Stop hook; other threads pay no extra round trip at the end of each turn.
func TestInitHooksStopOnlyWithView(t *testing.T) {
	if _, ok := initHooks(false)["Stop"]; ok {
		t.Fatal("a thread without the live view registers the Stop hook")
	}
	if _, ok := initHooks(true)["Stop"]; !ok {
		t.Fatal("a live view thread has no Stop hook")
	}
	if _, ok := initHooks(false)["PreToolUse"]; !ok {
		t.Fatal("the gate hook is gone")
	}
	// V0.52: the mid-turn reminder rides on PostToolUse, also only with the view.
	if _, ok := initHooks(false)["PostToolUse"]; ok {
		t.Fatal("a thread without the live view registers the reminder hook")
	}
	if _, ok := initHooks(true)["PostToolUse"]; !ok {
		t.Fatal("a live view thread has no reminder hook")
	}
}

// review 2026-11-15: a PR URL is the whole value, not a URL found inside
// it, so a value like "--subject=x <url>" never reaches gh as arguments.
func TestParsePRAnchored(t *testing.T) {
	for _, bad := range []string{
		"--subject=x https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/1 --admin",
		" https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/1x",
		"xhttps://github.com/o/r/pull/1",
	} {
		if _, _, ok := ParsePR(bad); ok {
			t.Errorf("ParsePR(%q) accepted", bad)
		}
		if _, ok := CanonicalPR(bad); ok {
			t.Errorf("CanonicalPR(%q) accepted", bad)
		}
	}
	for in, want := range map[string]string{
		"https://github.com/o/r/pull/1":                       "https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/1/":                      "https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/12/files#diff-abc":       "https://github.com/o/r/pull/12",
		"https://github.com/O.k/r-x_y/pull/3?notification=1": "https://github.com/O.k/r-x_y/pull/3",
	} {
		if got, ok := CanonicalPR(in); !ok || got != want {
			t.Errorf("CanonicalPR(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
}
