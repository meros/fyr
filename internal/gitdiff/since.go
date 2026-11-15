package gitdiff

// What changed since the owner last looked (SPEC 0.5 V0.65): every file's
// blob id, the files whose content differs from the look, and after a
// rebase the range-diff of the rewritten commits.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrBadSince is a since that is not a commit id.
var ErrBadSince = errors.New("since must be a commit id")

var hexID = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

// IsID reports whether s is a hex object id (abbreviated or whole).
func IsID(s string) bool { return hexID.MatchString(s) }

// Seen is the owner's last look: the head and every listed file's blob.
type Seen struct {
	Head  string
	Files map[string]string
}

// Since is what changed since the look.
type Since struct {
	Head string    `json:"head"`
	At   time.Time `json:"at"`
	// Mode is ancestor (the seen head is an ancestor of HEAD), rebased (it
	// is not) or missing (the seen commit is gone).
	Mode               string `json:"mode"`
	Commits            int    `json:"commits"`
	Files              []File `json:"files"`
	RangeDiff          string `json:"rangeDiff,omitempty"`
	RangeDiffTruncated bool   `json:"rangeDiffTruncated,omitempty"`
}

// fillBlobs sets each file's blob id: the committed blob when the working
// tree matches HEAD, else the hash of the file as it is now (not written).
func fillBlobs(ctx context.Context, s state, files []File) error {
	if len(files) == 0 {
		return nil
	}
	known := map[string]string{}
	if s.head != "" {
		d, err := out(ctx, s.root, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", "HEAD")
		if err != nil {
			return err
		}
		dirty := map[string]bool{}
		for _, p := range strings.Split(d, "\x00") {
			dirty[p] = true
		}
		args := []string{"ls-tree", "-r", "-z", "HEAD", "--"}
		n := len(args)
		for _, f := range files {
			if f.Status != "deleted" && !dirty[f.Path] {
				args = append(args, f.Path)
			}
		}
		if len(args) > n {
			o, err := out(ctx, s.root, args...)
			if err != nil {
				return err
			}
			for _, l := range strings.Split(o, "\x00") {
				// <mode> SP <type> SP <object> TAB <file>
				meta, p, ok := strings.Cut(l, "\t")
				if !ok {
					continue
				}
				if fs := strings.Fields(meta); len(fs) == 3 && fs[1] == "blob" {
					known[p] = fs[2]
				}
			}
		}
	}
	var hash []string
	for i := range files {
		f := &files[i]
		if b, ok := known[f.Path]; ok {
			f.Blob = b
			continue
		}
		if f.Status == "deleted" || strings.ContainsAny(f.Path, "\n") {
			continue
		}
		fi, err := os.Lstat(filepath.Join(s.root, f.Path))
		if err != nil {
			continue
		}
		switch {
		case fi.Mode().IsRegular():
			hash = append(hash, f.Path)
		case fi.Mode()&os.ModeSymlink != 0:
			// git stores a link as a blob of its target.
			target, err := os.Readlink(filepath.Join(s.root, f.Path))
			if err != nil {
				continue
			}
			var b strings.Builder
			if err := runGitIn(ctx, s.root, strings.NewReader(target), &b, "hash-object", "--no-filters", "--stdin"); err != nil {
				if isTimeout(err) {
					return err
				}
				continue
			}
			f.Blob = strings.TrimSpace(b.String())
		}
	}
	if len(hash) == 0 {
		return nil
	}
	var b strings.Builder
	// --no-filters: no clean filter (an external command) runs on a read.
	err := runGitIn(ctx, s.root, strings.NewReader(strings.Join(hash, "\n")+"\n"), &b, "hash-object", "--no-filters", "--stdin-paths")
	if err != nil {
		if isTimeout(err) {
			return err
		}
		return nil // a file vanished meanwhile: no blobs this time
	}
	ids := strings.Fields(b.String())
	if len(ids) != len(hash) {
		return nil
	}
	at := map[string]string{}
	for i, p := range hash {
		at[p] = ids[i]
	}
	for i := range files {
		if b, ok := at[files[i].Path]; ok {
			files[i].Blob = b
		}
	}
	return nil
}

func commitExists(ctx context.Context, root, id string) bool {
	_, err := out(ctx, root, "cat-file", "-e", id+"^{commit}")
	return err == nil
}

// SinceSeen compares the summary c of dir with the owner's look.
func SinceSeen(ctx context.Context, dir string, c Changes, seen Seen) (*Since, error) {
	s := &Since{Head: seen.Head, Files: []File{}}
	cur := map[string]File{}
	var paths []string
	for _, f := range c.Files {
		cur[f.Path] = f
		if b, ok := seen.Files[f.Path]; !ok || b != f.Blob {
			paths = append(paths, f.Path)
		}
	}
	if !c.Truncated {
		for p := range seen.Files {
			if _, ok := cur[p]; !ok {
				paths = append(paths, p) // back to its base content
			}
		}
	}
	sort.Strings(paths)
	root := c.Root
	if root == "" {
		root = dir
	}
	if !IsID(seen.Head) || !commitExists(ctx, root, seen.Head) {
		s.Mode = "missing"
		for _, p := range paths {
			if f, ok := cur[p]; ok {
				s.Files = append(s.Files, f)
			}
		}
		return s, nil
	}
	if c.Head != "" && runGit(ctx, root, nil, "merge-base", "--is-ancestor", seen.Head, "HEAD") == nil {
		s.Mode = "ancestor"
		if n, err := out(ctx, root, "rev-list", "--count", seen.Head+"..HEAD"); err == nil {
			s.Commits = atoi(n)
		}
	} else {
		s.Mode = "rebased"
		args := []string{"range-diff", "--no-color"}
		if c.Base != "" {
			args = append(args, c.Base, seen.Head, "HEAD")
		} else {
			args = append(args, seen.Head+"...HEAD")
		}
		w := &capWriter{max: maxPatch}
		if err := runGit(ctx, root, w, args...); err == nil {
			p := finish(Patch{}, w)
			s.RangeDiff, s.RangeDiffTruncated = p.Patch, p.Truncated
		}
	}
	if len(paths) == 0 {
		return s, nil
	}
	tracked, err := diffFiles(ctx, root, seen.Head, false, paths)
	if err != nil {
		return s, err
	}
	idx := map[string]File{}
	for _, f := range tracked {
		idx[f.Path] = f
	}
	for _, p := range paths {
		cf, listed := cur[p]
		f, ok := idx[p]
		switch {
		case ok:
			f.Blob = cf.Blob
		case listed && cf.Status == "added":
			f = cf // untracked, and not in the seen commit either
		case listed:
			// The content differs from what was seen but not from the
			// seen commit: the look held uncommitted edits.
			f = File{Path: p, Status: "modified", Blob: cf.Blob}
		default:
			continue
		}
		s.Files = append(s.Files, f)
	}
	return s, nil
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// FilePatchSince returns one file's diff from the commit since to the
// working tree; an untracked file shows as added.
func FilePatchSince(ctx context.Context, dir, path, since string) (Patch, error) {
	if !IsID(since) {
		return Patch{}, ErrBadSince
	}
	p := Patch{Path: path}
	if path == "" || !filepath.IsLocal(path) {
		return p, nil
	}
	s, ok, err := resolve(ctx, dir)
	if err != nil || !ok {
		return p, err
	}
	if !commitExists(ctx, s.root, since) {
		return p, nil
	}
	w := &capWriter{max: maxPatch}
	if err := runGit(ctx, s.root, w, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", since, "--", path); err != nil {
		return p, err
	}
	if w.buf.Len() == 0 {
		un, err := untrackedFiles(ctx, s.root, path)
		if err != nil {
			return p, err
		}
		if contains(un, path) {
			err = runGit(ctx, s.root, w, "diff", "--no-ext-diff", "--no-textconv", "--no-index", "--", "/dev/null", path)
			var ee *exec.ExitError
			if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
				return p, err
			}
		}
	}
	return finish(p, w), nil
}
