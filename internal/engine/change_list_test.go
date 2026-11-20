package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// manyChanges gives the thread 50 changes (the most a thread keeps): 30
// ended or landed, one landed with a failed condition, 19 open with long
// titles, so the open lines alone pass 4,000 characters.
func manyChanges(t *testing.T, h *harness, id string) (failing string, open []string) {
	t.Helper()
	call := func(tool string, a map[string]any) string {
		out, isErr := callAs(t, h.e, id, tool, a)
		if isErr {
			t.Fatalf("%s %v: %s", tool, a, out)
		}
		return out
	}
	long := strings.Repeat("with a long description of the work ", 5)
	for i := range 50 {
		key := fmt.Sprintf("code:%02d", i)
		title := fmt.Sprintf("Change %02d", i)
		if i >= 31 {
			title += " " + long
		}
		call("change_open", map[string]any{"key": key, "kind": "code", "title": title, "branch": fmt.Sprintf("b%02d", i)})
		switch {
		case i < 15:
			call("change_update", map[string]any{"change": key, "phase": "landed"})
		case i < 25:
			call("change_update", map[string]any{"change": key, "phase": "abandoned"})
		case i < 30:
			call("change_update", map[string]any{"change": key, "phase": "live"})
		case i == 30:
			call("change_update", map[string]any{"change": key, "phase": "landed", "conditions": []any{map[string]any{"type": "health", "status": "fail", "reason": "2 of 4 pods crash"}}})
		}
	}
	th, _ := h.e.Thread(id)
	if len(th.Changes) != 50 {
		t.Fatalf("%d changes, want 50", len(th.Changes))
	}
	for _, c := range th.Changes {
		if c.Key == "code:30" {
			failing = c.ID
		}
		if c.Key >= "code:31" {
			open = append(open, c.ID)
		}
	}
	return failing, open
}

// SPEC V0.96 (2): change_list lists only the open changes, one line each,
// in at most 4,000 characters, and ends with "+N more" when it cuts; id
// gives one change in full; done: true lists the done ones too; a landed
// change with a failed condition is open.
func TestChangeListOpenOnlyAndCapped(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	failing, open := manyChanges(t, h, id)

	out, isErr := callAs(t, h.e, id, "change_list", map[string]any{})
	if isErr {
		t.Fatalf("list: %s", out)
	}
	t.Logf("change_list: %d characters\n%s", len(out), out)
	if len(out) > 4000 {
		t.Fatalf("change_list is %d characters, want at most 4000", len(out))
	}
	if !strings.HasSuffix(out, "more; pass id for one change") || !strings.Contains(out, "+") {
		t.Fatalf("no \"+N more\" tail: %q", out[max(0, len(out)-200):])
	}
	if !strings.Contains(out, failing) || !strings.Contains(out, "health failed") {
		t.Fatal("the landed change with a failed condition is not listed")
	}
	if !strings.Contains(out, open[0]) {
		t.Fatal("the first open change is not listed")
	}
	th, _ := h.e.Thread(id)
	for _, c := range th.Changes {
		if c.Key < "code:30" && strings.Contains(out, c.ID) {
			t.Fatalf("done change %s (%s) is listed", c.Key, c.Phase)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "c_") && !strings.Contains(l, " · ") {
			t.Fatalf("not one line per change: %q", l)
		}
	}
	shown := strings.Count(out, "\nc_")
	var n int
	tail := out[strings.LastIndex(out, "\n")+1:]
	if _, err := fmt.Sscanf(tail, "+%d more", &n); err != nil || shown+n != 20 {
		t.Fatalf("shown %d + %d more, want 20 open changes: %q", shown, n, tail)
	}

	// id: one change in full, as JSON.
	one, isErr := callAs(t, h.e, id, "change_list", map[string]any{"id": failing})
	var rows []map[string]any
	if isErr || json.Unmarshal([]byte(one), &rows) != nil || len(rows) != 1 || rows[0]["id"] != failing || !strings.Contains(one, "2 of 4 pods crash") {
		t.Fatalf("id: %s", one)
	}

	// done: true lists ended and landed changes too.
	all, isErr := callAs(t, h.e, id, "change_list", map[string]any{"done": true})
	if isErr || len(all) > 4000 {
		t.Fatalf("done: %d characters, %s", len(all), all)
	}
	for _, c := range th.Changes[:3] {
		if !strings.Contains(all, c.ID) {
			t.Fatalf("done: true does not list %s (%s): %s", c.Key, c.Phase, all)
		}
	}
	if !strings.Contains(all, "abandoned") {
		t.Fatalf("done: true lists no abandoned change: %s", all)
	}

	// scope: project uses the same lines.
	proj, isErr := callAs(t, h.e, id, "change_list", map[string]any{"scope": "project"})
	if isErr || len(proj) > 4000 || strings.HasPrefix(strings.TrimSpace(proj), "[") {
		t.Fatalf("project: %d characters, %s", len(proj), proj)
	}
}

// SPEC V0.96 (3): fyr_state's changes part shows only the open changes
// within 4,000 characters, with the same tail.
func TestFyrStateChangesCapped(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	failing, _ := manyChanges(t, h, id)
	for _, about := range []string{"", "changes"} {
		out, isErr := callAs(t, h.e, id, "fyr_state", map[string]any{"about": about})
		if isErr {
			t.Fatalf("fyr_state %q: %s", about, out)
		}
		part := out[strings.Index(out, "changes"):]
		if v := strings.Index(part, "\nview:"); v >= 0 {
			part = part[:v]
		}
		if len(part) > 4000 {
			t.Fatalf("fyr_state %q: the changes part is %d characters", about, len(part))
		}
		if !strings.Contains(part, "more; pass id for one change") || !strings.Contains(part, failing) {
			t.Fatalf("fyr_state %q: %s", about, part)
		}
		th, _ := h.e.Thread(id)
		if strings.Contains(part, th.Changes[0].ID+" ") {
			t.Fatalf("fyr_state %q lists a landed change", about)
		}
		if about == "" && !strings.Contains(out, "view: off") {
			t.Fatalf("fyr_state lost the view part: %s", out)
		}
	}
	if one, isErr := callAs(t, h.e, id, "fyr_state", map[string]any{"about": failing}); isErr || !strings.Contains(one, "2 of 4 pods crash") {
		t.Fatalf("one change: %s", one)
	}
}

// SPEC V0.96 (4): the first message of a handover holds one line per open
// change of the thread: full id, title and phase.
func TestHandoverFirstMessageListsOpenChanges(t *testing.T) {
	h := newHandoverHarness(t)
	th := h.start("hello")
	callAs(t, h.e, th.ID, "change_open", map[string]any{"key": "k8s:prod/api", "title": "Raise api replicas", "branch": "feat/replicas"})
	callAs(t, h.e, th.ID, "change_open", map[string]any{"key": "dns:old", "title": "Old DNS record", "branch": "feat/dns"})
	callAs(t, h.e, th.ID, "change_update", map[string]any{"change": "dns:old", "phase": "abandoned"})
	open := changeOf(h.harness, th.ID, "k8s:prod/api")
	gone := changeOf(h.harness, th.ID, "dns:old")
	os.RemoveAll(filepath.Join(h.dirA, "projects"))
	if ev := h.handover(th.ID, "b"); ev.Rung != "adopt" {
		t.Fatalf("event %+v", ev)
	}
	var first string
	for _, u := range h.handoverTexts(th.ID, 1) {
		if text, ok := strings.CutPrefix(u, "handover|"); ok {
			first = text
		}
	}
	line := ""
	for _, l := range strings.Split(first, "\n") {
		if strings.Contains(l, open.ID) {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "Raise api replicas") || !strings.Contains(line, "working") {
		t.Fatalf("no line for the open change in the first message: %q", first)
	}
	if strings.Contains(first, gone.ID) {
		t.Fatalf("the abandoned change is in the first message: %q", first)
	}
}
