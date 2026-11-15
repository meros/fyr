package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/meros/fyr/internal/core"
)

// prRe finds a PR URL inside text (gh pr create's output); prURLRe is a
// whole value that is a PR URL. A value is never a URL found inside it, so
// "--subject=x <url>" is not a PR URL and never reaches gh (review
// 2026-11-15). A PR's own pages (/files, /checks) and a query or fragment
// are allowed after the number.
var (
	prRe    = regexp.MustCompile(`https://github\.com/([\w.-]+/[\w.-]+)/pull/(\d+)`)
	prURLRe = regexp.MustCompile(`^https://github\.com/([\w.-]+/[\w.-]+)/pull/(\d+)(?:/[\w/-]*)?(?:[?#]\S*)?$`)
)

// ParsePR splits a GitHub PR URL into "owner/repo" and the number.
func ParsePR(url string) (repo string, number int, ok bool) {
	m := prURLRe.FindStringSubmatch(url)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// CanonicalPR is the PR URL in the one form gh is given:
// https://github.com/owner/repo/pull/N.
func CanonicalPR(url string) (string, bool) {
	repo, n, ok := ParsePR(url)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", repo, n), true
}

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
				if u := prRe.FindString(resultText(b["content"])); u != "" {
					o.PRURL = u
				}
			}
		}
	}
	return o
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

// short picks the most telling input field of a tool call, one line.
func short(in map[string]any) string {
	for _, k := range []string{"command", "description", "file_path", "pattern", "url", "query", "path", "prompt"} {
		if s, ok := in[k].(string); ok && s != "" {
			return clip(firstLine(s), 80)
		}
	}
	return ""
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return clip(l, 160)
		}
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
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
		k.Description, k.Type, k.Status = str(m, "description"), str(m, "task_type"), "running"
		return k, "started", true
	case "task_progress":
		k.Activity = clip(firstLine(str(m, "description")), 120)
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
		k.Status = str(p, "status")
		if k.Status == "" || k.Status == "running" {
			return k, "", k.Status != ""
		}
		return k, "ended", true
	case "task_notification":
		k.Status, k.Summary = str(m, "status"), clip(firstLine(str(m, "summary")), 200)
		if k.Status == "" {
			k.Status = "completed"
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
		out = append(out, core.Task{ID: id, Description: d, Type: ty, Status: "running"})
	}
	return out
}

// activityLabel is the live, human-readable line for a tool call (SPEC 0.5
// V0.21): the agent's own description when there is one, else a short
// present-tense sentence. Bookkeeping calls return "" and leave the line.
func activityLabel(name string, in map[string]any) string {
	str := func(k string) string { s, _ := in[k].(string); return strings.TrimSpace(s) }
	if d := str("description"); d != "" && (name == "Bash" || name == "Task" || name == "Agent") {
		return clip(firstLine(d), 80)
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
		return "Running " + clip(firstLine(str("command")), 70)
	case "Read":
		return "Reading " + base(str("file_path"))
	case "Edit", "MultiEdit", "NotebookEdit":
		return "Editing " + base(str("file_path"))
	case "Write":
		return "Writing " + base(str("file_path"))
	case "Grep":
		return "Searching for " + clip(str("pattern"), 60)
	case "Glob":
		return "Listing " + clip(str("pattern"), 60)
	case "WebSearch":
		return "Searching the web for " + clip(str("query"), 60)
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
		return clip(firstLine(str("prompt")), 80)
	}
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
		if len(parts) == 2 {
			if parts[0] == "fyr" && parts[1] == "show" {
				if t := str("title"); t != "" {
					return "Showing " + clip(t, 60)
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
