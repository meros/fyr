package engine

import (
	"log"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/t3"
)

// T3 adopts threads from T3 Code (SPEC 0.5 V0.9, decision 0004): each open
// top-level thread with a Claude session becomes a Fyr thread with its
// history, its PR and its snooze.
type T3 struct {
	E    *Engine
	DB   string // T3's statev2.sqlite
	Home string // where ~/.claude* transcripts are
	// Profiles are the profile ids a thread may keep; the first is the
	// default for any other.
	Profiles func() []string
}

// Candidates are T3's threads that are not adopted yet.
func (x T3) Candidates() ([]t3.Candidate, error) {
	cs, err := t3.Candidates(x.DB, x.Home)
	if err != nil {
		return nil, err
	}
	out := []t3.Candidate{}
	for _, c := range cs {
		if !x.E.HasT3(c.T3ID) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Adopt adopts the candidates with these T3 ids, once each.
func (x T3) Adopt(ids []string) (int, error) {
	cs, err := t3.Candidates(x.DB, x.Home)
	if err != nil {
		return 0, err
	}
	return x.adopt(cs, ids)
}

func (x T3) adopt(cs []t3.Candidate, ids []string) (int, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	profiles := x.Profiles()
	known := map[string]bool{}
	for _, p := range profiles {
		known[p] = true
	}
	n := 0
	for _, c := range cs {
		if !want[c.T3ID] || x.E.HasT3(c.T3ID) {
			continue
		}
		prof := c.Profile
		if !known[prof] {
			prof = profiles[0]
		}
		t := &core.Thread{ID: NewID("t_"), Title: c.Title, Cwd: c.Cwd,
			Owner: core.Owner, Visibility: "private", Profile: prof, Model: c.Model, SessionID: c.SessionID,
			CreatedAt: c.UpdatedAt, UpdatedAt: c.UpdatedAt, ActivityAt: c.UpdatedAt, Source: "t3", T3ID: c.T3ID}
		if repo, num, ok := core.ParsePR(c.PRURL); ok {
			core.PRObserved(t, core.PR{URL: c.PRURL, Repo: repo, Number: num, State: "OPEN", Checks: core.ChecksPending}, c.UpdatedAt)
		}
		v := &core.View{User: core.Owner, Entity: t.ID, SeenAt: c.UpdatedAt}
		if c.SnoozedUntil != "" {
			if until, err := time.Parse(time.RFC3339Nano, c.SnoozedUntil); err == nil && until.After(time.Now()) {
				core.Snooze(v, until, time.Now())
			}
		}
		hist, at, err := t3.History(x.Home, c.SessionID, 400)
		if err != nil {
			log.Printf("adopt %s: history: %v", c.T3ID, err)
		}
		if len(hist) > 0 {
			if s := lastSummary(hist); s != "" {
				t.Summary = s
			}
		}
		// #48: the adopted thread goes down the one path, with its history.
		if _, err := x.E.Apply(core.Input{Source: core.Observer(core.ObsT3), Kind: core.KindThreadAdopted, Thread: t.ID,
			Args: core.Adopted{Thread: t, View: v, History: hist, At: at}}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// lastSummary is the start of the last assistant text, at most 160 runes.
func lastSummary(h []core.Event) string {
	for i := len(h) - 1; i >= 0; i-- {
		m, _ := h[i].Data.(map[string]any)
		if m["type"] != "assistant" {
			continue
		}
		msg, _ := m["message"].(map[string]any)
		bs, _ := msg["content"].([]any)
		for _, x := range bs {
			if b, _ := x.(map[string]any); b["type"] == "text" {
				if s, _ := b["text"].(string); s != "" {
					r := []rune(s)
					if len(r) > 160 {
						r = r[:159]
					}
					return string(r)
				}
			}
		}
	}
	return ""
}
