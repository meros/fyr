// Package claude is a Go-native client for the control protocol of the
// `claude` CLI (`--input-format stream-json --output-format stream-json`).
// Each message used here was proved against the real CLI before it was used.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/runner"
)

type Msg = map[string]any

// Spec says how to start one agent process.
type Spec struct {
	Binary string   // "claude" or a wrapper such as "claude-acme"
	Args   []string // extra flags: --model, --resume, --permission-mode
	Dir    string
	Env    []string  // added to os.Environ()
	Log    io.Writer // optional raw protocol log
	// Cgroup: the cgroup directory the process starts in (V0.80); "" is
	// the daemon's.
	Cgroup string
	// Wrap: a command line the process starts under, which puts it in a
	// scope of its own (V0.agents-unit); nil for none.
	Wrap []string
	// Runner, when set, starts the process in the runner that outlives
	// fyrd (V0.90); ID names it there and Meta comes back from List.
	Runner *runner.Client
	ID     string
	Key    string // one live process per key in the runner
	Meta   []byte
}

// CredentialVars are the variables that name who the claude CLI talks to
// and as whom (cli.Caps.CredentialVars, V0.agent-env).
var CredentialVars = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY",
	"ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CONFIG_DIR"}

// Env is the environment of a claude process: launchenv.AgentEnv with
// CredentialVars, CLAUDE_CODE_ENTRYPOINT=sdk-go, then extra (the adapter's).
func Env(extra ...string) []string {
	return launchenv.AgentEnv(CredentialVars, append([]string{"CLAUDE_CODE_ENTRYPOINT=sdk-go"}, extra...)...)
}

// SeqKey is the key under which a message from a runner carries its
// sequence number; Handled acks it.
const SeqKey = runner.SeqKey

type Client struct {
	// Link writes and logs lines and keeps the runner's ack state (V0.90).
	*runner.Link
	pmu     sync.Mutex
	pending map[string]chan Msg
	seq     atomic.Int64

	// Out carries every message that is not a control message. It closes
	// when the process stdout closes.
	Out  chan Msg
	done chan struct{}
	err  error

	// OnCanUseTool decides a permission request and returns the response
	// payload. It runs on its own goroutine, so it may block.
	OnCanUseTool func(req Msg) Msg
	// OnCancel is called when the CLI withdraws a control request.
	OnCancel func(requestID string)
	// OnHook answers a hook_callback (registered in Initialize). It gets
	// the callback id and the hook input; nil means "no opinion".
	OnHook func(callbackID string, input Msg) Msg
}

func Start(s Spec) (*Client, error) {
	base := []string{"--output-format", "stream-json", "--verbose", "--input-format", "stream-json",
		"--permission-prompt-tool", "stdio"}
	// #167: an agent process never gets the launcher's variables, which
	// name the live install (its runner and ready file). V0.agent-env: nor a
	// credential or endpoint variable from whoever started fyrd; the
	// adapter's (s.Env) are the only ones it gets.
	p, err := runner.Start(s.Runner, runner.Spawn{ID: s.ID, Key: s.Key, Binary: s.Binary, Args: append(base, s.Args...),
		Dir: s.Dir, Env: Env(s.Env...), Cgroup: s.Cgroup, Meta: s.Meta}, s.Wrap)
	if err != nil {
		return nil, err
	}
	return Open(p, s.Log, nil), nil
}

// Open speaks the protocol over a pipe: a new process, or one the runner
// kept across a restart of fyrd (V0.90). setup, when set, runs before the
// first line is read: a kept process may have a permission request waiting,
// and without OnCanUseTool the client would allow it.
func Open(p runner.Pipe, log io.Writer, setup func(*Client)) *Client {
	c := &Client{Link: runner.NewLink(p, log), pending: map[string]chan Msg{}, Out: make(chan Msg, 256), done: make(chan struct{})}
	if setup != nil {
		setup(c)
	}
	go c.read()
	return c
}

// Done closes after the process exited and stdout is drained.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is the exit error, valid after Done; it includes the stderr tail.
func (c *Client) Err() error { return c.err }

func (c *Client) read() {
	for {
		line, seq, err := c.Pipe().ReadLine()
		if err != nil {
			break
		}
		c.Logf("<<", line)
		var m Msg
		if err := json.Unmarshal(line, &m); err != nil {
			c.Note(seq, false)
			c.Ack()
			continue
		}
		switch m["type"] {
		case "control_response":
			resp, _ := m["response"].(Msg)
			id, _ := resp["request_id"].(string)
			c.pmu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.pmu.Unlock()
			if ch != nil {
				ch <- resp
			}
		case "control_request":
			id, _ := m["request_id"].(string)
			c.Opened(id, seq) // kept for the next fyrd until answered
			go c.handle(m)
		case "control_cancel_request":
			id, _ := m["request_id"].(string)
			c.Answered(id)
			if id != "" && c.OnCancel != nil {
				c.OnCancel(id)
			}
		case "keep_alive":
		default:
			if seq > 0 {
				m[SeqKey] = seq // acked by Handled, after the consumer is done
			}
			c.Note(seq, true)
			c.Out <- m
			continue
		}
		// A control line is done once dispatched; an open request stays in
		// the keep list until answered.
		c.Note(seq, false)
		c.Ack()
	}
	c.err = c.Pipe().Wait()
	c.pmu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pmu.Unlock()
	close(c.Out)
	close(c.done)
}

func (c *Client) handle(m Msg) {
	req, _ := m["request"].(Msg)
	id, _ := m["request_id"].(string)
	var payload Msg
	switch req["subtype"] {
	case "can_use_tool":
		if c.OnCanUseTool != nil {
			payload = c.OnCanUseTool(Msg{"request_id": id, "tool_name": req["tool_name"], "input": req["input"],
				"tool_use_id": req["tool_use_id"], "agent_id": req["agent_id"]})
		} else {
			payload = Msg{"behavior": "allow", "updatedInput": req["input"]}
		}
	case "hook_callback":
		payload = Msg{}
		if c.OnHook != nil {
			in, _ := req["input"].(Msg)
			if u, ok := req["tool_use_id"].(string); ok && u != "" && in != nil && in["tool_use_id"] == nil {
				in["tool_use_id"] = u
			}
			id, _ := req["callback_id"].(string)
			if r := c.OnHook(id, in); r != nil {
				payload = r
			}
		}
	default:
		c.Write(Msg{"type": "control_response", "response": Msg{"subtype": "error", "request_id": id,
			"error": "unsupported: " + fmt.Sprint(req["subtype"])}})
		c.Answered(id)
		return
	}
	if c.Write(Msg{"type": "control_response", "response": Msg{"subtype": "success", "request_id": id, "response": payload}}) == nil {
		c.Answered(id)
	}
}

// Request sends a control_request and waits for its control_response.
func (c *Client) Request(ctx context.Context, req Msg) (Msg, error) {
	id := "fyr_" + strconv.FormatInt(c.seq.Add(1), 10)
	ch := make(chan Msg, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	// A request that times out or fails to write is forgotten; an answer
	// or the process exit has already removed it otherwise (review
	// 2026-11-15).
	defer func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}()
	if err := c.Write(Msg{"type": "control_request", "request_id": id, "request": req}); err != nil {
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("process exited before %v was answered", req["subtype"])
		}
		if r["subtype"] == "error" {
			return r, fmt.Errorf("control error: %v", r["error"])
		}
		return r, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%v: %w", req["subtype"], ctx.Err())
	}
}

// Initialize starts the session. hooks maps a hook event (PreToolUse) to
// matchers with callback ids, as in the SDK: {"PreToolUse": [{"matcher":
// "Bash", "hookCallbackIds": ["gate"]}]}. It may be nil.
func (c *Client) Initialize(ctx context.Context, hooks Msg) (Msg, error) {
	req := Msg{"subtype": "initialize"}
	if hooks != nil {
		req["hooks"] = hooks
	}
	return c.Request(ctx, req)
}

func (c *Client) Interrupt(ctx context.Context) error {
	_, err := c.Request(ctx, Msg{"subtype": "interrupt"})
	return err
}

func (c *Client) SetModel(ctx context.Context, model string) error {
	_, err := c.Request(ctx, Msg{"subtype": "set_model", "model": model})
	return err
}

// SendUser sends one user message with text and optional content blocks.
// A uuid makes the CLI report the message's delivery as command_lifecycle
// (queued, started, completed, cancelled) and lets CancelMessage withdraw it.
func (c *Client) SendUser(content []Msg, uuid string) error {
	m := Msg{"type": "user", "session_id": "", "parent_tool_use_id": nil,
		"message": Msg{"role": "user", "content": content}}
	if uuid != "" {
		m["uuid"] = uuid
	}
	return c.Write(m)
}

// CancelMessage withdraws a queued user message (cancel_async_message). It
// reports false when the CLI has already taken the message in.
func (c *Client) CancelMessage(ctx context.Context, uuid string) (bool, error) {
	r, err := c.Request(ctx, Msg{"subtype": "cancel_async_message", "message_uuid": uuid})
	if err != nil {
		return false, err
	}
	inner, _ := r["response"].(Msg)
	ok, _ := inner["cancelled"].(bool)
	return ok, nil
}

// Close ends the session by closing stdin; the CLI exits on its own.
func (c *Client) Close() error { return c.Pipe().CloseInput() }

// Kill stops the process and everything it started, at once. Done closes
// even when a child that left the process group still holds stdout (#39).
func (c *Client) Kill() { c.Pipe().Kill() }
