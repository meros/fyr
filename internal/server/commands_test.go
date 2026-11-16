package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
)

// SPEC 0.5 V0.79 (2): a thread's commands and a folder's newest list are
// served on their own routes, not in the thread DTO.
func TestCommandsRoutes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := engine.New(st)
	dir := t.TempDir()
	s := &Server{Engine: e, BuildID: "b1", Profiles: []provider.Profile{{ID: "claude"}}}
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	r := e.Command(api.Cmd{CommandID: "c1", Action: "thread.create", Args: core.Args{"cwd": dir, "text": "x", "profile": "claude"}})
	if !r.OK {
		t.Fatalf("create %+v", r)
	}
	type reply struct {
		Commands []core.Command `json:"commands"`
		From     *string        `json:"from"`
	}
	get := func(path string) (int, reply) {
		resp := do(t, "GET", hs.URL+path, "", nil, "")
		defer resp.Body.Close()
		var out reply
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	from := func(o reply) string {
		if o.From == nil {
			return "<missing>"
		}
		return *o.From
	}
	if code, out := get("/api/threads/" + r.ThreadID + "/commands"); code != 200 || from(out) != "" || out.Commands == nil || len(out.Commands) != 0 {
		t.Fatalf("none yet: %d %+v", code, out)
	}
	e.SetCommands("other", "claude", dir, []core.Command{{Name: "review", Description: "Review a PR", ArgumentHint: "<pr>"}})
	if code, out := get("/api/threads/" + r.ThreadID + "/commands"); code != 200 || from(out) != "folder" || len(out.Commands) != 1 || out.Commands[0].ArgumentHint != "<pr>" {
		t.Fatalf("folder: %d %+v", code, out)
	}
	e.SetCommands(r.ThreadID, "claude", dir, []core.Command{{Name: "compact"}, {Name: "review"}})
	if code, out := get("/api/threads/" + r.ThreadID + "/commands"); code != 200 || from(out) != "thread" || len(out.Commands) != 2 {
		t.Fatalf("thread: %d %+v", code, out)
	}
	if code, out := get("/api/commands?profile=claude&cwd=" + url.QueryEscape(dir)); code != 200 || len(out.Commands) != 2 {
		t.Fatalf("newest for the folder: %d %+v", code, out)
	}
	if code, out := get("/api/commands?cwd=" + url.QueryEscape(dir)); code != 200 || len(out.Commands) != 2 {
		t.Fatalf("the default provider: %d %+v", code, out)
	}
	if code, out := get("/api/commands?profile=other&cwd=" + url.QueryEscape(dir)); code != 200 || out.Commands == nil || len(out.Commands) != 0 {
		t.Fatalf("another provider: %d %+v", code, out)
	}
	if code, _ := get("/api/threads/nope/commands"); code != 404 {
		t.Fatalf("unknown thread: %d", code)
	}
	var d map[string]any
	resp := do(t, "GET", hs.URL+"/api/threads/"+r.ThreadID, "", nil, "")
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&d)
	if _, ok := d["commands"]; ok {
		t.Fatal("the thread DTO carries the commands")
	}
}
