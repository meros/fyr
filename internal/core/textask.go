package core

import (
	"encoding/json"
	"strings"
	"time"
)

// A question in text (SPEC V0.210, decision 0033): the agent ended its turn
// with something only the owner can answer, but wrote it instead of asking.
// At a clean end of a turn Fyr has a system job read the end of the message;
// a yes lands here as an observed Input and puts the thread in Needs you,
// marked as Fyr's reading, last there and without a push.
const (
	// KindTextAskJudged is the job's answer about one run.
	KindTextAskJudged = "textAsk.judged"
	// KindNotQuestion is the owner's Not a question.
	KindNotQuestion = "thread.notQuestion"
	// ObsTextAsk is the observer that reports the job's answer.
	ObsTextAsk = "textAsk"
	// EffectTextAsk asks the engine to judge the run Effect.Run.
	EffectTextAsk EffectKind = "textAsk"
	// textAskWords is the most words a question line keeps.
	textAskWords = 12
)

// TextAsk is a question Fyr read in the agent's text.
type TextAsk struct {
	Question string    `json:"question"`
	At       time.Time `json:"at"`
	// Run is the run count (RunEnds) the question was read from.
	Run int `json:"run"`
}

// TextAskJudged is the job's answer about the run Run.
type TextAskJudged struct {
	Run      int    `json:"run"`
	Needs    bool   `json:"needs"`
	Question string `json:"question,omitempty"`
}

func init() {
	RegisterKind(&InputKind{Name: KindTextAskJudged, From: observed, Reduce: reduceTextAskJudged})
	register(&Action{Name: KindNotQuestion, Label: "Not a question",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.TextAsk == nil {
				return "Fyr read no question here"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			clearTextAsk(t)
			return nil, nil
		}})
}

func reduceTextAskJudged(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[TextAskJudged](in)
	if err != nil {
		return Result{}, err
	}
	t := r.T
	q := cleanTextAsk(a.Question)
	if !a.Needs || q == "" || a.Run != t.RunEnds || a.Run <= t.TextAskCleared || t.Running || t.Asleep() {
		return Result{}, nil
	}
	if t.TextAsk != nil && t.TextAsk.Question == q {
		return Result{}, nil
	}
	t.TextAsk = &TextAsk{Question: q, At: now, Run: a.Run}
	return Result{Events: []Event{{"textAsk.raised", map[string]any{"question": q, "run": a.Run}}}}, nil
}

// clearTextAsk ends the flag; a call in flight for a run up to now cannot
// bring it back.
func clearTextAsk(t *Thread) {
	t.TextAsk = nil
	t.TextAskCleared = t.RunEnds
}

// textAskAfter is the part of a reduce that reads its end state: a settled
// or archived thread drops the flag, the owner's dismissal is logged with
// what it dismissed, and a clean end of a turn asks for a judgment.
func textAskAfter(before, t *Thread, in Input, now time.Time) Result {
	var res Result
	if t.TextAsk != nil && t.Asleep() {
		clearTextAsk(t)
	}
	if in.Kind == KindNotQuestion && before.TextAsk != nil {
		res.Events = append(res.Events, Event{"textAsk.dismissed", map[string]any{"question": before.TextAsk.Question, "run": before.TextAsk.Run}})
	}
	if in.Kind == KindRunEnded && before.Running && textAskDue(t, in, now) {
		res.Effects = append(res.Effects, Effect{Kind: EffectTextAsk, Run: t.RunEnds})
	}
	return res
}

// textAskDue reports a run's end whose last words could be a question only
// the owner can answer: it ended cleanly, nothing is going on or due to wake
// the agent, and nothing else already puts the thread in Needs you. A thread
// already flagged is judged again.
func textAskDue(t *Thread, in Input, now time.Time) bool {
	a, ok := in.Args.(RunEndedArgs)
	if !ok || a.Failure != nil || t.Failure != nil {
		return false
	}
	if stopped, _ := a.Data["interrupted"].(bool); stopped {
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

// textAskOnly reports a thread that is in Needs you only because of a
// question Fyr read: it sends no push (V0.210 (4)).
func textAskOnly(t *Thread, on func(repo string) bool) bool {
	if t.TextAsk == nil || t.Unresponsive != nil || PendingRequest(t) != nil {
		return false
	}
	_, need := threadNeed(t, on)
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
