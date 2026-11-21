package core

import (
	"strings"
	"testing"
	"time"
)

func TestParseApprovalJudge(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 25))
	cases := []struct {
		name   string
		in     string
		ok     bool
		allow  bool
		reason string
	}{
		{"allow", `{"decision":"allow","reason":"Routine build and test command"}`, true, true, "Routine build and test command"},
		{"ask", `{"decision":"ask","reason":"Modifies files outside the project"}`, true, false, "Modifies files outside the project"},
		{"fenced", "```json\n{\"decision\": \"ALLOW\", \"reason\": \"  pushing feature branch  \"}\n```", true, true, "pushing feature branch"},
		{"long reason", `{"decision":"ask","reason":"` + long + `"}`, true, false, strings.TrimSpace(strings.Repeat("word ", 18)) + "…"},
		{"deny fails closed", `{"decision":"deny","reason":"dangerous"}`, false, false, ""},
		{"unknown fails closed", `{"decision":"maybe","reason":"unsure"}`, false, false, ""},
		{"malformed fails closed", `not json at all`, false, false, ""},
		{"empty fails closed", ``, false, false, ""},
	}
	for _, c := range cases {
		allow, reason, ok := ParseApprovalJudge(c.in)
		if ok != c.ok || allow != c.allow || reason != c.reason {
			t.Errorf("%s: got (%v, %q, %v) want (%v, %q, %v)", c.name, allow, reason, ok, c.allow, c.reason, c.ok)
		}
	}
}

func TestFormatApprovalPrompt(t *testing.T) {
	prompt := FormatApprovalPrompt(ApprovalPromptInput{
		ProjectName:   "acme",
		ProjectFolder: "/src/acme",
		ThreadTitle:   "Add feature",
		ThreadBranch:  "feat/x",
		Tool:          "Bash",
		Input:         map[string]any{"command": "go test ./...", "BypassSandbox": true},
		Subagent:      true,
		Messages: []ApprovalMessage{
			{Text: "Run tests and open a PR", FromParent: true},
			{Text: "Go ahead and push"},
		},
	})
	for _, want := range []string{
		"Project: acme",
		"Project folder: /src/acme",
		"Thread: Add feature",
		"Branch: feat/x",
		"Caller: subagent",
		"- [from parent thread] Run tests and open a PR",
		"- Go ahead and push",
		"<tool_call>",
		"</tool_call>",
		"go test ./...",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}

	// Huge tool input is truncated with an explicit marker.
	huge := FormatApprovalPrompt(ApprovalPromptInput{
		ProjectName: "acme",
		Tool:        "Bash",
		Input:       map[string]any{"command": strings.Repeat("a", 20000)},
	})
	if !strings.Contains(huge, "...[truncated]") {
		t.Fatalf("expected truncated marker in huge prompt")
	}
}

func TestCanonicalApprovalKeyAndSummary(t *testing.T) {
	k1 := CanonicalApprovalKey("Bash", map[string]any{"a": 1, "b": "two"})
	k2 := CanonicalApprovalKey("Bash", map[string]any{"b": "two", "a": 1})
	k3 := CanonicalApprovalKey("Bash", map[string]any{"a": 2, "b": "two"})
	if k1 == "" || k1 != k2 {
		t.Fatalf("canonical keys should match: %q vs %q", k1, k2)
	}
	if k1 == k3 {
		t.Fatalf("different inputs should differ")
	}
	if got := ApprovalToolSummary("Bash", map[string]any{"command": "echo hi\necho bye"}); got != "echo hi …" {
		t.Fatalf("summary: %q", got)
	}
}

func TestProjectToolApproval(t *testing.T) {
	var nilProj *Project
	if nilProj.AutoJudgesApprovals() {
		t.Fatal("nil project should not auto-judge")
	}
	p := &Project{ID: "p_1", Name: "acme", Folder: "/src/acme"}
	if p.AutoJudgesApprovals() {
		t.Fatal("default project should ask")
	}
	after, _, err := reduceP(t, p, FromOwner, KindProjectUpdate, Args{"toolApproval": "auto"})
	if err != nil || !after.AutoJudgesApprovals() || after.ToolApproval != ToolApprovalAuto {
		t.Fatalf("auto: %v %+v", err, after)
	}
	ask, _, err := reduceP(t, after, FromOwner, KindProjectUpdate, Args{"toolApproval": "ask"})
	if err != nil || ask.AutoJudgesApprovals() || ask.ToolApproval != "" {
		t.Fatalf("ask: %v %+v", err, ask)
	}
	cleared, _, err := reduceP(t, after, FromOwner, KindProjectUpdate, Args{"toolApproval": nil})
	if err != nil || cleared.AutoJudgesApprovals() || cleared.ToolApproval != "" {
		t.Fatalf("nil clear: %v %+v", err, cleared)
	}
	if _, _, err := reduceP(t, p, FromOwner, KindProjectUpdate, Args{"toolApproval": "allow-all"}); err == nil {
		t.Fatal("invalid toolApproval should be rejected")
	}

	// Linked project shared sync carries toolApproval.
	t0 := time.Unix(100, 0).UTC()
	const link = "l_0123456789abcdef"
	ps := map[string]*Project{"p1": {ID: "p1", Name: "acme", Link: link, SharedAt: t0}}
	_, synced, _, err := ReduceProject(ps, nil, Input{
		Source: FromOwner,
		Kind:   KindProjectShared,
		Args:   Args{"id": "p1", "link": link, "at": t0.Add(time.Second).Format(time.RFC3339Nano), "toolApproval": "auto"},
	}, t0)
	if err != nil || !synced.AutoJudgesApprovals() {
		t.Fatalf("shared sync: %v %+v", err, synced)
	}
}

func TestSystemAgentsApprovalJob(t *testing.T) {
	if !IsSystemJob(JobApproval) || !JobUsesThread(JobApproval) {
		t.Fatal("approval should be a thread-defaulting system job")
	}
	if JobUsesThread(JobTitle) {
		t.Fatal("title should not default to thread provider")
	}
	s := SystemAgents{Default: SystemAgentChoice{Profile: "claude", Model: "claude-haiku-5-5"}}
	if !s.UsesThread(JobApproval) {
		t.Fatal("unset approval job should use thread provider")
	}
	// Overriding to $default follows SystemAgents.Default.
	s.Jobs = map[string]SystemAgentChoice{JobApproval: {Profile: ProfileSystemDefault}}
	if s.UsesThread(JobApproval) {
		t.Fatal("$default override should not use thread provider")
	}
	if got := s.For(JobApproval); got != s.Default {
		t.Fatalf("$default resolved to %+v want %+v", got, s.Default)
	}
	// Explicit provider/model override.
	s.Jobs[JobApproval] = SystemAgentChoice{Profile: "agy", Model: "gemini-4-hydrogen"}
	if got := s.For(JobApproval); got.Profile != "agy" || got.Model != "gemini-4-hydrogen" {
		t.Fatalf("explicit override: %+v", got)
	}
}
