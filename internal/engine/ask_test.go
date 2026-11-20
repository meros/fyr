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

// V0.ask-cancel: the owner leaves a question unanswered on the card, or
// closes the card without answering it; the agent hears which, and the
// thread leaves Needs you either way.
func TestAskSkipAndCancel(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	ask := func(first, second string) string {
		t.Helper()
		if out, isErr := callAs(t, h.e, id, "ask", map[string]any{"questions": []any{
			map[string]any{"question": first, "options": []any{map[string]any{"label": "Yes"}}},
			map[string]any{"question": second, "options": []any{map[string]any{"label": "No"}}},
		}}); isErr || !strings.Contains(out, "question card") {
			t.Fatalf("ask: %s", out)
		}
		th, _ := h.e.Thread(id)
		if len(th.Decisions) == 0 {
			t.Fatal("no question opened")
		}
		return th.Decisions[len(th.Decisions)-1].ID
	}
	needsYou := func(want bool) {
		t.Helper()
		got := dtoOf(h.e, id).Attention.Section == core.NeedsYou
		if got != want {
			t.Fatalf("Needs you %v, want %v", got, want)
		}
	}

	// Skip the second question: the card is answered, its gap is named.
	d := ask("Ship it?", "Add a test?")
	needsYou(true)
	if r := h.cmdID("a1", "thread.answer", id, core.Args{"decisionId": d, "answers": map[string]any{"Ship it?": "Yes"}, "skipped": []any{"Add a test?"}}); !r.OK {
		t.Fatalf("answer with a skip: %+v", r)
	}
	needsYou(false)
	if evs := h.events(id); !hasEvent(evs, "decision.closed", "(skipped)") {
		t.Fatalf("the skip is not logged: %+v", evs)
	}

	// Cancel a card: it closes without an answer, and the agent is told.
	d = ask("Ship it now?", "Add a test?")
	needsYou(true)
	if r := h.cmdID("a2", "thread.cancelAsk", id, core.Args{"decisionId": d}); !r.OK {
		t.Fatalf("cancel: %+v", r)
	}
	needsYou(false)
	if th, _ := h.e.Thread(id); len(th.Decisions) != 0 {
		t.Fatalf("still open: %+v", th.Decisions)
	}
	// The notice itself is the core test's (TestCancelAskClosesTheCardAndTellsTheAgent): here it is the command
	// path that matters, and its delivery waits on the agent's turn, which this test does not own.
	if evs := h.events(id); !hasEvent(evs, "decision.closed", `"cancelled":true`) {
		t.Fatalf("the cancel is not logged: %+v", evs)
	}
}
