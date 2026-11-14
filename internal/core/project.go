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
	ID        string     `json:"id"`
	Sources   []Source   `json:"sources"`
	Edited    bool       `json:"edited,omitempty"` // the user changed it; seeds leave it alone
	Auto      bool       `json:"auto,omitempty"`   // made by matching before V0.28; migration only
	Seeded    bool       `json:"seeded,omitempty"` // came from another source (T3); kept when empty
	Name      string     `json:"name"`
	Color     string     `json:"color"` // a name from Palette
	Icon      Icon       `json:"icon"`
	Profile   string     `json:"profile,omitempty"`
	Model     string     `json:"model,omitempty"`
	Quiet     QuietHours `json:"quiet,omitempty"` // quiet hours (V0.46): a list of windows
	CreatedAt time.Time  `json:"createdAt"`
}

// Source is where a project's work lives: a folder on one host, or a
// normalised git remote. Later kinds add sources, not project types.
type Source struct {
	Kind string `json:"kind"`           // folder, repo
	Host string `json:"host,omitempty"` // folder
	Path string `json:"path,omitempty"` // folder
	URL  string `json:"url,omitempty"`  // repo: host/owner/repo
	// Main marks the folder new threads start in on its host (SPEC 0.5
	// V0.42); without one, the first folder on the host is main.
	Main bool `json:"main,omitempty"`
}

// Folder is the project's main folder on host, or "" (V0.42).
func (p Project) Folder(host string) string {
	if i := p.MainIndex(host); i >= 0 {
		return p.Sources[i].Path
	}
	return ""
}

// MainIndex is the index of the main folder on host: the one marked main,
// else the first folder there; -1 when the project has no folder there.
func (p Project) MainIndex(host string) int {
	first := -1
	for i, s := range p.Sources {
		if s.Kind != "folder" || s.Host != host {
			continue
		}
		if s.Main {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

// SetMain marks source i main and clears the mark on the other folders on
// its host. It reports false when i is not a folder source.
func (p *Project) SetMain(i int) bool {
	if i < 0 || i >= len(p.Sources) || p.Sources[i].Kind != "folder" {
		return false
	}
	src := append([]Source(nil), p.Sources...)
	for k := range src {
		if src[k].Kind == "folder" && src[k].Host == src[i].Host {
			src[k].Main = k == i
		}
	}
	p.Sources = src
	return true
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
