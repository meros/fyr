package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/meros/fyr/internal/core"
)

var prRe = regexp.MustCompile(`https://github\.com/([\w.-]+/[\w.-]+)/pull/(\d+)`)

// ParsePR splits a GitHub PR URL into "owner/repo" and the number.
func ParsePR(url string) (repo string, number int, ok bool) {
	m := prRe.FindStringSubmatch(url)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[2])
	return m[1], n, true
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
			o.Activity = strings.TrimSpace(name + " " + short(in))
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
