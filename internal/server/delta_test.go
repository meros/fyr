package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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

// SPEC 0.5 V0.38: GET /api/state?since=REV lists only threads changed after REV,
// so a client with a stored copy fetches the difference.
func TestStateSince(t *testing.T) {
	hs, _ := newServer(t)
	post := func(body string) map[string]any {
		r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, body)
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		return m
	}
	a := post(`{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"a"}}`)["threadId"].(string)
	post(`{"commandId":"c2","action":"thread.create","args":{"cwd":"/tmp","text":"b"}}`)
	get := func(q string) (threads []map[string]any, partial bool) {
		r := do(t, "GET", hs.URL+"/api/state"+q, "", nil, "")
		var s struct {
			Threads []map[string]any `json:"threads"`
			Partial bool             `json:"partial"`
		}
		json.NewDecoder(r.Body).Decode(&s)
		return s.Threads, s.Partial
	}
	all, partial := get("")
	if len(all) != 2 || partial {
		t.Fatalf("full state: %d threads, partial %v", len(all), partial)
	}
	var maxRev string
	for _, th := range all {
		if r := fmt.Sprint(th["rev"]); r > maxRev {
			maxRev = r
		}
	}
	// Exact digits: the rev is above 2^53.
	r := do(t, "GET", hs.URL+"/api/state", "", nil, "")
	raw, _ := io.ReadAll(r.Body)
	revs := regexp.MustCompile(`"rev":(\d+)`).FindAllStringSubmatch(string(raw), -1)
	max := "0"
	for _, m := range revs {
		if len(m[1]) > len(max) || (len(m[1]) == len(max) && m[1] > max) {
			max = m[1]
		}
	}
	none, partial := get("?since=" + max)
	if len(none) != 0 || !partial {
		t.Fatalf("since the newest rev: %d threads, partial %v", len(none), partial)
	}
	post(`{"commandId":"c3","action":"thread.rename","threadId":"` + a + `","args":{"title":"A2"}}`)
	one, _ := get("?since=" + max)
	if len(one) != 1 || one[0]["title"] != "A2" {
		t.Fatalf("after one change: %v", one)
	}
}
