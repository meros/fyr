package front

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func postFS(t *testing.T, url, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	req.Header.Set("X-Fyr", "1")
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b)
}

// SPEC V0.132: from host a's front the owner browses host b's folders, makes
// a folder and a project there, and starts threads on b: in that project, and
// in a folder of b with no project. Each lands on b; a has none of it.
func TestFrontCreatesOnTheChosenHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	bu := hostURL(t, fs.URL, "b")

	// The picker reads b's home, not a's.
	_, _, body := read(t, bu+"/fs/list?path="+b.home)
	if !strings.Contains(body, "only-on-b") || strings.Contains(body, "only-on-a") {
		t.Fatalf("b's folders: %s", body)
	}
	if code, _, body := read(t, fs.URL+"/api/fs/list?path="+b.home); code == 200 && strings.Contains(body, "only-on-b") {
		t.Fatalf("no host read b's folders: %s", body)
	}
	// New folder on b.
	code, out := postFS(t, bu+"/fs/mkdir", `{"parent":"`+b.home+`","name":"lab-notes"}`)
	if code != 200 || !strings.Contains(out, filepath.Join(b.home, "lab-notes")) {
		t.Fatalf("mkdir on b: %d %s", code, out)
	}

	// New project on b, in that folder.
	folder := filepath.Join(b.home, "lab-notes")
	code, rep := post(t, bu+"/command", `{"commandId":"np","action":"project.create","args":{"name":"Lab notes","folder":"`+folder+`"}}`)
	if code != 200 {
		t.Fatalf("project.create on b: %d %v", code, rep)
	}
	var pid string
	eventually(t, "b's project on the front", func() bool {
		for _, p := range getState(t, fs.URL).Projects {
			if p["name"] == "Lab notes" && p["host"] == "b" {
				pid, _ = p["id"].(string)
			}
		}
		return pid != ""
	})

	// A thread in that project and one in a folder of b, both on b's prefix.
	code, rep = post(t, bu+"/command", `{"commandId":"t1","action":"thread.create","args":{"projectId":"`+pid+`","cwd":"`+folder+`","text":"in the project"}}`)
	t1, _ := rep["threadId"].(string)
	if code != 200 || t1 == "" {
		t.Fatalf("thread in b's project: %d %v", code, rep)
	}
	code, rep = post(t, bu+"/command", `{"commandId":"t2","action":"thread.create","args":{"cwd":"`+filepath.Join(b.home, "only-on-b")+`","text":"no project"}}`)
	t2, _ := rep["threadId"].(string)
	if code != 200 || t2 == "" {
		t.Fatalf("thread in a folder of b: %d %v", code, rep)
	}
	for _, id := range []string{t1, t2} {
		if _, ok := b.eng.Thread(id); !ok {
			t.Fatalf("thread %s is not on b", id)
		}
		if _, ok := a.eng.Thread(id); ok {
			t.Fatalf("thread %s is on a", id)
		}
	}
	if th, _ := b.eng.Thread(t1); th.ProjectID != pid {
		t.Fatalf("thread t1 is in project %q, want %q", th.ProjectID, pid)
	}
	if len(a.eng.Projects()) != 0 {
		t.Fatal("a got b's project")
	}
}

// SPEC V0.132: while b is offline its folders say so; nothing falls back to a.
func TestFrontFoldersOfAnOfflineHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	bu := hostURL(t, fs.URL, "b")
	b.setDown(true)
	eventually(t, "b offline", func() bool { return getState(t, fs.URL).Hosts[1]["online"] == false })
	code, _, body := read(t, bu+"/fs/list?path="+a.home)
	var e struct{ Error string }
	json.Unmarshal([]byte(body), &e)
	if code != http.StatusServiceUnavailable || !strings.Contains(e.Error, "b is offline") || strings.Contains(body, "only-on-a") {
		t.Fatalf("folders of offline b: %d %s", code, body)
	}
	code, rep := post(t, bu+"/command", `{"commandId":"np","action":"project.create","args":{"name":"X","folder":"`+a.home+`"}}`)
	if code != http.StatusServiceUnavailable || len(a.eng.Projects()) != 0 {
		t.Fatalf("project.create on offline b: %d %v", code, rep)
	}
}
