package desktop

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// V0.desktop-chromium: the app drives its Chromium window over the DevTools
// protocol on --remote-debugging-pipe: Chromium reads commands on its fd 3 and
// writes answers and events on its fd 4, each message JSON ended by a NUL. No
// port is opened, so nothing else on the host can drive the window.

// cdpMsg is one message either way. Sessions are flat (Target.setAutoAttach
// with flatten): a page's commands and events carry its sessionId.
type cdpMsg struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// errCDPClosed: the browser went away (the owner closed the window).
var errCDPClosed = errors.New("the window is closed")

type cdpConn struct {
	wmu sync.Mutex
	w   io.Writer

	mu      sync.Mutex
	next    int64
	pending map[int64]chan cdpMsg
	closed  bool

	events chan cdpMsg
	done   chan struct{}
}

// newCDP reads r until it ends. Events reach onEvent one at a time on a
// goroutine of their own, so a handler may call c.call; done closes when r
// ends and the last event is handled.
func newCDP(r io.Reader, w io.Writer, onEvent func(cdpMsg)) *cdpConn {
	c := &cdpConn{w: w, pending: map[int64]chan cdpMsg{}, events: make(chan cdpMsg, 256), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for m := range c.events {
			onEvent(m)
		}
	}()
	go c.read(r)
	return c
}

func (c *cdpConn) read(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		b, err := br.ReadBytes(0)
		if len(b) > 1 {
			var m cdpMsg
			if json.Unmarshal(b[:len(b)-1], &m) == nil {
				if m.ID != 0 {
					c.mu.Lock()
					ch := c.pending[m.ID]
					delete(c.pending, m.ID)
					c.mu.Unlock()
					if ch != nil {
						ch <- m
					}
				} else if m.Method != "" {
					c.events <- m
				}
			}
		}
		if err != nil {
			break
		}
	}
	c.mu.Lock()
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.events)
}

// call sends one command and waits for its answer (10 s at most).
func (c *cdpConn) call(session, method string, params any, out any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if params == nil {
		p = []byte("{}")
	}
	ch := make(chan cdpMsg, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errCDPClosed
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	b, _ := json.Marshal(cdpMsg{ID: id, Method: method, Params: p, SessionID: session})
	c.wmu.Lock()
	_, err = c.w.Write(append(b, 0))
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return errCDPClosed
	}
	t := time.NewTimer(10 * time.Second)
	defer t.Stop()
	select {
	case m, ok := <-ch:
		if !ok {
			return errCDPClosed
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-t.C:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("%s: no answer in 10 s", method)
	}
}

// send is a call whose answer nobody waits for; an error is only logged by
// the caller's choice, so it is dropped here.
func (c *cdpConn) send(session, method string, params any) {
	go func() { _ = c.call(session, method, params, nil) }()
}
