package core

import (
	"encoding/json"
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

// The project kinds (#48, SPEC 3.15): the owner's project commands are Input
// kinds of the project record on the one path. The engine checks a folder
// on the host and puts the clean path in args.folder before the reduce;
// threads land in projects by their folders (MatchFolder), which the engine
// works out for every thread after each project Input.
const (
	KindProjectCreate = "project.create"
	KindProjectUpdate = "project.update"
	KindProjectDelete = "project.delete"
	// KindProjectMatched files a thread by its folder, not by hand (V0.55):
	// args ProjectMatch.
	KindProjectMatched = "project.matched"
)

// ProjectMatch is the project a thread's folder lands it in, "" for none.
type ProjectMatch struct{ ID string }

func init() {
	for _, k := range []*InputKind{
		{Name: KindProjectCreate, On: RecProject, Makes: true, From: ownerOnly, Reduce: reduceProjectCreate},
		{Name: KindProjectUpdate, On: RecProject, From: ownerOnly, Reduce: reduceProjectUpdate},
		{Name: KindProjectDelete, On: RecProject, From: ownerOnly, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			id := r.P.ID
			*r.P = Project{}
			return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": id}}, nil
		}},
		{Name: KindProjectMatched, From: observed, Untouched: true, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[ProjectMatch](in)
			if err == nil {
				r.T.ProjectID, r.T.ProjectManual = a.ID, false
			}
			return Result{}, err
		}},
	} {
		RegisterKind(k)
	}
}

// reduceProjectCreate makes a project (SPEC 0.5 V0.23, V0.55) and files the
// thread it was made from by hand (V0.30).
func reduceProjectCreate(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	p.Folder = a.Str("folder")
	p.Name = a.trimmed("name")
	if p.Name == "" && p.Folder != "" {
		p.Name = path.Base(p.Folder)
	}
	if p.Name == "" {
		return Result{}, reject(RejectBadArgs, "a project needs a name or a folder")
	}
	p.Color, p.Icon, p.CreatedAt, p.Edited = DefaultColor(p.Name), Icon{Kind: "monogram"}, now, true
	res := Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}
	if tid := a.Str("threadId"); tid != "" {
		res.Follow = []Input{{Source: in.Source, Kind: "thread.setProject", Thread: tid, Args: Args{"projectId": p.ID}}}
	}
	return res, nil
}

// reduceProjectUpdate changes the fields args name; quiet hours are a list
// of windows (decision 0014 addendum), and null or an empty list clears
// them (V0.46).
func reduceProjectUpdate(r Rec, in Input, _ time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	if _, ok := a["folder"]; ok {
		p.Folder = a.Str("folder")
	}
	if n := a.trimmed("name"); n != "" {
		p.Name = n
	}
	if col := a.Str("color"); col != "" {
		if !ValidColor(col) {
			return Result{}, reject(RejectBadArgs, "color must be one of %v", Palette)
		}
		p.Color = col
	}
	if raw, ok := a["icon"]; ok {
		var ic Icon
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &ic)
		if ic.Kind != "monogram" && ic.Kind != "emoji" && ic.Kind != "line" {
			return Result{}, reject(RejectBadArgs, "icon kind must be monogram, emoji or line")
		}
		p.Icon = ic
	}
	if v, ok := a["profile"].(string); ok {
		p.Profile = v
	}
	if v, ok := a["model"].(string); ok {
		p.Model = v
	}
	if raw, ok := a["quiet"]; ok {
		var qs QuietHours
		if raw != nil {
			b, _ := json.Marshal(raw)
			if err := json.Unmarshal(b, &qs); err != nil {
				return Result{}, reject(RejectBadArgs, "quiet must be a list of {from, to, days?}")
			}
			if err := qs.Validate(); err != nil {
				return Result{}, reject(RejectBadArgs, "%s", err.Error())
			}
		}
		if len(qs) == 0 {
			qs = nil
		}
		p.Quiet = qs
	}
	p.Edited = true
	return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}, nil
}
