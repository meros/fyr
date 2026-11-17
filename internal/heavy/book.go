package heavy

// V0.164 section 4.6: what undeclared episodes teach. A command that ran
// heavy without asking and matches no pattern becomes a suggestion; the owner
// adds it (as a pattern, which the gate then refuses without an admission),
// ignores it for good, or removes an added pattern. All of it is kept in the
// store's KV table on the host.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type bookView struct {
	sugg  []Suggestion
	added []Added
}

// KV is the store's key-value table.
type KV interface {
	GetKV(k string) (string, error)
	SetKV(k, v string) error
}

const (
	suggestionsKey = "heavy-suggestions"
	addedKey       = "heavy-added"
	ignoredKey     = "heavy-ignored"
	// MaxSuggestions is how many suggestions are kept, the newest first.
	MaxSuggestions = 30
)

// Seen is one undeclared episode's command and size.
type Seen struct {
	Thread  string
	Command string
	At      time.Time
	Memory  uint64  // peak bytes above the idle level
	CPU     float64 // mean cores
	Seconds int     // how long it ran
}

// Suggestion is a command to add as a heavy job.
type Suggestion struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	Pattern    string    `json:"pattern"`
	Kind       string    `json:"kind"`
	Seen       int       `json:"seen"`
	Threads    int       `json:"threads"`
	LastThread string    `json:"lastThread"`
	LastAt     time.Time `json:"lastAt"`
	Memory     uint64    `json:"memory"`
	CPU        float64   `json:"cpu"`
	Seconds    int       `json:"seconds"`
	ThreadIDs  []string  `json:"threadIds,omitempty"`
}

// Added is a pattern the owner added from a suggestion.
type Added struct {
	Pattern string    `json:"pattern"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
	Refused int       `json:"refused"`
}

// Book keeps the suggestions and the added patterns, and gives the added ones
// to Matcher.
type Book struct {
	KV      KV
	Matcher *Matcher
	// Base are the patterns of the host's config (`heavyAdded`), read with the
	// owner's added ones.
	Base []Pattern

	mu      sync.Mutex
	pub     atomic.Pointer[bookView] // for readers that must not wait (GET /api/load)
	loaded  bool
	sugg    []Suggestion
	added   []Added
	ignored []string
}

// Load reads the book from the KV and gives the added patterns to the
// matcher; call it at start.
func (b *Book) Load() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	b.applyLocked()
	b.publishLocked()
}

func (b *Book) loadLocked() {
	if b.loaded {
		return
	}
	b.loaded = true
	if b.KV == nil {
		return
	}
	get := func(k string, v any) {
		if raw, _ := b.KV.GetKV(k); raw != "" {
			json.Unmarshal([]byte(raw), v)
		}
	}
	get(suggestionsKey, &b.sugg)
	get(addedKey, &b.added)
	get(ignoredKey, &b.ignored)
}

func (b *Book) publishLocked() {
	b.pub.Store(&bookView{sugg: slices.Clone(b.sugg), added: slices.Clone(b.added)})
}

func (b *Book) save(k string, v any) {
	if b.KV == nil {
		return
	}
	raw, _ := json.Marshal(v)
	b.KV.SetKV(k, string(raw))
}

func (b *Book) applyLocked() error {
	if b.Matcher == nil {
		return nil
	}
	ps := slices.Clone(b.Base)
	for _, a := range b.added {
		ps = append(ps, Pattern{Name: a.Name, Re: a.Pattern})
	}
	return b.Matcher.SetAdded(ps)
}

func sid(pattern string) string {
	h := sha1.Sum([]byte(pattern))
	return "s_" + hex.EncodeToString(h[:4])
}

// Saw records an undeclared episode's command. It reports whether the command
// already matches a heavy pattern (then there is no suggestion).
func (b *Book) Saw(s Seen) (matched bool) {
	if b.Matcher != nil && b.Matcher.Class(s.Command) != "" {
		return true
	}
	pat, kind := Suggest(s.Command)
	if pat == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	if slices.Contains(b.ignored, pat) || slices.ContainsFunc(b.added, func(a Added) bool { return a.Pattern == pat }) {
		return false
	}
	i := slices.IndexFunc(b.sugg, func(x Suggestion) bool { return x.Pattern == pat })
	var x Suggestion
	if i >= 0 {
		x = b.sugg[i]
		b.sugg = slices.Delete(b.sugg, i, i+1)
	} else {
		x = Suggestion{ID: sid(pat), Pattern: pat, Kind: kind}
	}
	x.Command = s.Command
	x.Seen++
	if !slices.Contains(x.ThreadIDs, s.Thread) {
		x.ThreadIDs = append(x.ThreadIDs, s.Thread)
	}
	x.Threads = len(x.ThreadIDs)
	x.LastThread, x.LastAt = s.Thread, s.At
	x.Memory, x.CPU, x.Seconds = max(x.Memory, s.Memory), max(x.CPU, s.CPU), max(x.Seconds, s.Seconds)
	b.sugg = slices.Insert(b.sugg, 0, x)
	if len(b.sugg) > MaxSuggestions {
		b.sugg = b.sugg[:MaxSuggestions]
	}
	b.save(suggestionsKey, b.sugg)
	b.publishLocked()
	return false
}

// Suggestions are the suggestions, the newest first.
func (b *Book) Suggestions() []Suggestion {
	if v := b.pub.Load(); v != nil {
		return slices.Clone(v.sugg)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	b.publishLocked()
	return slices.Clone(b.sugg)
}

// Added are the owner's added patterns with how often the gate refused each.
func (b *Book) Added() []Added {
	var out []Added
	if v := b.pub.Load(); v != nil {
		out = slices.Clone(v.added)
	} else {
		b.mu.Lock()
		b.loadLocked()
		b.publishLocked()
		out = slices.Clone(b.added)
		b.mu.Unlock()
	}
	for i := range out {
		if b.Matcher != nil {
			out[i].Refused = b.Matcher.RefusedCount(out[i].Name)
		}
	}
	return out
}

// ErrNoSuggestion is Add's and Ignore's answer for an id that is not (or no
// longer) a suggestion.
var ErrNoSuggestion = errors.New("No such suggestion")

// Add makes the suggestion's pattern, as the owner edited it, a heavy command
// of kind; the suggestion goes. A pattern that does not compile is an error
// and nothing is stored.
func (b *Book) Add(id, pattern, kind string, now time.Time) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || len(pattern) > 300 {
		return errors.New("The pattern is empty or too long")
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("The pattern does not compile: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	i := slices.IndexFunc(b.sugg, func(x Suggestion) bool { return x.ID == id })
	if i < 0 {
		return ErrNoSuggestion
	}
	if kind != "test" && kind != "build" && kind != "e2e" {
		kind = b.sugg[i].Kind
	}
	name := strings.TrimSpace(strings.ReplaceAll(pattern, `\`, ""))
	if KindOf(name) != kind {
		name += " (" + kind + ")"
	}
	b.added = append(b.added, Added{Pattern: pattern, Kind: kind, Name: name, At: now})
	if err := b.applyLocked(); err != nil {
		b.added = b.added[:len(b.added)-1]
		b.applyLocked()
		return err
	}
	b.sugg = slices.Delete(b.sugg, i, i+1)
	b.save(addedKey, b.added)
	b.save(suggestionsKey, b.sugg)
	b.publishLocked()
	return nil
}

// Ignore drops a suggestion and never suggests its pattern again.
func (b *Book) Ignore(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	i := slices.IndexFunc(b.sugg, func(x Suggestion) bool { return x.ID == id })
	if i < 0 {
		return ErrNoSuggestion
	}
	b.ignored = append(b.ignored, b.sugg[i].Pattern)
	b.sugg = slices.Delete(b.sugg, i, i+1)
	b.save(ignoredKey, b.ignored)
	b.save(suggestionsKey, b.sugg)
	b.publishLocked()
	return nil
}

// Remove takes an added pattern out again; false when it is not one.
func (b *Book) Remove(pattern string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	i := slices.IndexFunc(b.added, func(a Added) bool { return a.Pattern == pattern })
	if i < 0 {
		return false
	}
	b.added = slices.Delete(b.added, i, i+1)
	b.applyLocked()
	b.save(addedKey, b.added)
	b.publishLocked()
	return true
}

var trivial = regexp.MustCompile(`^(?:cd|source|\.|export|set|echo|true|:|unset|pushd|popd|mkdir|rm|cat|ls)(?:\s|$)`)

// Suggest is the pattern and the kind to propose for a command: the program
// and the first argument that is not a flag, of the last segment that is not
// a cd or a source, escaped (V0.164 4.6).
func Suggest(cmd string) (pattern, kind string) {
	segs := segments(message.ReplaceAllString(cmd, " ''"))
	var pick string
	for _, s := range segs {
		if !trivial.MatchString(s) {
			pick = s
		}
	}
	if pick == "" {
		return "", ""
	}
	f := strings.Fields(pick)
	prog := f[0]
	arg := ""
	for _, a := range f[1:] {
		if !strings.HasPrefix(a, "-") {
			arg = a
			break
		}
	}
	text := prog
	pat := regexp.QuoteMeta(prog)
	if arg != "" {
		text += " " + arg
		pat += " " + regexp.QuoteMeta(arg)
	}
	return pat, KindOf(text)
}
