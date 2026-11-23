package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := WriteAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("two"), 0o640); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two" {
		t.Fatalf("content %q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode())
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("left files: %v", es)
	}
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestWriteAtomicFromFailureKeepsOld(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	WriteAtomic(p, []byte("old"), 0o600)
	if err := WriteAtomicFrom(p, io.MultiReader(strings.NewReader("new"), failing{}), 0o600); err == nil {
		t.Fatal("no error")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "old" {
		t.Fatalf("content %q", b)
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("temp left: %v", es)
	}
}

func TestWriteAtomicMissingDir(t *testing.T) {
	if err := WriteAtomic(filepath.Join(t.TempDir(), "no", "f"), nil, 0o600); err == nil {
		t.Fatal("no error")
	}
}

func TestSymlinkAtomic(t *testing.T) {
	dir := t.TempDir()
	l := filepath.Join(dir, "cur")
	for _, tg := range []string{"a", "b"} {
		if err := SymlinkAtomic(tg, l); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.Readlink(l); got != tg {
			t.Fatalf("link %q", got)
		}
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "s"), filepath.Join(dir, "d")
	os.WriteFile(src, []byte("x"), 0o644)
	if err := CopyFile(src, dst, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "x" {
		t.Fatalf("%q", b)
	}
	if err := CopyFile(filepath.Join(dir, "none"), dst, 0o600); err == nil {
		t.Fatal("no error for a missing source")
	}
}

func TestSymlinkAtomicConcurrentLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "current")
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- SymlinkAtomic(fmt.Sprintf("t%d", i), link)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("temporary links left behind: %v", ents)
	}
}
