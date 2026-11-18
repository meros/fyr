package heavy

// V0.164 section 4.6, V0.209: what undeclared episodes teach. A command that
// ran heavy without being admitted and matches no pattern is noted; after
// LearnAfter episodes Fyr treats it as a heavy job by itself. A learned pattern
// is soft: it is admitted when there is room and is never refused, because the
// command behind an episode is a best guess. The owner may remove one, and it
// is not learned again. All of it is kept in the store's KV table on the host.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
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
	// LearnAfter is how many undeclared episodes make a command a heavy job.
	LearnAfter = 3
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

// Suggestion is a command seen behind undeclared work, not yet learned.
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

// Added is a heavy command beyond the built-in ones: learned by Fyr, or added
// by the owner before V0.209.
type Added struct {
	Pattern string    `json:"pattern"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
	Refused int       `json:"refused"`
	Learned bool      `json:"learned,omitempty"`
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
	// Commands seen often enough before V0.209 waited for the owner's press.
	var keep []Suggestion
	for _, x := range b.sugg {
		if x.Seen < LearnAfter || !b.learnLocked(x) {
			keep = append(keep, x)
		}
	}
	if len(keep) != len(b.sugg) {
		b.sugg = keep
		b.save(suggestionsKey, b.sugg)
	}
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
	var learned []Pattern
	for _, a := range b.added {
		if a.Learned {
			learned = append(learned, Pattern{Name: a.Name, Re: a.Pattern})
		} else {
			ps = append(ps, Pattern{Name: a.Name, Re: a.Pattern})
		}
	}
	return b.Matcher.SetAdded(ps, learned...)
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
	if x.Seen >= LearnAfter && b.learnLocked(x) {
		b.save(suggestionsKey, b.sugg)
		b.publishLocked()
		return false
	}
	b.sugg = slices.Insert(b.sugg, 0, x)
	if len(b.sugg) > MaxSuggestions {
		b.sugg = b.sugg[:MaxSuggestions]
	}
	b.save(suggestionsKey, b.sugg)
	b.publishLocked()
	return false
}

// learnLocked makes x's pattern a learned heavy command; false when it was
// not taken (removed by the owner before, known already, or it does not
// compile). The caller drops x from the suggestions.
func (b *Book) learnLocked(x Suggestion) bool {
	if slices.Contains(b.ignored, x.Pattern) || slices.ContainsFunc(b.added, func(a Added) bool { return a.Pattern == x.Pattern }) {
		return false
	}
	name := strings.TrimSpace(strings.ReplaceAll(x.Pattern, `\`, ""))
	if KindOf(name) != x.Kind {
		name += " (" + x.Kind + ")"
	}
	b.added = append(b.added, Added{Pattern: x.Pattern, Kind: x.Kind, Name: name, At: x.LastAt, Learned: true})
	if err := b.applyLocked(); err != nil {
		b.added = b.added[:len(b.added)-1]
		b.applyLocked()
		return false
	}
	b.save(addedKey, b.added)
	return true
}

// Suggestions are the commands seen and not learned yet, the newest first.
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

// Added are the learned and added patterns with how often the gate refused each.
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

// Remove takes a pattern out for good (it is not learned again); false when
// it is not one.
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
	if !slices.Contains(b.ignored, pattern) {
		b.ignored = append(b.ignored, pattern)
		b.save(ignoredKey, b.ignored)
	}
	b.publishLocked()
	return true
}

// trivial commands are never what made an episode heavy; git, gh and the like
// are usually just the last thing the thread ran.
var trivial = regexp.MustCompile(`^(?:cd|source|\.|export|set|echo|true|:|unset|pushd|popd|mkdir|rm|cat|ls|git|gh|sleep|wait|tail|head|grep|rg|curl|ssh)(?:\s|$)`)

// Suggest is the pattern and the kind to learn for a command: the program
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
