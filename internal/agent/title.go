package agent

import (
	"context"
	"fmt"

	"github.com/meros/fyr/internal/core"
)

// titleSystem is the title call's whole system prompt (V0.108).
// V0.title-goal: the title is the goal of the work, as an issue title would
// say it, not the step the agent is at.
const titleSystem = "You name a work thread in a list of threads, so that its owner sees which piece of work it is. " +
	"Read the conversation and reply with only its title: one line of at most 80 characters, in the language of the conversation. " +
	"Write it like the title of an issue: the goal of the work and the thing it is about, for example " +
	"\"Fix the header that overlaps the menu on phones\" or \"Fix broken login after the OAuth move\". " +
	"Take the goal from what the owner asked for; when the owner changed the goal later, use the newest goal. " +
	"Do not name the step the agent is at now (running tests, waiting for CI, rebasing, opening a pull request, answering a review): " +
	"the title must still be right when that step is over. " +
	"No quotes, no prefix, no full stop at the end."

// Title makes a thread title from its conversation (V0.108): one system
// agent call of the title job, on the provider and model of the owner's
// setting (V0.115). It implements engine.Titler.
func (m *Manager) Title(ctx context.Context, threadID, conversation string) (string, error) {
	if _, ok := m.host.Thread(threadID); !ok {
		return "", fmt.Errorf("no thread %q", threadID)
	}
	return m.SystemPrint(ctx, core.JobTitle, titleSystem, "The conversation of the thread:\n\n"+conversation)
}
