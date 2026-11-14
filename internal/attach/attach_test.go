package attach

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		f := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// SPEC 0.5 V0.47: a folder preview copies regular files, skips dot names,
// node_modules and links out of the folder, and serves nothing outside it.
func TestTree(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"index.html": "<script src=app.js></script>", "app.js": "1", "models/duck.glb": "glTF",
		".env": "SECRET=1", ".git/config": "x", "node_modules/three/index.js": "x",
	})
	secret := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(secret, []byte("secret"), 0o644)
	os.Symlink(secret, filepath.Join(root, "out.txt"))
	os.Symlink("/etc", filepath.Join(root, "etc"))
	os.Symlink(filepath.Join(root, "app.js"), filepath.Join(root, "alias.js"))
	files, outside, err := ScanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, f := range files {
		rels = append(rels, f.Rel)
	}
	if strings.Join(rels, ",") != "alias.js,app.js,index.html,models/duck.glb" {
		t.Fatalf("files %v", rels)
	}
	if strings.Join(outside, ",") != "etc,out.txt" {
		t.Fatalf("outside %v", outside)
	}
	s := &Store{Dir: t.TempDir()}
	m, err := s.SaveTree("site", "index.html", files)
	if err != nil || m.Entry != "index.html" || m.MediaType != "text/html" || m.Size != int64(len("<script src=app.js></script>")+1+1+4) {
		t.Fatalf("%+v %v", m, err)
	}
	got, _ := s.Get(m.ID)
	if got != m {
		t.Fatalf("%+v", got)
	}
	if p, err := s.TreePath(m, "models/duck.glb"); err != nil {
		t.Fatal(err)
	} else if b, _ := os.ReadFile(p); string(b) != "glTF" {
		t.Fatalf("%q", b)
	}
	for _, bad := range []string{"../meta.json", "models/../../meta.json", "/etc/passwd", "", "models", "missing.js", "..", "a/../../x"} {
		if _, err := s.TreePath(m, bad); err == nil {
			t.Fatalf("%q served", bad)
		}
	}
	if _, err := s.SaveTree("site", "nope.html", files); err == nil {
		t.Fatal("an entry outside the files was accepted")
	}
	single, _ := s.Save("p.html", "text/html", strings.NewReader("x"))
	if _, err := s.TreePath(single, "p.html"); err == nil {
		t.Fatal("a single page served as a folder")
	}
}

// SPEC 0.5 V0.47: at most 500 files and 50 MB.
func TestTreeCaps(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= TreeMaxFiles; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644)
	}
	if _, _, err := ScanTree(root); !errors.Is(err, ErrTreeTooBig) {
		t.Fatalf("501 files: %v", err)
	}
	big := t.TempDir()
	f, _ := os.Create(filepath.Join(big, "big.bin"))
	f.Truncate(TreeMaxBytes + 1)
	f.Close()
	if _, _, err := ScanTree(big); !errors.Is(err, ErrTreeTooBig) {
		t.Fatalf("over 50 MB: %v", err)
	}
}
