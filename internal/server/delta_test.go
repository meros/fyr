package server

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// SPEC 0.5 V0.38: after the first full thread, the stream sends thread.patch
// against what this connection already has; GET /api/threads/{id} gives the
// whole thread when a client's copy does not match the patch's base.
func TestStreamSendsThreadPatches(t *testing.T) {
	hs, _ := newServer(t)
	resp := do(t, "GET", hs.URL+"/api/stream", "", nil, "")
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	next := func(want ...string) (string, map[string]any) {
		deadline := time.Now().Add(3 * time.Second)
		var ev, data string
		for time.Now().Before(deadline) {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				ev = line[7:]
			case strings.HasPrefix(line, "data: "):
				data = line[6:]
			case line == "" && ev != "":
				for _, w := range want {
					if ev == w {
						var m map[string]any
						json.Unmarshal([]byte(data), &m)
						return ev, m
					}
				}
				ev, data = "", ""
			}
		}
		t.Fatalf("no %v", want)
		return "", nil
	}
	next("hello")
	post := func(body string) {
		do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, body)
	}
	go post(`{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	ev, full := next("thread", "thread.patch")
	if ev != "thread" {
		t.Fatalf("first send is %s, want the whole thread", ev)
	}
	id, _ := full["id"].(string)
	rev := full["rev"]
	time.Sleep(150 * time.Millisecond)
	go post(`{"commandId":"c2","action":"thread.rename","threadId":"` + id + `","args":{"title":"Renamed"}}`)
	ev, p := next("thread", "thread.patch")
	for ev == "thread.patch" && p["patch"].(map[string]any)["title"] == nil {
		ev, p = next("thread", "thread.patch") // an earlier change of the same thread
	}
	if ev != "thread.patch" || p["id"] != id || p["base"] == nil {
		t.Fatalf("got %s %v", ev, p)
	}
	if patch := p["patch"].(map[string]any); patch["title"] != "Renamed" || len(patch) > 6 {
		t.Fatalf("patch %v", patch)
	}
	_ = rev
	g := do(t, "GET", hs.URL+"/api/threads/"+id, "", nil, "")
	var whole map[string]any
	json.NewDecoder(g.Body).Decode(&whole)
	if g.StatusCode != 200 || whole["title"] != "Renamed" || whole["attention"] == nil {
		t.Fatalf("GET thread: %d %v", g.StatusCode, whole["title"])
	}
}
