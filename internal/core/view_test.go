package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func js(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return v
}

// SPEC 0.5 V0.37: view_state applies an RFC 7396 merge patch.
func TestMergePatch(t *testing.T) {
	cases := []struct{ target, patch, want string }{
		{`{"a":"b"}`, `{"a":"c"}`, `{"a":"c"}`},
		{`{"a":"b"}`, `{"b":"c"}`, `{"a":"b","b":"c"}`},
		{`{"a":"b"}`, `{"a":null}`, `{}`},
		{`{"a":"b","b":"c"}`, `{"a":null}`, `{"b":"c"}`},
		{`{"a":["b"]}`, `{"a":"c"}`, `{"a":"c"}`},
		{`{"a":"c"}`, `{"a":["b"]}`, `{"a":["b"]}`},
		{`{"a":[1,2,3]}`, `{"a":[4]}`, `{"a":[4]}`},
		{`{"a":{"b":"c"}}`, `{"a":{"b":"d","c":null}}`, `{"a":{"b":"d"}}`},
		{`{"a":[{"b":"c"}]}`, `{"a":[1]}`, `{"a":[1]}`},
		{`{"e":null}`, `{"a":1}`, `{"e":null,"a":1}`},
		{`{}`, `{"a":{"bb":{"ccc":null}}}`, `{"a":{"bb":{}}}`},
		{`null`, `{"a":1}`, `{"a":1}`},
	}
	for _, c := range cases {
		got := MergePatch(js(c.target), js(c.patch))
		if !reflect.DeepEqual(got, js(c.want)) {
			t.Errorf("%s + %s = %v, want %s", c.target, c.patch, got, c.want)
		}
	}
	// The target is never changed in place: an event keeps its state.
	tgt := js(`{"a":{"b":1}}`)
	MergePatch(tgt, js(`{"a":{"b":2}}`))
	if !reflect.DeepEqual(tgt, js(`{"a":{"b":1}}`)) {
		t.Errorf("target changed: %v", tgt)
	}
}

// SPEC 0.5 V0.37: the Stop hook blocks once per run, only for a flagged
// thread whose run did not touch the view, and never while agents run.
func TestViewStopDecision(t *testing.T) {
	cases := []struct {
		name string
		in   ViewStop
		want bool
	}{
		{"flag on, untouched", ViewStop{Flag: true}, true},
		{"flag off", ViewStop{}, false},
		{"view call this run", ViewStop{Flag: true, Touched: true}, false},
		{"already blocked once", ViewStop{Flag: true, Blocked: true}, false},
		{"stop_hook_active", ViewStop{Flag: true, Active: true}, false},
		{"background task running", ViewStop{Flag: true, Tasks: 1}, false},
	}
	for _, c := range cases {
		if got := c.in.Block(); got != c.want {
			t.Errorf("%s: Block() = %v, want %v", c.name, got, c.want)
		}
	}
}

// SPEC 0.5 V0.37: the DTO's view folds from the events; the headline is
// state.headline when it is a string.
func TestViewFold(t *testing.T) {
	start := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	th := &Thread{LiveView: true}
	RunStarted(th, start)
	if ViewTouchedThisRun(th) {
		t.Fatal("touched before any call")
	}
	ViewPublished(th, "a1", map[string]any{"headline": "Tests run\nmore"}, start.Add(time.Second))
	if th.Live.PageID != "a1" || th.Live.Headline != "Tests run" || !ViewTouchedThisRun(th) {
		t.Fatalf("published: %+v", th.Live)
	}
	ViewStateSet(th, map[string]any{"headline": 3}, start.Add(2*time.Second))
	if th.Live.Headline != "" || th.Live.PageID != "a1" || !th.Live.UpdatedAt.Equal(start.Add(2*time.Second)) {
		t.Fatalf("state: %+v", th.Live)
	}
	RunEnded(th, nil, start.Add(3*time.Second))
	RunStarted(th, start.Add(4*time.Second))
	if ViewTouchedThisRun(th) {
		t.Fatal("a new run starts untouched")
	}
	ViewChecked(th, start.Add(5*time.Second))
	if !ViewTouchedThisRun(th) || !th.Live.UpdatedAt.Equal(start.Add(2*time.Second)) {
		t.Fatalf("unchanged: %+v", th.Live)
	}
}

// SPEC 0.5 V0.52: a running turn with a live view gets a reminder after
// ViewNudgeAfter without a view_* call, and at most one per interval.
func TestViewNudgeDue(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	n := ViewNudge{Flag: true, RunStarted: t0}
	if n.Due(t0.Add(ViewNudgeAfter - time.Second)) {
		t.Fatal("due before the interval")
	}
	if !n.Due(t0.Add(ViewNudgeAfter)) {
		t.Fatal("not due after the interval with no view call")
	}
	n.Checked = t0.Add(3 * time.Minute)
	if n.Due(t0.Add(ViewNudgeAfter + time.Minute)) {
		t.Fatal("due although the view changed 2 minutes ago")
	}
	n.Checked, n.Nudged = time.Time{}, t0.Add(ViewNudgeAfter)
	if n.Due(t0.Add(ViewNudgeAfter + time.Minute)) {
		t.Fatal("a second reminder within the interval")
	}
	if (ViewNudge{RunStarted: t0}).Due(t0.Add(time.Hour)) {
		t.Fatal("due without a live view")
	}
	if (ViewNudge{Flag: true}).Due(t0) {
		t.Fatal("due outside a run")
	}
}
