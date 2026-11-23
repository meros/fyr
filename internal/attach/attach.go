// Package attach stores files the user attaches to a message (SPEC 0.5
// V0.18): images, documents, logs, photos. Each attachment has its own
// directory under Dir: meta.json, and the file under file/<name> (a folder
// preview under tree/). Attachments saved before review 2026-11-15 have the
// file beside meta.json; meta.json's absolute path still finds it.
package attach

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/fsutil"
)

var ErrTooLarge = errors.New("the file is larger than the limit")

// Meta describes one stored attachment.
type Meta struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
	Path      string `json:"path"`
	// Entry is set for a folder preview (V0.47): the page, a slash path
	// inside the folder; Path is then that page's stored file.
	Entry string `json:"entry,omitempty"`
}

type Store struct {
	Dir string
	Max int64 // bytes; 0 means 25 MB
}

var idRe = regexp.MustCompile(`^[0-9a-f]{20}$`)

func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == ".." || strings.TrimSpace(name) == "" {
		name = "file"
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[len(r)-120:])
	}
	return name
}

// Save stores r under a new id. The media type falls back to the name's
// extension, then to application/octet-stream.
func (s *Store) Save(name, mediaType string, r io.Reader) (Meta, error) {
	max := s.Max
	if max == 0 {
		max = 25 << 20
	}
	b := make([]byte, 10)
	rand.Read(b)
	m := Meta{ID: hex.EncodeToString(b), Name: cleanName(name)}
	m.MediaType = strings.TrimSpace(strings.Split(mediaType, ";")[0])
	if m.MediaType == "" || m.MediaType == "application/octet-stream" {
		if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(m.Name))); t != "" {
			m.MediaType = strings.Split(t, ";")[0]
		} else {
			m.MediaType = "application/octet-stream"
		}
	}
	dir := filepath.Join(s.Dir, m.ID)
	// The file goes in its own subdirectory, so no upload name (meta.json)
	// can clash with the store's own files (review 2026-11-15).
	if err := os.MkdirAll(filepath.Join(dir, "file"), 0o700); err != nil {
		return Meta{}, err
	}
	m.Path = filepath.Join(dir, "file", m.Name)
	f, err := os.OpenFile(m.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Meta{}, err
	}
	n, err := io.Copy(f, io.LimitReader(r, max+1))
	f.Close()
	if err == nil && n > max {
		err = fmt.Errorf("%w (%d MB)", ErrTooLarge, max>>20)
	}
	if err != nil {
		os.RemoveAll(dir)
		return Meta{}, err
	}
	m.Size = n
	j, _ := json.Marshal(m)
	if err := fsutil.WriteAtomic(filepath.Join(dir, "meta.json"), j, 0o600); err != nil {
		return Meta{}, err
	}
	return m, nil
}

// Get returns a stored attachment by id.
func (s *Store) Get(id string) (Meta, error) {
	if !idRe.MatchString(id) {
		return Meta{}, fmt.Errorf("no attachment %q", id)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, id, "meta.json"))
	if err != nil {
		return Meta{}, fmt.Errorf("no attachment %q", id)
	}
	var m Meta
	return m, json.Unmarshal(b, &m)
}

// Folder previews (SPEC 0.5 V0.47).
const (
	TreeMaxFiles = 500
	TreeMaxBytes = 50 << 20
)

var ErrTreeTooBig = fmt.Errorf("the folder is over %d files or %d MB", TreeMaxFiles, TreeMaxBytes>>20)

// TreeFile is one file of a folder preview: Rel is its slash path in the
// folder, Abs the file to copy (a link's target).
type TreeFile struct {
	Rel  string
	Abs  string
	Size int64
}

// ScanTree lists the files of root for a folder preview, sorted by Rel:
// regular files and links to files inside root. Names that start with "."
// and node_modules are skipped; links that point outside root (or to a
// folder) are skipped and returned in outside.
func ScanTree(root string) (files []TreeFile, outside []string, err error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	var total int64
	err = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == real {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(real, p)
		rel = filepath.ToSlash(rel)
		abs := p
		if d.Type()&fs.ModeSymlink != 0 {
			t, err := filepath.EvalSymlinks(p)
			st, err2 := os.Stat(t)
			if err != nil || err2 != nil || !st.Mode().IsRegular() || !within(real, t) {
				outside = append(outside, rel)
				return nil
			}
			abs = t
		} else if !d.Type().IsRegular() {
			return nil
		}
		st, err := os.Stat(abs)
		if err != nil {
			return err
		}
		total += st.Size()
		files = append(files, TreeFile{Rel: rel, Abs: abs, Size: st.Size()})
		if len(files) > TreeMaxFiles || total > TreeMaxBytes {
			return ErrTreeTooBig
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	sort.Strings(outside)
	return files, outside, nil
}

func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}

// cleanRel returns rel as a clean slash path inside a folder, or false.
func cleanRel(rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") || strings.ContainsRune(rel, 0) {
		return "", false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", false
		}
	}
	c := path.Clean(rel)
	return c, c != "." && c != ""
}

// SaveTree copies files (from ScanTree) under a new id; entry, one of the
// files, is the page.
func (s *Store) SaveTree(name, entry string, files []TreeFile) (Meta, error) {
	entry, ok := cleanRel(entry)
	found := false
	for _, f := range files {
		found = found || f.Rel == entry
	}
	if !ok || !found {
		return Meta{}, fmt.Errorf("%s is not a file in the folder", entry)
	}
	b := make([]byte, 10)
	rand.Read(b)
	m := Meta{ID: hex.EncodeToString(b), Name: cleanName(name), MediaType: "text/html", Entry: entry}
	dir := filepath.Join(s.Dir, m.ID)
	fail := func(err error) (Meta, error) {
		os.RemoveAll(dir)
		return Meta{}, err
	}
	for _, f := range files {
		rel, ok := cleanRel(f.Rel)
		if !ok {
			return fail(fmt.Errorf("bad path %q", f.Rel))
		}
		dst := filepath.Join(dir, "tree", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fail(err)
		}
		n, err := copyFile(dst, f.Abs)
		if err != nil {
			return fail(err)
		}
		m.Size += n
	}
	m.Path = filepath.Join(dir, "tree", filepath.FromSlash(entry))
	j, _ := json.Marshal(m)
	if err := fsutil.WriteAtomic(filepath.Join(dir, "meta.json"), j, 0o600); err != nil {
		return fail(err)
	}
	return m, nil
}

func copyFile(dst, src string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(in, TreeMaxBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// TreePath returns the stored file rel of folder preview m: a regular file
// inside the folder, never a path with "..", an absolute path or a folder.
func (s *Store) TreePath(m Meta, rel string) (string, error) {
	c, ok := cleanRel(rel)
	if m.Entry == "" || !ok || !idRe.MatchString(m.ID) {
		return "", fmt.Errorf("no file %q", rel)
	}
	root := filepath.Join(s.Dir, m.ID, "tree")
	p := filepath.Join(root, filepath.FromSlash(c))
	if st, err := os.Lstat(p); err != nil || !st.Mode().IsRegular() || !within(root, p) {
		return "", fmt.Errorf("no file %q", rel)
	}
	return p, nil
}
