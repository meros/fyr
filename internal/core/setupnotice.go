package core

import (
	"errors"
	"time"
)

// V0.prompt-fits-setup: what the agent's setup lacks, said once in its feed.

// KindSetup is the agent manager's word that the thread's agent started in
// a setup that lacks something it needs: its Args is the [Fyr] line.
const KindSetup = "agent.setup"

// EventSetup is its event.
const EventSetup = "setup"

func init() {
	RegisterKind(&InputKind{Name: KindSetup, From: observed, Untouched: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		what, _ := in.Args.(string)
		if what == "" {
			return Result{}, errors.New("agent.setup needs the line")
		}
		return Result{Events: []Event{{EventSetup, map[string]any{"what": what}}}}, nil
	}})
}

// setupNotices is the line, said once while it stays the same: each start
// of the agent reports it again.
func setupNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		if ev.Type != EventSetup {
			continue
		}
		what := str(ev.Data.(map[string]any)["what"])
		out = append(out, Notice{About: "thread", Key: "setup", What: what, said: "setup", state: what})
	}
	return out
}
