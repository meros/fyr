// Package gitdiff computes a thread's branch diff against its base with
// git (SPEC 0.5 V0.25): from the merge base with the upstream default
// branch to the working tree, untracked files included. Read-only; git runs
// without a shell, with a timeout, and without external diff or textconv
// drivers.
package gitdiff

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	maxFiles = 1000
	maxPatch = 400 << 10
	timeout  = 10 * time.Second
)

// emptyTree is git's empty tree object: the base of a repository without commits.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// File is one changed file.
type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"` // added, modified, deleted, renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
	// Blob is the git blob id of the file's content now, "" when it is
	// deleted or not a file (V0.65).
	Blob string `json:"blob,omitempty"`
	// Viewed is the owner's Viewed mark for this content; the server sets it.
	Viewed bool `json:"viewed,omitempty"`
}

// Changes is the diff summary of a folder.
type Changes struct {
	Git       bool   `json:"git"`
	Root      string `json:"root,omitempty"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	MergeBase string `json:"mergeBase"`
	Commits   int    `json:"commits"`
	// Head is the commit checked out, "" without commits (V0.65).
	Head      string `json:"head"`
	Files     []File `json:"files"`
	Truncated bool   `json:"truncated"`
	// Since is what changed since the owner's last look (V0.65).
	Since *Since `json:"since,omitempty"`
}

// Patch is one file's unified diff.
type Patch struct {
	Path      string `json:"path"`
	Patch     string `json:"patch"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

// runGit runs one git command in dir, writing its stdout to stdout. Optional
// locks are off so a read never contends with the agent's own git commands.
func runGit(ctx context.Context, dir string, stdout io.Writer, args ...string) error {
	return runGitIn(ctx, dir, nil, stdout, args...)
}

// runGitIn is runGit with stdin.
func runGitIn(ctx context.Context, dir string, stdin io.Reader, stdout io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"-C", dir, "--literal-pathspecs", "-c", "core.fsmonitor=false", "-c", "core.quotepath=off"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdout = stdout
	cmd.Stdin = stdin
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func out(ctx context.Context, dir string, args ...string) (string, error) {
	var b bytes.Buffer
	err := runGit(ctx, dir, &b, args...)
	return strings.TrimRight(b.String(), "\n"), err
}

// state is where the diff starts.
type state struct {
	root, branch, base, mergeBase, from, head string
	commits                             int
}

func isTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// resolve finds the repository root, branch, base and the commit the diff
// starts from. ok is false outside git.
func resolve(ctx context.Context, dir string) (s state, ok bool, err error) {
	root, err := out(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		if isTimeout(err) {
			return s, false, err
		}
		return s, false, nil
	}
	s.root = root
	s.branch, _ = out(ctx, root, "symbolic-ref", "-q", "--short", "HEAD")
	if _, err := out(ctx, root, "rev-parse", "--verify", "-q", "HEAD^{commit}"); err != nil {
		s.from = emptyTree // no commits yet
		return s, true, nil
	}
	s.from = "HEAD"
	s.head, _ = out(ctx, root, "rev-parse", "--verify", "-q", "HEAD^{commit}")
	cands := []string{}
	if h, err := out(ctx, root, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && h != "" {
		cands = append(cands, h)
	}
	cands = append(cands, "origin/main", "origin/master", "main", "master")
	for _, c := range cands {
		if _, err := out(ctx, root, "rev-parse", "--verify", "-q", c+"^{commit}"); err != nil {
			continue
		}
		mb, err := out(ctx, root, "merge-base", c, "HEAD")
		if err != nil || mb == "" {
			continue // unrelated histories
		}
		s.base, s.mergeBase, s.from = c, mb, mb
		if n, err := out(ctx, root, "rev-list", "--count", mb+"..HEAD"); err == nil {
			s.commits, _ = strconv.Atoi(n)
		}
		break
	}
	return s, true, nil
}

// Summary lists the files changed in dir's repository since the merge base.
func Summary(ctx context.Context, dir string) (Changes, error) {
	s, ok, err := resolve(ctx, dir)
	if err != nil || !ok {
		return Changes{Files: []File{}}, err
	}
	c := Changes{Git: true, Root: s.root, Branch: s.branch, Base: s.base, MergeBase: s.mergeBase, Commits: s.commits, Head: s.head, Files: []File{}}
	tracked, err := trackedChanges(ctx, s)
	if err != nil {
		return c, err
	}
	untracked, err := untrackedFiles(ctx, s.root, "")
	if err != nil {
		return c, err
	}
	files := tracked
	for _, p := range untracked {
		files = append(files, File{Path: p, Status: "added"})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) > maxFiles {
		files, c.Truncated = files[:maxFiles], true
	}
	// Line counts of untracked files only for the files that are kept.
	for i := range files {
		if files[i].Status == "added" && contains(untracked, files[i].Path) {
			files[i].Additions, files[i].Binary = countLines(filepath.Join(s.root, files[i].Path))
		}
	}
	if files == nil {
		files = []File{}
	}
	if err := fillBlobs(ctx, s, files); err != nil {
		return c, err
	}
	c.Files = files
	return c, nil
}

func contains(sorted []string, p string) bool {
	i := sort.SearchStrings(sorted, p)
	return i < len(sorted) && sorted[i] == p
}

// trackedChanges merges git's name-status and numstat for the tracked files.
func trackedChanges(ctx context.Context, s state) ([]File, error) {
	return diffFiles(ctx, s.root, s.from, true, nil)
}

// diffFiles lists the tracked files that differ between from and the
// working tree, with renames when asked, and only the given paths when any.
func diffFiles(ctx context.Context, root, from string, renames bool, paths []string) ([]File, error) {
	mode := "-M"
	if !renames {
		mode = "--no-renames"
	}
	tail := []string{from}
	if len(paths) > 0 {
		tail = append(append(tail, "--"), paths...)
	}
	ns, err := out(ctx, root, append([]string{"diff", "--no-ext-diff", "--no-textconv", mode, "--name-status", "-z"}, tail...)...)
	if err != nil {
		return nil, err
	}
	var files []File
	idx := map[string]int{}
	f := strings.Split(ns, "\x00")
	for i := 0; i+1 < len(f); i++ {
		code := f[i]
		if code == "" {
			continue
		}
		fl := File{Path: f[i+1]}
		switch code[0] {
		case 'R', 'C':
			if i+2 >= len(f) {
				i = len(f)
				continue
			}
			fl.OldPath, fl.Path, fl.Status = f[i+1], f[i+2], "renamed"
			if code[0] == 'C' {
				fl.OldPath, fl.Status = "", "added"
			}
			i += 2
		case 'A':
			fl.Status = "added"
			i++
		case 'D':
			fl.Status = "deleted"
			i++
		default: // M, T, U
			fl.Status = "modified"
			i++
		}
		idx[fl.Path] = len(files)
		files = append(files, fl)
	}
	num, err := out(ctx, root, append([]string{"diff", "--no-ext-diff", "--no-textconv", mode, "--numstat", "-z"}, tail...)...)
	if err != nil {
		return nil, err
	}
	f = strings.Split(num, "\x00")
	for i := 0; i < len(f); i++ {
		parts := strings.SplitN(f[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(f) { // a rename: the old and new paths follow
			path = f[i+2]
			i += 2
		}
		j, ok := idx[path]
		if !ok {
			continue
		}
		if parts[0] == "-" {
			files[j].Binary = true
			continue
		}
		files[j].Additions, _ = strconv.Atoi(parts[0])
		files[j].Deletions, _ = strconv.Atoi(parts[1])
	}
	return files, nil
}

// untrackedFiles lists the files git neither tracks nor ignores, sorted;
// with a path, only that path when it is one of them.
func untrackedFiles(ctx context.Context, root, path string) ([]string, error) {
	args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	if path != "" {
		args = append(args, "--", path)
	}
	o, err := out(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var list []string
	for _, p := range strings.Split(o, "\x00") {
		if p != "" {
			list = append(list, p)
		}
	}
	sort.Strings(list)
	return list, nil
}

// countLines counts lines as git does (a last line without a newline
// counts) and spots binary content (a NUL in the first 8,000 bytes).
func countLines(path string) (lines int, binary bool) {
	fi, err := os.Lstat(path)
	if err != nil || fi.IsDir() {
		return 0, false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return 1, false // git shows the link target as one line
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	buf := make([]byte, 64<<10)
	first, last := true, byte('\n')
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if first && bytes.IndexByte(buf[:min(n, 8000)], 0) >= 0 {
				return 0, true
			}
			first = false
			lines += bytes.Count(buf[:n], []byte{'\n'})
			last = buf[n-1]
		}
		if err != nil {
			break
		}
	}
	if last != '\n' {
		lines++
	}
	return lines, false
}

// capWriter keeps the first max+1 bytes and drops the rest, so git never
// blocks on a full pipe.
type capWriter struct {
	buf bytes.Buffer
	max int
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.max + 1 - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// FilePatch returns the unified diff of one file, given relative to the
// repository root. A path that is not among the changes gives an empty patch.
func FilePatch(ctx context.Context, dir, path string) (Patch, error) {
	p := Patch{Path: path}
	if path == "" || !filepath.IsLocal(path) {
		return p, nil
	}
	s, ok, err := resolve(ctx, dir)
	if err != nil || !ok {
		return p, err
	}
	tracked, err := trackedChanges(ctx, s)
	if err != nil {
		return p, err
	}
	w := &capWriter{max: maxPatch}
	found := false
	for _, f := range tracked {
		if f.Path != path {
			continue
		}
		found = true
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "-M", s.from, "--"}
		if f.OldPath != "" {
			args = append(args, f.OldPath)
		}
		if err := runGit(ctx, s.root, w, append(args, f.Path)...); err != nil {
			return p, err
		}
		break
	}
	if !found {
		un, err := untrackedFiles(ctx, s.root, path)
		if err != nil {
			return p, err
		}
		if !contains(un, path) {
			return p, nil
		}
		// --no-index exits 1 when the files differ, which they always do here.
		err = runGit(ctx, s.root, w, "diff", "--no-ext-diff", "--no-textconv", "--no-index", "--", "/dev/null", path)
		var ee *exec.ExitError
		if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
			return p, err
		}
	}
	return finish(p, w), nil
}

// finish cuts a patch at maxPatch on a line end.
func finish(p Patch, w *capWriter) Patch {
	b := w.buf.Bytes()
	if len(b) > maxPatch {
		b = b[:maxPatch]
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			b = b[:i+1]
		}
		p.Truncated = true
	}
	p.Patch = string(b)
	p.Binary = bytes.HasPrefix(b, []byte("Binary files ")) || bytes.Contains(b, []byte("\nBinary files "))
	return p
}
