// Package fake is a scripted stand-in for the `claude` CLI that speaks the
// same stream-json control protocol. Test binaries run it when the
// environment has FYR_FAKE_CLAUDE=1, so tests drive the real client code
// against a real child process.
//
// The first word of each user message picks the behaviour:
//
//	ask      asks one AskUserQuestion and reports the answer
//	approve  asks for Bash permission and reports allow or deny
//	fail     ends the run with an API error
//	hang     waits until interrupted
//	pr       runs `gh pr create` and prints a PR URL
//	crash    exits with code 3 during the run
//	agentbg  starts a background agent, ends its turn, and reports the
//	         agent's progress and completion with the CLI's task messages
//	bg       ends its turn, then starts a second turn on its own (as the CLI
//	         does when a background task finishes) with no user message
//	sub      runs one subagent (Task tool) before it answers
//	merge U  calls mcp__fyr__effect_request {action pr.merge, args {pr: U}}
//	         over HTTP from --mcp-config and reports the tool's text
//	gate     asks the PreToolUse hook about "gh pr merge 1" and reports it
//	mcpstatus reports whether the MCP server answered at startup, as the
//	         real CLI connects its MCP servers before the first turn
//	other    replies "echo: <text>"
package fake

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type msg = map[string]any

var (
	wmu        sync.Mutex
	pmu        sync.Mutex
	pending    = map[string]chan msg{}
	interrupt  = make(chan struct{}, 1)
	session    = "fake-session"
	mcpURL     string
	mcpAuth    string
	mcpAtStart = "none"
	model      = "fake-model"
)

func write(m msg) {
	b, _ := json.Marshal(m)
	wmu.Lock()
	os.Stdout.Write(append(b, '\n'))
	wmu.Unlock()
}

func request(id string, req msg) msg {
	ch := make(chan msg, 1)
	pmu.Lock()
	pending[id] = ch
	pmu.Unlock()
	write(msg{"type": "control_request", "request_id": id, "request": req})
	select {
	case r := <-ch:
		return r
	case <-interrupt:
		write(msg{"type": "control_cancel_request", "request_id": id})
		return nil
	}
}

func assistant(content ...msg) {
	write(msg{"type": "assistant", "session_id": session, "parent_tool_use_id": nil,
		"message": msg{"role": "assistant", "content": content}})
}

func result(isErr bool, subtype, text string) {
	write(msg{"type": "result", "subtype": subtype, "is_error": isErr, "result": text,
		"session_id": session, "num_turns": 1, "duration_ms": 5, "total_cost_usd": 0.001})
}

func Main() {
	for i, a := range os.Args {
		if (a == "--session-id" || a == "--resume") && i+1 < len(os.Args) {
			session = os.Args[i+1]
		}
		if a == "--mcp-config" && i+1 < len(os.Args) {
			var cfg struct {
				MCPServers map[string]struct {
					URL     string            `json:"url"`
					Headers map[string]string `json:"headers"`
				} `json:"mcpServers"`
			}
			json.Unmarshal([]byte(os.Args[i+1]), &cfg)
			mcpURL, mcpAuth = cfg.MCPServers["fyr"].URL, cfg.MCPServers["fyr"].Headers["Authorization"]
		}
		if a == "--model" && i+1 < len(os.Args) {
			model = os.Args[i+1]
		}
	}
	if mcpURL != "" {
		// The real CLI connects MCP servers while it starts, before initialize.
		if out := callMCPList(); out == "" {
			mcpAtStart = "ok"
		} else {
			mcpAtStart = out
		}
	}
	turns := make(chan string, 16)
	go func() {
		for text := range turns {
			turn(text)
		}
	}()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var m msg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m["type"] {
		case "control_request":
			req, _ := m["request"].(msg)
			id, _ := m["request_id"].(string)
			if req["subtype"] == "interrupt" {
				select {
				case interrupt <- struct{}{}:
				default:
				}
			}
			resp := msg{}
			if req["subtype"] == "initialize" {
				resp = msg{"commands": []any{}, "models": []any{msg{"value": "fake-model"}}}
			}
			write(msg{"type": "control_response", "response": msg{"subtype": "success", "request_id": id, "response": resp}})
		case "control_response":
			r, _ := m["response"].(msg)
			id, _ := r["request_id"].(string)
			pmu.Lock()
			ch := pending[id]
			delete(pending, id)
			pmu.Unlock()
			if ch != nil {
				inner, _ := r["response"].(msg)
				ch <- inner
			}
		case "user":
			mm, _ := m["message"].(msg)
			text := ""
			if cs, ok := mm["content"].([]any); ok {
				for _, c := range cs {
					if b, _ := c.(msg); b["type"] == "text" {
						text, _ = b["text"].(string)
					} else if b["type"] == "image" {
						text += " [image]"
					}
				}
			}
			turns <- text
		}
	}
}

func turn(text string) {
	// drain a stale interrupt from an earlier turn
	select {
	case <-interrupt:
	default:
	}
	write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model, "cwd": mustWd()})
	word := strings.Fields(text + " x")[0]
	switch word {
	case "ask":
		r := request("ask1", msg{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "tool_use_id": "tu_ask",
			"input": msg{"questions": []any{
				msg{"question": "Which colour?", "header": "Colour", "multiSelect": false,
					"options": []any{msg{"label": "Red", "description": "Warm"}, msg{"label": "Blue", "description": "Cold"}}},
			}}})
		if r == nil {
			result(true, "error_during_execution", "interrupted")
			return
		}
		in, _ := r["updatedInput"].(msg)
		ans, _ := in["answers"].(msg)
		assistant(msg{"type": "text", "text": fmt.Sprintf("You chose %v", ans["Which colour?"])})
		result(false, "success", "done")
	case "approve":
		r := request("perm1", msg{"subtype": "can_use_tool", "tool_name": "Bash", "tool_use_id": "tu_b",
			"input": msg{"command": "rm -rf build"}})
		if r == nil {
			result(true, "error_during_execution", "interrupted")
			return
		}
		assistant(msg{"type": "text", "text": fmt.Sprintf("Permission: %v", r["behavior"])})
		result(false, "success", "done")
	case "fail":
		assistant(msg{"type": "text", "text": "Trying"})
		result(true, "success", "API Error: 500 Internal server error")
	case "hang":
		assistant(msg{"type": "tool_use", "id": "tu_h", "name": "Bash", "input": msg{"command": "sleep 600"}})
		select {
		case <-interrupt:
			result(true, "error_during_execution", "")
		case <-time.After(30 * time.Second):
			result(false, "success", "woke")
		}
	case "pr":
		assistant(msg{"type": "tool_use", "id": "tu_pr", "name": "Bash", "input": msg{"command": "gh pr create --fill"}})
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_pr", "content": "https://github.com/acme/app/pull/42\n"}}}})
		assistant(msg{"type": "text", "text": "Opened the PR."})
		result(false, "success", "done")
	case "sub":
		assistant(msg{"type": "tool_use", "id": "tu_task", "name": "Task", "input": msg{"description": "look", "prompt": "look"}})
		write(msg{"type": "assistant", "session_id": session, "parent_tool_use_id": "tu_task",
			"message": msg{"role": "assistant", "content": []any{msg{"type": "text", "text": "subagent text"}}}})
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_task", "content": "found it"}}}})
		assistant(msg{"type": "text", "text": "The subagent found it."})
		result(false, "success", "done")
	case "merge":
		url := strings.TrimSpace(strings.TrimPrefix(text, "merge"))
		assistant(msg{"type": "tool_use", "id": "tu_m", "name": "mcp__fyr__effect_request",
			"input": msg{"action": "pr.merge", "args": msg{"pr": url, "method": "squash"}}})
		out := callMCP("effect_request", msg{"action": "pr.merge", "args": msg{"pr": url, "method": "squash"}, "reason": "asked"})
		assistant(msg{"type": "text", "text": "tool said: " + out})
		result(false, "success", "requested")
	case "gate":
		r := request("hook1", msg{"subtype": "hook_callback", "callback_id": "gate",
			"input": msg{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": msg{"command": "gh pr merge 1 --squash"}}})
		hso, _ := r["hookSpecificOutput"].(msg)
		assistant(msg{"type": "text", "text": fmt.Sprintf("hook: %v %v", hso["permissionDecision"], hso["permissionDecisionReason"])})
		result(false, "success", "done")
	case "mcpstatus":
		assistant(msg{"type": "text", "text": "mcp: " + mcpAtStart})
		result(false, "success", "done")
	case "agentbg":
		write(msg{"type": "system", "subtype": "task_started", "task_id": "ag1", "description": "Design mobile", "task_type": "local_agent", "is_backgrounded": true})
		assistant(msg{"type": "text", "text": "agent launched"})
		result(false, "success", "turn 1")
		go func() {
			time.Sleep(100 * time.Millisecond)
			write(msg{"type": "system", "subtype": "task_progress", "task_id": "ag1", "description": "Running build", "usage": msg{"tool_uses": 3, "total_tokens": 1200}})
			time.Sleep(300 * time.Millisecond)
			write(msg{"type": "system", "subtype": "task_notification", "task_id": "ag1", "status": "completed", "summary": "Design delivered"})
		}()
	case "bg":
		assistant(msg{"type": "text", "text": "started a background task"})
		result(false, "success", "turn 1")
		go func() {
			time.Sleep(150 * time.Millisecond)
			write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model})
			assistant(msg{"type": "text", "text": "background task done"})
			time.Sleep(150 * time.Millisecond)
			result(false, "success", "turn 2")
		}()
	case "crash":
		fmt.Fprintln(os.Stderr, "boom")
		os.Exit(3)
	default:
		assistant(msg{"type": "text", "text": "echo: " + text})
		result(false, "success", "echo: "+text)
	}
}

func callMCP(tool string, args msg) string {
	if mcpURL == "" {
		return "no mcp server"
	}
	b, _ := json.Marshal(msg{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": msg{"name": tool, "arguments": args}})
	req, _ := http.NewRequest("POST", mcpURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", mcpAuth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "mcp error: " + err.Error()
	}
	defer resp.Body.Close()
	var out struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || len(out.Result.Content) == 0 {
		return fmt.Sprintf("mcp status %d", resp.StatusCode)
	}
	return out.Result.Content[0].Text
}

// callMCPList returns "" when tools/list succeeds, else a reason.
func callMCPList() string {
	b, _ := json.Marshal(msg{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req, _ := http.NewRequest("POST", mcpURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", mcpAuth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "error " + err.Error()
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Sprintf("status %d", resp.StatusCode)
	}
	return ""
}

func mustWd() string { d, _ := os.Getwd(); return d }
