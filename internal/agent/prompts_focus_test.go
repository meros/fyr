package agent

import (
	"strings"
	"testing"
)

// V0.thread-focus: every setup's prompt says one thread is one topic, that
// a stack or a group is one piece of work, and that a separate topic or a
// spec's implementation starts in a new thread with a full brief.
func TestPromptThreadFocus(t *testing.T) {
	for _, s := range []Setup{FullSetup, {Git: true, OS: "linux"}, {OS: "darwin"}} {
		p := Prompt(s)
		for _, w := range []string{
			"One thread is one topic and about one PR",
			"a stack (change_open with after) or a group",
			"start it with mcp__fyr__thread_new and a full brief",
			"A spec, review or audit thread hands its implementation to new threads",
		} {
			if !strings.Contains(p, w) {
				t.Errorf("setup %+v: prompt lacks %q", s, w)
			}
		}
	}
}
