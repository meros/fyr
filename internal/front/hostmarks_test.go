package front

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
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
	// Both empty: no entry, the automatic mark.
	post(t, fa.URL+"/api/command", `{"commandId":"m3","action":"front.hostMark","args":{"id":"BBBB-0001","icon":"","color":""}}`)
	eventually(t, "cleared", func() bool { return markOf(t, fb.URL, "BBBB-0001") == nil })
	for i, bad := range []string{`{"id":"","color":"teal"}`, `{"id":"X","color":"#f00"}`} {
		if code, _ := post(t, fa.URL+"/api/command", fmt.Sprintf(`{"commandId":"bad%d","action":"front.hostMark","args":%s}`, i, bad)); code == 200 {
			t.Fatalf("taken: %s", bad)
		}
	}
}
