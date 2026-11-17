// V0.164 section 4.6 and 12.2 (C3–C8): an undeclared episode whose command matches no heavy pattern becomes a
// suggestion; the owner adds it (edited), ignores it for good, or removes an added pattern.
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
		"yarn pre-push:check":                       `yarn pre-push:check`,
		"node test/layout.run.mjs --shard 2":         `node test/layout\.run\.mjs`,
		"cd web && node --max-old-space-size=4096 scripts/big.mjs --x": `node scripts/big\.mjs`,
		"FOO=1 timeout 60 ./gradlew assemble":        `\./gradlew assemble`,
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

func TestC5AddJoinsTheMatcher(t *testing.T) {
	b, m, kv := newBook(t)
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs --shard 1", At: t0})
	id := b.Suggestions()[0].ID
	if m.Class("node test/layout.run.mjs --shard 3") != "" {
		t.Fatal("matched before")
	}
	if err := b.Add(id, `node test/layout\.run\.mjs`, "e2e", t0); err != nil {
		t.Fatal(err)
	}
	c := m.Class("node test/layout.run.mjs --shard 3")
	if !strings.HasPrefix(c, "node test/layout.run.mjs") || KindOf(c) != "e2e" {
		t.Fatalf("class %q", c)
	}
	if len(b.Suggestions()) != 0 {
		t.Fatal("the suggestion stays")
	}
	if a := b.Added(); len(a) != 1 || a[0].Pattern != `node test/layout\.run\.mjs` || a[0].Kind != "e2e" || !a[0].At.Equal(t0) {
		t.Fatalf("%+v", a)
	}
	// It survives a restart: a new book over the same KV loads it into a new matcher.
	m2 := Default()
	b2 := &Book{KV: kv, Matcher: m2}
	b2.Load()
	if m2.Class("node test/layout.run.mjs") == "" {
		t.Fatal("added pattern lost across a restart")
	}
}

func TestC5AddCountsRefusals(t *testing.T) {
	b, m, _ := newBook(t)
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs", At: t0})
	b.Add(b.Suggestions()[0].ID, `node test/layout\.run\.mjs`, "e2e", t0)
	m.Refused(m.Class("node test/layout.run.mjs x"))
	m.Refused(m.Class("node test/layout.run.mjs y"))
	if a := b.Added(); a[0].Refused != 2 {
		t.Fatalf("%+v", a)
	}
}

func TestC6APatternThatDoesNotCompileIsAnError(t *testing.T) {
	b, m, kv := newBook(t)
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs", At: t0})
	id := b.Suggestions()[0].ID
	if err := b.Add(id, `node (test`, "e2e", t0); err == nil {
		t.Fatal("no error")
	}
	if len(b.Added()) != 0 || m.Class("node test/layout.run.mjs") != "" || kv["heavy-added"] != "" {
		t.Fatal("something was stored")
	}
	if len(b.Suggestions()) != 1 {
		t.Fatal("the suggestion went")
	}
	if err := b.Add("nope", `x`, "test", t0); err == nil {
		t.Fatal("an unknown suggestion")
	}
}

func TestC7IgnoreIsForever(t *testing.T) {
	b, _, kv := newBook(t)
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs", At: t0})
	if err := b.Ignore(b.Suggestions()[0].ID); err != nil {
		t.Fatal(err)
	}
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs --again", At: t0.Add(time.Hour)})
	if len(b.Suggestions()) != 0 {
		t.Fatal("suggested again")
	}
	b2 := &Book{KV: kv, Matcher: Default()}
	b2.Load()
	b2.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs", At: t0})
	if len(b2.Suggestions()) != 0 {
		t.Fatal("suggested again after a restart")
	}
}

func TestC8RemoveLetsItThrough(t *testing.T) {
	b, m, _ := newBook(t)
	b.Saw(Seen{Thread: "t_a", Command: "node test/layout.run.mjs", At: t0})
	b.Add(b.Suggestions()[0].ID, `node test/layout\.run\.mjs`, "e2e", t0)
	if !b.Remove(`node test/layout\.run\.mjs`) {
		t.Fatal("not removed")
	}
	if m.Class("node test/layout.run.mjs") != "" || len(b.Added()) != 0 {
		t.Fatal("still there")
	}
	if b.Remove("nope") {
		t.Fatal("removed an unknown pattern")
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
