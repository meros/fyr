package store

import (
	"os"
	"path/filepath"
	"testing"
)

// V0.review-robust (7): a data folder whose path holds '?', '#', '%' or a
// space opens the database at exactly that path.
func TestOpenPathWithURISpecialCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%20 d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "fyr.db")
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Write(func(*Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the database is not at %s: %v", p, err)
	}
	if es, _ := os.ReadDir(filepath.Dir(dir)); len(es) != 1 {
		t.Fatalf("files beside the data folder: %v", es)
	}
}
