package claudecli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// macOS: the CLI names the Keychain item by the folder (hashed) when
// CLAUDE_CONFIG_DIR is set, plain for its default folder without it.
func TestKeychainServices(t *testing.T) {
	h, _ := os.UserHomeDir()
	def := keychainServices(filepath.Join(h, ".claude"))
	if len(def) != 2 || def[1] != "Claude Code-credentials" {
		t.Fatalf("default folder: %v", def)
	}
	// sha256("/srv/claude-work") starts ad01f8cb
	if got := keychainServices("/srv/claude-work"); !slices.Equal(got, []string{"Claude Code-credentials-ad01f8cb"}) {
		t.Fatalf("other folder: %v", got)
	}
}
