package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Msg = map[string]any

// Client is a minimal Go-native control client for `claude --input-format stream-json`.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[string]chan Msg
	Out     chan Msg // every non-control message
	log     *os.File
	start   time.Time

	OnCanUseTool func(req Msg) Msg // returns response payload
	OnHook       func(req Msg) Msg
}

func Start(log *os.File, args ...string) (*Client, error) {
	base := []string{"--output-format", "stream-json", "--verbose", "--input-format", "stream-json"}
	cmd := exec.Command("claude", append(base, args...)...)
	cmd.Dir = os.Getenv("SPIKE_CWD")
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT=sdk-go-spike")
	cmd.Stderr = log
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, stdin: in, pending: map[string]chan Msg{}, Out: make(chan Msg, 1024), log: log, start: time.Now()}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go c.read(out)
	return c, nil
}

func (c *Client) Pid() int { return c.cmd.Process.Pid }

func (c *Client) logf(dir string, line []byte) {
	fmt.Fprintf(c.log, "%8.3f %s %s\n", time.Since(c.start).Seconds(), dir, line)
}

func (c *Client) write(m Msg) error {
	b, _ := json.Marshal(m)
	c.logf(">>", b)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.stdin.Write(append(b, '\n'))
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
			fmt.Println("non-JSON line:", string(line))
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
		case "control_cancel_request", "keep_alive":
		default:
			c.Out <- m
		}
	}
	close(c.Out)
}

func (c *Client) handle(m Msg) {
	req, _ := m["request"].(Msg)
	id, _ := m["request_id"].(string)
	var payload Msg
	switch req["subtype"] {
	case "can_use_tool":
		if c.OnCanUseTool != nil {
			payload = c.OnCanUseTool(req)
		} else {
			payload = Msg{"behavior": "allow", "updatedInput": req["input"]}
		}
	case "hook_callback":
		if c.OnHook != nil {
			payload = c.OnHook(req)
		} else {
			payload = Msg{}
		}
	default:
		c.write(Msg{"type": "control_response", "response": Msg{"subtype": "error", "request_id": id, "error": "unsupported: " + fmt.Sprint(req["subtype"])}})
		return
	}
	c.write(Msg{"type": "control_response", "response": Msg{"subtype": "success", "request_id": id, "response": payload}})
}

// Request sends a control_request and waits for its control_response.
func (c *Client) Request(req Msg, timeout time.Duration) (Msg, error) {
	id := strconv.FormatInt(rand.Int63(), 36)
	ch := make(chan Msg, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	if err := c.write(Msg{"type": "control_request", "request_id": id, "request": req}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r["subtype"] == "error" {
			return r, fmt.Errorf("control error: %v", r["error"])
		}
		return r, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for %v", req["subtype"])
	}
}

func (c *Client) SendUser(text string) error {
	return c.write(Msg{"type": "user", "session_id": "", "parent_tool_use_id": nil,
		"message": Msg{"role": "user", "content": []Msg{{"type": "text", "text": text}}}})
}

// UntilResult drains Out until a result message, calling fn for each message.
func (c *Client) UntilResult(timeout time.Duration, fn func(Msg)) (Msg, error) {
	t := time.After(timeout)
	for {
		select {
		case m, ok := <-c.Out:
			if !ok {
				return nil, fmt.Errorf("stdout closed")
			}
			if fn != nil {
				fn(m)
			}
			if m["type"] == "result" {
				return m, nil
			}
		case <-t:
			return nil, fmt.Errorf("timeout waiting for result")
		}
	}
}

func (c *Client) Kill() { c.cmd.Process.Kill(); c.cmd.Wait() }

func (c *Client) CloseAndWait() error { c.stdin.Close(); return c.cmd.Wait() }

// Summ is a short one-line rendering of a message for the console.
func Summ(m Msg) string {
	t, _ := m["type"].(string)
	st, _ := m["subtype"].(string)
	s := t
	if st != "" {
		s += "/" + st
	}
	if msg, ok := m["message"].(Msg); ok {
		if content, ok := msg["content"].([]any); ok {
			for _, x := range content {
				b, _ := x.(Msg)
				switch b["type"] {
				case "text":
					s += " text=" + trunc(fmt.Sprint(b["text"]), 120)
				case "tool_use":
					in, _ := json.Marshal(b["input"])
					s += fmt.Sprintf(" tool_use=%v %s", b["name"], trunc(string(in), 160))
				case "tool_result":
					s += " tool_result=" + trunc(fmt.Sprint(b["content"]), 160)
				}
			}
		}
	}
	if t == "result" {
		s += fmt.Sprintf(" is_error=%v result=%s cost=%v", m["is_error"], trunc(fmt.Sprint(m["result"]), 120), m["total_cost_usd"])
	}
	if t == "system" && st == "init" {
		s += fmt.Sprintf(" model=%v session=%v", m["model"], m["session_id"])
	}
	return s
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
