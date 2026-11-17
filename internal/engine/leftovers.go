package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// V0.176: what the leftover tracker and the temp sweep ask of the engine,
// the line on a thread whose leftovers Fyr ended, and keep_process.

// LeftoverThread says whether id is a thread of this host, asleep (settled or
// archived), and in a run. It names a cgroup, whose name is the thread's id.
func (e *Engine) LeftoverThread(id string) (known, asleep, running bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[id]
	if t == nil {
		return false, false, false
	}
	return true, t.Asleep(), t.Running
}

// TempThread is a thread as the temp sweep sees it.
type TempThread struct {
	ID, SessionID, Cwd string
	Asleep             bool
	Branches           []string
}

// TempThreads are the threads whose scratch and worktrees the sweep looks
// at: every awake thread (its session keeps its scratch) and the threads
// that went to sleep in the last two weeks.
func (e *Engine) TempThreads() []TempThread {
	e.mu.Lock()
	defer e.mu.Unlock()
	cut := e.Now().Add(-14 * 24 * time.Hour)
	var out []TempThread
	for _, t := range e.threads {
		asleep := t.Asleep()
		if asleep && t.UpdatedAt.Before(cut) {
			continue
		}
		x := TempThread{ID: t.ID, SessionID: t.SessionID, Cwd: t.Cwd, Asleep: asleep}
		for _, c := range t.Changes {
			if c.Branch != "" {
				x.Branches = append(x.Branches, c.Branch)
			}
		}
		out = append(out, x)
	}
	return out
}

// LeftoverItem is one ended leftover as the line names it.
type LeftoverItem struct {
	Name string
	RSS  uint64
}

// LeftoverLine is the line: "Fyr ended 2 leftover processes (the thread
// settled): chromium (1.9 GB), http.server".
func LeftoverLine(items []LeftoverItem, why string) string {
	var names []string
	for _, x := range items {
		n := x.Name
		if x.RSS >= 100<<20 {
			n += fmt.Sprintf(" (%s)", bytesText(x.RSS))
		}
		names = append(names, n)
	}
	what := "a leftover process"
	if len(items) != 1 {
		what = fmt.Sprintf("%d leftover processes", len(items))
	}
	return fmt.Sprintf("Fyr ended %s (%s): %s", what, why, strings.Join(names, ", "))
}

func bytesText(b uint64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%d MB", b>>20)
}

// LeftoversEnded leaves the line on the thread; a cgroup that is no thread's
// leaves it in the log only.
func (e *Engine) LeftoversEnded(thread string, items []LeftoverItem, why string) {
	if known, _, _ := e.LeftoverThread(thread); !known {
		return
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindLeftoverEnded, Thread: thread,
		Args: core.LeftoverEndedArgs{Text: LeftoverLine(items, why)}}); err != nil {
		log.Printf("leftovers %s: %v", thread, err)
	}
}

// leftoverTools is keep_process: the agent declares a process it started
// that must outlive its run (a dev server the owner asked for, a tunnel).
func (e *Engine) leftoverTools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "keep_process",
		Description: "Tell Fyr that a process you started must stay after your run ends (a dev server or a tunnel the owner asked for). Fyr ends a thread's leftover processes (anything in your thread that is not your CLI or its children: detached, orphaned or backgrounded and left) 30 min after your run ends, and all of them when the thread settles. A kept process stays until the keep expires; one started under `timeout` keeps its timeout without this. Name it by pid (from your pid file) or by a part of its command line.",
		Schema: obj(map[string]any{
			"pid":   map[string]any{"type": "integer", "description": "The process's pid, or its parent's."},
			"match": map[string]any{"type": "string", "description": "Text in its command line, e.g. \"vite --port 5173\" (at least 3 characters)."},
			"hours": map[string]any{"type": "number", "description": "How long to keep it, 1 to 24 (default 4)."},
			"why":   map[string]any{"type": "string", "description": "What it is for, as the owner will read it."},
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			if e.LeftoverDeclare == nil {
				return "This host does not end leftover processes; nothing to declare.", nil
			}
			pid := 0
			if f, ok := a["pid"].(float64); ok {
				pid = int(f)
			}
			h := 4.0
			if f, ok := a["hours"].(float64); ok {
				h = min(max(f, 1), 24)
			}
			if err := e.LeftoverDeclare(th, pid, sArg(a, "match"), time.Duration(h*float64(time.Hour)), sArg(a, "why")); err != nil {
				return "", err
			}
			return fmt.Sprintf("Kept for %g h, or until the thread settles. Stop it yourself when it is no longer needed.", h), nil
		}})
}
