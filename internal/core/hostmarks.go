package core

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The owner's host marks (V0.171): an icon and a colour per host, keyed by
// the host's install id (decision 0031: the id is the one key of a host). It
// belongs to the owner, so every front writes the whole set to every host it
// follows and reads the newest back (`at` decides), as the host order
// (V0.132). An entry with neither leaves the host its automatic mark. Owner
// only (a host command with a receipt); no agent tool sends it.
//
// V0.208: an agent fills in what the owner has not set, as for a project
// (V0.116). Its choice is not in this set: the set is one newest-wins value,
// and two hosts writing it would lose each other's. Each host keeps its own
// agent's choice and the fronts lay it under the owner's marks.
const (
	KindHostMarksSet = "host.hostMarks"
	// KindHostSetupRun starts this host's mark setup run (V0.208).
	KindHostSetupRun = "host.setupRun"
	// EffectHostMarksSet stores the marks on the host.
	EffectHostMarksSet EffectKind = "hostMarks.set"
	maxHostMarks                  = 100
	// MaxHostIcon bounds an icon: a glyph name or one emoji (a few code points).
	MaxHostIcon = 24
)

// hostColor is a palette name (the clients' ten hues).
var hostColor = regexp.MustCompile(`^[a-z]{1,16}$`)

// HostIcons are the glyph names of a host's icon, as the clients draw them.
var HostIcons = []string{"laptop", "monitor", "server", "house", "cloud", "cpu", "database", "terminal", "box"}

// HostMarkBy says who set a mark's icon and colour (V0.208): ByOwner, ByAgent
// or "". In the owner's set an empty value with ByOwner is the owner's choice
// of the automatic one, which an agent does not replace.
type HostMarkBy struct {
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
}

// HostMarkByOf reads a mark's by; anything else than the two names is left out.
func HostMarkByOf(v any) HostMarkBy {
	m, _ := v.(map[string]any)
	who := func(k string) string {
		if s, _ := m[k].(string); s == ByOwner || s == ByAgent {
			return s
		}
		return ""
	}
	return HostMarkBy{Icon: who("icon"), Color: who("color")}
}

// Map is by as a mark's JSON object.
func (b HostMarkBy) Map() map[string]any {
	m := map[string]any{}
	if b.Icon != "" {
		m["icon"] = b.Icon
	}
	if b.Color != "" {
		m["color"] = b.Color
	}
	return m
}

// HostMarkOwned is who set an entry of the owner's set: a value there is the
// owner's (marks from before V0.208 have no by), and so is a cleared field.
func HostMarkOwned(icon, color string, by HostMarkBy) HostMarkBy {
	var o HostMarkBy
	if icon != "" || by.Icon == ByOwner {
		o.Icon = ByOwner
	}
	if color != "" || by.Color == ByOwner {
		o.Color = ByOwner
	}
	return o
}

// HostIconOK reports whether an agent may set v as an icon: a glyph name or
// an emoji.
func HostIconOK(v string) bool {
	if slices.Contains(HostIcons, v) {
		return true
	}
	return v != "" && HostMarkOK("x", v, "") && strings.IndexFunc(v, func(r rune) bool { return r > unicode.MaxASCII && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) == 0
}

// HostMarkOK checks one mark; empty icon and colour are allowed (automatic).
func HostMarkOK(id, icon, color string) bool {
	return id != "" && len(id) <= 64 && utf8.RuneCountInString(icon) <= 8 && len(icon) <= MaxHostIcon && strings.TrimSpace(icon) == icon && (color == "" || hostColor.MatchString(color))
}

func init() {
	// V0.208: the engine starts the thread first, as for project.setupRun.
	RegisterKind(&InputKind{Name: KindHostSetupRun, On: RecHost, From: []string{ByOwner, ByObserved}, Reduce: func(Rec, Input, time.Time) (Result, error) { return Result{}, nil }})
	RegisterKind(&InputKind{Name: KindHostMarksSet, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		a := mapArgs(in)
		raw, ok := a["marks"].(map[string]any)
		if !ok || len(raw) > maxHostMarks {
			return Result{}, reject(RejectBadArgs, "marks must be an object of at most %d marks by host id", maxHostMarks)
		}
		at, err := time.Parse(time.RFC3339Nano, a.Str("at"))
		if err != nil || at.IsZero() {
			return Result{}, reject(RejectBadArgs, "at must be the time the owner set the marks (RFC 3339)")
		}
		marks := map[string]any{}
		for id, v := range raw {
			m, ok := v.(map[string]any)
			if !ok {
				return Result{}, reject(RejectBadArgs, "the mark of %s must be {icon, color}", id)
			}
			icon, _ := m["icon"].(string)
			color, _ := m["color"].(string)
			if !HostMarkOK(id, icon, color) {
				return Result{}, reject(RejectBadArgs, "the mark of %q: an icon is a glyph name or an emoji, a colour a palette name", id)
			}
			by := HostMarkByOf(m["by"])
			if by.Icon != ByOwner {
				by.Icon = ""
			}
			if by.Color != ByOwner {
				by.Color = ""
			}
			if icon == "" && color == "" && by == (HostMarkBy{}) {
				continue
			}
			e := map[string]any{}
			if by != (HostMarkBy{}) {
				e["by"] = by.Map()
			}
			if icon != "" {
				e["icon"] = icon
			}
			if color != "" {
				e["color"] = color
			}
			marks[id] = e
		}
		return Result{Effects: []Effect{{Kind: EffectHostMarksSet, Request: &EffectReq{Action: KindHostMarksSet, Args: map[string]any{"marks": marks, "at": at.UTC().Format(time.RFC3339Nano)}}}}}, nil
	}})
}
