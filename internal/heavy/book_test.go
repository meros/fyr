// V0.164 section 4.6, V0.209: an undeclared episode whose command matches no heavy pattern is noted, and the third one
// makes it a learned heavy command; the owner may remove one for good.
package heavy

import (
	"strings"
	"testing"
	"time"
)

type mapKV map[string]string

func (m mapKV) GetKV(k string) (string, error) { return m[k], nil }
func (m mapKV) SetKV(k, v string) error        { m[k] = v; return nil }

var t0 = time.Date(2026, 11, 17, 14, 0, 0, 0, time.UTC)

func newBook(t *testing.T) (*Book, *Matcher, mapKV) {
	t.Helper()
	m := Default()
	kv := mapKV{}
	return &Book{KV: kv, Matcher: m}, m, kv
}

func TestSuggestIsProgramAndFirstArgumentEscaped(t *testing.T) {
	for cmd, want := range map[string]string{
		"yarn pre-push:check":                                          `yarn pre-push:check`,
		"node test/layout.run.mjs --shard 2":                           `node test/layout\.run\.mjs`,
		"cd web && node --max-old-space-size=4096 scripts/big.mjs --x": `node scripts/big\.mjs`,
		"FOO=1 timeout 60 ./gradlew assemble":                          `\./gradlew assemble`,
	} {
		got, _ := Suggest(cmd)
		if got != want {
			t.Errorf("%q: %q, want %q", cmd, got, want)
		}
	}
	if _, kind := Suggest("node test/layout.run.mjs"); kind != "e2e" {
		t.Errorf("kind %q", kind)
	}
}

func TestC3TwoEpisodesOfOneCommandAreOneSuggestion(t *testing.T) {
	b, _, _ := newBook(t)
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs --shard 1", At: t0, Memory: 2e9, CPU: 1.5, Seconds: 120})
	b.Saw(Seen{Thread: "t_b", Command: "node test/layout.run.mjs --shard 2", At: t0.Add(time.Hour), Memory: 2.8e9, CPU: 2.1, Seconds: 240})
	got := b.Suggestions()
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	s := got[0]
	if s.Seen != 2 || s.Threads != 2 || s.Pattern != `node test/layout\.run\.mjs` || s.Kind != "e2e" || s.LastThread != "t_b" || s.Memory != 2.8e9 || s.CPU != 2.1 || s.Seconds != 240 {
		t.Fatalf("%+v", s)
	}
}

func TestC4ACommandThatMatchesAPatternMakesNoSuggestion(t *testing.T) {
	b, _, _ := newBook(t)
	if !b.Saw(Seen{Thread: "t_a", Command: "make test-go", At: t0}) {
		t.Fatal("not reported as matched")
	}
	if len(b.Suggestions()) != 0 {
		t.Fatal("a suggestion for a known pattern")
	}
}

func see(b *Book, n int) {
	for i := 0; i < n; i++ {
		b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs --shard 1", At: t0})
	}
}

// V0.209: the third episode makes the command a heavy job, soft, without anyone pressing anything.
func TestV209TheThirdEpisodeTeachesTheCommand(t *testing.T) {
	b, m, kv := newBook(t)
	see(b, LearnAfter-1)
	if m.Class("node test/layout.run.mjs") != "" || len(b.Added()) != 0 {
		t.Fatal("learned too early")
	}
	see(b, 1)
	c := m.Class("node test/layout.run.mjs --shard 3")
	if !strings.HasPrefix(c, "node test/layout.run.mjs") || KindOf(c) != "e2e" || !m.Soft(c) {
		t.Fatalf("class %q soft %v", c, m.Soft(c))
	}
	if len(b.Suggestions()) != 0 {
		t.Fatal("still noted")
	}
	if a := b.Added(); len(a) != 1 || a[0].Pattern != `node test/layout\.run\.mjs` || !a[0].Learned || !a[0].At.Equal(t0) {
		t.Fatalf("%+v", a)
	}
	m2 := Default()
	(&Book{KV: kv, Matcher: m2}).Load()
	if c := m2.Class("node test/layout.run.mjs"); c == "" || !m2.Soft(c) {
		t.Fatal("lost across a restart")
	}
}

// What waited for the owner's press before V0.209 is learned at the start; a pattern the owner added stays hard.
func TestV209OldSuggestionsAreLearnedAndAddedPatternsStayHard(t *testing.T) {
	kv := mapKV{suggestionsKey: `[{"id":"s_1","pattern":"gradle assemble","kind":"build","seen":3,"command":"gradle assemble"},{"id":"s_2","pattern":"tool x","kind":"test","seen":1}]`,
		addedKey: `[{"pattern":"mine run","kind":"test","name":"mine run"}]`}
	m := Default()
	b := &Book{KV: kv, Matcher: m}
	b.Load()
	if c := m.Class("gradle assemble"); c == "" || !m.Soft(c) {
		t.Fatalf("%q", c)
	}
	if c := m.Class("mine run"); c == "" || m.Soft(c) {
		t.Fatalf("%q", c)
	}
	if s := b.Suggestions(); len(s) != 1 || s[0].ID != "s_2" {
		t.Fatalf("%+v", s)
	}
}

func TestC5ARefusalIsCounted(t *testing.T) {
	m := Default()
	b := &Book{KV: mapKV{addedKey: `[{"pattern":"mine run","kind":"test","name":"mine run"}]`}, Matcher: m}
	b.Load()
	m.Refused(m.Class("mine run x"))
	m.Refused(m.Class("mine run y"))
	if a := b.Added(); a[0].Refused != 2 {
		t.Fatalf("%+v", a)
	}
}

func TestC8RemoveIsForever(t *testing.T) {
	b, m, kv := newBook(t)
	see(b, LearnAfter)
	if !b.Remove(`node test/layout\.run\.mjs`) {
		t.Fatal("not removed")
	}
	if m.Class("node test/layout.run.mjs") != "" || len(b.Added()) != 0 {
		t.Fatal("still there")
	}
	if b.Remove("nope") {
		t.Fatal("removed an unknown pattern")
	}
	see(b, LearnAfter+1)
	b2 := &Book{KV: kv, Matcher: Default()}
	b2.Load()
	b2.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs", At: t0})
	if len(b.Added())+len(b2.Added())+len(b.Suggestions())+len(b2.Suggestions()) != 0 {
		t.Fatal("learned again")
	}
}

func TestV209TheLastThingAThreadRanIsNotLearned(t *testing.T) {
	for _, c := range []string{"git push origin x", "gh pr view 1", "sleep 30", "cd web && git status"} {
		if p, _ := Suggest(c); p != "" {
			t.Errorf("%q: %q", c, p)
		}
	}
}

func TestV209DetachedCommands(t *testing.T) {
	for c, want := range map[string]bool{"make test": false, "make test && echo ok": false, "make test &": true, "nohup make test > log": true, "setsid make e2e": true,
		`git commit -m "a & b"`: false} {
		if Detached(c) != want {
			t.Errorf("%q: want %v", c, want)
		}
	}
}

func TestSuggestionsAreCappedAtThirtyNewestFirst(t *testing.T) {
	b, _, _ := newBook(t)
	for i := 0; i < 35; i++ {
		b.Saw(Seen{Thread: "t_a", Command: "tool" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + " run", At: t0.Add(time.Duration(i) * time.Minute)})
	}
	s := b.Suggestions()
	if len(s) != 30 || !s[0].LastAt.After(s[29].LastAt) {
		t.Fatalf("%d", len(s))
	}
}
