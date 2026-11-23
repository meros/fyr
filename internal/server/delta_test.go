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

// V0.state-index: GET /api/state?index=1 lists every thread as id, rev and
// state and no thread; ?ids=a,b answers those threads only, so a device that
// diffs the index fetches just the threads it lacks or holds out of date.
func TestStateIndex(t *testing.T) {
	hs, _ := newServer(t)
	var made []string
	for i := 0; i < 5; i++ {
		r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, fmt.Sprintf(`{"commandId":"i%d","action":"thread.create","args":{"cwd":"/tmp","text":"t%d"}}`, i, i))
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		made = append(made, m["threadId"].(string))
	}
	type answer struct {
		Threads []struct {
			ID  string `json:"id"`
			Rev int64  `json:"rev"`
		} `json:"threads"`
		Index []struct {
			ID    string `json:"id"`
			Rev   int64  `json:"rev"`
			State string `json:"state"`
		} `json:"index"`
		Partial bool `json:"partial"`
	}
	get := func(q string) answer {
		r := do(t, "GET", hs.URL+"/api/state"+q, "", nil, "")
		var a answer
		json.NewDecoder(r.Body).Decode(&a)
		return a
	}
	idx := get("?index=1")
	if len(idx.Threads) != 0 || len(idx.Index) != 5 || !idx.Partial {
		t.Fatalf("index: %d threads, %d entries, partial %v", len(idx.Threads), len(idx.Index), idx.Partial)
	}
	revs := map[string]int64{}
	for _, e := range idx.Index {
		if e.State == "" || e.Rev == 0 {
			t.Fatalf("entry %+v lacks a state or a rev", e)
		}
		revs[e.ID] = e.Rev
	}
	// The index and the threads agree on every rev: that is what the device diffs.
	got := get("?ids=" + made[3] + "," + made[1] + ",t_nope")
	if len(got.Threads) != 2 || got.Threads[0].ID != made[3] || got.Threads[1].ID != made[1] || !got.Partial {
		t.Fatalf("ids: %+v", got.Threads)
	}
	for _, th := range got.Threads {
		if th.Rev != revs[th.ID] {
			t.Fatalf("%s: thread rev %d, index rev %d", th.ID, th.Rev, revs[th.ID])
		}
	}
	if all := get(""); len(all.Threads) != 5 || all.Partial || all.Index != nil {
		t.Fatalf("whole state: %d threads, partial %v", len(all.Threads), all.Partial)
	}
}
