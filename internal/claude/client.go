// Package claude is a Go-native client for the control protocol of the
// `claude` CLI (`--input-format stream-json --output-format stream-json`).
// The spike in spikes/protocol/go proved each message used here.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Msg = map[string]any

// Spec says how to start one agent process.
type Spec struct {
	Binary string   // "claude" or a wrapper such as "claude-acme"
	Args   []string // extra flags: --model, --resume, --permission-mode
	Dir    string
	Env    []string  // added to os.Environ()
	Log    io.Writer // optional raw protocol log
}

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[string]chan Msg
	seq     atomic.Int64
	log     io.Writer
	stderr  *tail

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
	cmd := exec.Command(s.Binary, append(base, s.Args...)...)
	cmd.Dir = s.Dir
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT=sdk-go")
	cmd.Env = append(cmd.Env, s.Env...)
	tl := &tail{max: 4096}
	cmd.Stderr = tl
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, stdin: in, pending: map[string]chan Msg{}, Out: make(chan Msg, 256),
		log: s.Log, stderr: tl, done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go c.read(out)
	return c, nil
}

func (c *Client) Pid() int { return c.cmd.Process.Pid }

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
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *Client) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		c.logf("<<", line)
		var m Msg
		if err := json.Unmarshal(line, &m); err != nil {
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
			go c.handle(m)
		case "control_cancel_request":
			if id, _ := m["request_id"].(string); id != "" && c.OnCancel != nil {
				c.OnCancel(id)
			}
		case "keep_alive":
		default:
			c.Out <- m
		}
	}
	err := c.cmd.Wait()
	if err != nil {
		if s := c.stderr.String(); s != "" {
			err = fmt.Errorf("%w: %s", err, s)
		}
	}
	c.err = err
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
			id, _ := req["callback_id"].(string)
			if r := c.OnHook(id, in); r != nil {
				payload = r
			}
		}
	default:
		c.write(Msg{"type": "control_response", "response": Msg{"subtype": "error", "request_id": id,
			"error": "unsupported: " + fmt.Sprint(req["subtype"])}})
		return
	}
	c.write(Msg{"type": "control_response", "response": Msg{"subtype": "success", "request_id": id, "response": payload}})
}

// Request sends a control_request and waits for its control_response.
func (c *Client) Request(ctx context.Context, req Msg) (Msg, error) {
	id := "fyr_" + strconv.FormatInt(c.seq.Add(1), 10)
	ch := make(chan Msg, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
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
func (c *Client) SendUser(content []Msg) error {
	return c.write(Msg{"type": "user", "session_id": "", "parent_tool_use_id": nil,
		"message": Msg{"role": "user", "content": content}})
}

// Close ends the session by closing stdin; the CLI exits on its own.
func (c *Client) Close() error { return c.stdin.Close() }

// Kill stops the process at once.
func (c *Client) Kill() { c.cmd.Process.Kill() }

// tail keeps the last max bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
