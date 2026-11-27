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

// V0.agy-reactive-wakeup: once all open top-level tool calls return their
// tool_result (or a top-level thinking block arrives with no tools open),
// Activity updates to "Thinking" instead of staying on a completed tool.
func TestDigestActivityMovesToThinkingWhenToolsFinish(t *testing.T) {
	d := newDigest()
	o := d.feed(asst(nil, M{"type": "tool_use", "id": "t1", "name": "mcp__fyr__thread_rename", "input": M{"title": "New"}}))
	if o.Activity != "Fyr thread rename" {
		t.Fatalf("activity %q", o.Activity)
	}
	o = d.feed(result("t1", "renamed"))
	if o.Activity != "Thinking" {
		t.Fatalf("expected Thinking after tool_result, got %q", o.Activity)
	}

	// Parallel tools: Activity only moves to Thinking once the last open tool returns.
	d.feed(asst(nil, M{"type": "tool_use", "id": "p1", "name": "Read", "input": M{"file_path": "/a.go"}}))
	d.feed(asst(nil, M{"type": "tool_use", "id": "p2", "name": "Read", "input": M{"file_path": "/b.go"}}))
	if o := d.feed(result("p1", "a")); o.Activity != "" {
		t.Fatalf("expected empty activity while p2 still open, got %q", o.Activity)
	}
	if o := d.feed(asst(nil, M{"type": "thinking", "thinking": ""})); o.Activity != "" {
		t.Fatalf("expected empty activity on thinking while p2 still open, got %q", o.Activity)
	}
	if o := d.feed(result("p2", "b")); o.Activity != "Thinking" {
		t.Fatalf("expected Thinking once all tools finished, got %q", o.Activity)
	}

	// Top-level thinking block with no open tools sets Activity to Thinking.
	if o := d.feed(asst(nil, M{"type": "thinking", "thinking": ""})); o.Activity != "Thinking" {
		t.Fatalf("expected Thinking on top-level thinking block, got %q", o.Activity)
	}

	// resetOpen clears open tools across runs so a late background tool_result
	// between turns does not report Thinking.
	d.feed(asst(nil, M{"type": "tool_use", "id": "bg1", "name": "Bash", "input": M{"command": "go test"}}))
	d.resetOpen()
	if o := d.feed(result("bg1", "ok")); o.Activity != "" {
		t.Fatalf("expected empty activity for post-run background tool_result, got %q", o.Activity)
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
	// V0.multi-pr: a command that creates several PRs links all of them.
	d.feed(asst(nil, M{"type": "tool_use", "id": "m", "name": "Bash", "input": M{"command": "for r in a b; do gh pr create --fill; done"}}))
	om := d.feed(result("m", "https://github.com/acme/a/pull/1\nhttps://github.com/acme/b/pull/2\nhttps://github.com/acme/a/pull/1\n"))
	if len(om.PRURLs) != 2 || om.PRURLs[0] != "https://github.com/acme/a/pull/1" || om.PRURLs[1] != "https://github.com/acme/b/pull/2" {
		t.Fatalf("multi PRURLs: %v", om.PRURLs)
	}
}

// SPEC 0.5 V0.8: only a line that is a PR URL links, as gh pr create prints
// it; a command that mentions gh pr create and prints a PR URL inside other
// text (a test run, a grep) links nothing.
func TestDigestPRNeedsOwnLine(t *testing.T) {
	d := newDigest()
	d.feed(asst(nil, M{"type": "tool_use", "id": "g", "name": "Bash", "input": M{"command": "go test ./internal/github/ 2>&1 | grep -v 'gh pr create'"}}))
	o := d.feed(result("g", "detail_test.go:45: \"url\": \"https://github.com/acme/app/pull/88\"\nok\n"))
	if o.PRURL != "" {
		t.Fatalf("a URL inside test output linked: %q", o.PRURL)
	}
	d.feed(asst(nil, M{"type": "tool_use", "id": "h", "name": "Bash", "input": M{"command": "cd repo && gh pr create --fill"}}))
	if o := d.feed(result("h", "Warning: 1 uncommitted change\n  https://github.com/o/r/pull/5  \n")); o.PRURL != "https://github.com/o/r/pull/5" {
		t.Fatalf("pr %q", o.PRURL)
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
		if !ok || k.ID != "a1" || string(k.Status) != c.status || k.Activity != c.act || ev != c.event || k.ToolUses != c.uses || k.Tokens != c.tokens {
			t.Errorf("case %d: %+v %q %v", i, k, ev, ok)
		}
	}
	// V0.subagent-timeline: task_started names the Agent call (CLI 2.1.327).
	if k, _, _ := taskFromSystem(M{"type": "system", "subtype": "task_started", "task_id": "a2", "tool_use_id": "toolu_1", "task_type": "local_agent"}); k.ToolUseID != "toolu_1" {
		t.Errorf("tool use id %q", k.ToolUseID)
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

// V0.195: the CLI reports foreground Bash as tasks too, and a Monitor as a
// local_bash: is_backgrounded and the tool_use tell them apart.
func TestTaskBackgroundAndTool(t *testing.T) {
	d := newDigest()
	d.feed(M{"type": "assistant", "message": M{"content": []any{M{"type": "tool_use", "id": "tu1", "name": "Monitor", "input": M{}}}}})
	m := M{"type": "system", "subtype": "task_started", "task_id": "b1", "tool_use_id": "tu1", "task_type": "local_bash", "is_backgrounded": true}
	k, _, _ := taskFromSystem(m)
	if !k.Background || d.taskTool(m) != "Monitor" {
		t.Fatalf("%+v tool %q", k, d.taskTool(m))
	}
	m["is_backgrounded"] = false
	if k, _, _ = taskFromSystem(m); k.Background {
		t.Fatal("a foreground Bash read as background")
	}
}

// V0.thread-issues: an issue URL printed by gh issue create links the issue;
// gh issue view links nothing.
func TestDigestIssueFromGhIssueCreate(t *testing.T) {
	d := newDigest()
	d.feed(asst(nil, M{"type": "tool_use", "id": "i", "name": "Bash", "input": M{"command": "gh issue create --title x --body y"}}))
	o := d.feed(result("i", "Creating issue in acme/app\n\nhttps://github.com/acme/app/issues/12\n"))
	if len(o.IssueURLs) != 1 || o.IssueURLs[0] != "https://github.com/acme/app/issues/12" || o.PRURL != "" {
		t.Fatalf("issues %v pr %q", o.IssueURLs, o.PRURL)
	}
	d.feed(asst(nil, M{"type": "tool_use", "id": "v", "name": "Bash", "input": M{"command": "gh issue view 7"}}))
	if o := d.feed(result("v", "https://github.com/acme/app/issues/7\n")); len(o.IssueURLs) != 0 {
		t.Fatalf("gh issue view linked %v", o.IssueURLs)
	}
}
