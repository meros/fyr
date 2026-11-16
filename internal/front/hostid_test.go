package front

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// hostWithID is a real host that reports the key id given (V0.124).
func hostWithID(t *testing.T, name, id string) *httptest.Server {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &server.Server{Engine: engine.New(st), BuildID: "host-build", Host: name, ID: id}
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(func() { hs.CloseClientConnections(); hs.Close(); st.Close() })
	return hs
}

func hostRow(t *testing.T, base, name string) map[string]any {
	for _, h := range getState(t, base).Hosts {
		if h["name"] == name {
			return h
		}
	}
	return nil
}

// SPEC 0.5 V0.124 (decision 0026, slice 1): a host reports its key id; the
// front shows it in hosts[], pins the first one it sees, says so when the
// host under that name later reports another, and keeps following it. The
// owner trusts the new id with front.hostId; the pin survives a restart.
func TestFrontPinsHostIDs(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "front.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old := hostWithID(t, "a", "AAAA-AAAA-AAAA-AAAA")
	f := startFront(t, Options{Store: st, ID: "FFFF-FFFF-FFFF-FFFF"}, HostConfig{Name: "a", URL: old.URL})
	eventually(t, "the id in hosts[]", func() bool {
		h := hostRow(t, f.URL, "a")
		return h != nil && h["id"] == "AAAA-AAAA-AAAA-AAAA" && h["idWas"] == nil
	})
	resp, _ := http.Get(f.URL + "/api/state")
	var top struct {
		HostID string `json:"hostId"`
	}
	decodeJSON(t, resp, &top)
	if top.HostID != "FFFF-FFFF-FFFF-FFFF" {
		t.Fatalf("the front's own id: %q", top.HostID)
	}
	f.stop()

	// Another install under the same name: shown, still followed.
	other := hostWithID(t, "a", "BBBB-BBBB-BBBB-BBBB")
	g := startFront(t, Options{Store: st}, HostConfig{Name: "a", URL: other.URL})
	eventually(t, "the changed id", func() bool {
		h := hostRow(t, g.URL, "a")
		return h != nil && h["online"] == true && h["id"] == "BBBB-BBBB-BBBB-BBBB" && h["idWas"] == "AAAA-AAAA-AAAA-AAAA"
	})

	// The stream tells the client when the owner trusts it.
	req, _ := http.NewRequest("GET", g.URL+"/api/stream", nil)
	sresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	events := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(sresp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		ev := ""
		for sc.Scan() {
			line := sc.Text()
			if e, ok := strings.CutPrefix(line, "event: "); ok {
				ev = e
			} else if d, ok := strings.CutPrefix(line, "data: "); ok && ev == "hosts" {
				events <- d
			}
		}
	}()

	if code, rep := post(t, g.URL+"/api/command", `{"commandId":"h1","action":"front.hostId","args":{"host":"nope"}}`); code != 400 {
		t.Fatalf("unknown host: %d %v", code, rep)
	}
	code, rep := post(t, g.URL+"/api/command", `{"commandId":"h2","action":"front.hostId","args":{"host":"a"}}`)
	if code != 200 || rep["ok"] != true {
		t.Fatalf("%d %v", code, rep)
	}
	if h := hostRow(t, g.URL, "a"); h["idWas"] != nil || h["id"] != "BBBB-BBBB-BBBB-BBBB" {
		t.Fatalf("after trust: %v", h)
	}
	select {
	case d := <-waitFor(events, `"idWas"`, false):
		if !strings.Contains(d, "BBBB-BBBB-BBBB-BBBB") {
			t.Fatalf("hosts event %s", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no hosts event after the trust")
	}
	g.stop()

	h := startFront(t, Options{Store: st}, HostConfig{Name: "a", URL: other.URL})
	eventually(t, "the trusted pin after a restart", func() bool {
		r := hostRow(t, h.URL, "a")
		return r != nil && r["online"] == true && r["id"] == "BBBB-BBBB-BBBB-BBBB" && r["idWas"] == nil
	})
}

// An offline host keeps its pinned id on its row; a host older than V0.124
// reports none and the row has none.
func TestFrontHostIDOfflineAndOld(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "front.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := hostWithID(t, "a", "AAAA-AAAA-AAAA-AAAA")
	f := startFront(t, Options{Store: st}, HostConfig{Name: "a", URL: a.URL})
	eventually(t, "online", func() bool { r := hostRow(t, f.URL, "a"); return r != nil && r["id"] != nil })
	f.stop()
	a.Close()
	g := startFront(t, Options{Store: st}, HostConfig{Name: "a", URL: a.URL})
	if r := hostRow(t, g.URL, "a"); r == nil || r["online"] == true || r["id"] != "AAAA-AAAA-AAAA-AAAA" {
		t.Fatalf("offline row %v", r)
	}
	if code, _ := post(t, g.URL+"/api/command", `{"action":"front.hostId","args":{"host":"a"}}`); code != 409 {
		t.Fatalf("trusting an id never reported: %d", code)
	}
	old := hostWithID(t, "old", "")
	k := startFront(t, Options{}, HostConfig{Name: "old", URL: old.URL})
	eventually(t, "online", func() bool { r := hostRow(t, k.URL, "old"); return r != nil && r["online"] == true })
	if r := hostRow(t, k.URL, "old"); r["id"] != nil {
		t.Fatalf("an id for a host that sent none: %v", r)
	}
}

// waitFor passes on the first event that has (or, with want false, lacks) sub.
func waitFor(in <-chan string, sub string, want bool) <-chan string {
	out := make(chan string, 1)
	go func() {
		for d := range in {
			if strings.Contains(d, sub) == want {
				out <- d
				return
			}
		}
	}()
	return out
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}
