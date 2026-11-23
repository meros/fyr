package lockwatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// V0.review-robust (2): Latest reads a dump's head: its first line says
// whether fyrd exited, its fourth what held the lock. A half-written dump
// (WriteAtomic's temporary file) is not a dump.
func TestLatestReadsTheHead(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	name := at.Format("20060102T150405.000Z") + ".txt"
	head := "fyrd stall dump: fyrd exits\n\nHeld locks:\n  core lock held 2m by acme\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(head+strings.Repeat("goroutine 1 [running]:\n", 1<<16)), 0o600); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Minute).Format("20060102T150405.000Z")
	if err := os.WriteFile(filepath.Join(dir, "."+later+".txt.tmp-123"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok := Latest(dir, at.Add(-time.Second))
	if !ok || !s.Exiting || s.What != "core lock held 2m by acme" || !s.At.Equal(at) {
		t.Fatalf("Latest = %+v %v", s, ok)
	}
	if lines := headLines(filepath.Join(dir, "missing"), 4); lines != nil {
		t.Fatalf("a missing file has lines %v", lines)
	}
}
