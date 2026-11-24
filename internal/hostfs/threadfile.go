package hostfs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// V0.message-links-files: a file a message names is read from the thread's
// host, and only from the thread's own folders.

// ErrNoFile is the one answer for a path that is not served: missing, not a
// regular file, outside the thread's folders, or a name that holds secrets.
// One answer, so a caller learns nothing about files it may not read.
var ErrNoFile = errors.New("no such file in the thread's folders")

// File is one file of a thread's folders.
type File struct {
	// Abs is the file's real path (symlinks followed).
	Abs  string
	Size int64
	// Kind is "text", "image" or "other" (a binary file: saved, not shown).
	Kind string
	// MediaType is what the file is served as.
	MediaType string
}

// secretDirs are folders whose files are never served, wherever they are.
var secretDirs = map[string]bool{".git": true, ".ssh": true, ".gnupg": true, ".aws": true}

// secretName reports a file name that usually holds a secret. An env file's
// example (".env.example") is not one.
func secretName(name string) bool {
	switch name {
	case ".env", ".netrc", ".npmrc", ".pgpass":
		return true
	}
	if rest, ok := strings.CutPrefix(name, ".env."); ok {
		switch rest {
		case "example", "sample", "template", "dist":
			return false
		}
		return true
	}
	return false
}

// fileRoots is the real path of each root that may be served from. The home
// folder and what is above it are no root: a thread that runs in the home
// folder would otherwise serve everything the owner has.
func fileRoots(home string, roots []string) []string {
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	var out []string
	for _, r := range roots {
		if r == "" || !filepath.IsAbs(r) {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Clean(r))
		if err != nil || real == string(filepath.Separator) {
			continue
		}
		if home != "" && inside(real, home) {
			continue
		}
		out = append(out, real)
	}
	return out
}

var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".avif": "image/avif", ".svg": "image/svg+xml",
}

// sniff reads the start of the file: text when it is valid UTF-8 without a
// NUL byte.
func sniff(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return "other"
	}
	defer f.Close()
	b := make([]byte, 8192)
	n, _ := io.ReadFull(f, b)
	b = b[:n]
	for i := 0; i < 3 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1] // a character cut by the read's end
	}
	if !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return "other"
	}
	return "text"
}

// ThreadFile finds the file a message names as p: an absolute path, one
// under ~, or a path relative to one of dirs (the thread's folder first,
// then its worktrees). The file must lie inside dirs or extra (the temp
// folder), after symlinks are followed.
func ThreadFile(home string, dirs, extra []string, p string) (File, error) {
	if p == "" || strings.IndexByte(p, 0) >= 0 {
		return File{}, ErrNoFile
	}
	bases := fileRoots(home, dirs)
	roots := append(append([]string{}, bases...), fileRoots(home, extra)...)
	var cands []string
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		if home == "" {
			return File{}, ErrNoFile
		}
		cands = []string{filepath.Join(home, p[1:])}
	case filepath.IsAbs(p):
		cands = []string{filepath.Clean(p)}
	default:
		for _, b := range bases {
			cands = append(cands, filepath.Join(b, p))
		}
	}
	for _, c := range cands {
		real, err := filepath.EvalSymlinks(c)
		if err != nil {
			continue
		}
		var root string
		for _, r := range roots {
			if real != r && inside(r, real) {
				root = r
				break
			}
		}
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, real)
		if err != nil {
			continue
		}
		segs := strings.Split(rel, string(filepath.Separator))
		secret := secretName(segs[len(segs)-1])
		for _, s := range segs[:len(segs)-1] {
			secret = secret || secretDirs[s]
		}
		if secret {
			continue
		}
		st, err := os.Stat(real)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		f := File{Abs: real, Size: st.Size()}
		if mt := imageTypes[strings.ToLower(filepath.Ext(real))]; mt != "" {
			f.Kind, f.MediaType = "image", mt
		} else if sniff(real) == "text" {
			f.Kind, f.MediaType = "text", "text/plain; charset=utf-8"
		} else {
			f.Kind, f.MediaType = "other", "application/octet-stream"
		}
		return f, nil
	}
	return File{}, ErrNoFile
}
