package engine

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/store"
)

// toolSchemas are the listed tools' input schemas, by name.
func toolSchemas(t *testing.T, e *Engine, as string) map[string]map[string]any {
	t.Helper()
	srv := mcp.New(func(string) (string, bool) { return as, true })
	e.Tools(srv)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var out struct {
		Result struct {
			Tools []struct {
				Name        string
				InputSchema map[string]any
			}
		}
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	m := map[string]map[string]any{}
	for _, x := range out.Result.Tools {
		m[x.Name] = x.InputSchema
	}
	return m
}

// callRaw calls a tool and reports whether the server knew it.
func callRaw(t *testing.T, e *Engine, as, tool string) bool {
	t.Helper()
	srv := mcp.New(func(string) (string, bool) { return as, true })
	e.Tools(srv)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": map[string]any{"change": "x"}}})
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var out struct {
		Result *struct{ Content []struct{ Text string } }
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Result != nil && len(out.Result.Content) > 0 && !strings.Contains(strings.ToLower(out.Result.Content[0].Text), "unknown tool")
}

func enumOf(schema map[string]any, prop string) []string {
	props, _ := schema["properties"].(map[string]any)
	p, _ := props[prop].(map[string]any)
	raw, _ := p["enum"].([]any)
	var out []string
	for _, x := range raw {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// V0.changes-code-only (3), (5): change_open takes code only, change_request
// merge only; a kind or an ask that went answers with one line that names
// its replacement; the four aliases are gone.
func TestChangeToolsCodeOnly(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	idle(h)
	call := func(tool string, a map[string]any) (string, bool) { return callAs(t, h.e, id, tool, a) }

	for kind, want := range map[string]string{"infra": "run_request", "host": "run_request", "data": "run_request", "doc": "ask", "design": "ask", "other": "show"} {
		out, isErr := call("change_open", map[string]any{"key": "k:" + kind, "kind": kind, "title": "X"})
		if !isErr || !strings.HasPrefix(out, core.CodeOnlyReason) || !strings.Contains(out, want) || strings.Contains(out, "\n") {
			t.Fatalf("%s: %q", kind, out)
		}
	}
	if out, isErr := call("change_open", map[string]any{"key": "k", "kind": "code", "title": "X"}); !isErr || !strings.Contains(out, "branch") {
		t.Fatalf("a code change with no branch or PR: %q", out)
	}
	out, isErr := call("change_open", map[string]any{"key": "api:retry", "title": "Retry", "branch": "feat/retry"})
	if isErr || !strings.Contains(out, "Opened change c_") {
		t.Fatalf("open: %q", out)
	}
	if c := changeOf(h, id, "api:retry"); c == nil || c.Kind != core.KindCode {
		t.Fatalf("no kind is code: %+v", c)
	}
	for ask, want := range map[string]string{"apply": "run_request", "deploy": "fyr.deploy", "revert": "run_request", "review": "ask", "approve": "ask"} {
		out, isErr := call("change_request", map[string]any{"change": "api:retry", "ask": ask})
		if !isErr || !strings.HasPrefix(out, core.CodeOnlyReason) || !strings.Contains(out, want) {
			t.Fatalf("%s: %q", ask, out)
		}
	}
	if th, _ := h.e.Thread(id); len(th.Decisions) != 0 {
		t.Fatalf("a refused ask opened a decision: %+v", th.Decisions)
	}

	schemas := toolSchemas(t, h.e, id)
	if got := enumOf(schemas["change_open"], "kind"); !slices.Equal(got, []string{"code"}) {
		t.Fatalf("change_open kinds: %v", got)
	}
	if got := enumOf(schemas["change_request"], "ask"); !slices.Equal(got, []string{"merge"}) {
		t.Fatalf("change_request asks: %v", got)
	}
	for _, n := range []string{"change_revise", "change_status", "change_attach", "change_unchanged"} {
		if _, listed := schemas[n]; listed {
			t.Errorf("%s is listed", n)
		}
		if callRaw(t, h.e, id, n) {
			t.Errorf("%s still answers", n)
		}
	}
	if _, listed := schemas["effect_request"]; !listed {
		t.Error("effect_request went")
	}
	for _, d := range []string{ChangeOpenDesc, ChangeRequestDesc} {
		if strings.Contains(d, "document or design finishes") || strings.Contains(d, "cluster, cloud") {
			t.Errorf("a description still offers a change that is not code: %s", d)
		}
	}
}

// V0.changes-code-only (4): at load, a stored change that is not code and
// has not ended ends as abandoned, under Done here; its open ask is closed
// and the agent hears it once; the pass is stored.
func TestLoadEndsChangesThatAreNotCode(t *testing.T) {
	_, st, _ := bare(t)
	was := time.Now().Add(-time.Hour)
	th := &core.Thread{ID: "t_old", Cwd: "/w", ActivityAt: was, Changes: []core.Change{
		{ID: "c_infra", Key: "k8s:prod/api", Kind: core.KindInfra, Title: "Raise replicas", Phase: core.PhaseReview, PhaseBy: core.ByAgent, UpdatedAt: was},
		{ID: "c_code", Key: "api:retry", Kind: core.KindCode, Title: "Retry", Branch: "feat/retry", Phase: core.PhaseWorking, PhaseBy: core.ByAgent, UpdatedAt: was},
	}, Decisions: []core.Decision{
		{ID: "d_apply", Kind: core.DecisionEffect, Assignee: core.Owner, CreatedAt: was,
			Effect: &core.EffectReq{Action: "change.apply", Args: map[string]any{"change": "c_infra", "title": "Raise replicas"}, Reason: "scale"}},
	}}
	if _, err := st.Write(func(tx *store.Tx) error { return tx.PutThread(th) }); err != nil {
		t.Fatal(err)
	}
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	got := e.threads["t_old"]
	c := core.FindChange(got, "c_infra")
	if c.Phase != core.PhaseAbandoned || c.Label != core.CodeOnlyReason || !c.IsDone() {
		t.Fatalf("not ended: %+v", c)
	}
	if c := core.FindChange(got, "c_code"); c == nil || c.Phase != core.PhaseWorking || c.IsDone() {
		t.Fatalf("a code change was touched: %+v", c)
	}
	if len(got.Decisions) != 0 || len(got.Feed.Queue) != 1 || !strings.Contains(got.Feed.Queue[0].What, "run_request") {
		t.Fatalf("ask %+v notices %+v", got.Decisions, got.Feed.Queue)
	}
	if !got.ActivityAt.Equal(was) {
		t.Fatal("the pass moved the thread")
	}
	ts, _ := st.Threads()
	for _, x := range ts {
		if x.ID == "t_old" && core.FindChange(x, "c_infra").Phase != core.PhaseAbandoned {
			t.Fatal("the pass is not stored")
		}
	}
	// A second load says nothing again.
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	if q := e2.threads["t_old"].Feed.Queue; len(q) != 1 {
		t.Fatalf("the agent hears it twice: %+v", q)
	}
}
