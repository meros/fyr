package engine

import (
	"log"
	"regexp"
	"strings"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// A merge asked in text (V0.settle-merge-fixes (1)): the agent began a
// message with the bare word "merge" instead of calling change_request.

// mergeTextEvents is how far back the run's texts are looked for.
const mergeTextEvents = 200

// mergeWord is a text that starts with the lower-case word "merge", alone
// or glued to the next sentence ("mergeI've opened…"); "Merged." and
// "merges" are prose.
var mergeWord = regexp.MustCompile(`^merge($|[^a-z])`)

// readMergeText reads the texts of the run that ended and lands a match.
func (e *Engine) readMergeText(threadID string, run int) {
	evs, err := e.st.EventsFor(threadID, 0, mergeTextEvents, true)
	if err != nil {
		log.Printf("thread %s: merge in text: %v", threadID, err)
		return
	}
	if !mergeInText(runTexts(evs)) {
		return
	}
	in := core.Input{Source: core.Observer(core.ObsMergeText), Kind: core.KindMergeText, Thread: threadID, Args: core.MergeTextArgs{Run: run}}
	if _, err := e.Apply(in); err != nil {
		log.Printf("thread %s: merge in text: %v", threadID, err)
	}
}

// mergeInText reports a text that starts with the bare word.
func mergeInText(texts []string) bool {
	for _, s := range texts {
		if mergeWord.MatchString(strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}

// runTexts are the texts the agent wrote in the run that just ended, from
// the message that started it: nothing when a message came after its end.
func runTexts(evs []store.Event) []string {
	turns := readTurns(evs)
	var out []string
	ended := false
	for i := len(turns) - 1; i >= 0; i-- {
		switch t := turns[i]; t.Kind {
		case turnEnded:
			if ended {
				return out
			}
			ended = true
		case turnUser:
			if !ended {
				return nil
			}
			return out
		case turnAgent:
			if !ended {
				continue
			}
			for _, p := range t.Parts {
				if p.Tool == "" && strings.TrimSpace(p.Text) != "" {
					out = append(out, p.Text)
				}
			}
		}
	}
	return out
}
