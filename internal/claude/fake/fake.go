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
//	agentwork starts a background agent and ends its turn; the agent then
//	         writes its own assistant messages (parent_tool_use_id set)
//	         every 50 ms until an interrupt, which kills it with the CLI's
//	         task_updated killed, task_notification stopped and an empty
//	         background_tasks_changed, before the interrupt's control
//	         response, as claude does (V0.stop-ends-background)
//	bg       ends its turn, then starts a second turn on its own (as the CLI
//	         does when a background task finishes) with no user message
//	bgwait N starts a background task, ends its turn, and N ms later
//	         starts a turn on its own whose Bash call asks the PreToolUse
//	         hook; when stdin is closed by then, the call is refused with
//	         the CLI's reject text and no request (V0.133), as claude
//	         2.1.322 does. The fake, like the CLI, waits for its background
//	         tasks before it exits on a closed stdin.
//	deaf N   starts a background task of N ms, ends its turn, and from
//	         then on reads nothing more from stdin (no message, no control
//	         request), as claude 2.1.322 after its input closed; when the
//	         task ends it starts one turn by itself and exits (V0.143)
//	refused  makes one Bash call that the CLI refuses with its reject
//	         text, without asking Fyr (V0.133)
//	sub      runs one subagent (Task tool) before it answers
//	merge U  calls mcp__fyr__change_request {change: U, ask: merge}
//	         over HTTP from --mcp-config and reports the tool's text
//	gate     asks the PreToolUse hook about "gh pr merge 1" and reports it
//	mcpstatus reports whether the MCP server answered at startup, as the
//	         real CLI connects its MCP servers before the first turn
//	tool N   runs a tool for N ms, asks the PostToolUse hook and replies
//	         "context: <its additionalContext>"
//	subtool  runs a subagent (Agent tool) that makes one tool call, then
//	         ends the Agent tool; it asks the PostToolUse hook after each, as
//	         the CLI does (the subagent's input names agent_id and
//	         agent_type), and replies "sub: <context> top: <context>"
//	long N   runs a foreground Bash call ("sleep") for N ms; a control
//	         request background_tasks for its tool_use_id moves it to the
//	         background as claude 2.1.327 does: the call returns at once
//	         with the CLI's text, the queued messages are taken in, and the
//	         command ends N ms after it started with a task_notification
//	         (V0.background-on-message)
//	slow N   runs a tool for N ms, then takes in every queued message at
//	         that tool boundary and replies "took in: <texts>" (V0.33)
//	hold     runs a tool until a message "release" is queued, then takes
//	         in every queued message and replies "took in: <texts>"
//	stall    hold with a WebFetch call instead of Bash: a tool that hangs
//	view H   publishes a live view with headline H over MCP during the turn
//	launch   replies "launch: base=… token=set-<last 2>|unset effort=… model=…"
//	recall   replies "recall: <first user message of the session transcript>"
//	saymerge writes "merge" at the start of its text and calls nothing
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
// FYR_FAKE_OLD_CLI=1 plays a CLI before 2.1.327: its init names no
// capabilities and it answers background_tasks with an error.
//
// FYR_FAKE_INIT_DELAY_FILE names a file whose number is how many ms the
// fake waits before it answers initialize (V0.153).
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
	"sync/atomic"
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
	postHook bool
	stops    int

	// closed: stdin closed; eof is closed with it; bgwg: background tasks
	// the exit waits for.
	closed atomic.Bool
	// bgAgent is the running background agent of agentwork, killed by
	// the next interrupt (V0.stop-ends-background).
	bgAgent atomic.Bool
	// deaf: the fake reads nothing more (V0.143).
	deaf atomic.Bool
	eof  = make(chan struct{})
	bgwg sync.WaitGroup

	qmu   sync.Mutex
	queue []queued
	qsig  = make(chan struct{}, 1)
)

type queued struct{ uuid, text string }

// fg is the running foreground call of long, which background_tasks
// moves (V0.background-on-message).
var (
	fgmu   sync.Mutex
	fgID   string
	fgMove chan struct{}
)

// capabilities is what the init names: the move of a foreground Bash
// call, unless the fake plays an older CLI.
func capabilities() []any {
	if os.Getenv("FYR_FAKE_OLD_CLI") == "1" {
		return nil
	}
	return []any{"background_tasks_moves_bash_v1"}
}

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
	case <-eof:
		// Nobody can answer once stdin is closed: the CLI cancels the
		// request and refuses the call (V0.133).
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
			// V0.129 (1): Fyr passes a file; a JSON string still works.
			raw := []byte(os.Args[i+1])
			if !strings.HasPrefix(strings.TrimSpace(os.Args[i+1]), "{") {
				raw, _ = os.ReadFile(os.Args[i+1])
			}
			json.Unmarshal(raw, &cfg)
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
		if deaf.Load() || json.Unmarshal(sc.Bytes(), &m) != nil {
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
				if bgAgent.Swap(false) {
					write(msg{"type": "system", "subtype": "task_updated", "task_id": "ag2", "patch": msg{"status": "killed"}})
					write(msg{"type": "system", "subtype": "task_notification", "task_id": "ag2", "tool_use_id": "tu_ag2", "status": "stopped", "summary": "Audit sources"})
					write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
				}
			}
			resp := msg{}
			if req["subtype"] == "background_tasks" {
				// As claude 2.1.327: the answer first, then the tool call
				// sees the move and returns.
				if os.Getenv("FYR_FAKE_OLD_CLI") == "1" {
					write(msg{"type": "control_response", "response": msg{"subtype": "error", "request_id": id,
						"error": "Unsupported control request subtype: background_tasks"}})
					continue
				}
				use, _ := req["tool_use_id"].(string)
				fgmu.Lock()
				moved := use != "" && use == fgID
				move := fgMove
				if moved {
					fgID = ""
				}
				fgmu.Unlock()
				write(msg{"type": "control_response", "response": msg{"subtype": "success", "request_id": id, "response": msg{"backgrounded": moved}}})
				if moved {
					close(move)
				}
				continue
			}
			if req["subtype"] == "cancel_async_message" {
				uuid, _ := req["message_uuid"].(string)
				resp = msg{"cancelled": cancelQueued(uuid)}
			}
			if req["subtype"] == "initialize" {
				// V0.153: a start slowed by a thrashing host; the file holds
				// the delay in ms, read at each start.
				if f := os.Getenv("FYR_FAKE_INIT_DELAY_FILE"); f != "" {
					var ms int
					if b, err := os.ReadFile(f); err == nil {
						fmt.Sscan(string(b), &ms)
					}
					time.Sleep(time.Duration(ms) * time.Millisecond)
				}
				hooks, _ := req["hooks"].(msg)
				_, stopHook = hooks["Stop"]
				_, postHook = hooks["PostToolUse"]
				// V0.79: a built-in, a terminal-only one and a skill, in the
				// CLI's shape.
				resp = msg{"commands": []any{
					msg{"name": "compact", "description": "Clear the conversation but keep a summary", "argumentHint": "<optional custom summarization instructions>"},
					msg{"name": "clear", "description": "Clear the conversation", "argumentHint": ""},
					msg{"name": "fake-skill", "description": "A skill of the test folder", "argumentHint": "<topic>"},
				}, "models": []any{msg{"value": "fake-model"}}}
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
	// The CLI keeps running for its background tasks after stdin closes.
	closed.Store(true)
	close(eof)
	bgwg.Wait()
}

// rejectText is what claude 2.1.322 writes for a tool call it cancels when
// its input stream is closed (V0.133).
const rejectText = "The user doesn't want to take this action right now. STOP what you are doing and wait for the user to tell you how to proceed."

func refuse(id string) {
	write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
		"content": []any{msg{"type": "tool_result", "tool_use_id": id, "content": rejectText, "is_error": true}}},
		"tool_result_meta": []any{msg{"id": id, "non_execution_kind": "cancelled"}}})
}

// turn runs one scripted turn; it returns the messages it took in on the way.
func turn(text string) (took []queued) {
	// drain a stale interrupt from an earlier turn
	select {
	case <-interrupt:
	default:
	}
	write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model, "capabilities": capabilities(), "cwd": mustWd()})
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
		result(true, "success", "API Error: 400 invalid_request_error") // a failure that does not pass by itself (V0.overload-retry)
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
	case "saymerge":
		// V0.settle-merge-fixes (1): "merge" written in text, no call.
		assistant(msg{"type": "text", "text": "merge\n\nCI is green on the PR."})
		result(false, "success", "merge")
	case "merge":
		url := strings.TrimSpace(strings.TrimPrefix(text, "merge"))
		in := msg{"change": url, "ask": "merge", "args": msg{"method": "squash", "head": "abc1234def"}, "reason": "asked"}
		assistant(msg{"type": "tool_use", "id": "tu_m", "name": "mcp__fyr__change_request", "input": in})
		out := callMCP("change_request", in)
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
	case "agentwork":
		bgAgent.Store(true)
		write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{msg{"task_id": "ag2", "task_type": "local_agent", "description": "Audit sources"}}})
		write(msg{"type": "system", "subtype": "task_started", "task_id": "ag2", "tool_use_id": "tu_ag2", "description": "Audit sources", "task_type": "local_agent", "is_backgrounded": true})
		assistant(msg{"type": "text", "text": "agent launched"})
		result(false, "success", "turn 1")
		go func() {
			for bgAgent.Load() && !closed.Load() {
				time.Sleep(50 * time.Millisecond)
				write(msg{"type": "assistant", "session_id": session, "parent_tool_use_id": "tu_ag2",
					"message": msg{"role": "assistant", "content": []any{msg{"type": "text", "text": "reading sources"}}}})
			}
		}()
	case "bg":
		assistant(msg{"type": "text", "text": "started a background task"})
		result(false, "success", "turn 1")
		go func() {
			time.Sleep(150 * time.Millisecond)
			write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model, "capabilities": capabilities()})
			assistant(msg{"type": "text", "text": "background task done"})
			time.Sleep(150 * time.Millisecond)
			result(false, "success", "turn 2")
		}()
	case "bgwait":
		ms := 300
		if f := strings.Fields(text); len(f) > 1 {
			fmt.Sscan(f[1], &ms)
		}
		task := msg{"task_id": "bw1", "task_type": "local_bash", "description": "Wait for the build"}
		write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{task}})
		write(msg{"type": "system", "subtype": "task_started", "task_id": "bw1", "description": "Wait for the build", "task_type": "local_bash", "is_backgrounded": true})
		assistant(msg{"type": "text", "text": "waiting for the build"})
		// The task counts before the result: a reader that closes stdin on
		// the result must find the exit waiting for it (V0.fix-flakes).
		bgwg.Add(1)
		result(false, "success", "turn 1")
		go func() {
			defer bgwg.Done()
			time.Sleep(time.Duration(ms) * time.Millisecond)
			write(msg{"type": "system", "subtype": "task_notification", "task_id": "bw1", "status": "completed", "summary": "Background command \"Wait for the build\" completed"})
			write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
			write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model, "capabilities": capabilities()})
			assistant(msg{"type": "tool_use", "id": "tu_bw", "name": "Bash", "input": msg{"command": "cat build.output"}})
			if closed.Load() {
				refuse("tu_bw")
				assistant(msg{"type": "text", "text": "I've stopped as you asked."})
				write(msg{"type": "result", "subtype": "success", "is_error": false, "result": "stopped", "session_id": session})
				return
			}
			if request("bw_gate", msg{"subtype": "hook_callback", "callback_id": "gate",
				"input": msg{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": msg{"command": "cat build.output"}}}) == nil {
				refuse("tu_bw") // stdin closed while the hook was asked
				assistant(msg{"type": "text", "text": "I've stopped as you asked."})
				write(msg{"type": "result", "subtype": "success", "is_error": false, "result": "stopped", "session_id": session})
				return
			}
			write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
				"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_bw", "content": "build ok"}}}})
			assistant(msg{"type": "text", "text": "background read ok"})
			write(msg{"type": "result", "subtype": "success", "is_error": false, "result": "background read ok", "session_id": session})
		}()
	case "bgstuck", "bgmonitor":
		// V0.195: a background command that never ends, as a wait whose
		// condition can no longer turn true does.
		tool, desc := "Bash", "Wait for both experiments to finish"
		if word == "bgmonitor" {
			tool, desc = "Monitor", "CI on the PR"
		}
		assistant(msg{"type": "tool_use", "id": "tu_stuck", "name": tool, "input": msg{"command": "until false; do sleep 15; done", "run_in_background": true}})
		write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{msg{"task_id": "bs1", "task_type": "local_bash", "description": desc}}})
		write(msg{"type": "system", "subtype": "task_started", "task_id": "bs1", "tool_use_id": "tu_stuck", "description": desc, "task_type": "local_bash", "is_backgrounded": true})
		assistant(msg{"type": "text", "text": "I continue when it ends"})
		result(false, "success", "turn 1")
	case "deaf":
		ms := 3000
		if f := strings.Fields(text); len(f) > 1 {
			fmt.Sscan(f[1], &ms)
		}
		task := msg{"task_id": "bd1", "task_type": "local_bash", "description": "Run the proxy"}
		write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{task}})
		write(msg{"type": "system", "subtype": "task_started", "task_id": "bd1", "description": "Run the proxy", "task_type": "local_bash", "is_backgrounded": true})
		assistant(msg{"type": "text", "text": "proxy started"})
		result(false, "success", "turn 1")
		deaf.Store(true)
		go func() {
			time.Sleep(time.Duration(ms) * time.Millisecond)
			write(msg{"type": "system", "subtype": "task_notification", "task_id": "bd1", "status": "stopped", "summary": "Background command \"Run the proxy\" was stopped"})
			write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
			write(msg{"type": "system", "subtype": "init", "session_id": session, "model": model, "capabilities": capabilities()})
			assistant(msg{"type": "text", "text": "the proxy stopped"})
			write(msg{"type": "result", "subtype": "success", "is_error": false, "result": "the proxy stopped", "session_id": session})
			os.Exit(0)
		}()
	case "refused":
		assistant(msg{"type": "tool_use", "id": "tu_ref", "name": "Bash", "input": msg{"command": "cat build.output"}})
		refuse("tu_ref")
		assistant(msg{"type": "text", "text": "stopped"})
		result(false, "success", "stopped")
	case "tool":
		ms := 500
		if f := strings.Fields(text); len(f) > 1 {
			fmt.Sscan(f[1], &ms)
		}
		assistant(msg{"type": "tool_use", "id": "tu_t", "name": "Bash", "input": msg{"command": "sleep 1"}})
		time.Sleep(time.Duration(ms) * time.Millisecond)
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_t", "content": "slept"}}}})
		ctx := postToolUse("post1", msg{"hook_event_name": "PostToolUse", "session_id": session, "tool_name": "Bash", "tool_use_id": "tu_t"})
		assistant(msg{"type": "text", "text": "context: " + ctx})
		result(false, "success", "context: "+ctx)
	case "subtool":
		assistant(msg{"type": "tool_use", "id": "tu_agent", "name": "Agent", "input": msg{"description": "look", "prompt": "look", "subagent_type": "general-purpose"}})
		write(msg{"type": "assistant", "session_id": session, "parent_tool_use_id": "tu_agent",
			"message": msg{"role": "assistant", "content": []any{msg{"type": "tool_use", "id": "tu_sub_read", "name": "Read", "input": msg{"file_path": "go.mod"}}}}})
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": "tu_agent", "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_sub_read", "content": "module x"}}}})
		sub := postToolUse("post_sub", msg{"hook_event_name": "PostToolUse", "session_id": session, "agent_id": "a5c5b30d842d78842",
			"agent_type": "general-purpose", "tool_name": "Read", "tool_use_id": "tu_sub_read"})
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_agent", "content": "found it"}}}})
		top := postToolUse("post_top", msg{"hook_event_name": "PostToolUse", "session_id": session, "tool_name": "Agent", "tool_use_id": "tu_agent"})
		line := "sub: " + sub + " top: " + top
		assistant(msg{"type": "text", "text": line})
		result(false, "success", line)
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
	case "long":
		ms := 2000
		if f := strings.Fields(text); len(f) > 1 {
			fmt.Sscan(f[1], &ms)
		}
		end := time.Now().Add(time.Duration(ms) * time.Millisecond)
		move := make(chan struct{})
		fgmu.Lock()
		fgID, fgMove = "tu_long", move
		fgmu.Unlock()
		assistant(msg{"type": "tool_use", "id": "tu_long", "name": "Bash", "input": msg{"command": fmt.Sprintf("sleep %d", ms/1000)}})
		out := "slept"
		select {
		case <-time.After(time.Until(end)):
			fgmu.Lock()
			fgID = ""
			fgmu.Unlock()
		case <-move:
			out = "Command was manually backgrounded by user with ID: bl1. Output is being written to: /tmp/bl1.output"
			write(msg{"type": "system", "subtype": "task_started", "task_id": "bl1", "tool_use_id": "tu_long", "description": "sleep", "task_type": "local_bash", "is_backgrounded": false})
			write(msg{"type": "system", "subtype": "task_updated", "task_id": "bl1", "patch": msg{"is_backgrounded": true}})
			write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{msg{"task_id": "bl1", "task_type": "local_bash", "description": "sleep"}}})
			bgwg.Add(1)
			go func() {
				defer bgwg.Done()
				time.Sleep(time.Until(end))
				write(msg{"type": "system", "subtype": "task_notification", "task_id": "bl1", "tool_use_id": "tu_long", "status": "completed", "summary": "Background command \"sleep\" completed"})
				write(msg{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
			}()
		}
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_long", "content": out}}}})
		took = takeAll()
		var texts []string
		for _, q := range took {
			texts = append(texts, q.text)
		}
		assistant(msg{"type": "text", "text": "took in: " + strings.Join(texts, ", ")})
		result(false, "success", "done")
	case "hold", "stall":
		// Like slow, but the tool runs until a message "release" waits in
		// the queue, so a test orders its steps without timing. stall's
		// tool is no command: a WebFetch that hangs (V0.no-progress).
		if word == "stall" {
			assistant(msg{"type": "tool_use", "id": "tu_h", "name": "WebFetch", "input": msg{"url": "https://example.com/"}})
		} else {
			assistant(msg{"type": "tool_use", "id": "tu_h", "name": "Bash", "input": msg{"command": "sleep 1"}})
		}
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			qmu.Lock()
			release := false
			for _, q := range queue {
				release = release || q.text == "release"
			}
			qmu.Unlock()
			if release {
				break
			}
		}
		write(msg{"type": "user", "session_id": session, "parent_tool_use_id": nil, "message": msg{"role": "user",
			"content": []any{msg{"type": "tool_result", "tool_use_id": "tu_h", "content": "slept"}}}})
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

// postToolUse asks the PostToolUse hook, when one is registered, and
// returns its additionalContext.
func postToolUse(id string, input msg) string {
	if !postHook {
		return ""
	}
	r := request(id, msg{"subtype": "hook_callback", "callback_id": "viewnudge", "input": input})
	hso, _ := r["hookSpecificOutput"].(msg)
	ctx, _ := hso["additionalContext"].(string)
	return ctx
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
