package engine

// The ask tool (SPEC V0.ask-tool): the agent asks the owner a question on a
// card instead of writing it in its message. The call returns at once; the
// answer arrives in the agent feed.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// AskDesc is the ask tool's description.
const AskDesc = "Ask the owner a question on a card. Use it whenever the owner must answer or choose before the work can go on, instead of writing the question in your message: a question in text is easy to miss, because the thread looks finished. Fyr shows a question card in this thread on desktop and phone, puts the thread in the owner's Your move and notifies them. The call returns at once: the answer arrives as a message starting with [Fyr], also after a restart. Give each question 2 to 4 options when you can name them, the one you recommend first; the owner can always answer in their own words, and a question without options is answered that way. The owner may answer only some of the questions (the rest come back as (skipped)) or cancel the card without answering it: both arrive as a [Fyr] message that says so, and you go on from it instead of asking the same question again. Ask up to 4 questions that belong together in one call. Do not ask for permission to go on, and do not ask what you can find out yourself."

func (e *Engine) askTool() mcp.Tool {
	option := obj(map[string]any{
		"label":       str("The choice in 1 to 5 words."),
		"description": str("What choosing it means."),
	}, "label")
	question := obj(map[string]any{
		"question":    str("The whole question, as one sentence that stands on its own: the owner may read only the card."),
		"header":      str("A label of one or two words."),
		"options":     map[string]any{"type": "array", "maxItems": 4, "items": option, "description": "The choices, at most 4, the recommended one first. Leave it out for a question the owner answers in their own words."},
		"multiSelect": map[string]any{"type": "boolean", "description": "The owner may choose several options."},
	}, "question")
	return mcp.Tool{Name: "ask",
		Description: AskDesc,
		Schema:      obj(map[string]any{"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": question}}, "questions"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			var qs []core.Question
			b, _ := json.Marshal(a["questions"])
			if err := json.Unmarshal(b, &qs); err != nil || len(qs) == 0 {
				return "", errors.New("questions must be a list of {question, options?}")
			}
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindAsk, Thread: th, Args: core.AskArgs{ID: NewID("d_"), Questions: qs}}); err != nil {
				return "", err
			}
			return "The owner sees the question card. The answer arrives as a message starting with [Fyr]. Go on with work that does not depend on the answer, or end your turn; do not decide it yourself meanwhile, and do not repeat the question in your message.", nil
		}}
}
