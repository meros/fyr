package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LiveView is a thread's pinned live view (SPEC 0.5 V0.37, decision 0010):
// a page stored once by content hash and a small JSON state the agent
// patches. The client shows the page above the conversation and sends it
// the full state on load and on every change. Enabled is the owner's switch;
// the page, state and actions outlive it.
type LiveView struct {
	PageID    string         `json:"pageId,omitempty"`
	State     map[string]any `json:"state,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
	// CheckedAt is the last view_* call, view_unchanged included: the
	// Stop hook and the client's "not touched this run" look read it.
	CheckedAt time.Time `json:"checkedAt"`
	// Headline is state.headline when it is a string.
	Headline string `json:"headline,omitempty"`
	Enabled  bool   `json:"enabled,omitempty"`
	// Actions are the view's actions and their runs (V0.60 (6), #31); they
	// go when the owner turns the view off.
	Actions
}

// ViewStateMax is the largest view state, as JSON.
const ViewStateMax = 16 << 10

// ViewPublished records a new page and its full state.
func ViewPublished(t *Thread, pageID string, state map[string]any, now time.Time) {
	var on bool
	var acts Actions
	if t.Live != nil {
		on, acts = t.Live.Enabled, t.Live.Actions // the view's actions outlive a new page
	}
	t.Live = &LiveView{Enabled: on, PageID: pageID, State: state, UpdatedAt: now, CheckedAt: now, Headline: headline(state), Actions: acts}
}

// SetView turns the live view on or off; off drops the view's actions
// (V0.60). It reports whether anything changed.
func SetView(t *Thread, on bool) bool {
	if t.ViewOn() == on {
		return false
	}
	if t.Live == nil {
		t.Live = &LiveView{}
	}
	t.Live.Enabled = on
	if !on {
		t.Live.Actions = Actions{}
	}
	return true
}

// ViewStateSet records the full state after a patch.
func ViewStateSet(t *Thread, state map[string]any, now time.Time) {
	if t.Live == nil {
		t.Live = &LiveView{}
	}
	t.Live.State, t.Live.UpdatedAt, t.Live.CheckedAt, t.Live.Headline = state, now, now, headline(state)
}

// ViewChecked records a view_unchanged call: the view is current.
func ViewChecked(t *Thread, now time.Time) {
	if t.Live == nil {
		t.Live = &LiveView{}
	}
	t.Live.CheckedAt = now
}

// ViewTouchedThisRun reports whether a view_* call happened since the
// current run started.
func ViewTouchedThisRun(t *Thread) bool {
	if t.Live == nil || t.Live.CheckedAt.IsZero() || t.RunStartedAt == nil {
		return false
	}
	return !t.Live.CheckedAt.Before(*t.RunStartedAt)
}

func headline(state map[string]any) string {
	s, _ := state["headline"].(string)
	s = strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if r := []rune(s); len(r) > 160 {
		s = string(r[:159]) + "…"
	}
	return s
}

// MergePatch applies an RFC 7396 JSON Merge Patch: an object patch merges
// key by key, null deletes a key, anything else (arrays included) replaces
// the target whole.
func MergePatch(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, ok := target.(map[string]any)
	out := map[string]any{}
	if ok {
		for k, v := range t {
			out[k] = v
		}
	}
	for k, v := range p {
		if v == nil {
			delete(out, k)
			continue
		}
		out[k] = MergePatch(out[k], v)
	}
	return out
}

// StateSize is the JSON size of a view state.
func StateSize(state map[string]any) int {
	b, _ := json.Marshal(state)
	return len(b)
}

// CheckViewState refuses a view state over ViewStateMax.
func CheckViewState(s map[string]any) error {
	if n := StateSize(s); n > ViewStateMax {
		return fmt.Errorf("the view state is %d bytes as JSON; at most %d. Keep details in the thread and only the status in the state", n, ViewStateMax)
	}
	return nil
}
