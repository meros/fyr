package agycli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
)

// TestMain lets this test binary double as a real subprocess fake `agy` ACP
// server when FYR_FAKE_AGY=1, proving the process boundary, JSON-RPC framing,
// permissions, queuing, cancellation, model switching, Print, and ListModels.
func TestMain(m *testing.M) {
	if os.Getenv("FYR_FAKE_AGY") == "1" {
		runFakeACP()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeACP() {
	if logPath := os.Getenv("FYR_FAKE_AGY_ARGS"); logPath != "" {
		_ = os.WriteFile(logPath, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	write := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}
	sid := "agy-sess-1"
	curModel := "gemini-4-neon"
	curEffort := "high"
	var lastSysPrompt string
	var lastMCP []any
	var permCounter int

	modelsPayload := map[string]any{
		"availableModels": []any{
			map[string]any{"modelId": "gemini-4-neon-low", "name": "Gemini 4 Neon (Low)"},
			map[string]any{"modelId": "gemini-4-neon-high", "name": "Gemini 4 Neon (High)"},
			map[string]any{"modelId": "gemini-4-neon-xhigh", "name": "Gemini 4 Neon (XHigh)"},
			map[string]any{"modelId": "gemini-3.8-flash-low", "name": "Gemini 3.8 Flash (Low)"},
			map[string]any{"modelId": "gemini-3.8-flash-medium", "name": "Gemini 3.8 Flash (Medium)"},
			map[string]any{"modelId": "gpt-oss-120b-medium", "name": "GPT-OSS 120B (Medium)"},
		},
		"currentModelId": "gemini-4-neon-high",
	}
	cfgOpts := func() []any {
		return []any{
			map[string]any{
				"id": "model", "category": "model", "type": "select", "currentValue": curModel,
				"options": []any{
					map[string]any{"value": "gemini-4-neon", "name": "Gemini 4 Neon"},
					map[string]any{"value": "gemini-3.8-flash", "name": "Gemini 3.8 Flash"},
					map[string]any{"value": "gpt-oss-120b-medium", "name": "GPT-OSS 120B (Medium)"},
				},
			},
			map[string]any{
				"id": "thought_level", "category": "thought_level", "type": "select", "currentValue": curEffort,
				"options": []any{
					map[string]any{"value": "low", "name": "Low"},
					map[string]any{"value": "medium", "name": "Medium"},
					map[string]any{"value": "high", "name": "High"},
					map[string]any{"value": "xhigh", "name": "Extra"},
				},
			},
		}
	}
	modes := map[string]any{
		"currentModeId": "default",
		"availableModes": []any{
			map[string]any{"id": "default", "name": "Default"},
			map[string]any{"id": "auto", "name": "Auto"},
			map[string]any{"id": "plan", "name": "Plan"},
		},
	}

	readPermReply := func(wantID string) string {
		for sc.Scan() {
			var r map[string]any
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			if fmt.Sprint(r["id"]) == wantID {
				res, _ := r["result"].(map[string]any)
				out, _ := res["outcome"].(map[string]any)
				opt, _ := out["optionId"].(string)
				return opt
			}
		}
		return ""
	}

	for sc.Scan() {
		var msg map[string]any
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		method, _ := msg["method"].(string)
		id := msg["id"]
		params, _ := msg["params"].(map[string]any)
		switch method {
		case "initialize":
			write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"protocolVersion": 1,
				"agentInfo":       map[string]any{"name": "Fake AGY ACP", "version": "1.5.13"},
			}})
		case "session/new", "session/resume":
			if method == "session/resume" {
				if s, _ := params["sessionId"].(string); s != "" {
					sid = s
				}
			}
			if m, _ := params["_meta"].(map[string]any); m != nil {
				if a, _ := m["antigravity"].(map[string]any); a != nil {
					if sp, _ := a["systemPrompt"].(map[string]any); sp != nil {
						lastSysPrompt, _ = sp["append"].(string)
					}
				}
			}
			lastMCP, _ = params["mcpServers"].([]any)
			write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": sid,
				"update": map[string]any{
					"sessionUpdate": "available_commands_update",
					"availableCommands": []any{
						map[string]any{"name": "plan", "description": "Plan carefully"},
						map[string]any{"name": "compact", "description": "Compact context"},
					},
				},
			}})
			write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"sessionId": sid, "models": modelsPayload, "modes": modes, "configOptions": cfgOpts(),
			}})
		case "session/set_config_option":
			if params["configId"] == "model" {
				curModel, _ = params["value"].(string)
			}
			if params["configId"] == "thought_level" {
				curEffort, _ = params["value"].(string)
			}
			write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"configOptions": cfgOpts()}})
		case "session/set_model":
			curModel, _ = params["modelId"].(string)
			write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "session/set_mode":
			write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "session/close":
			write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "session/prompt":
			prompt, _ := params["prompt"].([]any)
			var text string
			hasImage := false
			for _, x := range prompt {
				b, _ := x.(map[string]any)
				if b["type"] == "text" {
					text += fmt.Sprint(b["text"])
				}
				if b["type"] == "image" {
					hasImage = true
				}
			}
			switch {
			case strings.HasPrefix(text, "tool:"):
				cmd := strings.TrimPrefix(text, "tool:")
				permCounter++
				pid := fmt.Sprintf("p%d", permCounter)
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid,
					"update": map[string]any{
						"sessionUpdate": "tool_call", "toolCallId": "tc1", "name": "run_command",
						"args": map[string]any{"CommandLine": cmd, "toolSummary": "Run test cmd"}, "status": "in_progress",
					},
				}})
				write(map[string]any{"jsonrpc": "2.0", "id": pid, "method": "session/request_permission", "params": map[string]any{
					"sessionId": sid,
					"toolCall": map[string]any{
						"toolCallId": "tc1", "name": "run_command",
						"args": map[string]any{"CommandLine": cmd},
					},
					"options": []any{
						map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
						map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
					},
				}})
				choice := readPermReply(pid)
				st := "completed"
				out := "ran:" + cmd
				if choice != "allow" {
					st = "failed"
					out = "denied:" + cmd
				}
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid,
					"update": map[string]any{
						"sessionUpdate": "tool_call_update", "toolCallId": "tc1", "name": "run_command",
						"status": st, "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": out}}},
					},
				}})
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid,
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "done:" + choice}},
				}})
				write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case strings.HasPrefix(text, "fyr-mcp"):
				permCounter++
				pid := fmt.Sprintf("p%d", permCounter)
				write(map[string]any{"jsonrpc": "2.0", "id": pid, "method": "session/request_permission", "params": map[string]any{
					"sessionId": sid,
					"toolCall":  map[string]any{"toolCallId": "tc_mcp", "name": "mcp_fyr_change_list", "args": map[string]any{}},
					"options": []any{
						map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
						map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
					},
				}})
				choice := readPermReply(pid)
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid,
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "mcp:" + choice}},
				}})
				write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn"}})
			case text == "slow":
				// Wait for session/cancel notification.
				for sc.Scan() {
					var m2 map[string]any
					if json.Unmarshal(sc.Bytes(), &m2) == nil && m2["method"] == "session/cancel" {
						write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "cancelled"}})
						break
					}
				}
			default:
				reply := fmt.Sprintf("model=%s effort=%s sys=%s mcp=%d img=%v echo=%s",
					curModel, curEffort, lastSysPrompt, len(lastMCP), hasImage, text)
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid,
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": reply[:10]}},
				}})
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": sid,
					"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": reply[10:]}},
				}})
				write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn"}})
			}
		}
	}
}

func fakeLaunch(t *testing.T) cli.Launch {
	t.Helper()
	return cli.Launch{
		Binary: os.Args[0],
		Dir:    t.TempDir(),
		Auth:   cli.Auth{Kind: cli.CLILogin},
		Env:    []string{"FYR_FAKE_AGY=1"},
	}
}

func TestEnvAndArgs(t *testing.T) {
	a := Adapter{}
	if a.ID() != "agy" || !cli.Accepts(a, cli.CLILogin) || !cli.Accepts(a, cli.APIKey) {
		t.Fatalf("caps: %+v", a.Caps())
	}
	env, err := a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil || len(env) != 0 {
		t.Fatalf("cli-login: %v %v", env, err)
	}
	env, err = a.Env(cli.Launch{
		Auth:     cli.Auth{Kind: cli.APIKey},
		Key:      "gk-123",
		Endpoint: cli.Endpoint{BaseURL: "https://gemini.example", ModelEnv: map[string]string{"Z": "1", "A": "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GEMINI_API_KEY=gk-123", "GOOGLE_GEMINI_BASE_URL=https://gemini.example", "A=2", "Z=1"}
	if !slices.Equal(env, want) {
		t.Fatalf("env %v want %v", env, want)
	}
	if _, err := a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.APIKey}}); err == nil {
		t.Fatal("api-key without key must fail")
	}
	gotArgs := strings.Join(Args(cli.Launch{
		Auth:           cli.Auth{ConfigDir: "/cfg/.gemini"},
		AddDirs:        []string{"/extra"},
		PermissionMode: "bypassPermissions",
	}, ""), " ")
	if gotArgs != "acp --stdio --gemini-dir /cfg/.gemini --add-dir /extra --dangerously-skip-permissions" {
		t.Fatalf("args: %q", gotArgs)
	}
	if got := a.FastModel([]cli.Model{{ID: "gemini-4-neon"}, {ID: "gemini-3.8-flash"}, {ID: "gemini-4-hydrogen"}}, nil, false); got != "gemini-4-hydrogen" {
		t.Fatalf("FastModel should prefer gemini-4-hydrogen, got %q", got)
	}
	if got := a.FastModel([]cli.Model{{ID: "gemini-4-neon"}, {ID: "gemini-3.8-flash"}}, nil, false); got != "gemini-3.8-flash" {
		t.Fatalf("FastModel should fall back to flash, got %q", got)
	}
}

func TestACPLifecycleMCPSystemPromptAndImages(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	os.WriteFile(mcpPath, []byte(`{"mcpServers":{"fyr":{"type":"http","url":"http://127.0.0.1:9999/mcp","headers":{"Authorization":"Bearer tok"}}}}`), 0o600)
	argsPath := filepath.Join(dir, "args")

	l := fakeLaunch(t)
	l.Env = append(l.Env, "FYR_FAKE_AGY_ARGS="+argsPath)
	l.Model = "gemini-3.8-flash"
	l.Effort = "medium"
	l.SystemPrompt = "SYS_BLOCK"
	l.MCPConfigFile = mcpPath
	l.AllowedTools = []string{"mcp__fyr__*"}

	sess, err := Adapter{}.Start(l, cli.Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ans, err := sess.Initialize(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	inner, _ := ans["response"].(cli.Msg)
	if inner["session_id"] != "agy-sess-1" {
		t.Fatalf("init answer: %+v", ans)
	}
	if cmds, _ := inner["commands"].([]any); len(cmds) != 2 {
		t.Fatalf("commands: %+v", inner["commands"])
	}

	if err := sess.SendUser([]cli.Msg{
		{"type": "image", "source": cli.Msg{"type": "base64", "media_type": "image/png", "data": "AAAA"}},
		{"type": "text", "text": "hello"},
	}, "u1"); err != nil {
		t.Fatal(err)
	}
	var gotInit, gotAssistant, gotResult cli.Msg
	var states []string
	for msg := range sess.Out() {
		switch msg["type"] {
		case "command_lifecycle":
			states = append(states, fmt.Sprint(msg["state"]))
		case "system":
			if msg["subtype"] == "init" {
				gotInit = msg
			}
		case "assistant":
			gotAssistant = msg
		case "result":
			gotResult = msg
			sess.Close()
		}
	}
	if !slices.Equal(states, []string{"started", "completed"}) {
		t.Fatalf("lifecycle: %v", states)
	}
	if gotInit["session_id"] != "agy-sess-1" || gotInit["model"] != "gemini-3.8-flash" {
		t.Fatalf("init msg: %+v", gotInit)
	}
	wantText := "model=gemini-3.8-flash effort=medium sys=SYS_BLOCK mcp=1 img=true echo=hello"
	if gotResult["result"] != wantText {
		t.Fatalf("result: %q want %q", gotResult["result"], wantText)
	}
	msgInner, _ := gotAssistant["message"].(cli.Msg)
	blocks, _ := msgInner["content"].([]any)
	if len(blocks) != 1 || blocks[0].(cli.Msg)["text"] != wantText {
		t.Fatalf("chunked text should buffer into one assistant msg: %+v", gotAssistant)
	}
}

func TestACPPermissionsGateAndAllowedMCP(t *testing.T) {
	l := fakeLaunch(t)
	l.AllowedTools = []string{"mcp__fyr__*"}
	var asked []string
	var gated []string
	sess, err := Adapter{}.Start(l, cli.Handlers{
		Hook: func(id string, in cli.Msg) cli.Msg {
			if id == "gate" {
				ti, _ := in["tool_input"].(map[string]any)
				cmd, _ := ti["command"].(string)
				gated = append(gated, cmd)
				if strings.Contains(cmd, "gh pr merge") {
					return cli.Msg{"hookSpecificOutput": cli.Msg{"permissionDecision": "deny", "permissionDecisionReason": "Fyr owns merges"}}
				}
			}
			return nil
		},
		CanUseTool: func(req cli.Msg) cli.Msg {
			asked = append(asked, fmt.Sprint(req["tool_name"]))
			return cli.Msg{"behavior": "allow"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sess.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}

	readResult := func() cli.Msg {
		for msg := range sess.Out() {
			if msg["type"] == "result" {
				return msg
			}
		}
		t.Fatal("channel closed before result")
		return nil
	}

	// 1. Gated Bash command denied by PreToolUse hook without asking CanUseTool.
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "tool:gh pr merge 1"}}, "u1"); err != nil {
		t.Fatal(err)
	}
	if r := readResult(); r["result"] != "done:deny" {
		t.Fatalf("expected gate deny, got %+v", r)
	}
	if len(asked) != 0 {
		t.Fatalf("gate should have denied before CanUseTool: %v", asked)
	}

	// 2. Normal Bash command goes through CanUseTool as "Bash".
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "tool:echo hi"}}, "u2"); err != nil {
		t.Fatal(err)
	}
	if r := readResult(); r["result"] != "done:allow" {
		t.Fatalf("expected allow, got %+v", r)
	}
	if !slices.Equal(asked, []string{"Bash"}) {
		t.Fatalf("asked: %v", asked)
	}

	// 3. Fyr MCP tool matches AllowedTools and is auto-allowed without asking.
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "fyr-mcp"}}, "u3"); err != nil {
		t.Fatal(err)
	}
	if r := readResult(); r["result"] != "mcp:allow" {
		t.Fatalf("expected auto-allow for fyr mcp tool, got %+v", r)
	}
	if len(asked) != 1 {
		t.Fatalf("fyr mcp tool should not have asked owner: %v", asked)
	}
}

func TestACPQueueCancelAndInterrupt(t *testing.T) {
	l := fakeLaunch(t)
	sess, err := Adapter{}.Start(l, cli.Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sess.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Start a slow turn, queue a second message, cancel it, then interrupt the slow turn.
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "slow"}}, "m1"); err != nil {
		t.Fatal(err)
	}
	// Wait until m1 starts.
	for msg := range sess.Out() {
		if msg["type"] == "command_lifecycle" && msg["command_uuid"] == "m1" && msg["state"] == "started" {
			break
		}
	}
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "second"}}, "m2"); err != nil {
		t.Fatal(err)
	}
	ok, err := sess.CancelMessage(ctx, "m2")
	if err != nil || !ok {
		t.Fatalf("cancel m2: %v %v", ok, err)
	}
	if err := sess.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	var gotCancelled, gotInterrupted bool
	for msg := range sess.Out() {
		if msg["type"] == "command_lifecycle" && msg["command_uuid"] == "m2" && msg["state"] == "cancelled" {
			gotCancelled = true
		}
		if msg["type"] == "result" && msg["subtype"] == "interrupted" {
			gotInterrupted = true
			break
		}
	}
	if !gotCancelled || !gotInterrupted {
		t.Fatalf("cancelled=%v interrupted=%v", gotCancelled, gotInterrupted)
	}
}

// V0.agy-interrupt-before-prompt: a turn is announced "started" only once its
// prompt is on the agent's pipe. Announced first, an Interrupt could reach the
// agent before the prompt it cancels, and a real agent cancels no turn it has
// not received: both sides waited forever (CI hit it once in 10 minutes, and
// `go test -count=60` under load hangs the same way).
func TestACPStartedFollowsThePrompt(t *testing.T) {
	l := fakeLaunch(t)
	sess, err := Adapter{}.Start(l, cli.Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := sess.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		uuid := fmt.Sprintf("r%d", i)
		if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "slow"}}, uuid); err != nil {
			t.Fatal(err)
		}
		// Interrupt the moment the turn is announced, as Fyr's Stop does. The
		// prompt must already be out: a "started" sent first leaves the cancel
		// to cross the prompt, the fake is still reading its own stdin for the
		// cancel that round, and the wait below never returns.
		for msg := range sess.Out() {
			if msg["type"] == "command_lifecycle" && msg["command_uuid"] == uuid && msg["state"] == "started" {
				break
			}
		}
		if err := sess.Interrupt(ctx); err != nil {
			t.Fatal(err)
		}
		// The fake's "slow" turn lasts until it sees session/cancel.
		var gotResult bool
		for msg := range sess.Out() {
			if msg["type"] == "result" {
				gotResult = true
				break
			}
		}
		if !gotResult {
			t.Fatalf("turn %d ended with no result", i)
		}
	}
}

func TestPrintAndListModels(t *testing.T) {
	l := fakeLaunch(t)
	l.Model = "gemini-3.8-flash"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := Adapter{}.Print(ctx, l, "TITLE_SYS", "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sys=TITLE_SYS") || !strings.Contains(out, "echo=hello world") || !strings.Contains(out, "model=gemini-3.8-flash") {
		t.Fatalf("print: %q", out)
	}
	lst, err := Adapter{}.ListModels(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(lst.Models) != 3 {
		t.Fatalf("models: %+v", lst.Models)
	}
	if lst.Models[0].ID != "gemini-4-neon" || !slices.Equal(lst.Models[0].Efforts, []string{"low", "high", "xhigh"}) {
		t.Fatalf("first model: %+v", lst.Models[0])
	}
	if lst.Models[1].ID != "gemini-3.8-flash" || !slices.Equal(lst.Models[1].Efforts, []string{"low", "medium"}) {
		t.Fatalf("second model: %+v", lst.Models[1])
	}
	if lst.Models[2].ID != "gpt-oss-120b-medium" || len(lst.Models[2].Efforts) != 0 {
		t.Fatalf("third model: %+v", lst.Models[2])
	}
}

func TestSessionsLoggedInAndCopy(t *testing.T) {
	home := t.TempDir()
	g1 := filepath.Join(home, ".gemini-a")
	g2 := filepath.Join(home, ".gemini-b")
	a1 := cli.Auth{Kind: cli.CLILogin, ConfigDir: g1}
	a2 := cli.Auth{Kind: cli.CLILogin, ConfigDir: g2}
	if LoggedIn(a1) {
		t.Fatal("empty dir should not be logged in")
	}
	d1 := SessionsDir(a1)
	d2 := SessionsDir(a2)
	os.MkdirAll(filepath.Join(d1, "cache"), 0o700)
	os.WriteFile(filepath.Join(d1, "cache", "onboarding.json"), []byte(`{"onboardingComplete":true}`), 0o600)
	if !LoggedIn(a1) {
		t.Fatal("onboardingComplete should report logged in")
	}
	os.MkdirAll(filepath.Join(d1, "conversations"), 0o700)
	os.MkdirAll(filepath.Join(d1, "brain", "s1", "logs"), 0o700)
	db := filepath.Join(d1, "conversations", "s1.db")
	os.WriteFile(db, []byte("sqlite-bytes"), 0o600)
	os.WriteFile(db+"-wal", []byte("wal-bytes"), 0o600)
	os.WriteFile(filepath.Join(d1, "brain", "s1", "logs", "transcript.jsonl"), []byte("{}\n"), 0o600)

	found := SessionFile(d1, "/work", "s1")
	if found != db {
		t.Fatalf("session file: %q", found)
	}
	if SessionFile(d1, "/work", "../escape") != "" {
		t.Fatal("path traversal must return empty")
	}
	if err := CopySession(found, d1, d2, "s1"); err != nil {
		t.Fatal(err)
	}
	if SessionFile(d2, "/work", "s1") == "" {
		t.Fatal("copied session db missing")
	}
	if _, err := os.Stat(filepath.Join(d2, "conversations", "s1.db-wal")); err != nil {
		t.Fatal("copied wal missing")
	}
	if _, err := os.Stat(filepath.Join(d2, "brain", "s1", "logs", "transcript.jsonl")); err != nil {
		t.Fatal("copied brain tree missing")
	}
}

func TestParseLoginAndTools(t *testing.T) {
	url := "https://accounts.google.com/o/oauth2/auth?client_id=abc&state=xyz"
	out := "Open this link in a browser to log in with Google:\n\n  " + url + "\n\nThen paste the authorization code, or the full URL of the page you land on, and press Enter:\n"
	o := ParseLogin([]byte(out))
	if o.URL != url || !o.Prompt || o.Line != "" {
		t.Fatalf("parse login: %+v", o)
	}
	o2 := ParseLogin([]byte("login cancelled: no authorization code was entered\n"))
	if !strings.Contains(o2.Line, "login cancelled") {
		t.Fatalf("parse login err: %+v", o2)
	}
	args, term := LoginArgs("/cfg/.gemini")
	if term || strings.Join(args, " ") != "acp --login --gemini-dir /cfg/.gemini" {
		t.Fatalf("login args: %v term=%v", args, term)
	}
	name, in := translateToolCall("call_mcp_tool", map[string]any{"ServerName": "fyr", "ToolName": "change_open", "Arguments": map[string]any{"title": "x"}}, []string{"fyr"})
	if name != "mcp__fyr__change_open" || in["title"] != "x" {
		t.Fatalf("call_mcp_tool: %q %+v", name, in)
	}
	name, _ = translateToolCall("mcp_fyr_settle_request", map[string]any{"why": "done"}, []string{"fyr"})
	if name != "mcp__fyr__settle_request" {
		t.Fatalf("eager mcp: %q", name)
	}
	name, in = translateToolCall("run_command", map[string]any{"CommandLine": "go test ./..."}, nil)
	if name != "Bash" || in["command"] != "go test ./..." {
		t.Fatalf("run_command: %q %+v", name, in)
	}
	name, in = translateToolCall("ask_question", map[string]any{
		"questions": []any{map[string]any{"question": "Pick one?", "options": []any{"A", "B"}, "is_multi_select": true}},
	}, nil)
	if name != "AskUserQuestion" {
		t.Fatalf("ask_question: %q %+v", name, in)
	}
	qs, _ := in["questions"].([]any)
	q0, _ := qs[0].(map[string]any)
	opts, _ := q0["options"].([]any)
	if q0["multiSelect"] != true || len(opts) != 2 || opts[0].(map[string]any)["label"] != "A" {
		t.Fatalf("ask_question shape: %+v", in)
	}
}

type fakePipe struct {
	mu    sync.Mutex
	lines []struct {
		seq int64
		b   []byte
	}
	idx     int
	ackedTo int64
}

func (p *fakePipe) WriteLine([]byte) error { return nil }
func (p *fakePipe) ReadLine() ([]byte, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.idx >= len(p.lines) {
		return nil, 0, io.EOF
	}
	item := p.lines[p.idx]
	p.idx++
	return item.b, item.seq, nil
}
func (p *fakePipe) Ack(upTo int64, _ []int64) {
	p.mu.Lock()
	p.ackedTo = upTo
	p.mu.Unlock()
}
func (p *fakePipe) CloseInput() error { return nil }
func (p *fakePipe) Kill()             {}
func (p *fakePipe) Wait() error       { return nil }
func (p *fakePipe) Pid() int          { return 42 }

// V0.agy-adapter: a reattached ACP process finishing an in-flight pre-restart
// turn must flush buffered text and emit a result carrying its runner seq.
func TestACPReattachFinishesMidTurn(t *testing.T) {
	p := &fakePipe{
		lines: []struct {
			seq int64
			b   []byte
		}{
			{10, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"part1 "}}}}`)},
			{11, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"part2"}}}}`)},
			{12, []byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`)},
		},
	}
	sess, err := Adapter{}.ReattachSession(p, cli.ReattachState{
		SessionID: "s1",
		Model:     "gemini-4-neon",
		Running:   true,
	}, nil, cli.Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	var gotAssistant, gotResult cli.Msg
	for msg := range sess.Out() {
		switch msg["type"] {
		case "assistant":
			gotAssistant = msg
		case "result":
			gotResult = msg
		}
		if h, ok := sess.(interface{ Handled(cli.Msg) }); ok {
			h.Handled(msg)
		}
	}
	<-sess.Done()
	if gotAssistant == nil || gotResult == nil {
		t.Fatalf("expected assistant and result on reattach, got assistant=%v result=%v", gotAssistant, gotResult)
	}
	if gotResult["result"] != "part1 part2" || gotResult[claude.SeqKey] != int64(12) {
		t.Fatalf("unexpected reattached result: %+v", gotResult)
	}
	p.mu.Lock()
	acked := p.ackedTo
	p.mu.Unlock()
	if acked != 12 {
		t.Fatalf("runner ack=%d want 12", acked)
	}
}

// V0.agy-adapter: tool completion must not invoke the viewnudge hook and
// discard returned feed notices (turn.go handles toolDone via tool_result).
func TestACPDoesNotDropFeedNoticesOnToolResult(t *testing.T) {
	l := fakeLaunch(t)
	var hooks []string
	var mu sync.Mutex
	sess, err := Adapter{}.Start(l, cli.Handlers{
		Hook: func(id string, in cli.Msg) cli.Msg {
			mu.Lock()
			hooks = append(hooks, id)
			mu.Unlock()
			return nil
		},
		CanUseTool: func(cli.Msg) cli.Msg {
			return cli.Msg{"behavior": "allow"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sess.Initialize(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "tool:echo ok"}}, "u1"); err != nil {
		t.Fatal(err)
	}
	for msg := range sess.Out() {
		if msg["type"] == "result" {
			break
		}
	}
	mu.Lock()
	got := append([]string(nil), hooks...)
	mu.Unlock()
	if slices.Contains(got, "viewnudge") {
		t.Fatalf("acpSession must not call viewnudge hook and drop notices: %v", got)
	}
}

type pipeLine struct {
	seq int64
	b   []byte
}

// chanPipe is an interactive runner.Pipe for driving ACP streams step by step
// in unit tests without spawning a subprocess. push/closeRead are the common
// way in; line carries a runner seq, which the reattach tests need.
type chanPipe struct {
	in      chan pipeLine
	written chan []byte
	writeCh chan []byte
	mu      sync.Mutex
	kept    [][]byte
	ackedTo int64
	closed  bool
}

func newChanPipe() *chanPipe {
	return &chanPipe{
		in:      make(chan pipeLine, 64),
		written: make(chan []byte, 64),
		writeCh: make(chan []byte, 64),
	}
}

func (p *chanPipe) push(line string) {
	p.in <- pipeLine{b: []byte(line)}
}

func (p *chanPipe) line(seq int64, line string) {
	p.in <- pipeLine{seq: seq, b: []byte(line)}
}

func (p *chanPipe) WriteLine(b []byte) error {
	cp := append([]byte(nil), b...)
	p.written <- cp
	p.mu.Lock()
	p.kept = append(p.kept, cp)
	p.mu.Unlock()
	select {
	case p.writeCh <- cp:
	default:
	}
	return nil
}

func (p *chanPipe) ReadLine() ([]byte, int64, error) {
	item, ok := <-p.in
	if !ok {
		return nil, 0, io.EOF
	}
	return item.b, item.seq, nil
}

func (p *chanPipe) Ack(upTo int64, _ []int64) {
	p.mu.Lock()
	p.ackedTo = upTo
	p.mu.Unlock()
}

func (p *chanPipe) acked() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ackedTo
}

func (p *chanPipe) closeRead() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.in)
	}
	p.mu.Unlock()
}

func (p *chanPipe) CloseInput() error { p.closeRead(); return nil }
func (p *chanPipe) Kill()             { p.closeRead() }
func (p *chanPipe) Wait() error       { return nil }
func (p *chanPipe) Pid() int          { return 42 }

// V0.agy-reattach-queue: reattaching mid-turn restores queued messages, holds
// them until the in-flight pre-restart turn ends, allows cancelling them before
// they run, queues new messages behind them, and holds prompt response runner
// seqs until Fyr handles the result.
func TestACPReattachRestoresQueueAndAllowsCancel(t *testing.T) {
	p := newChanPipe()
	sess, err := Adapter{}.ReattachSession(p, cli.ReattachState{
		SessionID: "s1",
		Model:     "gemini-4-neon",
		Running:   true,
		Queued: []cli.QueuedMessage{
			{ID: "q1", Content: []cli.Msg{{"type": "text", "text": "first queued"}}},
			{ID: "q2", Content: []cli.Msg{{"type": "text", "text": "second queued"}}},
		},
	}, nil, cli.Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	handled := sess.(interface{ Handled(cli.Msg) })
	readMsg := func() cli.Msg {
		t.Helper()
		select {
		case m, ok := <-sess.Out():
			if !ok {
				t.Fatal("session output closed unexpectedly")
			}
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for session output")
			return nil
		}
	}
	readWritten := func() cli.Msg {
		t.Helper()
		select {
		case b := <-p.written:
			var m cli.Msg
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("unmarshal written: %v", err)
			}
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for session/prompt write")
			return nil
		}
	}

	// A new message sent while the pre-restart turn is still active must queue
	// behind the restored messages rather than sending a concurrent prompt.
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "third queued"}}, "q3"); err != nil {
		t.Fatal(err)
	}
	if m := readMsg(); m["type"] != "command_lifecycle" || m["command_uuid"] != "q3" || m["state"] != "queued" {
		t.Fatalf("expected q3 queued, got %+v", m)
	}

	// Cancelling a restored pre-restart queued message must succeed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, err := sess.CancelMessage(ctx, "q1")
	if err != nil || !ok {
		t.Fatalf("CancelMessage(q1) = (%v, %v), want (true, nil)", ok, err)
	}
	if m := readMsg(); m["type"] != "command_lifecycle" || m["command_uuid"] != "q1" || m["state"] != "cancelled" {
		t.Fatalf("expected q1 cancelled, got %+v", m)
	}

	// Nothing may be written to the agent pipe before the pre-restart turn ends.
	select {
	case b := <-p.written:
		t.Fatalf("unexpected write while reattached turn still running: %s", b)
	default:
	}

	// Finish the pre-restart turn (seq 12).
	p.in <- pipeLine{11, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pre-restart done"}}}}`)}
	p.in <- pipeLine{12, []byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`)}

	if m := readMsg(); m["type"] != "assistant" {
		t.Fatalf("expected pre-restart assistant text, got %+v", m)
	}
	res1 := readMsg()
	if res1["type"] != "result" || res1["result"] != "pre-restart done" || res1[claude.SeqKey] != int64(12) {
		t.Fatalf("unexpected pre-restart result: %+v", res1)
	}
	handled.Handled(res1)

	// Now q2 ("second queued") must run next (q1 was cancelled).
	req2 := readWritten()
	if req2["method"] != "session/prompt" {
		t.Fatalf("expected session/prompt for q2, got %+v", req2)
	}
	rawParams2, _ := json.Marshal(req2["params"])
	if !strings.Contains(string(rawParams2), "second queued") {
		t.Fatalf("expected q2 prompt text, got %s", rawParams2)
	}
	if m := readMsg(); m["type"] != "command_lifecycle" || m["command_uuid"] != "q2" || m["state"] != "started" {
		t.Fatalf("expected q2 started, got %+v", m)
	}

	// Answer q2 at seq 13; runner ack must wait until Fyr handles the result.
	id2 := int64(req2["id"].(float64))
	p.in <- pipeLine{13, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn"}}`, id2))}
	if m := readMsg(); m["type"] != "command_lifecycle" || m["command_uuid"] != "q2" || m["state"] != "completed" {
		t.Fatalf("expected q2 completed, got %+v", m)
	}
	res2 := readMsg()
	if res2["type"] != "result" || res2[claude.SeqKey] != int64(13) {
		t.Fatalf("expected q2 result carrying seq 13, got %+v", res2)
	}
	if got := p.acked(); got >= 13 {
		t.Fatalf("runner acked %d before q2 result was handled", got)
	}
	handled.Handled(res2)
	if got := p.acked(); got != 13 {
		t.Fatalf("runner ack=%d after handling q2 result, want 13", got)
	}

	// Finally q3 ("third queued") must run.
	req3 := readWritten()
	rawParams3, _ := json.Marshal(req3["params"])
	if req3["method"] != "session/prompt" || !strings.Contains(string(rawParams3), "third queued") {
		t.Fatalf("expected session/prompt for q3, got %+v", req3)
	}
	if m := readMsg(); m["type"] != "command_lifecycle" || m["command_uuid"] != "q3" || m["state"] != "started" {
		t.Fatalf("expected q3 started, got %+v", m)
	}
	id3 := int64(req3["id"].(float64))
	p.in <- pipeLine{14, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn"}}`, id3))}
	if m := readMsg(); m["type"] != "command_lifecycle" || m["command_uuid"] != "q3" || m["state"] != "completed" {
		t.Fatalf("expected q3 completed, got %+v", m)
	}
	res3 := readMsg()
	if res3["type"] != "result" || res3[claude.SeqKey] != int64(14) {
		t.Fatalf("expected q3 result carrying seq 14, got %+v", res3)
	}
	handled.Handled(res3)
	if got := p.acked(); got != 14 {
		t.Fatalf("runner ack=%d after handling q3 result, want 14", got)
	}
}

func nextOut(t *testing.T, out <-chan cli.Msg, timeout time.Duration) cli.Msg {
	t.Helper()
	select {
	case m, ok := <-out:
		if !ok {
			t.Fatal("Out closed unexpectedly")
		}
		return m
	case <-time.After(timeout):
		t.Fatal("timed out waiting for message on Out()")
		return nil
	}
}

func nextWritten(t *testing.T, p *chanPipe, timeout time.Duration) cli.Msg {
	t.Helper()
	select {
	case b := <-p.writeCh:
		var m cli.Msg
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal written line: %v", err)
		}
		return m
	case <-time.After(timeout):
		t.Fatal("timed out waiting for written JSON-RPC message")
		return nil
	}
}

// V0.agy-reactive-wakeup: a turn ending with an open tool promotes it to a
// background task; when the tool later finishes and agy runs a reactive-wakeup
// turn without a session/prompt RPC, acpSession wakes immediately on
// agent_thought_chunk, settles after usage_update, flushes the final text, and
// emits result so Fyr never stays stuck in Working.
func TestACPReactiveWakeupSettlesAndTracksBackgroundTool(t *testing.T) {
	p := newChanPipe()
	sess := openACP(p, acpSpec{
		SessionID:   "s-bg",
		Model:       "gemini-4-neon",
		MCPServers:  []cli.Msg{{"name": "fyr"}},
		SettleDelay: 40 * time.Millisecond,
	}, cli.Handlers{}, false)
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()

	// Turn 1: prompt starts a command that stays open when stopReason arrives.
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "run tests"}}, "u1"); err != nil {
		t.Fatal(err)
	}
	req1 := nextWritten(t, p, 2*time.Second)
	if req1["method"] != "session/prompt" {
		t.Fatalf("expected session/prompt, got %+v", req1)
	}
	reqID := req1["id"]

	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"tool_call","toolCallId":"call_bg","name":"run_command","args":{"CommandLine":"go test ./...","toolSummary":"Run full test suite"},"status":"in_progress"}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Waiting for tests to finish."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"usage_update","used":1000,"size":1048576}}}`)
	p.push(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"stopReason":"end_turn"}}`, reqID))

	var turn1Msgs []cli.Msg
	for {
		m := nextOut(t, sess.Out(), 2*time.Second)
		turn1Msgs = append(turn1Msgs, m)
		if m["type"] == "result" {
			break
		}
	}
	var sawBGChanged, sawTaskStarted, sawWaitText bool
	for _, m := range turn1Msgs {
		if m["type"] == "system" && m["subtype"] == "background_tasks_changed" {
			if tasks, _ := m["tasks"].([]any); len(tasks) == 1 {
				sawBGChanged = true
			}
		}
		if m["type"] == "system" && m["subtype"] == "task_started" && m["task_id"] == "call_bg" && m["is_backgrounded"] == true {
			sawTaskStarted = true
		}
		if m["type"] == "assistant" {
			inner, _ := m["message"].(cli.Msg)
			if blocks, _ := inner["content"].([]any); len(blocks) == 1 {
				if b, _ := blocks[0].(cli.Msg); b["type"] == "text" && b["text"] == "Waiting for tests to finish." {
					sawWaitText = true
				}
			}
		}
	}
	if !sawBGChanged || !sawTaskStarted || !sawWaitText {
		t.Fatalf("turn 1 missing bg tracking or text: bg=%v task=%v text=%v msgs=%+v", sawBGChanged, sawTaskStarted, sawWaitText, turn1Msgs)
	}

	// Reactive wakeup: background command completes, agent thinks, renames thread,
	// writes final reply, emits usage_update, and stops (NO JSON-RPC response!).
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_bg","name":"run_command","status":"completed","rawOutput":{"output":"ok all tests"}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"All tests passed."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"usage_update","used":1200,"size":1048576}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"tool_call","toolCallId":"call_ren","name":"mcp_fyr_thread_rename","args":{"title":"Fixed tests"},"status":"in_progress"}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_ren","name":"mcp_fyr_thread_rename","status":"completed"}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"All tests passed and PR is ready."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-bg","update":{"sessionUpdate":"usage_update","used":1400,"size":1048576}}}`)

	var wakeupMsgs []cli.Msg
	for {
		m := nextOut(t, sess.Out(), 2*time.Second)
		wakeupMsgs = append(wakeupMsgs, m)
		if m["type"] == "result" {
			break
		}
	}
	var sawBGResult, sawDupBGUse, sawTaskNotif, sawBGEmpty, sawThinking, sawFinalText, sawResult bool
	for _, m := range wakeupMsgs {
		switch m["type"] {
		case "user":
			inner, _ := m["message"].(cli.Msg)
			if blocks, _ := inner["content"].([]any); len(blocks) == 1 {
				if b, _ := blocks[0].(cli.Msg); b["type"] == "tool_result" && b["tool_use_id"] == "call_bg" {
					sawBGResult = true
				}
			}
		case "assistant":
			inner, _ := m["message"].(cli.Msg)
			if blocks, _ := inner["content"].([]any); len(blocks) == 1 {
				b, _ := blocks[0].(cli.Msg)
				if b["type"] == "tool_use" && b["id"] == "call_bg" {
					sawDupBGUse = true
				}
				if b["type"] == "thinking" {
					sawThinking = true
				}
				if b["type"] == "text" && b["text"] == "All tests passed and PR is ready." {
					sawFinalText = true
				}
			}
		case "system":
			if m["subtype"] == "task_notification" && m["task_id"] == "call_bg" && m["status"] == "completed" {
				sawTaskNotif = true
			}
			if m["subtype"] == "background_tasks_changed" {
				if tasks, _ := m["tasks"].([]any); len(tasks) == 0 {
					sawBGEmpty = true
				}
			}
		case "result":
			if m["subtype"] == "success" && m["result"] == "All tests passed and PR is ready." {
				sawResult = true
			}
		}
	}
	if sawDupBGUse {
		t.Fatalf("duplicate tool_use emitted for completed background tool: %+v", wakeupMsgs)
	}
	if !sawBGResult || !sawTaskNotif || !sawBGEmpty || !sawThinking || !sawFinalText || !sawResult {
		t.Fatalf("reactive wakeup missing events: bgRes=%v notif=%v bgEmpty=%v think=%v text=%v res=%v msgs=%+v",
			sawBGResult, sawTaskNotif, sawBGEmpty, sawThinking, sawFinalText, sawResult, wakeupMsgs)
	}
}

// V0.agy-reactive-wakeup: a usage_update without assistant text (context
// compaction) must not settle an unsolicited turn, and a tool_call arriving
// after text+usage_update cancels the settle timer and keeps the turn open.
func TestACPReactiveWakeupCompactionAndMidTurnToolDoNotPrematurelySettle(t *testing.T) {
	p := newChanPipe()
	sess := openACP(p, acpSpec{
		SessionID:   "s-compact",
		SettleDelay: 40 * time.Millisecond,
	}, cli.Handlers{}, false)
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()

	// Start unsolicited turn with thought + usage_update (no text) like compaction.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Compacting..."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"usage_update","used":500000,"size":1048576}}}`)

	// Drain the initial thinking message.
	m := nextOut(t, sess.Out(), time.Second)
	if m["type"] != "assistant" {
		t.Fatalf("expected thinking assistant message, got %+v", m)
	}

	// Wait longer than 2x SettleDelay: must NOT emit a premature result.
	select {
	case unexpected := <-sess.Out():
		t.Fatalf("unexpected message during compaction pause: %+v", unexpected)
	case <-time.After(110 * time.Millisecond):
	}

	// Now emit interim text + usage_update, immediately followed by a tool_call.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Checking file."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"usage_update","used":28000,"size":1048576}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"tool_call","toolCallId":"c1","name":"read_file","args":{"Path":"/x"},"status":"in_progress"}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","name":"read_file","status":"completed"}}}`)

	// Drain interim text, tool_use, tool_result — none should be result yet.
	for i := 0; i < 3; i++ {
		got := nextOut(t, sess.Out(), time.Second)
		if got["type"] == "result" {
			t.Fatalf("premature result before final reply: %+v", got)
		}
	}

	// Final reply + usage_update settles cleanly.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-compact","update":{"sessionUpdate":"usage_update","used":29000,"size":1048576}}}`)

	gotText := nextOut(t, sess.Out(), time.Second)
	gotRes := nextOut(t, sess.Out(), time.Second)
	if gotText["type"] != "assistant" || gotRes["type"] != "result" || gotRes["subtype"] != "success" {
		t.Fatalf("expected final assistant+result, got %+v and %+v", gotText, gotRes)
	}
}

// V0.agy-reactive-wakeup: a user message sent while a reactive-wakeup turn is
// active is queued, the reactive turn's reply is flushed intact (never wiped),
// and the queued prompt starts immediately afterward.
func TestACPReactiveWakeupQueuesPromptAndPreservesReply(t *testing.T) {
	p := newChanPipe()
	sess := openACP(p, acpSpec{
		SessionID:   "s-q",
		SettleDelay: 40 * time.Millisecond,
	}, cli.Handlers{}, false)
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()

	// Reactive wakeup starts a tool.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-q","update":{"sessionUpdate":"tool_call","toolCallId":"c1","name":"read_file","args":{"Path":"/a"},"status":"in_progress"}}}`)
	_ = nextOut(t, sess.Out(), time.Second) // tool_use

	// User sends a follow-up while reactive wakeup is running.
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": "next question"}}, "u2"); err != nil {
		t.Fatal(err)
	}
	queuedMsg := nextOut(t, sess.Out(), time.Second)
	if queuedMsg["type"] != "command_lifecycle" || queuedMsg["command_uuid"] != "u2" || queuedMsg["state"] != "queued" {
		t.Fatalf("expected queued lifecycle, got %+v", queuedMsg)
	}

	// Reactive wakeup finishes tool and emits final reply + usage_update.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-q","update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","name":"read_file","status":"completed"}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-q","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Reactive reply preserved."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-q","update":{"sessionUpdate":"usage_update","used":2000,"size":1048576}}}`)

	// Expect tool_result, assistant("Reactive reply preserved."), result, then started for u2.
	var sawPreserved, sawWakeResult, sawStarted bool
	for !sawStarted {
		m := nextOut(t, sess.Out(), 2*time.Second)
		if m["type"] == "assistant" {
			inner, _ := m["message"].(cli.Msg)
			if blocks, _ := inner["content"].([]any); len(blocks) == 1 {
				if b, _ := blocks[0].(cli.Msg); b["type"] == "text" && b["text"] == "Reactive reply preserved." {
					sawPreserved = true
				}
			}
		}
		if m["type"] == "result" && m["result"] == "Reactive reply preserved." {
			sawWakeResult = true
		}
		if m["type"] == "command_lifecycle" && m["command_uuid"] == "u2" && m["state"] == "started" {
			if !sawPreserved || !sawWakeResult {
				t.Fatalf("queued prompt started before reactive reply flushed and ended")
			}
			sawStarted = true
		}
	}

	req := nextWritten(t, p, 2*time.Second)
	if req["method"] != "session/prompt" {
		t.Fatalf("expected queued session/prompt, got %+v", req)
	}
	p.push(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"stopReason":"end_turn"}}`, req["id"]))
	for {
		m := nextOut(t, sess.Out(), 2*time.Second)
		if m["type"] == "result" {
			break
		}
	}
}

// V0.agy-open-tool-settle: a reactive turn whose model wrote a note and then
// started a command is still running: its usage_update must not settle the
// turn while the tool is open, and the tool's own completion is what arms the
// settle. Before this, a 1.5 s settle over a five-minute command ended the
// turn mid-flight and promoted the command to a background task.
func TestACPUnsolicitedTurnWaitsForItsOpenTool(t *testing.T) {
	p := newChanPipe()
	sess := openACP(p, acpSpec{
		SessionID:   "s-open",
		SettleDelay: 40 * time.Millisecond,
	}, cli.Handlers{}, false)
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()

	// An unsolicited turn in the order the live sessions emit it: the model
	// starts a command, writes a note about waiting for it, and its
	// usage_update follows — all while the command is still open.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-open","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Checking the tests."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-open","update":{"sessionUpdate":"tool_call","toolCallId":"call_long","name":"run_command","args":{"CommandLine":"go test ./..."},"status":"in_progress"}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-open","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Running the full suite now."}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-open","update":{"sessionUpdate":"usage_update","used":1000,"size":1048576}}}`)

	// Drain what the turn emitted, then prove nothing settled it.
	sawToolUse := false
	for !sawToolUse {
		m := nextOut(t, sess.Out(), 2*time.Second)
		if m["type"] == "result" {
			t.Fatalf("the turn settled while its command was open: %+v", m)
		}
		if m["type"] == "assistant" {
			inner, _ := m["message"].(cli.Msg)
			if blocks, _ := inner["content"].([]any); len(blocks) == 1 {
				if b, _ := blocks[0].(cli.Msg); b["type"] == "tool_use" && b["id"] == "call_long" {
					sawToolUse = true
				}
			}
		}
	}
	// Well past the settle window: still nothing.
	select {
	case m := <-sess.Out():
		if m["type"] == "result" {
			t.Fatalf("the turn settled while its command was open: %+v", m)
		}
	case <-time.After(6 * sess.settleDelay):
	}

	// The command ends; the model answers and the turn settles on its own.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-open","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_long","name":"run_command","status":"completed","rawOutput":{"output":"ok all tests"}}}}`)
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-open","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"All green."}}}}`)
	var sawResult bool
	for {
		m := nextOut(t, sess.Out(), 2*time.Second)
		if m["type"] == "background_tasks_changed" || (m["type"] == "system" && m["subtype"] == "task_notification") {
			t.Fatalf("a finished command became background work: %+v", m)
		}
		if m["type"] == "result" {
			if m["result"] != "Running the full suite now.\nAll green." && m["result"] != "Running the full suite now.All green." {
				t.Fatalf("result text %q", m["result"])
			}
			sawResult = true
			break
		}
	}
	if !sawResult {
		t.Fatal("no result after the command ended")
	}
}

// V0.agy-reactive-wakeup: Interrupt during an unsolicited turn sends
// session/cancel and settles as interrupted without waiting forever for a
// JSON-RPC stopReason response; an unsolicited message without usage_update
// (such as a quota notice) settles via the fallback window.
func TestACPReactiveWakeupInterruptAndFallbackSettle(t *testing.T) {
	p := newChanPipe()
	sess := openACP(p, acpSpec{
		SessionID:   "s-int",
		SettleDelay: 30 * time.Millisecond,
	}, cli.Handlers{}, false)
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()

	// Case 1: Interrupt an active unsolicited turn.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-int","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Working..."}}}}`)
	_ = nextOut(t, sess.Out(), time.Second) // thinking
	if err := sess.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancelMsg := nextWritten(t, p, time.Second)
	if cancelMsg["method"] != "session/cancel" {
		t.Fatalf("expected session/cancel, got %+v", cancelMsg)
	}
	res := nextOut(t, sess.Out(), time.Second)
	if res["type"] != "result" || res["subtype"] != "interrupted" {
		t.Fatalf("expected interrupted result, got %+v", res)
	}

	// Case 2: Unsolicited agent_message_chunk with NO usage_update settles via fallback.
	p.push(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-int","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Quota reached."}}}}`)
	txt := nextOut(t, sess.Out(), time.Second)
	res2 := nextOut(t, sess.Out(), time.Second)
	if txt["type"] != "assistant" || res2["type"] != "result" || res2["result"] != "Quota reached." {
		t.Fatalf("expected fallback assistant+result, got %+v and %+v", txt, res2)
	}
}
