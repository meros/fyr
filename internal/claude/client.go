// Package claude is a Go-native client for the control protocol of the
// `claude` CLI (`--input-format stream-json --output-format stream-json`).
// Each message used here was proved against the real CLI before it was used.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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

// SeqKey is the key under which a message from a runner carries its
// sequence number; Handled acks it.
const SeqKey = "_fyrSeq"

type Client struct {
	pipe runner.Pipe
	wmu  sync.Mutex
	// amu guards the ack state (V0.90). An ack covers every line up to it,
	// so it stops before the oldest message the consumer has not handled,
	// and it keeps the CLI's requests that wait for an answer: the runner
	// knows nothing of the protocol, so the next fyrd gets them only if
	// this one says so.
	amu      sync.Mutex
	unacked  []int64          // seqs on Out, not handled yet, in order
	open     map[string]int64 // the CLI's control requests not answered → seq
	seen     int64            // the newest seq read
	lastAck  int64
	lastKeep []int64
	pmu      sync.Mutex
	pending  map[string]chan Msg
	seq      atomic.Int64
	log      io.Writer

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
	args := append(base, s.Args...)
	// #167: an agent process never gets the launcher's variables, which
	// name the live install (its runner and ready file). V0.agent-env: nor a
	// credential or endpoint variable from whoever started fyrd; the
	// adapter's (s.Env) are the only ones it gets.
	env := launchenv.ClaudeEnv(launchenv.Scrub(s.Env)...)
	if len(s.Wrap) > 0 {
		args = append(append(append([]string{}, s.Wrap[1:]...), s.Binary), args...)
		s.Binary = s.Wrap[0]
	}
	if s.Runner != nil {
		p, err := s.Runner.Spawn(runner.Spawn{ID: s.ID, Key: s.Key, Binary: s.Binary, Args: args, Dir: s.Dir, Env: env, Cgroup: s.Cgroup, Meta: s.Meta})
		if err != nil {
			return nil, err
		}
		return Open(p, s.Log, nil), nil
	}
	p, err := startChild(s.Binary, args, s.Dir, env, s.Cgroup)
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
	c := &Client{pipe: p, pending: map[string]chan Msg{}, Out: make(chan Msg, 256), log: log, done: make(chan struct{}), open: map[string]int64{}}
	if setup != nil {
		setup(c)
	}
	go c.read()
	return c
}

func (c *Client) Pid() int { return c.pipe.Pid() }

// Pipe is the link to the process.
func (c *Client) Pipe() runner.Pipe { return c.pipe }

// Handled says that the consumer of Out is done with m: a later fyrd does
// not get it again from the runner (V0.90).
func (c *Client) Handled(m Msg) {
	seq, ok := m[SeqKey].(int64)
	if !ok {
		return
	}
	c.amu.Lock()
	for i, x := range c.unacked {
		if x == seq {
			c.unacked = append(c.unacked[:i], c.unacked[i+1:]...)
			break
		}
	}
	c.amu.Unlock()
	c.ack()
}

// read notes a line: queued for the consumer, or dispatched here.
func (c *Client) note(seq int64, queued bool) {
	if seq == 0 {
		return
	}
	c.amu.Lock()
	c.seen = seq
	if queued {
		c.unacked = append(c.unacked, seq)
	}
	c.amu.Unlock()
}

// ack acks every line before the oldest unhandled message, and keeps the
// requests still open.
func (c *Client) ack() {
	c.amu.Lock()
	upTo := c.seen
	if len(c.unacked) > 0 {
		upTo = c.unacked[0] - 1
	}
	var keep []int64
	for _, seq := range c.open {
		if seq <= upTo {
			keep = append(keep, seq)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i] < keep[j] })
	same := upTo == c.lastAck && slices.Equal(keep, c.lastKeep)
	c.lastAck, c.lastKeep = upTo, keep
	c.amu.Unlock()
	if !same {
		c.pipe.Ack(upTo, keep)
	}
}

// opened and answered track the CLI's control requests (V0.90).
func (c *Client) opened(id string, seq int64) {
	if seq == 0 || id == "" {
		return
	}
	c.amu.Lock()
	c.open[id] = seq
	c.amu.Unlock()
}

func (c *Client) answered(id string) {
	c.amu.Lock()
	_, ok := c.open[id]
	delete(c.open, id)
	c.amu.Unlock()
	if ok {
		c.ack()
	}
}

// Done closes after the process exited and stdout is drained.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is the exit error, valid after Done; it includes the stderr tail.
func (c *Client) Err() error { return c.err }

func (c *Client) logf(dir string, line []byte) {
	if c.log != nil {
		fmt.Fprintf(c.log, "%s %s %s\n", time.Now().Format("15:04:05.000"), dir, line)
	}
}

func (c *Client) write(m Msg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.logf(">>", b)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.pipe.WriteLine(b)
}

func (c *Client) read() {
	for {
		line, seq, err := c.pipe.ReadLine()
		if err != nil {
			break
		}
		c.logf("<<", line)
		var m Msg
		if err := json.Unmarshal(line, &m); err != nil {
			c.note(seq, false)
			c.ack()
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
			c.opened(id, seq) // kept for the next fyrd until answered
			go c.handle(m)
		case "control_cancel_request":
			id, _ := m["request_id"].(string)
			c.answered(id)
			if id != "" && c.OnCancel != nil {
				c.OnCancel(id)
			}
		case "keep_alive":
		default:
			if seq > 0 {
				m[SeqKey] = seq // acked by Handled, after the consumer is done
			}
			c.note(seq, true)
			c.Out <- m
			continue
		}
		// A control line is done once dispatched; an open request stays in
		// the keep list until answered.
		c.note(seq, false)
		c.ack()
	}
	c.err = c.pipe.Wait()
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
		c.write(Msg{"type": "control_response", "response": Msg{"subtype": "error", "request_id": id,
			"error": "unsupported: " + fmt.Sprint(req["subtype"])}})
		c.answered(id)
		return
	}
	if c.write(Msg{"type": "control_response", "response": Msg{"subtype": "success", "request_id": id, "response": payload}}) == nil {
		c.answered(id)
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
	if err := c.write(Msg{"type": "control_request", "request_id": id, "request": req}); err != nil {
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
	return c.write(m)
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
func (c *Client) Close() error { return c.pipe.CloseInput() }

// Kill stops the process and everything it started, at once. Done closes
// even when a child that left the process group still holds stdout (#39).
func (c *Client) Kill() { c.pipe.Kill() }
