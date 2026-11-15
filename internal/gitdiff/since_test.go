package gitdiff

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// look is what the client records when the owner leaves the Changes tab.
func look(c Changes) Seen {
	s := Seen{Head: c.Head, Files: map[string]string{}}
	for _, f := range c.Files {
		s.Files[f.Path] = f.Blob
	}
	return s
}

func paths(fs []File) string {
	var p []string
	for _, f := range fs {
		p = append(p, f.Path)
	}
	return strings.Join(p, ",")
}

// SPEC 0.5 V0.65: the head and every file's blob id; a blob is the content,
// so the same content has the same id committed or not.
func TestHeadAndBlobs(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "edit.txt", "one\nTWO\nthree\n")
	write(t, dir, "fresh.txt", "hello\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	ctx := context.Background()
	c, err := Summary(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Head != git(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("head %q", c.Head)
	}
	f := byPath(c)
	if f["edit.txt"].Blob != git(t, dir, "hash-object", "edit.txt") || f["fresh.txt"].Blob != git(t, dir, "hash-object", "fresh.txt") {
		t.Fatalf("blobs of uncommitted files: %+v", c.Files)
	}
	if f["gone.txt"].Blob != "" {
		t.Fatalf("a deleted file has no blob: %+v", f["gone.txt"])
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "work")
	c2, _ := Summary(ctx, dir)
	if g := byPath(c2); g["edit.txt"].Blob != f["edit.txt"].Blob || g["fresh.txt"].Blob != f["fresh.txt"].Blob {
		t.Fatalf("committing changed the blob ids: %+v vs %+v", c2.Files, c.Files)
	}
	if c2.Head == c.Head {
		t.Fatal("head did not move")
	}
}

// SPEC 0.5 V0.65: since an ancestor look: only files whose content differs
// from what was seen, with stats from the seen head; commits counted;
// uncommitted edits count; a file back to its base content counts too.
func TestSinceAncestor(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "edit.txt", "one\nTWO\nthree\n")
	write(t, dir, "keep.txt", "a\nB\nc\n")
	git(t, dir, "commit", "-qam", "first")
	ctx := context.Background()
	c, _ := Summary(ctx, dir)
	seen := look(c)

	// Nothing changed: nothing new.
	s, err := SinceSeen(ctx, dir, c, seen)
	if err != nil || s.Mode != "ancestor" || s.Commits != 0 || len(s.Files) != 0 {
		t.Fatalf("unchanged: %+v %v", s, err)
	}

	write(t, dir, "edit.txt", "one\nTWO\nthree\nfour\n")
	git(t, dir, "commit", "-qam", "second")
	write(t, dir, "fresh.txt", "x\ny\n")           // untracked, new
	write(t, dir, "keep.txt", "a\nb\nc\n")         // back to base: no longer in the branch
	c, _ = Summary(ctx, dir)
	s, err = SinceSeen(ctx, dir, c, seen)
	if err != nil || s.Mode != "ancestor" || s.Commits != 1 || s.Head != seen.Head {
		t.Fatalf("%+v %v", s, err)
	}
	if paths(s.Files) != "edit.txt,fresh.txt,keep.txt" {
		t.Fatalf("new files %s", paths(s.Files))
	}
	f := map[string]File{}
	for _, x := range s.Files {
		f[x.Path] = x
	}
	if f["edit.txt"].Additions != 1 || f["edit.txt"].Deletions != 0 || f["edit.txt"].Status != "modified" {
		t.Fatalf("edit.txt since the look: %+v", f["edit.txt"])
	}
	if f["fresh.txt"].Status != "added" || f["fresh.txt"].Additions != 2 || f["fresh.txt"].Blob == "" {
		t.Fatalf("fresh.txt: %+v", f["fresh.txt"])
	}
	if f["keep.txt"].Additions != 1 || f["keep.txt"].Deletions != 1 {
		t.Fatalf("keep.txt: %+v", f["keep.txt"])
	}

	// The per-file diff runs from the seen head to the working tree.
	p, err := FilePatchSince(ctx, dir, "edit.txt", seen.Head)
	if err != nil || !strings.Contains(p.Patch, "+four") || strings.Contains(p.Patch, "+TWO") {
		t.Fatalf("since patch: %q %v", p.Patch, err)
	}
	p, _ = FilePatchSince(ctx, dir, "fresh.txt", seen.Head)
	if !strings.Contains(p.Patch, "+x\n+y") {
		t.Fatalf("untracked since patch: %q", p.Patch)
	}

	// An uncommitted edit counts once its content differs from what was seen.
	c, _ = Summary(ctx, dir)
	seen = look(c)
	write(t, dir, "fresh.txt", "x\ny\nz\n")
	c, _ = Summary(ctx, dir)
	s, _ = SinceSeen(ctx, dir, c, seen)
	if paths(s.Files) != "fresh.txt" {
		t.Fatalf("uncommitted edit: %s", paths(s.Files))
	}
}

// SPEC 0.5 V0.65: after a rebase the seen head is not an ancestor: the
// range-diff names the rewritten commits, and the files are those whose
// content differs; a file the rebase only carried over is not new.
func TestSinceRebased(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "edit.txt", "one\nTWO\nthree\n")
	git(t, dir, "commit", "-qam", "change two")
	write(t, dir, "fresh.txt", "f\n")
	git(t, dir, "add", "fresh.txt")
	git(t, dir, "commit", "-qm", "add fresh")
	ctx := context.Background()
	c, _ := Summary(ctx, dir)
	seen := look(c)

	// main moves on; the branch is rebased and its second commit amended.
	git(t, dir, "checkout", "-q", "main")
	write(t, dir, "keep.txt", "a\nb\nc\nd\n")
	git(t, dir, "commit", "-qam", "main moves")
	git(t, dir, "checkout", "-q", "feature")
	git(t, dir, "rebase", "-q", "main")
	write(t, dir, "fresh.txt", "f\ng\n")
	git(t, dir, "commit", "-qa", "--amend", "-m", "add fresh")

	c, _ = Summary(ctx, dir)
	s, err := SinceSeen(ctx, dir, c, seen)
	if err != nil || s.Mode != "rebased" {
		t.Fatalf("%+v %v", s, err)
	}
	if paths(s.Files) != "fresh.txt" {
		t.Fatalf("only fresh.txt changed content: %s", paths(s.Files))
	}
	if !strings.Contains(s.RangeDiff, "change two") || !strings.Contains(s.RangeDiff, "add fresh") || !strings.Contains(s.RangeDiff, " = ") || !strings.Contains(s.RangeDiff, " ! ") {
		t.Fatalf("range-diff: %q", s.RangeDiff)
	}
	p, err := FilePatchSince(ctx, dir, "fresh.txt", seen.Head)
	if err != nil || !strings.Contains(p.Patch, "+g") {
		t.Fatalf("since patch after rebase: %q %v", p.Patch, err)
	}
}

// SPEC 0.5 V0.65: a seen commit that no longer exists, and a since that is
// not a commit id.
func TestSinceMissingAndBadIDs(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "edit.txt", "one\nTWO\nthree\n")
	ctx := context.Background()
	c, _ := Summary(ctx, dir)
	s, err := SinceSeen(ctx, dir, c, Seen{Head: strings.Repeat("ab", 20), Files: map[string]string{}})
	if err != nil || s.Mode != "missing" || paths(s.Files) != "edit.txt" || s.Files[0].Additions != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	for _, bad := range []string{"HEAD", "--output=/tmp/x", "main", "abc"} {
		if _, err := FilePatchSince(ctx, dir, "edit.txt", bad); err != ErrBadSince {
			t.Fatalf("%q: want ErrBadSince, got %v", bad, err)
		}
	}
}
