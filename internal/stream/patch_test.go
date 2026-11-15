package stream

import (
	"encoding/json"
	"reflect"
	"testing"
)

// SPEC 0.5 V0.38: a thread update on the stream is a JSON Merge Patch (RFC 7396)
// against what this connection got before; applying it gives the new DTO exactly.
func TestMergePatchDiff(t *testing.T) {
	old := map[string]any{"id": "t1", "rev": 1.0, "title": "a", "running": true, "activity": "Reading x", "tasks": []any{map[string]any{"id": "k", "status": "running"}}, "gone": "x"}
	neu := map[string]any{"id": "t1", "rev": 2.0, "title": "a", "running": false, "tasks": []any{map[string]any{"id": "k", "status": "completed"}}, "new": map[string]any{"a": 1.0}}
	p := MergePatchDiff(old, neu)
	want := map[string]any{"rev": 2.0, "running": false, "activity": nil, "tasks": []any{map[string]any{"id": "k", "status": "completed"}}, "gone": nil, "new": map[string]any{"a": 1.0}}
	if !reflect.DeepEqual(p, want) {
		b, _ := json.Marshal(p)
		t.Fatalf("patch %s", b)
	}
	got := ApplyMergePatch(old, p)
	if !reflect.DeepEqual(got, neu) {
		t.Fatalf("applied %v", got)
	}
	// Nested objects patch field by field.
	p2 := MergePatchDiff(map[string]any{"pr": map[string]any{"state": "OPEN", "checks": "pending"}}, map[string]any{"pr": map[string]any{"state": "OPEN", "checks": "pass"}})
	if !reflect.DeepEqual(p2, map[string]any{"pr": map[string]any{"checks": "pass"}}) {
		t.Fatalf("nested %v", p2)
	}
}
