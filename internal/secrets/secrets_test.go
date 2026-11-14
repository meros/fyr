package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SPEC 0.5 V0.47: a secret is a 0600 file in a 0700 folder, written
// atomically; names cannot leave the folder; clients see only the last 4.
func TestFileStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	s := Files(dir)
	if s.Has("p1") {
		t.Fatal("empty store has p1")
	}
	if _, err := s.Get("p1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if err := s.Put("p1", "  sk-or-v1-abcdef123456\n"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, "p1"))
	dst, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o600 || dst.Mode().Perm() != 0o700 {
		t.Fatalf("modes %v %v", st.Mode(), dst.Mode())
	}
	if v, _ := s.Get("p1"); v != "sk-or-v1-abcdef123456" {
		t.Fatalf("got %q", v)
	}
	if s.Last4("p1") != "3456" || Last4("short") != "" {
		t.Fatal("last4")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp file left: %v", ents)
	}
	for _, bad := range []string{"../x", "a/b", "", ".hidden", "a..b"} {
		if err := s.Put(bad, "v"); err == nil {
			t.Fatalf("name %q accepted", bad)
		}
	}
	if err := s.Put("p1", ""); err != nil || s.Has("p1") {
		t.Fatal("empty put should delete")
	}
	if err := s.Delete("p1"); err != nil {
		t.Fatal(err)
	}
}

type memBackend map[string][]byte

func (m memBackend) Put(n string, v []byte) error { m[n] = v; return nil }
func (m memBackend) Get(n string) ([]byte, error) {
	if v, ok := m[n]; ok {
		return v, nil
	}
	return nil, ErrNotFound
}
func (m memBackend) Delete(n string) error { delete(m, n); return nil }

// The backend is swappable: callers see the same Store.
func TestOtherBackend(t *testing.T) {
	s := &Store{B: memBackend{}}
	s.Put("k", "0123456789abcdef")
	if !s.Has("k") || s.Last4("k") != "cdef" {
		t.Fatal("mem backend")
	}
}

// An error never carries the value.
func TestErrorsHoldNoValue(t *testing.T) {
	dir := t.TempDir()
	s := Files(filepath.Join(dir, "file-not-dir"))
	os.WriteFile(filepath.Join(dir, "file-not-dir"), nil, 0o600)
	err := s.Put("k", "supersecretvalue123")
	if err == nil || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("%v", err)
	}
}
