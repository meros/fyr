package peer

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

// Ignored is the owner's list of found installs not to show (#259, V0.lan-candidates):
// local to this install like Drive, never part of the roster, and reversible. An
// ignored install is not listed under Not paired; it is never a member, so nothing
// dials it either way.
type Ignored struct {
	mu   sync.Mutex
	path string
	at   map[string]time.Time
}

// OpenIgnored reads the list in the data folder; "" keeps it in memory only.
func OpenIgnored(dir string) *Ignored {
	g := &Ignored{at: map[string]time.Time{}}
	if dir == "" {
		return g
	}
	g.path = dir + "/ignored.json"
	if b, err := readFile(g.path); err == nil {
		var m map[string]time.Time
		if json.Unmarshal(b, &m) == nil {
			for id, t := range m {
				g.at[id] = t
			}
		}
	}
	return g
}

// Has says the install is ignored.
func (g *Ignored) Has(id string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.at[id]
	return ok
}

// Set ignores an install (on) or shows it again (off), and saves the list.
func (g *Ignored) Set(id string, on bool, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if on {
		if _, ok := g.at[id]; !ok {
			g.at[id] = now.UTC()
		}
	} else {
		delete(g.at, id)
	}
	if g.path == "" {
		return nil
	}
	b, _ := json.Marshal(g.at)
	return writeFile(g.path, b)
}

// IgnoredView is an ignored install as Settings → Hosts lists it under "Show ignored".
type IgnoredView struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
	// Addr is where it is heard now, "" while it is not.
	Addr string `json:"addr,omitempty"`
}

// IgnoredList lists the ignored installs, oldest first; never nil (the client reads a list).
func (e *Env) IgnoredList(ctx context.Context) []IgnoredView {
	out := []IgnoredView{}
	if e.Ignore == nil {
		return out
	}
	e.Ignore.mu.Lock()
	for id, at := range e.Ignore.at {
		out = append(out, IgnoredView{ID: id, At: at})
	}
	e.Ignore.mu.Unlock()
	for i := range out {
		if a := e.LANAddrs(ctx, out[i].ID); len(a) > 0 {
			out[i].Addr = a[0]
		}
	}
	slices.SortFunc(out, func(a, b IgnoredView) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// SetIgnored ignores a found install, or shows it again. A member is removed, not ignored.
func (e *Env) SetIgnored(id string, on bool) error {
	if e.Ignore == nil {
		return errors.New("this install keeps no ignore list")
	}
	if id == "" || len(id) > 64 {
		return errors.New("no install id")
	}
	if on {
		if id == e.Key.ID() {
			return errors.New("that is this install")
		}
		if _, in := e.Roster.Member(id); in {
			return errors.New("that install is paired: remove it instead")
		}
	}
	return e.Ignore.Set(id, on, time.Now())
}
