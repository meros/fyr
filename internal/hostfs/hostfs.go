// Package hostfs is the host folder picker's read-only view of the owner's
// folders (SPEC 0.5 V0.42, decision 0012): list a folder, the folders of
// recent threads, git repositories found by a shallow scan, and making a new
// folder. Every path resolves through symlinks and must stay inside the
// owner's home folder.
package hostfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrOutside is a path that is not inside the home folder (or does not
// exist); the server answers 400.
var ErrOutside = errors.New("outside the home folder")

// Dir is one folder in a listing.
type Dir struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Repo bool   `json:"repo,omitempty"` // it has .git
}

// Listing is a folder and its subfolders.
type Listing struct {
	Path   string `json:"path"`
	Parent string `json:"parent"` // "" at the home folder
	Dirs   []Dir  `json:"dirs"`
}

// FS answers the picker's questions for one owner.
type FS struct {
	Home string
	// ScanRoots are extra folders to scan for repositories (config
	// scanRoots, `~` already expanded).
	ScanRoots []string
	// Folders are the project folders on this host (scanned too).
	Folders func() []string
	// Recent are thread folders, newest first, possibly repeated.
	Recent func() []string
	// TTL of the repo scan; default five minutes.
	TTL time.Duration
	Now func() time.Time

	mu       sync.Mutex
	repos    []Dir
	scanned  time.Time
	scanning bool
}

func (f *FS) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *FS) home() (string, error) {
	h, err := filepath.EvalSymlinks(f.Home)
	if err != nil || f.Home == "" {
		return "", fmt.Errorf("no home folder")
	}
	return h, nil
}

func inside(home, p string) bool {
	return p == home || strings.HasPrefix(p, home+string(filepath.Separator))
}

// Expand turns "", "~" and "~/x" into paths under home; anything else must
// be absolute.
func (f *FS) Expand(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "~" {
		return f.Home, nil
	}
	p = ExpandHome(p, f.Home)
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %s is not a full path", ErrOutside, p)
	}
	return filepath.Clean(p), nil
}

// Resolve expands p, follows symlinks and checks that the result is an
// existing folder inside the home folder. It returns the path as the owner
// typed it (cleaned) and the resolved one.
func (f *FS) Resolve(p string) (clean, real string, err error) {
	home, err := f.home()
	if err != nil {
		return "", "", err
	}
	clean, err = f.Expand(p)
	if err != nil {
		return "", "", err
	}
	real, err = filepath.EvalSymlinks(clean)
	if err != nil {
		return "", "", fmt.Errorf("%w: no folder %s", ErrOutside, clean)
	}
	if !inside(home, real) {
		return "", "", fmt.Errorf("%w: %s", ErrOutside, clean)
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return "", "", fmt.Errorf("%w: %s is not a folder", ErrOutside, clean)
	}
	// Shown paths start at the home folder as configured.
	if !inside(filepath.Clean(f.Home), clean) {
		clean = real
		if rel, err := filepath.Rel(home, real); err == nil {
			clean = filepath.Join(filepath.Clean(f.Home), rel)
		}
	}
	return clean, real, nil
}

func isRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// List lists the subfolders of p (hidden ones too; the client hides them
// unless asked). A symlink is listed only when it leads to a folder inside
// the home folder.
func (f *FS) List(p string) (Listing, error) {
	clean, real, err := f.Resolve(p)
	if err != nil {
		return Listing{}, err
	}
	home, _ := f.home()
	ents, err := os.ReadDir(real)
	if err != nil {
		return Listing{}, fmt.Errorf("%w: cannot read %s", ErrOutside, clean)
	}
	out := Listing{Path: clean, Dirs: []Dir{}}
	if clean != filepath.Clean(f.Home) {
		out.Parent = filepath.Dir(clean)
	}
	for _, e := range ents {
		target := filepath.Join(real, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			r, err := filepath.EvalSymlinks(target)
			if err != nil || !inside(home, r) {
				continue
			}
			if fi, err := os.Stat(r); err != nil || !fi.IsDir() {
				continue
			}
			target = r
		} else if !e.IsDir() {
			continue
		}
		out.Dirs = append(out.Dirs, Dir{Name: e.Name(), Path: filepath.Join(clean, e.Name()), Repo: isRepo(target)})
	}
	sort.Slice(out.Dirs, func(i, j int) bool { return strings.ToLower(out.Dirs[i].Name) < strings.ToLower(out.Dirs[j].Name) })
	return out, nil
}

// Recent are the distinct existing thread folders inside the home folder,
// newest first, at most 30.
func (f *FS) RecentDirs() []Dir {
	out := []Dir{}
	if f.Recent == nil {
		return out
	}
	seen := map[string]bool{}
	for _, p := range f.Recent() {
		clean, real, err := f.Resolve(p)
		if err != nil || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, Dir{Name: filepath.Base(clean), Path: clean, Repo: isRepo(real)})
		if len(out) == 30 {
			break
		}
	}
	return out
}

// skip is a folder the scan does not enter.
func skip(name string) bool { return strings.HasPrefix(name, ".") || name == "node_modules" }

// Scan finds git repositories under roots, two levels deep: a root that is a
// repository counts itself; a repository is not descended into; hidden
// folders, node_modules and symlinks are skipped. Roots outside the home
// folder are ignored.
func (f *FS) Scan(roots []string) []Dir {
	home, err := f.home()
	if err != nil {
		return []Dir{}
	}
	found := map[string]Dir{}
	var walk func(shown, real string, depth int)
	walk = func(shown, real string, depth int) {
		if isRepo(real) {
			found[shown] = Dir{Name: filepath.Base(shown), Path: shown, Repo: true}
			return
		}
		if depth == 2 {
			return
		}
		ents, err := os.ReadDir(real)
		if err != nil {
			return
		}
		for _, e := range ents {
			if !e.IsDir() || skip(e.Name()) {
				continue
			}
			walk(filepath.Join(shown, e.Name()), filepath.Join(real, e.Name()), depth+1)
		}
	}
	seen := map[string]bool{}
	for _, r := range roots {
		clean, real, err := f.Resolve(r)
		if err != nil || seen[real] || !inside(home, real) {
			continue
		}
		seen[real] = true
		walk(clean, real, 0)
	}
	out := make([]Dir, 0, len(found))
	for _, d := range found {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (f *FS) roots() []string {
	var r []string
	if f.Folders != nil {
		r = append(r, f.Folders()...)
	}
	return append(r, f.ScanRoots...)
}

// Repos is the cached scan of the project folders and the scan roots. A
// stale cache is returned at once and refreshed in the background; the first
// call scans.
func (f *FS) Repos() []Dir {
	ttl := f.TTL
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	f.mu.Lock()
	if f.repos == nil {
		f.mu.Unlock()
		r := f.Scan(f.roots())
		f.mu.Lock()
		f.repos, f.scanned = r, f.now()
		f.mu.Unlock()
		return r
	}
	r := f.repos
	if f.now().Sub(f.scanned) > ttl && !f.scanning {
		f.scanning = true
		go func() {
			n := f.Scan(f.roots())
			f.mu.Lock()
			f.repos, f.scanned, f.scanning = n, f.now(), false
			f.mu.Unlock()
		}()
	}
	f.mu.Unlock()
	return r
}

// ValidName checks a new folder's name.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("the folder needs a name")
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, 0) {
		return fmt.Errorf("a folder name cannot contain /")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%s is not a folder name", name)
	}
	return nil
}

// Mkdir makes the folder name inside parent and returns its path.
func (f *FS) Mkdir(parent, name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	clean, real, err := f.Resolve(parent)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(filepath.Join(real, name)); err == nil {
		return "", fmt.Errorf("%s already exists", filepath.Join(clean, name))
	}
	if err := os.Mkdir(filepath.Join(real, name), 0o755); err != nil {
		return "", err
	}
	return filepath.Join(clean, name), nil
}

// ExpandHome turns "~" and "~/x" into paths under home ("" is the user's
// home folder); any other path is returned as it is. It is the one home-folder
// expansion of fyrd (config roots, profile and quota folders, project folders).
func ExpandHome(p, home string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	if home == "" {
		if home, _ = os.UserHomeDir(); home == "" {
			return p
		}
	}
	return filepath.Join(home, p[1:])
}
