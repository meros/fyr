package core

import (
	"path"
	"sort"
	"strings"
)

// Ref names what a place is (SPEC 0.5 V0.28, decision 0008): a folder on
// one host (key "<host>:<path>") or a normalised git remote (key
// "github.com/meros/fyr").
type Ref struct {
	Kind string `json:"kind"` // folder, repo
	Key  string `json:"key"`
}

func FolderRef(host, dir string) Ref { return Ref{Kind: "folder", Key: host + ":" + path.Clean(dir)} }
func RepoRef(remote string) Ref      { return Ref{Kind: "repo", Key: NormalizeRemote(remote)} }

// Folder splits a folder ref into its host and path.
func (r Ref) Folder() (host, dir string) {
	host, dir, _ = strings.Cut(r.Key, ":")
	return host, dir
}

// Workspace is where one thread runs: a folder on a host, what it is (its
// refs) and the provider that identified it.
type Workspace struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Path     string `json:"path"`
	Provider string `json:"provider"` // scratch, clone, folder
	Refs     []Ref  `json:"refs"`
	Branch   string `json:"branch,omitempty"`
}

// Provider is the only place where a kind of place has its own code.
type Provider interface {
	Kind() string
	// Identify reports whether path is this provider's kind of place and
	// what it is.
	Identify(host, path string) (refs []Ref, branch string, ok bool)
	// Covers reports whether a project source covers a workspace ref.
	Covers(source, ref Ref) bool
	// Prepare finds a place to run a thread for the project on host. Slice
	// 1: the project's folder there; worktrees and clones come later.
	Prepare(p Project, host string) (Workspace, bool)
}

// Covers is the one rule per ref kind: a folder covers itself and every
// folder below it on the same host; a repo covers an equal key on any host.
func Covers(source, ref Ref) bool {
	if source.Kind != ref.Kind || source.Key == "" {
		return false
	}
	switch source.Kind {
	case "folder":
		sh, sp := source.Folder()
		rh, rp := ref.Folder()
		if sh != rh || sp == "" {
			return false
		}
		return rp == sp || sp == "/" || strings.HasPrefix(rp, sp+"/")
	case "repo":
		return source.Key == ref.Key
	}
	return false
}

// specificity ranks a cover: any folder (longer first) before any repo.
func specificity(source Ref) int {
	if source.Kind == "folder" {
		_, p := source.Folder()
		return 1 + len(p)
	}
	return 0
}

// Ref is a source in the matching model.
func (s Source) Ref() Ref {
	switch s.Kind {
	case "folder":
		return FolderRef(s.Host, s.Path)
	case "repo":
		return RepoRef(s.URL)
	}
	return Ref{Kind: s.Kind}
}

// SourceOf is the wire form of a ref.
func SourceOf(r Ref) Source {
	if r.Kind == "folder" {
		h, p := r.Folder()
		return Source{Kind: "folder", Host: h, Path: p}
	}
	return Source{Kind: r.Kind, URL: r.Key}
}

// MatchProject is the one matching function (SPEC 0.5 V0.28): the project
// whose sources cover any of refs, most specific cover first, ties by id;
// "" when none covers.
func MatchProject(ps []Project, refs []Ref) string {
	sorted := append([]Project(nil), ps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	best, bestScore := "", -1
	for _, p := range sorted {
		for _, s := range p.Sources {
			src := s.Ref()
			for _, r := range refs {
				if Covers(src, r) && specificity(src) > bestScore {
					best, bestScore = p.ID, specificity(src)
				}
			}
		}
	}
	return best
}

// Suggestion is the project a thread without one could get: made from its
// workspace's main folder and repo (SPEC 0.5 V0.28).
type Suggestion struct {
	Name    string   `json:"name"`
	Sources []Source `json:"sources"`
}

// Suggest returns a suggestion for a workspace with a repo ref, else nil.
func Suggest(w Workspace) *Suggestion {
	var repo, folder *Ref
	for i := range w.Refs {
		r := &w.Refs[i]
		if r.Kind == "repo" && repo == nil {
			repo = r
		}
		if r.Kind == "folder" && folder == nil {
			folder = r
		}
	}
	if repo == nil || repo.Key == "" {
		return nil
	}
	s := &Suggestion{Name: path.Base(repo.Key)}
	if folder != nil {
		s.Sources = append(s.Sources, SourceOf(*folder))
	}
	s.Sources = append(s.Sources, SourceOf(*repo))
	return s
}

// Identify asks the providers in order; the first that knows the folder
// identifies it. The last provider should accept every folder.
func Identify(providers []Provider, host, dir string) Workspace {
	dir = path.Clean(dir)
	for _, p := range providers {
		if refs, branch, ok := p.Identify(host, dir); ok {
			if refs == nil {
				refs = []Ref{}
			}
			return Workspace{Host: host, Path: dir, Provider: p.Kind(), Refs: refs, Branch: branch}
		}
	}
	return Workspace{Host: host, Path: dir, Refs: []Ref{}}
}

type coverRule struct{}

func (coverRule) Covers(source, ref Ref) bool { return Covers(source, ref) }

func folderPrepare(kind string, p Project, host string) (Workspace, bool) {
	dir := p.Folder(host)
	if dir == "" {
		return Workspace{}, false
	}
	return Workspace{Host: host, Path: dir, Provider: kind}, true
}

// ScratchProvider knows throwaway folders (T3's and Fyr's scratch roots):
// they have no refs, so they match no project.
type ScratchProvider struct {
	coverRule
	Roots []string
}

func (ScratchProvider) Kind() string { return "scratch" }

func (s ScratchProvider) Identify(_, dir string) ([]Ref, string, bool) {
	for _, r := range s.Roots {
		r = path.Clean(r)
		if r != "" && r != "." && (dir == r || strings.HasPrefix(dir, r+"/")) {
			return []Ref{}, "", true
		}
	}
	return nil, "", false
}

func (ScratchProvider) Prepare(Project, string) (Workspace, bool) { return Workspace{}, false }

// FolderProvider is any folder: its own folder ref.
type FolderProvider struct{ coverRule }

func (FolderProvider) Kind() string { return "folder" }
func (FolderProvider) Identify(host, dir string) ([]Ref, string, bool) {
	return []Ref{FolderRef(host, dir)}, "", true
}
func (FolderProvider) Prepare(p Project, host string) (Workspace, bool) {
	return folderPrepare("folder", p, host)
}

// Git runs git in dir and returns its trimmed output. The engine passes a
// real one; core stays free of I/O.
type Git func(dir string, args ...string) (string, error)

// CloneProvider knows git checkouts: the main repository root's folder ref,
// the folder's own ref when it differs (a subfolder or a worktree), and the
// origin remote as a repo ref.
type CloneProvider struct {
	coverRule
	Git Git
}

func (CloneProvider) Kind() string { return "clone" }

func (c CloneProvider) Identify(host, dir string) ([]Ref, string, bool) {
	if c.Git == nil {
		return nil, "", false
	}
	common, err := c.Git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || common == "" {
		return nil, "", false
	}
	root := common
	if path.Base(common) == ".git" {
		root = path.Dir(common)
	}
	refs := []Ref{FolderRef(host, root)}
	if path.Clean(dir) != path.Clean(root) {
		refs = append(refs, FolderRef(host, dir))
	}
	if remote, err := c.Git(dir, "remote", "get-url", "origin"); err == nil && NormalizeRemote(remote) != "" {
		refs = append(refs, RepoRef(remote))
	}
	branch, _ := c.Git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		branch = ""
	}
	return refs, branch, true
}

func (CloneProvider) Prepare(p Project, host string) (Workspace, bool) {
	return folderPrepare("clone", p, host)
}

// Draft is what "New project from this thread" starts with (SPEC 0.5
// V0.30): the suggestion for a clone with a remote, else the folder's name
// with the folder as source. A scratch workspace, the home folder and / give
// no folder source; a source that taken reports (an existing project has it)
// is left out; nil when no source is left.
func Draft(w Workspace, home string, taken func(Source) bool) *Suggestion {
	if w.Provider == "scratch" {
		return nil
	}
	var repo, folder *Ref
	for i := range w.Refs {
		r := &w.Refs[i]
		if r.Kind == "repo" && repo == nil && r.Key != "" {
			repo = r
		}
		if r.Kind == "folder" && folder == nil {
			folder = r
		}
	}
	var srcs []Source
	if folder != nil {
		if _, p := folder.Folder(); p != "/" && p != "" && p != path.Clean(home) {
			srcs = append(srcs, SourceOf(*folder))
		}
	}
	if repo != nil {
		srcs = append(srcs, SourceOf(*repo))
	}
	s := &Suggestion{}
	for _, src := range srcs {
		if !taken(src) {
			s.Sources = append(s.Sources, src)
		}
	}
	if len(s.Sources) == 0 {
		return nil
	}
	if repo != nil {
		s.Name = path.Base(repo.Key)
	} else {
		s.Name = path.Base(s.Sources[0].Path)
	}
	return s
}
