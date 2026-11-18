package front

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

var probes atomic.Int64

// probe is a fresh command id: a repeated one answers its first reply.
func probe() string { return fmt.Sprintf("probe-%d", probes.Add(1)) }

// projectOn makes a project on the host and returns its ref.
func projectOn(t *testing.T, front string, h *settingsHost, id, name string) string {
	t.Helper()
	code, rep := post(t, front+"/api/h/"+id+"/command", fmt.Sprintf(`{"commandId":"mk-%s-%s","action":"project.create","args":{"name":%q}}`, id, name, name))
	if code != 200 || rep["ok"] != true {
		t.Fatalf("project.create on %s: %d %v", id, code, rep)
	}
	pid, _ := rep["result"].(map[string]any)["id"].(string)
	ref := id + "/" + pid
	eventually(t, "the front has "+name, func() bool { return frontSees(t, front, ref, "") })
	return ref
}

// frontSees reports whether the front's copy of the project has this link.
func frontSees(t *testing.T, front, ref, link string) bool {
	t.Helper()
	resp, err := http.Get(front + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st struct {
		Projects []struct{ ID, Link string }
	}
	json.NewDecoder(resp.Body).Decode(&st)
	_, id, _ := strings.Cut(ref, "/")
	for _, p := range st.Projects {
		if p.ID == id {
			return p.Link == link
		}
	}
	return false
}

func linkOf(h *settingsHost, ref string) string {
	for _, p := range h.eng.Projects() {
		if ref[len(ref)-len(p.ID):] == p.ID {
			return p.Link
		}
	}
	return "?"
}

func linkCmd(t *testing.T, front, id string, refs ...string) (int, map[string]any) {
	t.Helper()
	list := ""
	for i, r := range refs {
		if i > 0 {
			list += ","
		}
		list += fmt.Sprintf("%q", r)
	}
	return post(t, front+"/api/front/command", fmt.Sprintf(`{"commandId":%q,"action":"front.projectLink","args":{"refs":[%s]}}`, id, list))
}

// Decision 0034: a link is stored on each host's own record, by any front,
// one member per host, and only while the hosts can be written.
func TestProjectLink(t *testing.T) {
	a, b := newSettingsHostID(t, "a", "TEST-A"), newSettingsHostID(t, "b", "TEST-B")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa, fb := newFront(t, hosts...), newFront(t, hosts...)
	online(t, fa.URL, 2)
	online(t, fb.URL, 2)
	onA, onB := projectOn(t, fa.URL, a, "TEST-A", "Fyr"), projectOn(t, fa.URL, b, "TEST-B", "fyr-mac")
	second := projectOn(t, fa.URL, a, "TEST-A", "Other")

	if code, rep := linkCmd(t, fa.URL, "same-host", onA, second); code != 400 {
		t.Fatalf("two members on one host: %d %v", code, rep)
	}
	code, rep := linkCmd(t, fa.URL, "l1", onA, onB)
	if code != 200 || rep["ok"] != true {
		t.Fatalf("link: %d %v", code, rep)
	}
	link := linkOf(a, onA)
	if link == "" || link != linkOf(b, onB) {
		t.Fatalf("links %q and %q", link, linkOf(b, onB))
	}
	// A repeated command does nothing new.
	if code, again := linkCmd(t, fa.URL, "l1", onA, onB); code != 200 || again["result"].(map[string]any)["link"] != link {
		t.Fatalf("repeat: %d %v", code, again)
	}
	// The other front knows the link from the hosts' frames: a second member
	// on a host that has one is refused there too.
	eventually(t, "front b sees the link", func() bool { return frontSees(t, fb.URL, onA, link) && frontSees(t, fb.URL, onB, link) })
	if code, rep := linkCmd(t, fb.URL, probe(), second, onB); code != 409 {
		t.Fatalf("a second member on a host: %d %v", code, rep)
	}

	if code, rep := post(t, fb.URL+"/api/front/command", fmt.Sprintf(`{"commandId":"u1","action":"front.projectUnlink","args":{"ref":%q}}`, onB)); code != 200 || rep["ok"] != true {
		t.Fatalf("unlink: %d %v", code, rep)
	}
	if linkOf(b, onB) != "" || linkOf(a, onA) != link {
		t.Fatalf("after unlink: a %q, b %q", linkOf(a, onA), linkOf(b, onB))
	}
	// A record that kept the link brings it to the next member.
	eventually(t, "front a sees the unlink", func() bool { return frontSees(t, fa.URL, onB, "") && frontSees(t, fa.URL, onA, link) })
	if code, rep := linkCmd(t, fa.URL, probe(), onA, onB); code != 200 || linkOf(b, onB) != link {
		t.Fatalf("relink: %d %v, b %q", code, rep, linkOf(b, onB))
	}

	b.setDown(true)
	eventually(t, "b offline", func() bool { return getState(t, fa.URL).Hosts[1]["online"] == false })
	if code, rep := linkCmd(t, fa.URL, "offline", second, onB); code != 409 {
		t.Fatalf("an offline host: %d %v", code, rep)
	}
}

// A host too old to keep a link says so; the hosts that can keep it do.
func TestProjectLinkOldHost(t *testing.T) {
	a, b := newSettingsHostID(t, "a", "TEST-A"), newSettingsHostID(t, "b", "TEST-B")
	f := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, f.URL, 2)
	onA, onB := projectOn(t, f.URL, a, "TEST-A", "Fyr"), projectOn(t, f.URL, b, "TEST-B", "Fyr")
	b.noLink.Store(true)
	code, rep := linkCmd(t, f.URL, "old", onA, onB)
	if code != 200 {
		t.Fatalf("link: %d %v", code, rep)
	}
	failed, _ := rep["result"].(map[string]any)["failed"].([]any)
	if len(failed) != 1 || failed[0].(map[string]any)["host"] != "TEST-B" || linkOf(a, onA) == "" || linkOf(b, onB) != "" {
		t.Fatalf("failed %v, a %q, b %q", failed, linkOf(a, onA), linkOf(b, onB))
	}
	if msg := failed[0].(map[string]any)["error"].(string); msg != "b needs an update of Fyr to link projects" {
		t.Fatalf("words: %q", msg)
	}
}

func projectNamed(h *settingsHost, ref string) (name, color string) {
	for _, p := range h.eng.Projects() {
		if strings.HasSuffix(ref, "/"+p.ID) {
			return p.Name, p.Color
		}
	}
	return "", ""
}

// Decision 0034, slice 2: a new link takes the first project's values, an edit
// reaches every member, and a member that was offline catches up.
func TestProjectSharedValues(t *testing.T) {
	a, b := newSettingsHostID(t, "a", "TEST-A"), newSettingsHostID(t, "b", "TEST-B")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	f, g := newFront(t, hosts...), newFront(t, hosts...)
	online(t, f.URL, 2)
	online(t, g.URL, 2)
	onA, onB := projectOn(t, f.URL, a, "TEST-A", "Fyr"), projectOn(t, f.URL, b, "TEST-B", "fyr-mac")
	code, rep := linkCmd(t, f.URL, "l", onA, onB)
	if code != 200 {
		t.Fatalf("link: %d %v", code, rep)
	}
	link, _ := rep["result"].(map[string]any)["link"].(string)
	if n, _ := projectNamed(b, onB); n != "Fyr" {
		t.Fatalf("b's name after linking: %q", n)
	}
	eventually(t, "front sees both", func() bool { return frontSees(t, f.URL, onA, link) && frontSees(t, f.URL, onB, link) })
	shared := func(front, id, args string) (int, map[string]any) {
		return post(t, front+"/api/front/command", fmt.Sprintf(`{"commandId":%q,"action":"front.projectShared","args":{"link":%q,%s}}`, id, link, args))
	}
	if code, rep := shared(f.URL, "bad", `"color":"amber"`); code != 400 {
		t.Fatalf("a bad colour: %d %v", code, rep)
	}
	if code, rep := shared(f.URL, "s1", `"name":"Fyr 2","color":"teal"`); code != 200 || rep["ok"] != true {
		t.Fatalf("shared: %d %v", code, rep)
	}
	for _, h := range []*settingsHost{a, b} {
		ref := onA
		if h == b {
			ref = onB
		}
		if n, c := projectNamed(h, ref); n != "Fyr 2" || c != "teal" {
			t.Fatalf("after the edit: %q %q", n, c)
		}
	}
	// B is away for an edit made through the other front.
	b.setDown(true)
	eventually(t, "b offline", func() bool { return getState(t, g.URL).Hosts[1]["online"] == false })
	eventually(t, "g has the stamp", func() bool {
		code, _ := shared(g.URL, probe(), `"name":"While away"`)
		return code == 200
	})
	if n, _ := projectNamed(b, onB); n != "Fyr 2" {
		t.Fatalf("an offline host changed: %q", n)
	}
	b.setDown(false)
	eventually(t, "b catches up", func() bool { n, c := projectNamed(b, onB); return n == "While away" && c == "teal" })
}
