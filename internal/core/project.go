package core

import (
	"hash/fnv"
	"path"
	"strings"
	"time"
	"unicode"
)

// Project is a name, a colour and one folder on its host (SPEC 0.5 V0.55,
// decision 0017), with defaults and quiet hours.
type Project struct {
	ID string `json:"id"`
	// Folder is a full path on this host; threads that run in it or below
	// land in the project. "" means threads are only filed by hand.
	Folder    string     `json:"folder,omitempty"`
	Edited    bool       `json:"edited,omitempty"` // the user changed it
	Auto      bool       `json:"auto,omitempty"`   // made by matching before V0.28; migration only
	Name      string     `json:"name"`
	Color     string     `json:"color"` // a name from Palette
	Icon      Icon       `json:"icon"`
	Profile   string     `json:"profile,omitempty"`
	Model     string     `json:"model,omitempty"`
	Quiet     QuietHours `json:"quiet,omitempty"` // quiet hours (V0.46): a list of windows
	CreatedAt time.Time  `json:"createdAt"`
}

// Covers reports whether a thread in dir lands in a project with folder:
// the folder itself and every folder below it.
func Covers(folder, dir string) bool {
	if folder == "" || dir == "" {
		return false
	}
	folder, dir = path.Clean(folder), path.Clean(dir)
	return dir == folder || folder == "/" || strings.HasPrefix(dir, folder+"/")
}

// MatchFolder is the one matching rule (SPEC 0.5 V0.55): the project whose
// folder is dir or contains it, the longest folder first, ties by id; ""
// when none does.
func MatchFolder(ps []*Project, dir string) string {
	best, bestLen := "", -1
	for _, p := range ps {
		if !Covers(p.Folder, dir) {
			continue
		}
		n := len(path.Clean(p.Folder))
		if n > bestLen || (n == bestLen && p.ID < best) {
			best, bestLen = p.ID, n
		}
	}
	return best
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
