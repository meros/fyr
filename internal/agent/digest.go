package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
