package core

import (
	"hash/fnv"
	"strings"
	"time"
	"unicode"
)

// Project is an identity that does not depend on a host; it points at its
// work through sources (SPEC 0.5 V0.23).
type Project struct {
	ID        string    `json:"id"`
	Sources   []Source  `json:"sources"`
	Edited    bool      `json:"edited,omitempty"` // the user changed it; seeds leave it alone
	Name      string    `json:"name"`
	Color     string    `json:"color"` // a name from Palette
	Icon      Icon      `json:"icon"`
	Profile   string    `json:"profile,omitempty"`
	Model     string    `json:"model,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Source is where a project's work lives: a folder on one host, or a
// normalised git remote. Later kinds add sources, not project types.
type Source struct {
	Kind string `json:"kind"`           // folder, repo
	Host string `json:"host,omitempty"` // folder
	Path string `json:"path,omitempty"` // folder
	URL  string `json:"url,omitempty"`  // repo: host/owner/repo
}

// Folder is the project's folder on host, or "".
func (p Project) Folder(host string) string {
	for _, s := range p.Sources {
		if s.Kind == "folder" && s.Host == host {
			return s.Path
		}
	}
	return ""
}

// Icon is a monogram (Value empty: derived from the name), an emoji, or a
// line icon from the client's curated set.
type Icon struct {
	Kind  string `json:"kind"` // monogram, emoji, line
	Value string `json:"value,omitempty"`
}

// Palette is the project hues. They work in light and dark themes; amber is
// left out because the beam marks only what needs the user.
var Palette = []string{"slate", "blue", "sky", "teal", "green", "olive", "violet", "indigo", "rose", "brown"}

func ValidColor(c string) bool {
	for _, p := range Palette {
		if p == c {
			return true
		}
	}
	return false
}

// DefaultColor picks a palette hue from the name, so it is stable.
func DefaultColor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return Palette[int(h.Sum32()%uint32(len(Palette)))]
}

// Monogram is one or two capital letters from the name's words.
func Monogram(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var out []rune
	for _, w := range words {
		r := []rune(w)
		out = append(out, unicode.ToUpper(r[0]))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

// ProjectFor returns the id of the project whose folder source on host is
// the longest root containing cwd; failing that, the project with a repo
// source equal to remote (normalised); else "".
func ProjectFor(ps []Project, host, cwd, remote string) string {
	cwd = strings.TrimRight(cwd, "/")
	best, bestLen := "", -1
	byRepo := ""
	for _, p := range ps {
		for _, s := range p.Sources {
			switch s.Kind {
			case "folder":
				root := strings.TrimRight(s.Path, "/")
				if s.Host != host || root == "" {
					continue
				}
				if (cwd == root || strings.HasPrefix(cwd, root+"/")) && len(root) > bestLen {
					best, bestLen = p.ID, len(root)
				}
			case "repo":
				if remote != "" && s.URL == remote && byRepo == "" {
					byRepo = p.ID
				}
			}
		}
	}
	if best != "" {
		return best
	}
	return byRepo
}

// NormalizeRemote turns a git remote URL into host/owner/repo, lower case.
func NormalizeRemote(u string) string {
	u = strings.TrimSpace(strings.ToLower(u))
	if u == "" {
		return ""
	}
	for _, p := range []string{"git+ssh://", "ssh://", "https://", "http://", "git://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	u = strings.Replace(u, ":", "/", 1)
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	return u
}
