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

// SPEC 0.5 V0.39: the deploy's progress is in the state (a reloaded page sees it at
// once) and on the stream; a finished one fades from the state after a while.
func TestDeployProgressInState(t *testing.T) {
	hs, e := newServer(t)
	e.SetDeploy(map[string]any{"phase": "drain", "blocking": []string{"t_x"}}, "drain")
	r := do(t, "GET", hs.URL+"/api/state", "", nil, "")
	var s struct {
		Deploy map[string]any `json:"deploy"`
	}
	json.NewDecoder(r.Body).Decode(&s)
	if s.Deploy["phase"] != "drain" {
		t.Fatalf("deploy %v", s.Deploy)
	}
	e.SetDeploy(map[string]any{"phase": "done"}, "done")
	if e.DeployState(time.Now().Add(3*time.Minute)) != nil {
		t.Fatal("a finished deploy stays in the state forever")
	}
	if e.DeployState(time.Now()) == nil {
		t.Fatal("a just-finished deploy is gone at once")
	}
}

// V0.state-paging: GET /api/state?limit=N answers the newest N threads with a
// cursor, and ?before=CURSOR the next older page, so a client that was away for
// long shows the live threads first and backfills the rest.
func TestStatePaging(t *testing.T) {
	hs, _ := newServer(t)
	for i := 0; i < 5; i++ {
		r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, fmt.Sprintf(`{"commandId":"p%d","action":"thread.create","args":{"cwd":"/tmp","text":"t%d"}}`, i, i))
		r.Body.Close()
	}
	type page struct {
		Threads []struct {
			ID  string `json:"id"`
			Rev int64  `json:"rev"`
		} `json:"threads"`
		Partial bool   `json:"partial"`
		More    bool   `json:"more"`
		Cursor  string `json:"cursor"`
	}
	get := func(q string) page {
		r := do(t, "GET", hs.URL+"/api/state"+q, "", nil, "")
		var p page
		json.NewDecoder(r.Body).Decode(&p)
		return p
	}
	seen := map[string]bool{}
	var last int64 = 1<<63 - 1
	q := "?limit=2"
	for i := 0; ; i++ {
		p := get(q)
		if i > 5 {
			t.Fatal("paging does not end")
		}
		for _, th := range p.Threads {
			if seen[th.ID] || th.Rev >= last {
				t.Fatalf("page %d: %s repeated or out of order", i, th.ID)
			}
			seen[th.ID], last = true, th.Rev
		}
		if !p.More {
			if p.Cursor != "" && len(p.Threads) == 0 {
				t.Fatal("a last empty page has a cursor")
			}
			break
		}
		if len(p.Threads) != 2 || p.Cursor == "" {
			t.Fatalf("page %d: %d threads, cursor %q", i, len(p.Threads), p.Cursor)
		}
		q = "?limit=2&before=" + p.Cursor
	}
	if len(seen) != 5 {
		t.Fatalf("the pages held %d of 5 threads", len(seen))
	}
}
