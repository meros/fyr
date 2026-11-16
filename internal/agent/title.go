package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
)

// titleSystem is the title call's whole system prompt (V0.108).
const titleSystem = "You name a work thread in a list of threads. Read the conversation and reply with only its title: " +
	"one line of at most 80 characters that says specifically what the thread works on now, in the language of the conversation. " +
	"No quotes, no prefix, no full stop at the end."

// Title makes a thread title from its conversation (V0.108): one print run
// of the thread's provider on a small model, in the temp folder so no
// project instructions load. It implements engine.Titler.
func (m *Manager) Title(ctx context.Context, threadID, conversation string) (string, error) {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return "", fmt.Errorf("no thread %q", threadID)
	}
	p := m.cfg.Profile(t.Profile)
	ad, err := m.adapter(p)
	if err != nil {
		return "", err
	}
	pr, ok := ad.(cli.Printer)
	if !ok {
		return "", fmt.Errorf("the %s CLI cannot make a title", ad.ID())
	}
	l := cli.Launch{Binary: p.Binary, Dir: os.TempDir(), Auth: authOf(p), Endpoint: p.Endpoint, Env: p.Env, Model: titleModel(p, t.Model)}
	if p.Key != nil {
		k, kerr := p.Key()
		if (kerr != nil || k == "") && (p.KeyRequired || l.Auth.Kind == cli.APIKey) {
			return "", fmt.Errorf("%s needs a key or a login token: add it in Settings → Providers", name(p))
		}
		l.Key = k
	}
	return pr.Print(ctx, l, titleSystem, "The conversation of the thread:\n\n"+conversation)
}

// titleModel is the model of a title call: the provider's Haiku model, else
// the model its endpoint names for Haiku, else the thread's own model, else
// the provider's default.
func titleModel(p provider.Profile, threadModel string) string {
	for _, id := range p.Models {
		if strings.Contains(strings.ToLower(id), "haiku") {
			return id
		}
	}
	if m := p.Endpoint.ModelEnv["ANTHROPIC_DEFAULT_HAIKU_MODEL"]; m != "" {
		return m
	}
	if threadModel != "" {
		return threadModel
	}
	return p.DefaultModel
}
