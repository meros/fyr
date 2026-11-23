package agycli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/cli"
)

func geminiLaunch(dir string) cli.Launch {
	return cli.Launch{Auth: cli.Auth{Kind: cli.APIKey, ConfigDir: dir}}
}

// The user's settings file is never overwritten when it cannot be read as JSON.
func TestPrepareGeminiDirKeepsAnUnreadableSettingsFile(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "antigravity-cli")
	os.MkdirAll(app, 0o700)
	p := filepath.Join(app, "settings.json")
	const mine = `{"theme": "dark", // a comment`
	os.WriteFile(p, []byte(mine), 0o600)
	if _, _, err := prepareGeminiDir(geminiLaunch(dir)); err == nil {
		t.Fatal("expected an error for a settings file that is not JSON")
	}
	if b, _ := os.ReadFile(p); string(b) != mine {
		t.Fatalf("settings overwritten: %s", b)
	}
}

func TestPrepareGeminiDirMergesIntoSettings(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "antigravity-cli")
	os.MkdirAll(app, 0o700)
	p := filepath.Join(app, "settings.json")
	os.WriteFile(p, []byte(`{"theme":"dark"}`), 0o600)
	if _, _, err := prepareGeminiDir(geminiLaunch(dir)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"theme": "dark"`) || !strings.Contains(string(b), `"modelProvider": "gemini"`) {
		t.Fatalf("settings: %s", b)
	}
}
