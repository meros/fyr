package authtest_test

// Decision 0031 test 1, the neutrality test at the API level (slice 1): two
// scratch fyrds, role both, no FYR_* in the environment, paired, each following
// the other. One script drives the same flows through a front's /api/front and
// /api/h/<id> routes, once through A's front and once through B's, each on a
// fresh pair with fresh data. What the hosts hold and what the client would
// show (the front's own identity and the links left out) must be equal.
// Slice 4 runs the same script through the browser.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/authtest"
)

type neutralHost struct{ ID, Name string }

// neutralFlow drives one front and returns what it saw, as plain values. Ids a
// host minted are replaced by their place in the script.
func neutralFlow(t *testing.T, n *authtest.MeshNode, cookie string) map[string]any {
	t.Helper()
	call := func(method, path, body string, hdr map[string]string) (int, []byte) {
		h := map[string]string{"Content-Type": "application/json"}
		for k, v := range hdr {
			h[k] = v
		}
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		r, err := authtest.Do(n.Fyrd.URL(), authtest.Request{Method: method, Path: path, Cookie: cookie, Header: h, Body: rd})
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return r.Status, r.Body
	}
	out := map[string]any{}

	// 1. The hosts, by install id; the front's own identity is not part of the result.
	hostsNow := func() (hs []neutralHost, online map[string]bool) {
		code, b := call("GET", "/api/front/state", "", nil)
		if code != 200 {
			t.Fatalf("front state: %d %s", code, b)
		}
		var st struct {
			Front struct{ ID, Name string }
			Hosts []struct {
				ID, Name string
				Online   bool
				Link     struct{ State string }
			}
		}
		json.Unmarshal(b, &st)
		online = map[string]bool{}
		for _, h := range st.Hosts {
			hs = append(hs, neutralHost{h.ID, h.Name})
			online[h.Name] = h.Online && h.Link.State == "up"
		}
		return hs, online
	}
	var hosts []neutralHost
	var on map[string]bool
	for end := time.Now().Add(40 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		hosts, on = hostsNow()
		if len(hosts) == 2 && on["a"] && on["b"] && hosts[0].ID != "" && hosts[1].ID != "" {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("hosts not both up: %v %v\n%s", hosts, on, n.Fyrd.Log())
		}
	}
	byName := map[string]string{}
	var names []string
	for _, h := range hosts {
		byName[h.Name] = h.ID
		names = append(names, h.Name)
	}
	sort.Strings(names)
	out["hosts"] = names
	out["firstHostIsNotSpecial"] = true

	// 2. Per host: a project, a thread with an attachment, its events, the
	//    Changes tab, a settings page, a host that does not exist.
	for _, name := range names {
		id := byName[name]
		pfx := "/api/h/" + id
		dir := t.TempDir()
		code, b := call("POST", pfx+"/command", fmt.Sprintf(`{"commandId":"proj-%s","action":"project.create","args":{"name":"P-%s","folder":%q}}`, name, name, dir), nil)
		out[name+" project.create"] = code
		_ = b
		code, b = call("GET", pfx+"/state", "", nil)
		var st struct {
			HostID   string
			Projects []struct{ ID, Name string }
		}
		json.Unmarshal(b, &st)
		var pnames []string
		var projectID string
		for _, p := range st.Projects {
			pnames = append(pnames, p.Name)
			projectID = p.ID
		}
		out[name+" state"] = []any{code, st.HostID == id, pnames}

		// an attachment, sent to this host and read back
		code, b = call("POST", pfx+"/attachments", "neutral "+name, map[string]string{"X-Fyr-Filename": "n.txt", "Content-Type": "text/plain"})
		var att struct{ ID string }
		json.Unmarshal(b, &att)
		code2, got := call("GET", pfx+"/attachments/"+att.ID, "", nil)
		out[name+" attachment"] = []any{code, code2, string(got)}

		code, b = call("POST", pfx+"/command", fmt.Sprintf(`{"commandId":"th-%s","action":"thread.create","args":{"projectId":%q,"text":"hello from the script","attachments":[%q]}}`, name, projectID, att.ID), nil)
		var rep struct {
			OK       bool
			ThreadID string
		}
		json.Unmarshal(b, &rep)
		out[name+" thread.create"] = []any{code, rep.OK, rep.ThreadID != ""}
		code, b = call("GET", pfx+"/threads/"+rep.ThreadID+"/events?limit=50", "", nil)
		var ev struct {
			Events []struct {
				Type string
				Data map[string]any
			}
		}
		json.Unmarshal(b, &ev)
		first := []any{}
		if len(ev.Events) > 0 {
			first = []any{ev.Events[0].Type, ev.Events[0].Data["text"]}
		}
		out[name+" events"] = []any{code, first}
		code, _ = call("GET", pfx+"/threads/"+rep.ThreadID+"/changes", "", nil)
		out[name+" changes"] = code
		code, b = call("GET", pfx+"/providers", "", nil)
		out[name+" settings"] = []any{code, json.Valid(b)}
		// the thread of this host is not on the other, by the path alone
		other := byName[names[0]]
		if other == id {
			other = byName[names[1]]
		}
		code, b = call("GET", "/api/h/"+other+"/threads/"+rep.ThreadID+"/events", "", nil)
		var oev struct{ Events []any }
		json.Unmarshal(b, &oev)
		out[name+" thread on the other host"] = len(oev.Events) // none: its events are on its own host
	}
	code, b := call("GET", "/api/h/ZZZZ-9999-ZZZZ-9999/state", "", nil)
	var nh struct{ Code string }
	json.Unmarshal(b, &nh)
	out["unknown host"] = []any{code, nh.Code}
	code, _ = call("GET", "/api/state", "", nil) // an old route: 404 (V0.old-routes-end), through either front
	out["old route"] = code

	// 3. Reorder the hosts: the order in the front's state is the order the owner set.
	before := make([]string, len(hosts))
	for i, h := range hosts {
		before[i] = h.ID
	}
	rev := []string{before[1], before[0]}
	code, b = call("POST", "/api/front/command", fmt.Sprintf(`{"commandId":"ord-1","action":"front.hostOrder","args":{"ids":[%q,%q]}}`, rev[0], rev[1]), nil)
	out["hostOrder"] = code
	after, _ := hostsNow()
	out["reordered"] = len(after) == 2 && after[0].ID == rev[0] && after[1].ID == rev[1]
	return out
}

func TestNeutralityThroughEitherFront(t *testing.T) {
	if testing.Short() {
		t.Skip("two pairs of real fyrds")
	}
	run := func(through string) map[string]any {
		m, err := authtest.NewMesh(authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "FYR_") {
				t.Fatalf("the environment carries %s", e)
			}
		}
		a, b := m.Node("a"), m.Node("b")
		waitOthers(t, a, "b")
		waitOthers(t, b, "a")
		n := m.Node(through)
		return neutralFlow(t, n, ownerFronts(t, n))
	}
	viaA := run("a")
	viaB := run("b")
	if !reflect.DeepEqual(viaA, viaB) {
		ja, _ := json.MarshalIndent(viaA, "", " ")
		jb, _ := json.MarshalIndent(viaB, "", " ")
		t.Errorf("the two fronts give different results.\nthrough A:\n%s\nthrough B:\n%s", ja, jb)
	}
	// and the script saw what it should have: a project and a thread on each host
	for _, name := range []string{"a", "b"} {
		if viaA[name+" project.create"] != 200 {
			t.Errorf("project on %s: %v", name, viaA[name+" project.create"])
		}
		if tc, _ := viaA[name+" thread.create"].([]any); len(tc) != 3 || tc[0] != 200 || tc[1] != true || tc[2] != true {
			t.Errorf("thread on %s: %v", name, viaA[name+" thread.create"])
		}
		if viaA[name+" thread on the other host"] != 0 {
			t.Errorf("a thread of %s answered through the other host", name)
		}
	}
}
