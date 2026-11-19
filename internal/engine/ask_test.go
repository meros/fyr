package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.ask-tool: ask shows a question card and returns at once; the owner's
// answer closes it without a process waiting on it.
func TestAskTool(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))

	if out, isErr := callAs(t, h.e, id, "ask", map[string]any{"questions": []any{}}); !isErr {
		t.Fatalf("no questions accepted: %s", out)
	}
	out, isErr := callAs(t, h.e, id, "ask", map[string]any{"questions": []any{
		map[string]any{"question": "Which database?", "options": []any{map[string]any{"label": "Postgres"}, map[string]any{"label": "SQLite"}}},
	}})
	if isErr || !strings.Contains(out, "question card") {
		t.Fatalf("ask: %s", out)
	}
	th, _ := h.e.Thread(id)
	if len(th.Decisions) != 1 || !th.Decisions[0].Async {
		t.Fatalf("decisions: %+v", th.Decisions)
	}
	if d := dtoOf(h.e, id); d.Attention.Section != core.NeedsYou {
		t.Fatalf("attention: %+v", d.Attention)
	}
	if r := h.cmdID("a1", "thread.answer", id, core.Args{"decisionId": th.Decisions[0].ID, "answers": map[string]any{"Which database?": "SQLite"}}); !r.OK {
		t.Fatalf("answer: %+v", r)
	}
	if th, _ := h.e.Thread(id); len(th.Decisions) != 0 {
		t.Fatalf("still open: %+v", th.Decisions)
	}
}
