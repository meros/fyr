package front

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

func orderOf(t *testing.T, base string) []string {
	resp, err := http.Get(base + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s struct {
		ProjectOrder []string `json:"projectOrder"`
	}
	json.NewDecoder(resp.Body).Decode(&s)
	return s.ProjectOrder
}

// V0.121: the front keeps one project order across its hosts. The owner's
// browser sets it with front.projectOrder; it is in the state, survives a
// restart, and reaches no host.
func TestFrontProjectOrder(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "front.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, b := newHost(t, "a"), newHost(t, "b")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	f := startFront(t, Options{Store: st}, hosts...)
	if o := orderOf(t, f.URL); len(o) != 0 {
		t.Fatalf("an order before the owner set one: %v", o)
	}
	code, rep := post(t, f.URL+"/api/command", `{"commandId":"o1","action":"front.projectOrder","args":{"ids":["p3","p1","p3","","p2"]}}`)
	if code != 200 || rep["ok"] != true {
		t.Fatalf("%d %v", code, rep)
	}
	if o := orderOf(t, f.URL); !slices.Equal(o, []string{"p3", "p1", "p2"}) {
		t.Fatalf("order %v", o)
	}
	// A retried commandId does not undo a later order.
	post(t, f.URL+"/api/command", `{"commandId":"o2","action":"front.projectOrder","args":{"ids":["p1"]}}`)
	post(t, f.URL+"/api/command", `{"commandId":"o1","action":"front.projectOrder","args":{"ids":["p3","p1","p2"]}}`)
	if o := orderOf(t, f.URL); !slices.Equal(o, []string{"p1"}) {
		t.Fatalf("a retry replayed: %v", o)
	}
	// Without the browser's header, nothing changes.
	r := do(t, "POST", f.URL+"/api/command", "", map[string]string{"Content-Type": "application/json"}, `{"action":"front.projectOrder","args":{"ids":["evil"]}}`)
	if r.StatusCode != 403 {
		t.Fatalf("no X-Fyr: %d", r.StatusCode)
	}
	if o := orderOf(t, f.URL); !slices.Equal(o, []string{"p1"}) {
		t.Fatalf("order %v", o)
	}
	f.stop()
	g := startFront(t, Options{Store: st}, hosts...)
	if o := orderOf(t, g.URL); !slices.Equal(o, []string{"p1"}) {
		t.Fatalf("after a restart: %v", o)
	}
	// No host and no agent path knows the command: the host engine has no
	// such kind, so an agent's Input or MCP call cannot set it.
	if core.Kinds["front.projectOrder"] != nil {
		t.Fatal("a host kind front.projectOrder exists")
	}
}

type roStore struct{}

func (roStore) GetKV(string) (string, error) { return "", nil }
func (roStore) SetKV(string, string) error   { return errors.New("read-only file system") }

// V0.121: a front that cannot store the order refuses with the reason.
func TestFrontProjectOrderReadOnly(t *testing.T) {
	h := newHost(t, "a")
	f := startFront(t, Options{Store: roStore{}}, HostConfig{Name: "a", URL: h.hs.URL})
	code, rep := post(t, f.URL+"/api/command", `{"commandId":"o1","action":"front.projectOrder","args":{"ids":["p1"]}}`)
	if code == 200 || rep["ok"] == true || !strings.Contains(rep["error"].(string), "read-only") {
		t.Fatalf("%d %v", code, rep)
	}
	if o := orderOf(t, f.URL); len(o) != 0 {
		t.Fatalf("order %v", o)
	}
}
