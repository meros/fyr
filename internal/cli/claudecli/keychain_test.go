package claudecli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
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

// V0.review-hangs (4): a `security` call that does not return (a locked
// Keychain waiting on a dialog) is killed, with whatever it started.
func TestSecurityIsBounded(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "security")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldBin, oldTimeout := securityBin, keychainTimeout
	securityBin, keychainTimeout = bin, 200*time.Millisecond
	t.Cleanup(func() { securityBin, keychainTimeout = oldBin, oldTimeout })
	start := time.Now()
	if _, err := security("find-generic-password"); err == nil {
		t.Fatal("a hung security call gave no error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("security returned after %v", d)
	}
}
