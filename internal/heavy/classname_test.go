// V0.freeze-fast-class-name: a learned class is named after the command that
// does the work, never a filter or a wrapper at the end of the line; a filter
// learned before is dropped when the book is read.
package heavy

import (
	"encoding/json"
	"testing"
)

// The shapes of 20 Nov 07:33: an episode's command that ended in `| cut -c…`
// became the heavy class "cut", and fyrd's gate hook then held a diagnostic
// `tail … | grep … | cut …` as a heavy job.
func TestClassNameSkipsFiltersAndWrappers(t *testing.T) {
	for cmd, want := range map[string]string{
		"scripts/ci-local.sh 2>&1 | tail -8":                                    `scripts/ci-local\.sh`,
		"go test ./... | cut -c1-100":                                           `go test`,
		"tail -f x | grep y | cut -c1-80":                                       ``,
		"tail -200 ~/.local/share/fyr/daemon.log | grep -a guard | cut -c1-200": ``,
		"bash -c 'node scripts/big.mjs | sort | uniq -c | wc -l'":               `node scripts/big\.mjs`,
		"nix develop -c ./gradlew assemble 2>&1 | tee log | sed -n 1,5p":        `\./gradlew assemble`,
		"ionice -c 3 nice -n 19 ./gradlew check | awk '{print $1}' | column -t": `\./gradlew check`,
		"cat out.json | jq .x | egrep y | tr a b | less":                        ``,
		"echo hi && true && sleep 1 && time timeout 5 env":                      ``,
	} {
		if got, _ := Suggest(cmd); got != want {
			t.Errorf("%q: %q, want %q", cmd, got, want)
		}
	}
}

func TestClassNameAFilterIsNeverLearned(t *testing.T) {
	b, m, _ := newBook(t)
	for i := 0; i < LearnAfter+1; i++ {
		b.Saw(Seen{Thread: "t_a", Command: "tail -f x | grep y | cut -c1-80", At: t0})
	}
	if len(b.Added()) != 0 || len(b.Suggestions()) != 0 || m.Class("cut -c1-80") != "" {
		t.Fatalf("learned %+v, suggested %+v", b.Added(), b.Suggestions())
	}
}

func TestClassNameALearnedFilterIsDroppedOnLoad(t *testing.T) {
	m := Default()
	kv := mapKV{}
	added, _ := json.Marshal([]Added{
		{Pattern: `cut`, Kind: "test", Name: "cut", At: t0, Learned: true},
		{Pattern: `node scripts/big\.mjs`, Kind: "test", Name: "node scripts/big.mjs", At: t0, Learned: true},
		{Pattern: `grep -a`, Kind: "test", Name: "grep -a", At: t0, Learned: true},
	})
	sugg, _ := json.Marshal([]Suggestion{{ID: sid(`sort`), Pattern: `sort`, Kind: "test", Seen: 2}})
	kv[addedKey], kv[suggestionsKey] = string(added), string(sugg)
	b := &Book{KV: kv, Matcher: m}
	b.Load()
	if m.Class("tail -f x | grep y | cut -c1-80") != "" {
		t.Fatalf("the learned filter still makes a heavy class: %q", m.Class("tail -f x | grep y | cut -c1-80"))
	}
	got := b.Added()
	if len(got) != 1 || got[0].Name != "node scripts/big.mjs" {
		t.Fatalf("kept %+v", got)
	}
	if len(b.Suggestions()) != 0 {
		t.Fatalf("kept the suggestion %+v", b.Suggestions())
	}
	var stored []Added
	json.Unmarshal([]byte(kv[addedKey]), &stored)
	if len(stored) != 1 {
		t.Fatalf("stored %+v", stored)
	}
}
