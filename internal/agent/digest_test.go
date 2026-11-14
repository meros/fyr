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
	if o.Activity != "Bash yarn test --watch=false" {
		t.Fatalf("activity %q", o.Activity)
	}
	o = d.feed(asst(nil, M{"type": "tool_use", "id": "b", "name": "Task", "input": M{"description": "explore gestures"}}))
	if o.Subagents != 1 || o.Activity != "Task explore gestures" {
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
	repo, n, ok := ParsePR("https://github.com/NorbanDev/norban-web/pull/12345")
	if !ok || repo != "NorbanDev/norban-web" || n != 12345 {
		t.Fatal(repo, n, ok)
	}
	if _, _, ok := ParsePR("https://github.com/a/b/issues/1"); ok {
		t.Fatal("issue parsed as PR")
	}
}
