package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

// CopyTree copies folders and regular files, skips a symlink, and a missing
// source is no error.
func TestCopyTree(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	os.MkdirAll(filepath.Join(src, "a", "b"), 0o700)
	os.WriteFile(filepath.Join(src, "a", "b", "f.txt"), []byte("hi"), 0o600)
	os.Symlink("/etc/passwd", filepath.Join(src, "link"))
	if err := CopyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a", "b", "f.txt")); err != nil || string(b) != "hi" {
		t.Fatalf("copied file %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "link")); err == nil {
		t.Fatal("a symlink was copied")
	}
	if err := CopyTree(filepath.Join(src, "missing"), dst); err != nil {
		t.Fatalf("missing source: %v", err)
	}
}
