package front

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/mcp"
)

func getStateRaw(t *testing.T, base string) map[string]any {
	t.Helper()
	resp, err := http.Get(base + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return m
}

func markOf(t *testing.T, base, id string) map[string]any {
	t.Helper()
	ms, _ := getStateRaw(t, base)["hostMarks"].(map[string]any)
	m, _ := ms[id].(map[string]any)
	return m
}

// SPEC V0.171: the owner sets a host's icon and colour on one front, by the
// host's install id; it is stored on every host, every front that follows
// them shows it, and clearing both gives the automatic mark back. A host
// that was offline gets it when it is back.
func TestV171OwnerSetsAHostMark(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa, fb := newFront(t, hosts...), newFront(t, hosts...)
	online(t, fa.URL, 2)
	online(t, fb.URL, 2)
	b.setDown(true)
	eventually(t, "b offline", func() bool { return getState(t, fa.URL).Hosts[1]["online"] == false })
	if code, rep := post(t, fa.URL+"/api/command", `{"commandId":"m1","action":"front.hostMark","args":{"id":"BBBB-0001","icon":"🏠","color":"teal"}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("front.hostMark: %d %v", code, rep)
	}
	if m := a.eng.HostMarks().Marks["BBBB-0001"]; m.Icon != "🏠" || m.Color != "teal" {
		t.Fatalf("host a keeps %+v", m)
	}
	for _, f := range []string{fa.URL, fb.URL} {
		eventually(t, "the mark on "+f, func() bool { m := markOf(t, f, "BBBB-0001"); return m["icon"] == "🏠" && m["color"] == "teal" })
	}
	b.setDown(false)
	eventually(t, "b catches up", func() bool { return b.eng.HostMarks().Marks["BBBB-0001"].Icon == "🏠" })
	// Only the colour: the icon goes back to the automatic one.
	if code, rep := post(t, fb.URL+"/api/command", `{"commandId":"m2","action":"front.hostMark","args":{"id":"BBBB-0001","icon":"","color":"rose"}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("front.hostMark from the other front: %d %v", code, rep)
	}
	eventually(t, "the new colour on fa", func() bool { m := markOf(t, fa.URL, "BBBB-0001"); return m["color"] == "rose" && m["icon"] == nil })
	// Both empty: the automatic mark, kept as the owner's choice (V0.208).
	post(t, fa.URL+"/api/command", `{"commandId":"m3","action":"front.hostMark","args":{"id":"BBBB-0001","icon":"","color":""}}`)
	eventually(t, "cleared", func() bool { m := markOf(t, fb.URL, "BBBB-0001"); return m["icon"] == nil && m["color"] == nil })
	for i, bad := range []string{`{"id":"","color":"teal"}`, `{"id":"X","color":"#f00"}`} {
		if code, _ := post(t, fa.URL+"/api/command", fmt.Sprintf(`{"commandId":"bad%d","action":"front.hostMark","args":%s}`, i, bad)); code == 200 {
			t.Fatalf("taken: %s", bad)
		}
	}
}

// hostSetup calls the agent's tool on a host and returns its answer.
func hostSetup(t *testing.T, e *engine.Engine, args map[string]any) string {
	t.Helper()
	srv := mcp.New(func(string) (string, bool) { return "t_x", true })
	e.Tools(srv)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "host_setup", "arguments": args}})
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var out struct {
		Result struct{ Content []struct{ Text string } }
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Result.Content) == 0 {
		t.Fatalf("no result: %s", w.Body.String())
	}
	return out.Result.Content[0].Text
}

// SPEC V0.208: what a host's agent sets shows on every front as the agent's;
// the owner's change makes a field the owner's, an unchanged field stays the
// agent's, and the agent cannot change the owner's value or a cleared field.
func TestV208AgentFillsAHostMark(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	b.eng.InstallID, b.srv.ID = "BBBB-0001", "BBBB-0001"
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa, fb := newFront(t, hosts...), newFront(t, hosts...)
	online(t, fa.URL, 2)
	online(t, fb.URL, 2)
	by := func(m map[string]any, f string) any { o, _ := m["by"].(map[string]any); return o[f] }
	hostSetup(t, b.eng, map[string]any{"icon": "cloud", "color": "rose"})
	for _, u := range []string{fa.URL, fb.URL} {
		eventually(t, "the agent's mark on "+u, func() bool {
			m := markOf(t, u, "BBBB-0001")
			return m["icon"] == "cloud" && m["color"] == "rose" && by(m, "icon") == "agent" && by(m, "color") == "agent"
		})
	}
	// The owner changes the colour; the page sends the icon unchanged.
	post(t, fa.URL+"/api/command", `{"commandId":"m1","action":"front.hostMark","args":{"id":"BBBB-0001","icon":"cloud","color":"teal"}}`)
	eventually(t, "the owner's colour", func() bool {
		m := markOf(t, fb.URL, "BBBB-0001")
		return m["icon"] == "cloud" && m["color"] == "teal" && by(m, "icon") == "agent" && by(m, "color") == "owner"
	})
	if got := hostSetup(t, b.eng, map[string]any{"icon": "box", "color": "violet"}); !strings.Contains(got, "Kept the colour") || !strings.Contains(got, "icon (box)") {
		t.Fatalf("the tool says %q", got)
	}
	eventually(t, "the agent's new icon, the owner's colour", func() bool {
		m := markOf(t, fa.URL, "BBBB-0001")
		return m["icon"] == "box" && m["color"] == "teal"
	})
	// Automatic is the owner's choice: the agent does not fill it again.
	post(t, fa.URL+"/api/command", `{"commandId":"m2","action":"front.hostMark","args":{"id":"BBBB-0001","icon":"","color":"teal"}}`)
	eventually(t, "automatic icon", func() bool { m := markOf(t, fb.URL, "BBBB-0001"); return m["icon"] == nil && by(m, "icon") == "owner" })
	if got := hostSetup(t, b.eng, map[string]any{"icon": "server"}); !strings.Contains(got, "Kept the icon") {
		t.Fatalf("the tool says %q", got)
	}
	if m := markOf(t, fa.URL, "BBBB-0001"); m["icon"] != nil {
		t.Fatalf("the agent filled a cleared icon: %v", m)
	}
}
