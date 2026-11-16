package front

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/meros/fyr/internal/peers"
)

// SPEC 0.5 V0.123 (decision 0025): a host that discovery found is followed
// by the front exactly as a configured one: its threads join the state, its
// row in hosts[] says found; the client keeps its one uplink. The front
// answers /.well-known/fyr, by which other installs find it.
func TestFrontFollowsAFoundHost(t *testing.T) {
	a := newHost(t, "laptop")
	b := newHost(t, "server")
	f := New(Options{Name: "front", MinBackoff: 50 * time.Millisecond}, []HostConfig{{Name: "laptop", URL: a.hs.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	fs := httptest.NewServer(f.Handler())
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close() })
	online(t, fs.URL, 1)
	_, rep := post(t, b.hs.URL+"/api/command", `{"commandId":"c-found","action":"thread.create","args":{"cwd":"/tmp","text":"on the server"}}`)
	id, _ := rep["threadId"].(string)
	if id == "" {
		t.Fatalf("create on b: %v", rep)
	}
	if !f.AddHost(HostConfig{Name: "server", URL: b.hs.URL, Remote: true, Found: true}) {
		t.Fatal("a new found host was not added")
	}
	if f.AddHost(HostConfig{Name: "server", URL: b.hs.URL, Found: true}) || f.AddHost(HostConfig{Name: "laptop", URL: "http://elsewhere:1", Found: true}) {
		t.Fatal("a host already followed was added again")
	}
	online(t, fs.URL, 2)
	eventually(t, "the found host's thread in the state", func() bool { return threadOf(getState(t, fs.URL), id) != nil })
	st := getState(t, fs.URL)
	if th := threadOf(st, id); th["host"] != "server" {
		t.Fatalf("thread host %v", th["host"])
	}
	found := map[string]bool{}
	for _, h := range st.Hosts {
		found[h["name"].(string)] = h["found"] == true
	}
	if !found["server"] || found["laptop"] {
		t.Fatalf("found flags %v", found)
	}
	// A command for the found host's thread goes to it.
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c-ren-found","action":"thread.rename","threadId":"`+id+`","args":{"title":"B"}}`); code != 200 {
		t.Fatalf("command via front: %d %v", code, rep)
	}
}

func TestFrontAnswersWellKnown(t *testing.T) {
	f := New(Options{Name: "front", WellKnown: func() peers.WellKnown {
		return peers.WellKnown{Fyr: 1, Name: "laptop", Role: "both", Owner: "", Host: "http://100.64.0.1:7778"}
	}}, nil)
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/.well-known/fyr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var w map[string]any
	json.NewDecoder(resp.Body).Decode(&w)
	if w["fyr"] != float64(1) || w["name"] != "laptop" || w["host"] != "http://100.64.0.1:7778" {
		t.Fatalf("%v", w)
	}
}
