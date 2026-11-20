package core

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// A question asked with Fyr's ask tool (SPEC V0.ask-tool). It is the same
// ask Decision the CLI's AskUserQuestion opens, with one difference: the
// agent is not waiting in a tool call. The Decision lives in the thread, not
// in the process, so it survives a restart and the agent can go on with
// other work; the owner's answer reaches the agent as a notice.
const (
	// KindAsk is the agent's ask {questions}.
	KindAsk = "ask.open"
	// EffectAnswered is the owner's answer to a question nobody waits on in
	// a tool call: the reduce logs it, and the agent feed tells the agent.
	EffectAnswered EffectKind = "answered"
	// EffectCancelled is the owner closing a question card without an answer
	// (V0.ask-cancel): the reduce logs it, and the agent feed tells the agent.
	// A question a run waits on is withdrawn in the process instead
	// (EffectCancel).
	EffectCancelled EffectKind = "cancelled"
	// SkippedAnswer is the answer logged for a question the owner skipped
	// (V0.ask-cancel): no answer, and the agent is told so.
	SkippedAnswer = "(skipped)"

	// The limits of one ask: the card shows one question at a time with its
	// options in a row of four.
	maxAskQuestions = 4
	maxAskOptions   = 4
	// maxOpenAsks bounds the questions that wait at once on one thread.
	maxOpenAsks = 4
)

// AskArgs are ask's.
type AskArgs struct {
	ID        string     `json:"id"`
	Questions []Question `json:"questions"`
}

// NeedsProcess reports a Decision the agent process waits on in a request
// of its own (AskUserQuestion, a permission): it dies with the process. An
// effect ask and a question asked with Fyr's tool do not.
func (d Decision) NeedsProcess() bool { return d.Kind != DecisionEffect && !d.Async }

func init() {
	RegisterKind(&InputKind{Name: KindAsk, From: agentOnly, Reduce: reduceAsk})
}

func reduceAsk(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[AskArgs](in)
	if err != nil {
		return Result{}, err
	}
	t := r.T
	if t.ArchivedAt != nil {
		return Result{}, reject(RejectUnavailable, "The thread is archived")
	}
	qs, err := cleanQuestions(a.Questions)
	if err != nil {
		return Result{}, err
	}
	open := 0
	for _, d := range t.Decisions {
		if d.Kind != DecisionAsk || !d.Async {
			continue
		}
		if reflect.DeepEqual(d.Questions, qs) {
			return Result{Reply: d}, nil // asked already: one card
		}
		open++
	}
	if open >= maxOpenAsks {
		return Result{}, reject(RejectUnavailable, "%d of your questions already wait for the owner; wait for an answer first", open)
	}
	d := Decision{ID: a.ID, Kind: DecisionAsk, Assignee: Owner, Questions: qs, Async: true, CreatedAt: now}
	DecisionOpened(t, d)
	return Result{Reply: d, Events: []Event{{"decision.opened", map[string]any{"decision": d}}}}, nil
}

// cleanQuestions checks an ask's questions and trims their text. A question
// may have no options: the owner then answers in their own words.
func cleanQuestions(in []Question) ([]Question, error) {
	if len(in) == 0 || len(in) > maxAskQuestions {
		return nil, fmt.Errorf("%w: ask 1 to %d questions", ErrBadArgs, maxAskQuestions)
	}
	seen := map[string]bool{}
	out := make([]Question, 0, len(in))
	for _, q := range in {
		q.Question, q.Header = strings.TrimSpace(q.Question), strings.TrimSpace(q.Header)
		if q.Question == "" {
			return nil, fmt.Errorf("%w: a question has no text", ErrBadArgs)
		}
		if seen[q.Question] {
			return nil, fmt.Errorf("%w: the question %q is asked twice", ErrBadArgs, q.Question)
		}
		seen[q.Question] = true
		if len(q.Options) > maxAskOptions {
			return nil, fmt.Errorf("%w: %q has %d options; give at most %d (the owner can always answer in their own words)", ErrBadArgs, q.Question, len(q.Options), maxAskOptions)
		}
		labels := map[string]bool{}
		opts := make([]Option, 0, len(q.Options))
		for _, o := range q.Options {
			o.Label, o.Description = strings.TrimSpace(o.Label), strings.TrimSpace(o.Description)
			if o.Label == "" || labels[o.Label] {
				return nil, fmt.Errorf("%w: the options of %q need labels that differ", ErrBadArgs, q.Question)
			}
			labels[o.Label] = true
			opts = append(opts, o)
		}
		q.Options = opts
		out = append(out, q)
	}
	return out, nil
}

// stringsOf is the non-empty strings of a JSON list of arguments.
func stringsOf(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// answered logs the owner's answer to a question asked with Fyr's tool. A
// question the owner skipped is logged as skipped, so the agent does not
// take it for an answer (V0.ask-cancel).
func answered(res *Result, d Decision, answers map[string]string, skipped []string) {
	skip := map[string]bool{}
	for _, q := range skipped {
		skip[q] = true
	}
	var lines []string
	for _, q := range d.Questions {
		a := SkippedAnswer
		if !skip[q.Question] {
			a = answers[q.Question]
		}
		lines = append(lines, q.Question+" → "+a)
	}
	data := map[string]any{"id": d.ID, "answer": strings.Join(lines, "\n"), "asked": true}
	if len(skipped) > 0 {
		data["skipped"] = skipped
	}
	res.Events = append(res.Events, Event{"decision.closed", data})
}

// cancelled logs a question card the owner closed without answering it
// (V0.ask-cancel): the agent is told, since it waits on no tool call.
func cancelled(res *Result, d Decision) {
	var qs []string
	for _, q := range d.Questions {
		qs = append(qs, q.Question)
	}
	res.Events = append(res.Events, Event{"decision.closed", map[string]any{
		"id": d.ID, "answer": strings.Join(qs, "\n"), "cancelled": true, "asked": d.Async,
	}})
}

// askAnswerText is the agent's notice of the owner's answer.
func askAnswerText(answer string) string {
	s := "The owner answered your question: "
	if strings.Contains(answer, "\n") {
		s = "The owner answered your questions:\n"
	}
	s += answer
	if strings.Contains(answer, " → "+SkippedAnswer) {
		s += "\nA question marked " + SkippedAnswer + " was left without an answer on purpose: decide it yourself if you can, or go on without it; do not ask it again as it is."
	}
	return s
}

// askCancelText is the agent's notice of a question card the owner closed
// without answering: the agent is not left waiting for an answer.
func askCancelText(questions string) string {
	return "The owner cancelled your question without answering it:\n" + questions +
		"\nDecide it yourself if you can, or stop and say what you need; do not ask it again as it is."
}

// TextQuestionText is the nudge of a turn that ended with a question in its
// text (V0.text-ask-nudge): the owner sees nothing of Fyr's reading, so the
// agent asks on a card, offers a chip, or ends its turn.
func TextQuestionText(q string) string {
	return "Fyr read the end of your last message as a question for the owner (\"" + q + "\"), but you asked no card, so the owner may miss it: the thread looks finished. " +
		"If the owner must answer or choose before the work can go on, call mcp__fyr__ask with it now. " +
		"If it was an optional offer, use mcp__fyr__offer_action or mcp__fyr__run_request {optional: true}. " +
		"If nothing waits on the owner, end your turn without writing anything more."
}

// textQuestionNotices: Fyr read a question in the agent's text. It wakes the
// agent once (the rule's budget bounds it; TextNudged keeps the answer from
// being read again).
func textQuestionNotices(r Reduced) []Notice {
	for _, ev := range r.Events {
		if ev.Type == EventTextAskNudged {
			d, _ := ev.Data.(map[string]any)
			return []Notice{{About: "thread", Key: "text-question", What: TextQuestionText(str(d["question"]))}}
		}
	}
	return nil
}
