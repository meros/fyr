package agent

import (
	"fmt"
	"strings"

	"github.com/meros/fyr/internal/core"
)

type toolInfo struct {
	name  string
	input map[string]any
}

// digest turns stream messages into row facts. It keeps the tool uses of
// the session so a tool_result can be matched to its call.
type digest struct{ tools map[string]toolInfo }

func newDigest() *digest { return &digest{tools: map[string]toolInfo{}} }

// digestOut is what one message changes. Empty fields change nothing.
type digestOut struct {
	Activity  string
	Summary   string
	Subagents int // added
	PRURL     string
}

func (d *digest) feed(m map[string]any) digestOut {
	var o digestOut
	msg, _ := m["message"].(map[string]any)
	blocks, _ := msg["content"].([]any)
	top := m["parent_tool_use_id"] == nil
	for _, x := range blocks {
		b, _ := x.(map[string]any)
		switch b["type"] {
		case "tool_use":
			id, _ := b["id"].(string)
			name, _ := b["name"].(string)
			in, _ := b["input"].(map[string]any)
			d.tools[id] = toolInfo{name, in}
			if !top {
				continue
			}
			if a := activityLabel(name, in); a != "" {
				o.Activity = a
			}
			if name == "Task" || name == "Agent" {
				o.Subagents++
			}
		case "text":
			if m["type"] == "assistant" && top {
				if s := firstLine(fmt.Sprint(b["text"])); s != "" {
					o.Summary = s
				}
			}
		case "tool_result":
			id, _ := b["tool_use_id"].(string)
			ti := d.tools[id]
			cmd, _ := ti.input["command"].(string)
			if ti.name == "Bash" && strings.Contains(cmd, "gh pr create") {
				if u := createdPR(resultText(b["content"])); u != "" {
					o.PRURL = u
				}
			}
		}
	}
	return o
}

// createdPR is the PR URL gh pr create printed: the last line that is a PR
// URL and nothing else. A URL inside other text (a test run that mentions
// the command) is not a created PR.
func createdPR(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if u, ok := core.CanonicalPR(strings.TrimSpace(lines[i])); ok {
			return u
		}
	}
	return ""
}

func resultText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, x := range v {
			if b, _ := x.(map[string]any); b["type"] == "text" {
				sb.WriteString(fmt.Sprint(b["text"]))
				sb.WriteByte('\n')
			}
		}
		return sb.String()
	}
	return ""
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return core.TruncHead(l, 160)
		}
	}
	return ""
}

// taskFromSystem reads one task message of the CLI (SPEC 0.5 V0.16). event
// is "started" or "ended" when the timeline should record it, else "".
func taskFromSystem(m map[string]any) (k core.Task, event string, ok bool) {
	str := func(x map[string]any, key string) string { s, _ := x[key].(string); return s }
	k.ID = str(m, "task_id")
	if k.ID == "" {
		return k, "", false
	}
	switch str(m, "subtype") {
	case "task_started":
		k.Description, k.Type, k.Status = str(m, "description"), str(m, "task_type"), core.TaskRunning
		return k, "started", true
	case "task_progress":
		k.Activity = core.TruncHead(firstLine(str(m, "description")), 120)
		if k.Activity == "" {
			k.Activity = str(m, "last_tool_name")
		}
		if u, _ := m["usage"].(map[string]any); u != nil {
			if n, ok := u["tool_uses"].(float64); ok {
				k.ToolUses = int(n)
			}
			if n, ok := u["total_tokens"].(float64); ok {
				k.Tokens = int(n)
			}
		}
		return k, "", true
	case "task_updated":
		p, _ := m["patch"].(map[string]any)
		k.Status = core.TaskStatus(str(p, "status"))
		if k.Status == "" || k.Status == core.TaskRunning {
			return k, "", k.Status != ""
		}
		return k, "ended", true
	case "task_notification":
		k.Status, k.Summary = core.TaskStatus(str(m, "status")), core.TruncHead(firstLine(str(m, "summary")), 200)
		if k.Status == "" {
			k.Status = core.TaskCompleted
		}
		return k, "ended", true
	}
	return k, "", false
}

// taskList reads background_tasks_changed: the tasks that run now.
func taskList(m map[string]any) []core.Task {
	raw, _ := m["tasks"].([]any)
	var out []core.Task
	for _, x := range raw {
		t, _ := x.(map[string]any)
		id, _ := t["task_id"].(string)
		if id == "" {
			continue
		}
		d, _ := t["description"].(string)
		ty, _ := t["task_type"].(string)
		out = append(out, core.Task{ID: id, Description: d, Type: ty, Status: core.TaskRunning})
	}
	return out
}

// activityLabel is the live, human-readable line for a tool call (SPEC 0.5
// V0.21): the agent's own description when there is one, else a short
// present-tense sentence. Bookkeeping calls return "" and leave the line.
func activityLabel(name string, in map[string]any) string {
	str := func(k string) string { s, _ := in[k].(string); return strings.TrimSpace(s) }
	if d := str("description"); d != "" && (name == "Bash" || name == "Task" || name == "Agent") {
		return core.TruncHead(firstLine(d), 80)
	}
	base := func(p string) string {
		if i := strings.LastIndex(p, "/"); i >= 0 {
			return p[i+1:]
		}
		return p
	}
	switch name {
	case "ToolSearch", "TodoWrite", "ListMcpResourcesTool", "ReadMcpResourceTool":
		return ""
	case "Bash":
		return "Running " + core.TruncHead(firstLine(str("command")), 70)
	case "Read":
		return "Reading " + base(str("file_path"))
	case "Edit", "MultiEdit", "NotebookEdit":
		return "Editing " + base(str("file_path"))
	case "Write":
		return "Writing " + base(str("file_path"))
	case "Grep":
		return "Searching for " + core.TruncHead(str("pattern"), 60)
	case "Glob":
		return "Listing " + core.TruncHead(str("pattern"), 60)
	case "WebSearch":
		return "Searching the web for " + core.TruncHead(str("query"), 60)
	case "WebFetch":
		u := str("url")
		if i := strings.Index(u, "://"); i >= 0 {
			u = u[i+3:]
		}
		if i := strings.Index(u, "/"); i >= 0 {
			u = u[:i]
		}
		return "Reading " + u
	case "Task", "Agent":
		return core.TruncHead(firstLine(str("prompt")), 80)
	}
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
		if len(parts) == 2 {
			if parts[0] == "fyr" && parts[1] == "show" {
				if t := str("title"); t != "" {
					return "Showing " + core.TruncHead(t, 60)
				}
				return "Showing a preview"
			}
			server := parts[0]
			switch server {
			case "github":
				server = "GitHub"
			default:
				if server != "" {
					server = strings.ToUpper(server[:1]) + server[1:]
				}
			}
			return server + " " + strings.ReplaceAll(parts[1], "_", " ")
		}
	}
	return name
}
