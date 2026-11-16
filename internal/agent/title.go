package agent

import (
	"context"
	"fmt"

	"github.com/meros/fyr/internal/core"
)

// titleSystem is the title call's whole system prompt (V0.108).
const titleSystem = "You name a work thread in a list of threads. Read the conversation and reply with only its title: " +
	"one line of at most 80 characters that says specifically what the thread works on now, in the language of the conversation. " +
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
