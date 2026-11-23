package agent

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.background-on-message (#19): the owner's message reaches the
// agent while a long foreground command runs. The CLI takes a queued message
// in only at a tool boundary (decision 0009), so a message sent during a
// ten-minute build waited ten minutes. When every open top-level tool call
// is a foreground Bash call and one has run BackgroundAfter or longer, Fyr
// writes the message and then asks the CLI to move those calls to the
// background, as Ctrl-B does: the calls return at once, the commands keep
// running and their results come as task notifications, and the message is
// read at that boundary. A CLI that cannot (no MovesBash in its init, an
// error, no such call) leaves today's behaviour: the message waits.

// DefaultBackgroundAfter is how long a foreground command runs before the
// owner's message moves it to the background: a shorter one ends soon
// enough by itself.
const DefaultBackgroundAfter = 5 * time.Second

// backgroundAsk bounds the background_tasks request; the message is already
// written, so a slow answer delays nothing but the next message.
const backgroundAsk = 5 * time.Second

// openCall is one open top-level tool call: its tool, its command (Bash)
// and when Fyr saw it start.
type openCall struct {
	name, command string
	at            time.Time
}

// noteCalls keeps s.calls, the open top-level tool calls of the turn. A
// subagent's calls are left out: moving one would not let the main agent
// read a message, since it still waits on its Agent call. A Bash call with
// run_in_background returns at once and is not a wait.
func (m *Manager) noteCalls(s *session, msg claude.Msg) {
	if fromSubagent(msg) {
		return
	}
	inner, _ := msg["message"].(map[string]any)
	blocks, _ := inner["content"].([]any)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range blocks {
		b, _ := x.(map[string]any)
		switch b["type"] {
		case "tool_use":
			id, _ := b["id"].(string)
			name, _ := b["name"].(string)
			in, _ := b["input"].(map[string]any)
			if id == "" || (name == "Bash" && in["run_in_background"] == true) {
				continue
			}
			if s.calls == nil {
				s.calls = map[string]openCall{}
			}
			cmd, _ := in["command"].(string)
			s.calls[id] = openCall{name: name, command: cmd, at: now}
		case "tool_result":
			id, _ := b["tool_use_id"].(string)
			delete(s.calls, id)
		}
	}
}

// movable returns the open foreground Bash calls old enough to move, in
// the order they started, or nil when another kind of call is open too
// (the boundary would still wait for it).
func (m *Manager) movable(s *session) (ids, cmds []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !s.live {
		return nil, nil
	}
	now := time.Now()
	for id, c := range s.calls {
		if c.name != "Bash" {
			return nil, nil
		}
		if now.Sub(c.at) >= m.cfg.BackgroundAfter {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int { return s.calls[a].at.Compare(s.calls[b].at) })
	for _, id := range ids {
		cmds = append(cmds, s.calls[id].command)
	}
	return ids, cmds
}

// backgroundLine is the one line the agent gets in front of the message.
// The CLI's tool result for a moved call already gives the task id and its
// output file, and the task notification brings the result, so the line
// says only why and which command. The message is written before the
// request (see toBackground), so the line says what Fyr asked, which stays
// true when the CLI then finds the command ended.
func backgroundLine(cmds []string) string {
	var named []string
	for _, c := range cmds {
		named = append(named, "`"+core.TruncHead(firstLine(c), 120)+"`")
	}
	return "[Fyr] So that this message reaches you now, Fyr asked the CLI to move your running command " + strings.Join(named, ", ") +
		" to the background, as Ctrl-B does. It is not stopped: if it moved, its tool result says where its output goes and you are told when it ends."
}

// toBackground decides, before the owner's message is written, whether its
// write moves the running commands; it returns the agent's line ("" for
// no) and the call to make after the write. The write comes first: the CLI
// reads its input in order, so the message is in its queue when the moved
// call returns and the boundary takes it in. Asked first, the call could
// return and the step go on before the message arrived.
func (m *Manager) toBackground(s *session) (string, func()) {
	b, ok := s.c.(cli.Backgrounder)
	if !ok {
		return "", nil
	}
	ids, cmds := m.movable(s)
	if len(ids) == 0 {
		return "", nil
	}
	if !b.CanBackground() {
		log.Printf("thread %s: a message waits for a running command: this CLI cannot move it to the background", s.thread)
		return "", nil
	}
	return backgroundLine(cmds), func() {
		ctx, cancel := context.WithTimeout(context.Background(), backgroundAsk)
		defer cancel()
		for i, id := range ids {
			moved, err := b.Background(ctx, id)
			switch {
			case err != nil:
				log.Printf("thread %s: moving %q to the background failed, the message waits for it: %v", s.thread, firstLine(cmds[i]), err)
				return
			case !moved:
				log.Printf("thread %s: %q was not moved to the background (it ended already)", s.thread, firstLine(cmds[i]))
			}
		}
	}
}
