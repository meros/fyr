package attach

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// SPEC 0.5 V0.18: save, read back, limit, and names that cannot escape.
func TestSaveGet(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Max: 10}
	m, err := s.Save("../../etc/passwd", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "passwd" || m.Size != 5 || m.MediaType != "text/plain" || !strings.HasPrefix(m.Path, s.Dir) {
		t.Fatalf("%+v", m)
	}
	got, err := s.Get(m.ID)
	if err != nil || got != m {
		t.Fatalf("%+v %v", got, err)
	}
	if b, _ := os.ReadFile(got.Path); string(b) != "hello" {
		t.Fatalf("content %q", b)
	}
	if _, err := s.Save("big.bin", "", strings.NewReader("01234567890")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := s.Get("../x"); err == nil {
		t.Fatal("bad id accepted")
	}
	if m, _ := s.Save("", "", strings.NewReader("x")); m.Name != "file" || m.MediaType != "application/octet-stream" {
		t.Fatalf("defaults %+v", m)
	}
	if m, _ := s.Save("photo.JPG", "", strings.NewReader("x")); m.MediaType != "image/jpeg" {
		t.Fatalf("type from extension %+v", m)
	}
}
