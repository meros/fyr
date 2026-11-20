package core

import (
	"encoding/json"
	"strings"
	"time"
)

// A question in text (SPEC V0.210, decision 0033, V0.text-ask-nudge): the
// agent ended its turn with something only the owner can answer, but wrote
// it instead of asking. At a clean end of a turn Fyr has a system job read
// the end of the message; a yes lands here as an observed Input and wakes the
// agent once, to ask on a card or to end the turn. The owner sees only the
// card, never Fyr's guess.
const (
	// KindTextAskJudged is the job's answer about one run.
	KindTextAskJudged = "textAsk.judged"
	// ObsTextAsk is the observer that reports the job's answer.
	ObsTextAsk = "textAsk"
	// EffectTextAsk asks the engine to judge the run Effect.Run.
	EffectTextAsk EffectKind = "textAsk"
	// EventTextAskNudged is the event of a yes: the agent is told.
	EventTextAskNudged = "textAsk.nudged"
	// textAskWords is the most words a question line keeps.
	textAskWords = 12
)

// TextAskJudged is the job's answer about the run Run.
type TextAskJudged struct {
	Run      int    `json:"run"`
	Needs    bool   `json:"needs"`
	Question string `json:"question,omitempty"`
}

func init() {
	RegisterKind(&InputKind{Name: KindTextAskJudged, From: observed, Reduce: reduceTextAskJudged})
}

// reduceTextAskJudged turns a yes about the run that ended last into the
// nudge: an event the feed makes a wake of (textQuestionNotices). A run
// that started since, or a thread asleep, drops it: the agent has moved on.
func reduceTextAskJudged(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[TextAskJudged](in)
	if err != nil {
		return Result{}, err
	}
	t := r.T
	q := cleanTextAsk(a.Question)
	if !a.Needs || q == "" || a.Run != t.RunEnds || a.Run <= t.TextNudged || t.Running || t.Asleep() {
		return Result{}, nil
	}
	t.TextNudged = a.Run
	return Result{Events: []Event{{EventTextAskNudged, map[string]any{"question": q, "run": a.Run}}}}, nil
}

// textAskAfter is the part of a reduce that reads its end state: a clean end
// of a turn asks for a judgment.
func textAskAfter(before, t *Thread, in Input, now time.Time) Result {
	var res Result
	if in.Kind == KindRunEnded && before.Running && textAskDue(t, in, now) {
		res.Effects = append(res.Effects, Effect{Kind: EffectTextAsk, Run: t.RunEnds})
	}
	return res
}

// textAskDue reports a run's end whose last words could be a question only
// the owner can answer: it ended cleanly, nothing is going on or due to wake
// the agent, nothing else already puts the thread in Needs you, the agent
// offered no chip, and the run is not the agent's answer to a nudge (one
// nudge, never a loop).
func textAskDue(t *Thread, in Input, now time.Time) bool {
	a, ok := in.Args.(RunEndedArgs)
	if !ok || a.Failure != nil || t.Failure != nil {
		return false
	}
	if stopped, _ := a.Data["interrupted"].(bool); stopped {
		return false
	}
	if t.TextNudged > 0 && t.RunEnds == t.TextNudged+1 {
		return false
	}
	if len(t.Offers) > 0 { // the agent offered a chip or a command: it used the tools
		return false
	}
	if t.Asleep() || t.Running || t.RunningTasks() > 0 || t.SlotQueued || t.Handover != nil || t.Unresponsive != nil || t.MachineWait != nil {
		return false
	}
	if len(t.Decisions) > 0 || len(t.HeldAsks) > 0 || PendingRequest(t) != nil || t.Feed.wakes(now) {
		return false
	}
	for _, r := range t.Rules {
		if r.Purpose == "watch" && r.Status != "suspended" {
			return false
		}
	}
	_, need := ThreadNeed(t)
	return need == nil
}

// ParseTextAsk reads the job's answer: {"needs": bool, "q": "..."}, possibly
// in a code fence. ok is false for anything else, and for a yes that does
// not say what is needed.
func ParseTextAsk(s string) (needs bool, question string, ok bool) {
	from, to := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if from < 0 || to < from {
		return false, "", false
	}
	var a struct {
		Needs *bool  `json:"needs"`
		Q     string `json:"q"`
	}
	if json.Unmarshal([]byte(s[from:to+1]), &a) != nil || a.Needs == nil {
		return false, "", false
	}
	if !*a.Needs {
		return false, "", true
	}
	q := cleanTextAsk(a.Q)
	return q != "", q, q != ""
}

// cleanTextAsk is a question as one line of at most textAskWords words.
func cleanTextAsk(s string) string {
	w := strings.Fields(s)
	if len(w) > textAskWords {
		return strings.Join(w[:textAskWords], " ") + "…"
	}
	return strings.Join(w, " ")
}
