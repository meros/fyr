package core

import (
	"encoding/json"
	"reflect"
	"strings"
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

// SPEC 0.5 V0.37: the DTO's view folds from the events; the headline is
// state.headline when it is a string.
func TestViewFold(t *testing.T) {
	start := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	th := &Thread{Live: &LiveView{Enabled: true}}
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

// SPEC 0.5 V0.37: view_state needs a page first.
func TestViewStateNeedsAPage(t *testing.T) {
	th := &Thread{ID: "t", Live: &LiveView{Enabled: true}}
	_, _, _, err := Reduce(th, nil, Input{Source: FromAgent, Kind: KindViewState, Thread: "t", Args: map[string]any{}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "view_publish first") {
		t.Fatalf("state before any page: %v", err)
	}
}
