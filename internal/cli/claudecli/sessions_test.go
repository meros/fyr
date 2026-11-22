package claudecli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionFileFindsCutDirs(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "projects", "-a-b-c"), 0o700)
	os.MkdirAll(filepath.Join(d, "projects", "-cut-123"), 0o700)
	os.WriteFile(filepath.Join(d, "projects", "-a-b-c", "s1.jsonl"), nil, 0o600)
	os.WriteFile(filepath.Join(d, "projects", "-cut-123", "s2.jsonl"), nil, 0o600)
	if got := SessionFile(d, "/a/b.c", "s1"); got != filepath.Join(d, "projects", "-a-b-c", "s1.jsonl") {
		t.Errorf("exact: %q", got)
	}
	if got := SessionFile(d, "/somewhere/else", "s2"); got != filepath.Join(d, "projects", "-cut-123", "s2.jsonl") {
		t.Errorf("glob: %q", got)
	}
	if got := SessionFile(d, "/a", "../x"); got != "" {
		t.Errorf("bad id: %q", got)
	}
}

// SPEC V0.trans-host-handover, decision 0039: ExportSession and ImportSession
// transfer a session transcript, its folder and file-history across hosts,
// remapping into the target cwd's project directory and enforcing caps/safety.
func TestExportImportSessionAcrossCwds(t *testing.T) {
	fromDir, toDir := t.TempDir(), t.TempDir()
	sid := "sess-123"
	srcProj := filepath.Join(fromDir, "projects", nonAlnum("/home/alice/fyr"))
	os.MkdirAll(filepath.Join(srcProj, sid, "tool-results"), 0o700)
	os.MkdirAll(filepath.Join(fromDir, "file-history", sid), 0o700)
	src := filepath.Join(srcProj, sid+".jsonl")
	os.WriteFile(src, []byte("{\"msg\":\"hello\"}\n"), 0o600)
	os.WriteFile(filepath.Join(srcProj, sid, "tool-results", "out.txt"), []byte("result"), 0o600)
	os.WriteFile(filepath.Join(fromDir, "file-history", sid, "h1"), []byte("hist"), 0o600)

	arc, err := ExportSession(src, fromDir, sid, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := ImportSession(arc, toDir, "/Users/bob/fyr", sid, 1<<20); err != nil {
		t.Fatal(err)
	}
	got := SessionFile(toDir, "/Users/bob/fyr", sid)
	want := filepath.Join(toDir, "projects", nonAlnum("/Users/bob/fyr"), sid+".jsonl")
	if got != want {
		t.Fatalf("imported path %q, want %q", got, want)
	}
	if b, err := os.ReadFile(want); err != nil || string(b) != "{\"msg\":\"hello\"}\n" {
		t.Fatalf("transcript %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(toDir, "projects", nonAlnum("/Users/bob/fyr"), sid, "tool-results", "out.txt")); err != nil || string(b) != "result" {
		t.Fatalf("tool result %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(toDir, "file-history", sid, "h1")); err != nil || string(b) != "hist" {
		t.Fatalf("file history %q %v", b, err)
	}
	// Oversized session triggers ErrSessionTooLarge so handover falls back to rung 2.
	if _, err := ExportSession(src, fromDir, sid, 8); !errors.Is(err, ErrSessionTooLarge) {
		t.Fatalf("size cap: got %v, want ErrSessionTooLarge", err)
	}
}

func TestImportSessionRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o600, Size: 2})
	tw.Write([]byte("no"))
	tw.Close()
	gz.Close()
	if err := ImportSession(buf.Bytes(), t.TempDir(), "/work", "s1", 1<<20); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestCapReader(t *testing.T) {
	b, err := io.ReadAll(&capReader{r: strings.NewReader("abcd"), left: 4})
	if err != nil || string(b) != "abcd" {
		t.Fatalf("exact fit: %q %v", b, err)
	}
	if _, err := io.ReadAll(&capReader{r: strings.NewReader("abcde"), left: 4}); !errors.Is(err, ErrSessionTooLarge) {
		t.Fatalf("over the cap: %v", err)
	}
}
