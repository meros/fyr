package t3

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE orchestration_v2_projection_threads (thread_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, title TEXT NOT NULL,
  active_provider_thread_id TEXT, created_at TEXT, updated_at TEXT NOT NULL, archived_at TEXT, deleted_at TEXT, payload_json TEXT NOT NULL);
CREATE TABLE orchestration_v2_projection_provider_threads (provider_thread_id TEXT PRIMARY KEY, thread_id TEXT, payload_json TEXT NOT NULL);
CREATE TABLE projection_projects (project_id TEXT PRIMARY KEY, title TEXT, workspace_root TEXT NOT NULL, project_icon_json TEXT, deleted_at TEXT, archived_at TEXT);
CREATE TABLE projection_thread_pull_requests (thread_id TEXT NOT NULL, host TEXT, repository TEXT, number INTEGER, url TEXT NOT NULL, source TEXT, linked_at TEXT NOT NULL);
`

func fixture(t *testing.T) (db, home string) {
	dir := t.TempDir()
	db = filepath.Join(dir, "statev2.sqlite")
	home = filepath.Join(dir, "home")
	c, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mustExec := func(q string, args ...any) {
		if _, err := c.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(schema)
	mustExec(`INSERT INTO projection_projects VALUES ('p1','app','/src/app',NULL,NULL,NULL)`)
	mustExec(`INSERT INTO projection_projects VALUES ('p2','No project','/home/u/.t3/scratch','{"kind":"lucide","name":"message-square-dashed","color":"gray"}',NULL,NULL)`)
	mustExec(`INSERT INTO projection_projects VALUES ('p3','gone','/old',NULL,'2026-01-01',NULL)`)
	add := func(id, payload, pt string) {
		mustExec(`INSERT INTO orchestration_v2_projection_threads VALUES (?,?,?,?,?,?,NULL,NULL,?)`, id, "p1", "T "+id, "pt-"+id, "2026-11-13T10:00:00Z", "2026-11-13T11:00:00Z", payload)
		mustExec(`INSERT INTO orchestration_v2_projection_provider_threads VALUES (?,?,?)`, "pt-"+id, id, pt)
	}
	native := func(s string) string { return `{"nativeThreadRef":{"nativeId":"` + s + `"}}` }
	add("open", `{"providerInstanceId":"claudeNorban","modelSelection":{"model":"claude-opus-5-5"},"worktreePath":"/src/app-wt","lineage":{"parentThreadId":null},"settledAt":null}`, native("sess-open"))
	add("child", `{"lineage":{"parentThreadId":"open"},"settledAt":null}`, native("sess-child"))
	add("settled", `{"lineage":{"parentThreadId":null},"settledAt":"2026-11-13T12:00:00Z"}`, native("sess-settled"))
	add("nosession", `{"lineage":{"parentThreadId":null}}`, `{}`)
	mustExec(`INSERT INTO projection_thread_pull_requests VALUES ('open','github.com','acme/app',41,'https://github.com/acme/app/pull/41','x','2026-11-13T10:00:00Z')`)
	mustExec(`INSERT INTO projection_thread_pull_requests VALUES ('open','github.com','acme/app',42,'https://github.com/acme/app/pull/42','x','2026-11-13T10:30:00Z')`)

	tdir := filepath.Join(home, ".claude", "projects", "-src-app-wt")
	os.MkdirAll(tdir, 0o755)
	os.WriteFile(filepath.Join(tdir, "sess-open.jsonl"), []byte(
		`{"type":"user","message":{"role":"user","content":"Fix the lasso"},"timestamp":"2026-11-13T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"On it."}]},"timestamp":"2026-11-13T10:00:05Z"}
{"type":"user","isMeta":true,"message":{"role":"user","content":"meta"},"timestamp":"2026-11-13T10:00:06Z"}
{"type":"summary","summary":"x"}
not json
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"ok"}]},"timestamp":"2026-11-13T10:00:07Z"}
`), 0o644)
	return db, home
}

// SPEC 0.5 V0.9, decision 0004: only open top-level threads with a session.
func TestCandidates(t *testing.T) {
	db, home := fixture(t)
	cs, err := Candidates(db, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("got %d candidates: %+v", len(cs), cs)
	}
	c := cs[0]
	if c.T3ID != "open" || c.SessionID != "sess-open" || c.Cwd != "/src/app-wt" || c.Profile != "claudeNorban" ||
		c.Model != "claude-opus-5-5" || c.PRURL != "https://github.com/acme/app/pull/42" || !c.HasTranscript {
		t.Fatalf("%+v", c)
	}
}

func TestHistoryFromTranscript(t *testing.T) {
	_, home := fixture(t)
	evs, at, err := History(home, "sess-open", 400)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 || len(at) != 3 {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	if evs[0].Type != "user" || evs[0].Data.(map[string]any)["text"] != "Fix the lasso" || evs[1].Type != "msg" || evs[2].Type != "msg" {
		t.Fatalf("%+v", evs)
	}
}

func TestProfilesFromSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"providerInstances":{"claudeNorban":{"driver":"claudeAgent","displayName":"Claude (norban)","accentColor":"#262428","enabled":true,"config":{"binaryPath":"/nix/store/x/bin/claude-norban"}},"codex":{"driver":"codex","enabled":false}},
 "providerModelPreferences":{"claudeNorban":{"hiddenModels":["claude-fable-5-1"],"modelOrder":["claude-opus-5-5","claude-fable-5-1","claude-sonnet-5-5"]}}}`), 0o644)
	ps, err := Profiles(p)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, x := range ps {
		if x.ID == "claudeNorban" {
			found = true
			if x.Binary != "/nix/store/x/bin/claude-norban" || len(x.Models) != 2 || x.Models[0] != "claude-opus-5-5" || x.Name != "Claude (norban)" {
				t.Fatalf("%+v", x)
			}
		}
		if x.ID == "codex" {
			t.Fatal("non-claude driver became a profile")
		}
	}
	if !found || ps[0].ID != "claudeAgent" {
		t.Fatalf("%+v", ps)
	}
}

// SPEC 0.5 V0.23: T3's projects seed Fyr's, with their titles and icons.
func TestProjectsSeed(t *testing.T) {
	db, _ := fixture(t)
	ps, err := Projects(db, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("%+v", ps)
	}
	for _, p := range ps {
		if p.Folder("h1") == "/home/u/.t3/scratch" {
			if p.Name != "Scratch" || p.Icon.Kind != "line" || p.Icon.Value != "message-square-dashed" || p.Color != "slate" {
				t.Fatalf("%+v", p)
			}
		}
	}
}
