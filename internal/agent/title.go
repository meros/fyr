package agent

import (
	"context"
	"fmt"

	"github.com/meros/fyr/internal/core"
)

// titleSystem is the title call's whole system prompt (V0.108).
// V0.title-goal: the title is the goal of the work, as an issue title would
// say it, not the step the agent is at. V0.auto-title: short and specific,
// in the owner's language, and named from the agent's work when the
// owner's first message is vague or only an image.
const titleSystem = "You name a work thread in a list of threads, so that its owner sees at a glance which piece of work it is. " +
	"Reply with only the title: one line, 3 to 8 words, at most 50 characters. " +
	"Write it in the language of the owner's own words (the Owner: lines, the first one when they differ), " +
	"even when the agent or a pasted text uses another language; only when the owner wrote no words, use the agent's language. " +
	"Name the specific thing: the feature, bug, component, issue or question, with the detail that tells it apart from similar threads. " +
	"Write it like an issue title, for example \"Header overlaps the menu on phones\", \"Fix login after the OAuth move\" " +
	"or \"Why are unsold homes deleted?\". " +
	"The owner's first message is often short or vague (\"Is this right?\", \"Not nice\", \"fix 4711\", a link) or only an image " +
	"([attached image]): then take the subject from what the agent found and did, never from the vague words, " +
	"and never write a generic title such as \"Image\", \"Question\", \"Bug\", \"Check this\" or \"Help\". " +
	"When the owner changed the goal later, or a question turned into building something, name the newest goal. " +
	"When the input first gives the thread's current title, keep it word for word if it still names what the thread is for; " +
	"write a new one only when the goal moved, for example from a question to the fix or feature it led to. " +
	"The titles of the thread's changes, when given, say what is being built: name that goal, in the owner's language. " +
	"Do not name the step the agent is at (running tests, waiting for CI, rebasing, opening a pull request, answering a review), " +
	"and do not start with \"Thread\" or \"Conversation\". " +
	"No quotes, no prefix, no full stop at the end."

// Title makes a thread title from its conversation (V0.108): one system
// agent call of the title job, on the provider and model of the owner's
// setting (V0.115). It implements engine.Titler.
func (m *Manager) Title(ctx context.Context, threadID, conversation string) (string, error) {
	if _, ok := m.host.Thread(threadID); !ok {
		return "", fmt.Errorf("no thread %q", threadID)
	}
	return m.SystemPrintFor(ctx, core.JobTitle, threadID, titleSystem, "The conversation of the thread:\n\n"+conversation)
}
