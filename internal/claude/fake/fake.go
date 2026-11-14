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
//	slow N   runs a tool for N ms, then takes in every queued message at
//	         that tool boundary and replies "took in: <texts>" (V0.33)
//	view H   publishes a live view with headline H over MCP during the turn
//	launch   replies "launch: base=… token=set-<last 2>|unset effort=… model=…"
//	recall   replies "recall: <first user message of the session transcript>"
//	other    replies "echo: <text>"
//
// When initialize registers a Stop hook (callback "view"), every turn that
// ends without an error asks it, as the CLI does: a block reply writes the
// synthetic user message and the stop-hook-error notification, then runs a
// forced turn that publishes a view with headline "after stop hook", and
// asks again with stop_hook_active true (V0.37).
//
// With CLAUDE_CONFIG_DIR set, the fake keeps a session transcript as the CLI
// does, in <dir>/projects/<cwd with every non-alphanumeric as ->/<session>.jsonl
// plus a <session>/tool-results/ folder, and --resume of a session without a
// transcript there exits at once with "No conversation found" (V0.45).
// FYR_FAKE_NORESUME=1 makes every --resume fail that way.
//
// As the real CLI does, a user message with a uuid waits in a queue and is
// reported as command_lifecycle queued, started (taken in), completed (its
// turn ended) or cancelled (control request cancel_async_message).
package fake

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

	stopHook bool
	stops    int

	qmu   sync.Mutex
	queue []queued
	qsig  = make(chan struct{}, 1)
)

type queued struct{ uuid, text string }

func lifecycle(uuid, state string) {
	if uuid != "" {
		write(msg{"type": "command_lifecycle", "command_uuid": uuid, "state": state, "session_id": session})
	}
}

func enqueue(q queued) {
	qmu.Lock()
	queue = append(queue, q)
	lifecycle(q.uuid, "queued")
	qmu.Unlock()
	select {
	case qsig <- struct{}{}:
	default:
	}
}

// takeAll empties the queue, reporting each message as started.
func takeAll() []queued {
	qmu.Lock()
	defer qmu.Unlock()
	out := queue
	queue = nil
	for _, q := range out {
		lifecycle(q.uuid, "started")
	}
	return out
}

func takeOne() (queued, bool) {
	qmu.Lock()
	defer qmu.Unlock()
	if len(queue) == 0 {
		return queued{}, false
	}
	q := queue[0]
	queue = queue[1:]
	lifecycle(q.uuid, "started")
	return q, true
}

func cancelQueued(uuid string) bool {
	qmu.Lock()
	defer qmu.Unlock()
	for i, q := range queue {
		if q.uuid == uuid {
			queue = append(queue[:i:i], queue[i+1:]...)
			lifecycle(uuid, "cancelled")
			return true
		}
	}
	return false
}

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

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// transcript is the session's transcript file, or "" without a config dir.
func transcript() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "projects", nonAlnum.ReplaceAllString(mustWd(), "-"), session+".jsonl")
}

func record(role, text string) {
	p := transcript()
	if p == "" {
		return
	}
	os.MkdirAll(filepath.Join(strings.TrimSuffix(p, ".jsonl"), "tool-results"), 0o700)
	os.WriteFile(filepath.Join(strings.TrimSuffix(p, ".jsonl"), "tool-results", "fake.txt"), []byte("saved tool output"), 0o600)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(msg{"type": role, "sessionId": session, "message": msg{"role": role, "content": text}})
	f.Write(append(b, '\n'))
}

// firstUser is the first user text of the transcript.
func firstUser() string {
	b, err := os.ReadFile(transcript())
	if err != nil {
		return "no transcript"
	}
	for _, l := range strings.Split(string(b), "\n") {
		var m struct {
			Type    string `json:"type"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(l), &m) == nil && m.Type == "user" {
			return m.Message.Content
		}
	}
	return "empty transcript"
}

func assistant(content ...msg) {
	for _, c := range content {
		if c["type"] == "text" {
			t, _ := c["text"].(string)
			record("assistant", t)
		}
	}
	write(msg{"type": "assistant", "session_id": session, "parent_tool_use_id": nil,
		"message": msg{"role": "assistant", "content": content}})
}

func result(isErr bool, subtype, text string) {
	if !isErr && stopHook {
		active := false
		for i := 0; i < 5; i++ {
			stops++
			r := request(fmt.Sprintf("stop%d", stops), msg{"subtype": "hook_callback", "callback_id": "view",
				"input": msg{"hook_event_name": "Stop", "stop_hook_active": active, "background_tasks": []any{}, "last_assistant_message": text}})
			if r == nil || r["decision"] != "block" {
				break
			}
			reason, _ := r["reason"].(string)
			write(msg{"type": "user", "isSynthetic": true, "session_id": session, "parent_tool_use_id": nil,
				"message": msg{"role": "user", "content": []any{msg{"type": "text", "text": "Stop hook feedback:\n" + reason}}}})
			write(msg{"type": "system", "subtype": "notification", "key": "stop-hook-error", "priority": "immediate", "text": "Stop hook error occurred"})
			publishView("after stop hook", fmt.Sprintf("tu_v%d", stops))
			assistant(msg{"type": "text", "text": "Updated the view."})
			text, active = "Updated the view.", true
		}
	}
	write(msg{"type": "result", "subtype": subtype, "is_error": isErr, "result": text,
		"session_id": session, "num_turns": 1, "duration_ms": 5, "total_cost_usd": 0.001})
}

func Main() {
	resume := false
	for i, a := range os.Args {
		if (a == "--session-id" || a == "--resume") && i+1 < len(os.Args) {
			session = os.Args[i+1]
			resume = a == "--resume"
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
	if resume {
		if _, err := os.Stat(transcript()); os.Getenv("FYR_FAKE_NORESUME") == "1" || (os.Getenv("CLAUDE_CONFIG_DIR") != "" && err != nil) {
			fmt.Fprintln(os.Stderr, "No conversation found with session ID: "+session)
			write(msg{"type": "result", "subtype": "error_during_execution", "is_error": true, "session_id": session})
			os.Exit(1)
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
	go func() {
		for {
			q, ok := takeOne()
			if !ok {
				<-qsig
				continue
			}
			took := turn(q.text)
			lifecycle(q.uuid, "completed")
			for _, t := range took {
				lifecycle(t.uuid, "completed")
			}
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
			if req["subtype"] == "cancel_async_message" {
				uuid, _ := req["message_uuid"].(string)
				resp = msg{"cancelled": cancelQueued(uuid)}
			}
			if req["subtype"] == "initialize" {
				hooks, _ := req["hooks"].(msg)
				_, stopHook = hooks["Stop"]
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
				var parts []string
				images := 0
				for _, c := range cs {
					if b, _ := c.(msg); b["type"] == "text" {
						t, _ := b["text"].(string)
						parts = append(parts, t)
					} else if b["type"] == "image" {
						images++
					}
				}
				text = strings.Join(parts, "\n")
				for i := 0; i < images; i++ {
					text += " [image]"
				}
			}
			uuid, _ := m["uuid"].(string)
			enqueue(queued{uuid: uuid, text: text})
		}
	}
}

// turn runs one scripted turn; it returns the messages it took in on the way.
func turn(text string) (took []queued) {
	// drain a stale interrupt from an earlier turn
	select {
	case <-interrupt:
	default:
	}
	write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model, "cwd": mustWd()})
	record("user", text)
	word := strings.Fields(text + " x")[0]
	switch word {
	case "launch":
		// V0.50: what the adapter launched with (the key only as set or not).
		tok := "unset"
		if os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" {
			tok = "set-" + os.Getenv("ANTHROPIC_AUTH_TOKEN")[len(os.Getenv("ANTHROPIC_AUTH_TOKEN"))-2:]
		}
		effort := ""
		for i, a := range os.Args {
			if a == "--effort" && i+1 < len(os.Args) {
				effort = os.Args[i+1]
			}
		}
		line := fmt.Sprintf("launch: base=%s token=%s effort=%s model=%s", os.Getenv("ANTHROPIC_BASE_URL"), tok, effort, model)
		assistant(msg{"type": "text", "text": line})
		result(false, "success", line)
	case "recall":
		assistant(msg{"type": "text", "text": "recall: " + firstUser()})
		result(false, "success", "done")
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
	case "slow":
		ms := 500
		if f := strings.Fields(text); len(f) > 1 {
			fmt.Sscan(f[1], &ms)
		}
		assistant(msg{"type": "tool_use", "id": "tu_s", "name": "Bash", "input": msg{"command": "sleep 1"}})
		time.Sleep(time.Duration(ms) * time.Millisecond)
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_s", "content": "slept"}}}})
		took = takeAll()
		var texts []string
		for _, q := range took {
			texts = append(texts, q.text)
		}
		assistant(msg{"type": "text", "text": "took in: " + strings.Join(texts, ", ")})
		result(false, "success", "done")
	case "view":
		h := strings.TrimSpace(strings.TrimPrefix(text, "view"))
		out := publishView(h, "tu_view")
		assistant(msg{"type": "text", "text": "view: " + out})
		result(false, "success", "done")
	case "crash":
		fmt.Fprintln(os.Stderr, "boom")
		os.Exit(3)
	default:
		assistant(msg{"type": "text", "text": "echo: " + text})
		result(false, "success", "echo: "+text)
	}
	return took
}

// publishView calls view_publish as a tool use and returns the tool's text.
func publishView(headline, id string) string {
	args := msg{"html": "<h1 data-text=\"headline\"></h1>", "state": msg{"headline": headline}}
	assistant(msg{"type": "tool_use", "id": id, "name": "mcp__fyr__view_publish", "input": args})
	out := callMCP("view_publish", args)
	write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
		"content": []any{msg{"type": "tool_result", "tool_use_id": id, "content": out}}}})
	return out
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
