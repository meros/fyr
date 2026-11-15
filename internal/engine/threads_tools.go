package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/store"
)

// window allows max calls per key in a sliding span: the runaway limits of
// agent tools (V0.22 threads, V0.19 screenshots).
type window struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
	span time.Duration
}

func (w *window) allow(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hits == nil {
		w.hits = map[string][]time.Time{}
	}
	keep := w.hits[key][:0]
	for _, t := range w.hits[key] {
		if now.Sub(t) < w.span {
			keep = append(keep, t)
		}
	}
	if len(keep) >= w.max {
		w.hits[key] = keep
		return false
	}
	w.hits[key] = append(keep, now)
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
func threadText(evs []store.Event) string {
	var b strings.Builder
	tools := 0
	flush := func() {
		if tools > 0 {
			fmt.Fprintf(&b, "  (%d tool calls)\n", tools)
			tools = 0
		}
	}
	for _, t := range readTurns(evs) {
		switch t.Kind {
		case turnUser:
			flush()
			fmt.Fprintf(&b, "%s: %s\n", speaker("User", t.From), strings.TrimSpace(t.Text))
		case turnAgent:
			for _, p := range t.Parts {
				if p.Tool != "" {
					tools++
					continue
				}
				flush()
				fmt.Fprintf(&b, "Agent: %s\n", strings.TrimSpace(p.Text))
			}
		case turnEnded:
			flush()
		}
	}
	flush()
	return core.TruncTail(b.String(), 8000)
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
			head := fmt.Sprintf("Thread %s · %s · %s\n", t.ID, t.Title, t.Cwd)
			return head + threadText(evs), nil
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

	s.Add(mcp.Tool{Name: "thread_rename",
		Description: "Rename this Fyr thread, or a thread this thread started (id), so the owner's list says what the work is. Give a short, specific title (at most 80 characters) when the topic becomes clear or changes. A name the owner gave is kept.",
		Schema:      obj(map[string]any{"title": str("The new title."), "id": str("Optional: a thread this thread started; default this thread.")}, "title"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			id := sArg(a, "id")
			if id == "" {
				id = self
			}
			t, ok := e.Thread(id)
			if !ok {
				return "", fmt.Errorf("no thread %q", id)
			}
			if id != self && t.Parent != self {
				return "", errors.New("you can rename this thread or a thread it started")
			}
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindThreadRename, Thread: id, Args: map[string]any{"title": sArg(a, "title")}}); err != nil {
				return "", err
			}
			t, _ = e.Thread(id)
			return fmt.Sprintf("The thread is now %q.", t.Title), nil
		}})

	s.Add(mcp.Tool{Name: "thread_new",
		Description: "Start a new Fyr thread (a separate agent session) in a folder with a first message, for work that should run on its own. The user sees it in the inbox, marked as started by this thread. At most 5 an hour.",
		Schema: obj(map[string]any{"cwd": str("The folder to work in (absolute path)."), "text": str("The first message: a full brief."),
			"title": str("A short title."), "model": str("Optional model id.")}, "cwd", "text"),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			if !e.newLimit.allow(self, time.Now()) {
				return "", errors.New("limit: 5 new threads an hour per thread")
			}
			args := core.Args{}
			for _, k := range []string{"cwd", "text", "title", "model"} {
				if v, ok := a[k].(string); ok && v != "" {
					args[k] = v
				}
			}
			args = childArgs(e, self, args)
			r := e.Command(Cmd{CommandID: NewID("agent_"), Action: "thread.create", Args: args})
			if !r.OK {
				return "", errors.New(r.Error)
			}
			return fmt.Sprintf("Started thread %s.", r.ThreadID), nil
		}})
}
