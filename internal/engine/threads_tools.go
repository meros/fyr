package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// hourly counts calls per thread in a sliding hour (V0.22 runaway limits).
type hourly struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
}

func (h *hourly) allow(key string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hits == nil {
		h.hits = map[string][]time.Time{}
	}
	keep := h.hits[key][:0]
	for _, t := range h.hits[key] {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	if len(keep) >= h.max {
		h.hits[key] = keep
		return false
	}
	h.hits[key] = append(keep, now)
	return true
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d d", int(d.Hours()/24))
}

// threadText renders a thread's last turns as plain text for thread_read.
func threadText(evs []json.RawMessage, types []string) string {
	var b strings.Builder
	tools := 0
	flush := func() {
		if tools > 0 {
			fmt.Fprintf(&b, "  (%d tool calls)\n", tools)
			tools = 0
		}
	}
	for i, raw := range evs {
		switch types[i] {
		case "user":
			var u struct {
				Text string `json:"text"`
				From string `json:"from"`
			}
			json.Unmarshal(raw, &u)
			flush()
			who := "User"
			if u.From != "" {
				who = "Thread " + u.From
			}
			fmt.Fprintf(&b, "%s: %s\n", who, strings.TrimSpace(u.Text))
		case "msg":
			var m struct {
				Type   string `json:"type"`
				Parent any    `json:"parent_tool_use_id"`
				Msg    struct {
					Content []map[string]any `json:"content"`
				} `json:"message"`
			}
			json.Unmarshal(raw, &m)
			if m.Type != "assistant" || m.Parent != nil {
				continue
			}
			for _, c := range m.Msg.Content {
				switch c["type"] {
				case "text":
					flush()
					fmt.Fprintf(&b, "Agent: %s\n", strings.TrimSpace(fmt.Sprint(c["text"])))
				case "tool_use":
					tools++
				}
			}
		case "run.ended":
			flush()
		}
	}
	flush()
	s := b.String()
	if len(s) > 8000 {
		s = "…" + s[len(s)-7999:]
	}
	return s
}

func (e *Engine) threadTools(s *mcp.Server) {

	s.Add(mcp.Tool{Name: "threads",
		Description: "List the user's Fyr threads (other agent sessions): id, state, title, project, PR and age, newest first, at most 30. Use it to find related work before starting new work.",
		Schema:      obj(map[string]any{"query": str("Filter on title, project or PR."), "state": str("Only this state: asking, failed, blocked, done, working, waiting, seen, snoozed, settled.")}),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			q := strings.ToLower(strings.TrimSpace(fmt.Sprint(orEmpty(a["query"]))))
			want, _ := a["state"].(string)
			var b strings.Builder
			n := 0
			for _, d := range e.Snapshot() {
				if d.ArchivedAt != nil || n >= 30 {
					continue
				}
				pr := ""
				if d.PR != nil {
					pr = fmt.Sprintf(" · PR #%d %s", d.PR.Number, strings.ToLower(d.PR.State))
				}
				line := fmt.Sprintf("%s · %s · %s · %s%s · %s", d.ID, d.Attention.State, d.Title, d.Project, pr, age(d.UpdatedAt))
				if q != "" && !strings.Contains(strings.ToLower(line), q) {
					continue
				}
				if want != "" && string(d.Attention.State) != want {
					continue
				}
				if d.ID == self {
					line += " (this thread)"
				}
				b.WriteString(line + "\n")
				n++
			}
			if n == 0 {
				return "No threads match.", nil
			}
			return b.String(), nil
		}})

	s.Add(mcp.Tool{Name: "thread_read",
		Description: "Read the last turns of another Fyr thread as plain text: the user's and the agent's words, with tool calls counted. At most 8,000 characters.",
		Schema:      obj(map[string]any{"id": str("The thread id from threads."), "last": map[string]any{"type": "integer", "description": "How many events to look at, default 200."}}, "id"),
		Call: func(_ context.Context, _ string, a map[string]any) (string, error) {
			id, _ := a["id"].(string)
			t, ok := e.Thread(id)
			if !ok {
				return "", fmt.Errorf("no thread %q", id)
			}
			last := 200
			if f, ok := a["last"].(float64); ok && f > 0 && f <= 1000 {
				last = int(f)
			}
			evs, err := e.Events(id, 0, last)
			if err != nil {
				return "", err
			}
			raws := make([]json.RawMessage, len(evs))
			types := make([]string, len(evs))
			for i, ev := range evs {
				raws[i], types[i] = ev.Data, ev.Type
			}
			head := fmt.Sprintf("Thread %s · %s · %s\n", t.ID, t.Title, t.Cwd)
			return head + threadText(raws, types), nil
		}})

	s.Add(mcp.Tool{Name: "thread_send",
		Description: "Send a message to another Fyr thread's agent; it starts or steers that thread's run, and the receiver sees which thread wrote it. You cannot answer another thread's questions or approvals: those belong to the user.",
		Schema:      obj(map[string]any{"id": str("The thread id."), "text": str("The message.")}, "id", "text"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			id, _ := a["id"].(string)
			text, _ := a["text"].(string)
			if id == self {
				return "", errors.New("that is this thread")
			}
			t, ok := e.Thread(id)
			if !ok {
				return "", fmt.Errorf("no thread %q", id)
			}
			if len(t.Decisions) > 0 {
				return "", errors.New("that thread has an open question or approval for the user; only the user can answer it")
			}
			if !e.sendLimit.allow(self, time.Now()) {
				return "", errors.New("send limit: 30 messages an hour per thread")
			}
			r := e.Command(Cmd{CommandID: NewID("agent_"), Action: "thread.send", ThreadID: id, Args: core.Args{"text": text, "from": self}})
			if !r.OK {
				return "", errors.New(r.Error)
			}
			return fmt.Sprintf("Sent to %q (%s).", t.Title, id), nil
		}})

	s.Add(mcp.Tool{Name: "thread_new",
		Description: "Start a new Fyr thread (a separate agent session) in a folder with a first message, for work that should run on its own. The user sees it in the inbox, marked as started by this thread. At most 5 an hour.",
		Schema: obj(map[string]any{"cwd": str("The folder to work in (absolute path)."), "text": str("The first message: a full brief."),
			"title": str("A short title."), "model": str("Optional model id.")}, "cwd", "text"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			if !e.newLimit.allow(self, time.Now()) {
				return "", errors.New("limit: 5 new threads an hour per thread")
			}
			args := core.Args{"parent": self}
			for _, k := range []string{"cwd", "text", "title", "model"} {
				if v, ok := a[k].(string); ok && v != "" {
					args[k] = v
				}
			}
			if p, ok := e.Thread(self); ok {
				args["profile"] = p.Profile
				if _, has := args["model"]; !has && p.Model != "" {
					args["model"] = p.Model
				}
			}
			r := e.Command(Cmd{CommandID: NewID("agent_"), Action: "thread.create", Args: args})
			if !r.OK {
				return "", errors.New(r.Error)
			}
			return fmt.Sprintf("Started thread %s.", r.ThreadID), nil
		}})
}
