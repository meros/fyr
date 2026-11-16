package identity

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// SPEC 0.5 V0.124: the key is made once and kept; its id is stable.
func TestLoadMakesOnceAndKeeps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	a, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Z2-7]{4}(-[A-Z2-7]{4}){3}$`).MatchString(a.ID()) {
		t.Fatalf("id %q", a.ID())
	}
	st, err := os.Stat(filepath.Join(dir, File))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	b, err := Load(dir)
	if err != nil || b.ID() != a.ID() {
		t.Fatalf("second load: %v %v / %v", b, err, a.ID())
	}
	if Short(a.ID()) != a.ID()[:4] {
		t.Fatalf("short %q", Short(a.ID()))
	}
}

func TestLoadRefusesABrokenKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, File), []byte("not a key"), 0o600)
	if _, err := Load(dir); err == nil {
		t.Fatal("a broken key file must be an error, not replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, File)); string(b) != "not a key" {
		t.Fatal("the broken file was replaced")
	}
}

func TestTwoInstallsDiffer(t *testing.T) {
	a, _ := Load(t.TempDir())
	b, _ := Load(t.TempDir())
	if a.ID() == b.ID() {
		t.Fatal("two keys, one id")
	}
}
