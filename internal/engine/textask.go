package engine

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// Questions in text (SPEC V0.210, decision 0033): after a clean end of a
// turn a system job reads the end of the agent's last message. The call runs
// on its own, so the run's end and the effects after it never wait, and any
// failure leaves the thread as it is.

// TextJudge answers whether a message's end waits on the owner (thread id,
// the text). *agent.Manager's JudgeText implements it.
type TextJudge func(ctx context.Context, threadID, text string) (string, error)

const (
	// textAskChars is how much of the message the job reads: the length the
	// wording was tested on.
	textAskChars = 500
	// textAskEvents is how far back the run's last text is looked for.
	textAskEvents = 200
	// textAskTimeout bounds one call.
	textAskTimeout = 60 * time.Second
	// textAskCalls is how many calls may run at once on this host.
	textAskCalls = 2
)

// judgeText runs the job for the run that ended and lands a clear answer.
func (e *Engine) judgeText(threadID string, run int) {
	if e.TextJudge == nil || e.textAskQuiet(threadID) {
		return
	}
	evs, err := e.st.EventsFor(threadID, 0, textAskEvents, true)
	if err != nil {
		log.Printf("thread %s: question in text: %v", threadID, err)
		return
	}
	text := clipTextAsk(lastRunText(evs))
	if text == "" {
		return
	}
	e.textAskSlots <- struct{}{}
	defer func() { <-e.textAskSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), textAskTimeout)
	defer cancel()
	out, err := e.TextJudge(ctx, threadID, text)
	if err != nil {
		log.Printf("thread %s: question in text: %v", threadID, err)
		return
	}
	needs, q, ok := core.ParseTextAsk(out)
	if !ok || !needs {
		return
	}
	in := core.Input{Source: core.Observer(core.ObsTextAsk), Kind: core.KindTextAskJudged, Thread: threadID, Args: core.TextAskJudged{Run: run, Needs: true, Question: q}}
	if _, err := e.Apply(in); err != nil {
		log.Printf("thread %s: question in text: %v", threadID, err)
	}
}

// textAskQuiet reports a thread that is gone or whose project is in its quiet
// hours: the owner is not to be reached, so the call is not worth making.
func (e *Engine) textAskQuiet(threadID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[threadID]
	return t == nil || e.quietUntil(t.ProjectID, e.Now()) != nil
}

// lastRunText is the last text the agent wrote in the run that just ended:
// nothing when the run wrote none, or when a message came after it.
func lastRunText(evs []store.Event) string {
	turns := readTurns(evs)
	ended := false
	for i := len(turns) - 1; i >= 0; i-- {
		switch t := turns[i]; t.Kind {
		case turnEnded:
			if ended {
				return ""
			}
			ended = true
		case turnUser:
			return ""
		case turnAgent:
			var b strings.Builder
			for _, p := range t.Parts {
				if p.Tool == "" {
					b.WriteString(p.Text)
				}
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				return s
			}
		}
	}
	return ""
}

// clipTextAsk keeps the end of the message.
func clipTextAsk(s string) string {
	r := []rune(s)
	if len(r) > textAskChars {
		r = r[len(r)-textAskChars:]
	}
	return string(r)
}
