package main

// SPEC V0.180 (#264): fyrd remembers the config it loaded and says when the file
// changed under it: in daemon.log, /healthz and Settings → Hosts.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/deploy"
)

func TestConfigWatchSaysWhenTheFileChanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"peerListen": ""}`), 0o600)
	w := newConfigWatch(p)
	if w.check(); w.Note() != "" {
		t.Fatalf("unchanged: %q", w.Note())
	}
	// the same bytes written again (a switch that rewrote nothing new) is no change
	os.WriteFile(p, []byte(`{"peerListen": ""}`), 0o600)
	os.Chtimes(p, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if w.check(); w.Note() != "" {
		t.Fatalf("the same bytes: %q", w.Note())
	}
	os.WriteFile(p, []byte(`{"peerListen": "0.0.0.0:7779"}`), 0o600)
	if !w.check() || !strings.Contains(w.Note(), "config changed; restart fyrd to apply") {
		t.Fatalf("changed: %q", w.Note())
	}
	// back to what was loaded: nothing to apply
	os.WriteFile(p, []byte(`{"peerListen": ""}`), 0o600)
	if w.check(); w.Note() != "" {
		t.Fatalf("back to the loaded bytes: %q", w.Note())
	}
}

func TestConfigWatchSeesAFileThatAppearsOrGoes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	w := newConfigWatch(p)
	if w.check(); w.Note() != "" {
		t.Fatalf("still absent: %q", w.Note())
	}
	os.WriteFile(p, []byte(`{}`), 0o600)
	if w.check(); w.Note() == "" {
		t.Fatal("a config that appeared is not a change")
	}
	os.Remove(p)
	if w.check(); w.Note() != "" {
		t.Fatalf("gone again: %q", w.Note())
	}
}

func TestHealthzNotesAChangedConfigAndStaysGreen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{}`), 0o600)
	w := newConfigWatch(p)
	os.WriteFile(p, []byte(`{"listen":"127.0.0.1:1"}`), 0o600)
	w.check()
	rec := httptest.NewRecorder()
	deploy.Health(http.NotFoundHandler(), w.health).ServeHTTP(rec, httptest.NewRequest("GET", deploy.HealthPath, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "healthy") || !strings.Contains(rec.Body.String(), "config changed; restart fyrd to apply") {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}
