package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

// stable makes the fixture the same on every run: ids, times, revs, the
// temporary home folder and the host name.
func stable(b []byte, home string) []byte {
	s := strings.ReplaceAll(string(b), home, "/home/owner")
	if h, err := os.Hostname(); err == nil && h != "" {
		s = strings.ReplaceAll(s, `"`+h+`"`, `"host"`)
	}
	s = regexp.MustCompile(`"\d{4}-\d\d-\d\dT[^"]*"`).ReplaceAllString(s, `"2026-01-01T00:00:00Z"`)
	s = regexp.MustCompile(`"rev": \d+`).ReplaceAllString(s, `"rev": 1`)
	ids := map[string]string{}
	s = regexp.MustCompile(`\b([tpdw])_[0-9a-f]{8,}\b`).ReplaceAllStringFunc(s, func(id string) string {
		if _, ok := ids[id]; !ok {
			ids[id] = fmt.Sprintf("%s_%d", id[:1], len(ids)+1)
		}
		return ids[id]
	})
	return []byte(s)
}

// SPEC 0.5 V0.24: the client is tested against what the host really sends.
// This test writes the host's /api/state for a few edge cases to
// web/test/fixtures/host-state.json; the web tests decode that file and
// drop every optional field in turn. V0.55: a project's one folder and a
// thread's newProject. (Found live: the host omitted an empty
// icon value and the whole client failed to start.)
func TestStateContractFixture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hs, e := newServer(t)
	post := func(body string) {
		r := do(t, "POST", hs.URL+"/api/command", "", map[string]string{"X-Fyr": "1"}, body)
		if r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("%s: %d %s", body, r.StatusCode, b)
		}
		// V0.starting-moves: no agent runs here, so no start ends by
		// itself; the fixture shows each thread as it is after its run.
		for _, d := range e.Snapshot() {
			if d.Starting != nil {
				coretest.Patch(e, d.ID, func(t *core.Thread, _ time.Time) []core.Event {
					t.Starting = nil
					return nil
				})
			}
		}
	}
	src := filepath.Join(home, "src", "app")
	os.MkdirAll(src, 0o755)
	post(`{"commandId":"k1","action":"thread.create","args":{"cwd":"` + src + `","text":"a thread in a folder"}}`)
	post(`{"commandId":"k2","action":"thread.create","args":{"cwd":"` + home + `","text":"a thread in the home folder"}}`)
	post(`{"commandId":"k3","action":"project.create","args":{"name":"A label"}}`)
	// V0.28: a thread in a clone without a project carries a suggestion.
	lib := filepath.Join(home, "src", "lib")
	os.MkdirAll(lib, 0o755)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", "git@github.com:owner/lib.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", lib}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	post(`{"commandId":"k4","action":"thread.create","args":{"cwd":"` + lib + `","text":"a thread in a clone"}}`)
	// V0.37: a thread with a live view carries the flag and the view.
	post(`{"commandId":"k5","action":"thread.create","args":{"cwd":"` + src + `","text":"a thread with a live view","view":true}}`)
	for _, d := range e.Snapshot() {
		if d.Title == "a thread with a live view" {
			coretest.Patch(e, d.ID, func(t *core.Thread, now time.Time) []core.Event {
				core.ViewPublished(t, "a_page", map[string]any{"headline": "Tests pass", "steps": []any{"build", "test"}}, now)
				return nil
			})
		}
	}
	// V0.46: a project in its quiet hours (a whole day, every day) and its thread carry quietUntil.
	work := filepath.Join(home, "src", "work")
	os.MkdirAll(work, 0o755)
	post(`{"commandId":"k6","action":"project.create","args":{"name":"Work","folder":"` + work + `"}}`)
	for _, p := range e.Projects() {
		if p.Name == "Work" {
			post(`{"commandId":"k7","action":"project.update","args":{"id":"` + p.ID + `","quiet":[{"from":"00:00","to":"00:00"}]}}`)
		}
	}
	post(`{"commandId":"k8","action":"thread.create","args":{"cwd":"` + work + `","text":"a thread in a quiet project"}}`)
	r := do(t, "GET", hs.URL+"/api/state", "", nil, "")
	var v any
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(filepath.Join("..", "..", "web", "test", "fixtures", "host-state.json"), append(stable(b, home), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
