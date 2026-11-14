package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.38: what the host sends cuts large tool output (4 KB inside a
// subagent, 16 KB in the main thread) and says so; the store keeps it whole.
func TestLeanEventCutsToolOutput(t *testing.T) {
	big := strings.Repeat("x", 40_000)
	mk := func(parent any, content any) store.Event {
		d, _ := json.Marshal(map[string]any{"type": "user", "parent_tool_use_id": parent,
			"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "u1", "content": content}}}})
		return store.Event{Seq: 7, Type: "msg", Data: d}
	}
	for _, c := range []struct {
		name   string
		ev     store.Event
		max    int
		before int
	}{
		{"main, string", mk(nil, big), 16 * 1024, 40_000},
		{"subagent, string", mk("p1", big), 4 * 1024, 40_000},
		{"subagent, text blocks", mk("p1", []any{map[string]any{"type": "text", "text": big}}), 4 * 1024, 40_000},
	} {
		out := Lean(c.ev)
		if len(out.Data) > c.max+600 {
			t.Fatalf("%s: %d bytes sent", c.name, len(out.Data))
		}
		var d map[string]any
		json.Unmarshal(out.Data, &d)
		if d["cut"] != true || !strings.Contains(string(out.Data), "cut") {
			t.Fatalf("%s: not marked as cut: %s", c.name, out.Data[:200])
		}
		if len(c.ev.Data) < c.before {
			t.Fatalf("%s: the original changed", c.name)
		}
	}
	small := mk(nil, "ok")
	if string(Lean(small).Data) != string(small.Data) {
		t.Fatal("a small event changed")
	}
	other := store.Event{Type: "run.ended", Data: json.RawMessage(`{"x":1}`)}
	if string(Lean(other).Data) != `{"x":1}` {
		t.Fatal("a non-message event changed")
	}
}

// SPEC 0.5 V0.38: base64 images in messages (screenshots in tool results) are left out
// on the wire; the conversation does not draw them and the full event still has them.
func TestLeanEventDropsImages(t *testing.T) {
	img := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": strings.Repeat("A", 200_000)}}
	d, _ := json.Marshal(map[string]any{"type": "user", "parent_tool_use_id": nil,
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "u1", "content": []any{img, map[string]any{"type": "text", "text": "ok"}}}}}})
	out := Lean(store.Event{Type: "msg", Data: d})
	if len(out.Data) > 2000 {
		t.Fatalf("%d bytes sent", len(out.Data))
	}
	s := string(out.Data)
	if !strings.Contains(s, `"omitted":true`) || !strings.Contains(s, `"media_type":"image/png"`) || !strings.Contains(s, `"cut":true`) || !strings.Contains(s, `"text":"ok"`) {
		t.Fatalf("lean image event: %s", s)
	}
}
