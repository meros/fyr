package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
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
	if p := t.MainPR(); p != nil {
		fmt.Fprintf(&b, " · PR %s (%s)", p.URL, p.State)
	}
	b.WriteString("\n")
	n := 0
	for _, tn := range readTurns(evs) {
		at := tn.At.In(time.Local).Format("2006-01-02 15:04")
		switch tn.Kind {
		case turnUser:
			if strings.TrimSpace(tn.Text) == "" && len(tn.Files) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n## %s · %s\n\n%s\n", speaker("Owner", tn.From), at, tn.Text)
			// The new agent cannot see the images: list each file with
			// its path so it can Read it.
			for _, f := range tn.Files {
				fmt.Fprintf(&b, "\nAttached: %s (%s) at %s\n", f.Name, f.MediaType, e.attachmentPath(f.ID))
			}
			n++
		case turnAgent:
			var parts []string
			for _, p := range tn.Parts {
				if p.Tool != "" {
					parts = append(parts, "- "+p.Tool+" "+toolLine(p.Input))
				} else if strings.TrimSpace(p.Text) != "" {
					parts = append(parts, p.Text)
				}
			}
			if len(parts) > 0 {
				fmt.Fprintf(&b, "\n## Agent · %s\n\n%s\n", at, strings.Join(parts, "\n"))
				n++
			}
		case turnHandover:
			fmt.Fprintf(&b, "\n---\n*Handed over from %s to %s (%s) · %s*\n", tn.FromProfile, tn.ToProfile, tn.Rung, at)
		case turnEnded:
			if tn.Failure != "" {
				fmt.Fprintf(&b, "\n*The run failed: %s*\n", tn.Failure)
			}
		case turnEffect:
			fmt.Fprintf(&b, "\n*Fyr ran %s (ok: %v): %s*\n", tn.Action, tn.OK, core.TruncHead(tn.Text, 300))
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
			return "`" + core.TruncHead(strings.ReplaceAll(s, "\n", " "), 200) + "`"
		}
	}
	return ""
}

// attachmentPath is where an attachment's file is stored, or a note that it
// is gone.
func (e *Engine) attachmentPath(id string) string {
	if e.Attach != nil {
		if m, err := e.Attach.Get(id); err == nil && m.Path != "" {
			return m.Path
		}
	}
	return "(file no longer stored)"
}
