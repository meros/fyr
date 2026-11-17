package core

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// The owner's host marks (V0.171): an icon and a colour per host, keyed by
// the host's install id (decision 0031: the id is the one key of a host). It
// belongs to the owner, so every front writes the whole set to every host it
// follows and reads the newest back (`at` decides), as the host order
// (V0.132). An entry with neither leaves the host its automatic mark. Owner
// only (a host command with a receipt); no agent tool sends it.
const (
	KindHostMarksSet = "host.hostMarks"
	// EffectHostMarksSet stores the marks on the host.
	EffectHostMarksSet EffectKind = "hostMarks.set"
	maxHostMarks                  = 100
	// MaxHostIcon bounds an icon: a glyph name or one emoji (a few code points).
	MaxHostIcon = 24
)

// hostColor is a palette name (the clients' ten hues).
var hostColor = regexp.MustCompile(`^[a-z]{1,16}$`)

// HostMarkOK checks one mark; empty icon and colour are allowed (automatic).
func HostMarkOK(id, icon, color string) bool {
	return id != "" && len(id) <= 64 && utf8.RuneCountInString(icon) <= 8 && len(icon) <= MaxHostIcon && strings.TrimSpace(icon) == icon && (color == "" || hostColor.MatchString(color))
}

func init() {
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
			if icon == "" && color == "" {
				continue
			}
			e := map[string]any{}
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
