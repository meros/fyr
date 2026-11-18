package authtest_test

// SPEC V0.180 (#264): a running fyrd notices that its config file changed, by itself:
// the log, /healthz (still 200) and fyr host list say so.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/authtest"
)

func TestARunningFyrdSaysItsConfigChanged(t *testing.T) {
	t.Parallel()
	web := t.TempDir() // CI's Go job has no built client, and /healthz needs one to be green
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := authtest.Start(authtest.Options{Name: "drift", Web: web, Extra: map[string]any{"configCheckMillis": 50}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	const note = "config changed; restart fyrd to apply"
	if r, err := f.Get("/healthz", ""); err != nil || r.Status != 200 || strings.Contains(string(r.Body), note) {
		t.Fatalf("before the change: %v %+v", err, r)
	}
	b, err := os.ReadFile(f.Config)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	c["idleStopMinutes"] = 11
	b, _ = json.Marshal(c)
	if err := os.WriteFile(f.Config, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if !f.WaitLog("changed since fyrd read it", 10*time.Second) {
		t.Fatalf("no log line:\n%s", f.Log())
	}
	if r, err := f.Get("/healthz", ""); err != nil || r.Status != 200 || !strings.Contains(string(r.Body), note) {
		t.Fatalf("/healthz after the change: %v %+v", err, r)
	}
	if out, err := f.OwnerCall("GET", "/host/list", nil); err != nil || out["configChanged"] != note {
		t.Fatalf("host list: %v %v", err, out["configChanged"])
	}
	if r, err := f.Get("/api/front/peer/state", ""); err != nil || r.Status != 200 || !strings.Contains(string(r.Body), note) {
		t.Fatalf("Settings → Hosts' state: %v %+v", err, r)
	}
}
