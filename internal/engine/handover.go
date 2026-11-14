package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Conversation writes a thread's conversation from Fyr's log as Markdown,
// for a handover whose new agent reads the old conversation (SPEC 0.5
// V0.45, rung 2). Subagent messages and tool output are left out; tool
// calls are one line each.
func (e *Engine) Conversation(id string) (string, error) {
	t, ok := e.Thread(id)
	if !ok {
		return "", fmt.Errorf("no thread %s", id)
	}
	evs, err := e.st.EventsFor(id, 0, 1<<30, false)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nFolder: %s", t.Title, t.Cwd)
	if t.Branch != "" {
		fmt.Fprintf(&b, " · branch %s", t.Branch)
	}
	if t.PR != nil {
		fmt.Fprintf(&b, " · PR %s (%s)", t.PR.URL, t.PR.State)
	}
	b.WriteString("\n")
	n := 0
	for _, ev := range evs {
		at := ev.At.In(time.Local).Format("2006-01-02 15:04")
		switch ev.Type {
		case "user":
			var d struct{ Text, From string }
			json.Unmarshal(ev.Data, &d)
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			who := "Owner"
			switch {
			case d.From == "handover":
				who = "Fyr"
			case d.From == "view":
				who = "Owner (live view)"
			case d.From != "":
				who = "Thread " + d.From
			}
			fmt.Fprintf(&b, "\n## %s · %s\n\n%s\n", who, at, d.Text)
			n++
		case "msg":
			var d struct {
				Type   string `json:"type"`
				Parent any    `json:"parent_tool_use_id"`
				Msg    struct {
					Content []map[string]any `json:"content"`
				} `json:"message"`
			}
			json.Unmarshal(ev.Data, &d)
			if d.Type != "assistant" || d.Parent != nil {
				continue
			}
			var parts []string
			for _, c := range d.Msg.Content {
				switch c["type"] {
				case "text":
					if s, _ := c["text"].(string); strings.TrimSpace(s) != "" {
						parts = append(parts, s)
					}
				case "tool_use":
					nm, _ := c["name"].(string)
					parts = append(parts, "- "+nm+" "+toolLine(c["input"]))
				}
			}
			if len(parts) > 0 {
				fmt.Fprintf(&b, "\n## Agent · %s\n\n%s\n", at, strings.Join(parts, "\n"))
				n++
			}
		case "handover":
			var d struct{ From, To, Rung string }
			json.Unmarshal(ev.Data, &d)
			fmt.Fprintf(&b, "\n---\n*Handed over from %s to %s (%s) · %s*\n", d.From, d.To, d.Rung, at)
		case "run.ended":
			var d struct {
				Failure *struct{ Message string } `json:"failure"`
			}
			json.Unmarshal(ev.Data, &d)
			if d.Failure != nil {
				fmt.Fprintf(&b, "\n*The run failed: %s*\n", d.Failure.Message)
			}
		case "effect.result":
			var d struct {
				Action string
				OK     bool
				Text   string
			}
			json.Unmarshal(ev.Data, &d)
			fmt.Fprintf(&b, "\n*Fyr ran %s (ok: %v): %s*\n", d.Action, d.OK, clip(d.Text, 300))
		}
	}
	if n == 0 {
		return "", nil
	}
	return b.String(), nil
}

// toolLine is one short line for a tool call's input.
func toolLine(in any) string {
	m, _ := in.(map[string]any)
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "description", "query"} {
		if s, ok := m[k].(string); ok && s != "" {
			return "`" + clip(strings.ReplaceAll(s, "\n", " "), 200) + "`"
		}
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
