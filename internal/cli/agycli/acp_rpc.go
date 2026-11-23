package agycli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/runner"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("acp error %d: %s", e.Code, e.Message)
}

type rpcResp struct {
	result cli.Msg
	err    *rpcError
	seq    int64
}

type pendingReq struct {
	ch      chan rpcResp
	holdSeq bool
}

func (c *acpSession) emit(m cli.Msg, seq int64) {
	if seq > 0 {
		m[runner.SeqKey] = seq
		c.Note(seq, true)
	}
	c.out <- m
}

// emitHeld emits a message whose runner seq was already recorded in c.unacked
// by read() so the runner keeps it until Fyr handles the message (V0.agy-reattach-queue).
func (c *acpSession) emitHeld(m cli.Msg, seq int64) {
	if seq > 0 {
		m[runner.SeqKey] = seq
	}
	c.out <- m
}

func (c *acpSession) request(ctx context.Context, method string, params cli.Msg) (cli.Msg, error) {
	res, _, err := c.requestOn(ctx, method, params, false, nil)
	return res, err
}

// requestOn is request, with onWritten run once the request is on the agent's
// pipe and before its answer is waited for: a turn is announced "started"
// there, so an Interrupt written after that announcement reaches the agent
// after the prompt it cancels (V0.agy-interrupt-before-prompt). When holdSeq is
// true, read() records the response's runner seq in c.unacked and returns it so
// the caller can attach it to the emitted result message (V0.agy-reattach-queue).
func (c *acpSession) requestOn(ctx context.Context, method string, params cli.Msg, holdSeq bool, onWritten func()) (cli.Msg, int64, error) {
	idNum := c.seq.Add(1)
	idKey := strconv.FormatInt(idNum, 10)
	ch := make(chan rpcResp, 1)
	c.pmu.Lock()
	c.pending[idKey] = pendingReq{ch: ch, holdSeq: holdSeq}
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, idKey)
		c.pmu.Unlock()
	}()
	req := cli.Msg{"jsonrpc": "2.0", "id": idNum, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.Write(req); err != nil {
		return nil, 0, err
	}
	if onWritten != nil {
		onWritten()
	}
	select {
	case r, open := <-ch:
		if !open {
			return nil, 0, fmt.Errorf("process exited before %s was answered", method)
		}
		if r.err != nil {
			return nil, r.seq, r.err
		}
		return r.result, r.seq, nil
	case <-c.readDone:
		return nil, 0, fmt.Errorf("process exited before %s was answered", method)
	case <-ctx.Done():
		return nil, 0, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (c *acpSession) notify(method string, params cli.Msg) error {
	m := cli.Msg{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	return c.Write(m)
}

func (c *acpSession) read() {
	for {
		line, seq, err := c.Pipe().ReadLine()
		if err != nil {
			break
		}
		c.Logf("<<", line)
		var m cli.Msg
		if err := json.Unmarshal(line, &m); err != nil {
			c.Note(seq, false)
			c.Ack()
			continue
		}
		method, hasMethod := m["method"].(string)
		rawID, hasID := m["id"]
		switch {
		case !hasMethod && hasID:
			// JSON-RPC response to one of our requests.
			idKey := jsonIDKey(rawID)
			var resp rpcResp
			if errObj, _ := m["error"].(map[string]any); errObj != nil {
				code, _ := errObj["code"].(float64)
				msg, _ := errObj["message"].(string)
				resp.err = &rpcError{Code: int(code), Message: msg}
			} else if res, ok := m["result"].(map[string]any); ok {
				resp.result = res
			}
			c.pmu.Lock()
			pr, ok := c.pending[idKey]
			delete(c.pending, idKey)
			c.pmu.Unlock()
			if ok {
				if pr.holdSeq && seq > 0 {
					c.Note(seq, true)
					resp.seq = seq
				}
				pr.ch <- resp
				if !pr.holdSeq {
					c.Note(seq, false)
					c.Ack()
				}
			} else if c.finishReattachedTurn(resp, seq) {
				// finishReattachedTurn queued a result message carrying seq.
			} else {
				c.Note(seq, false)
				c.Ack()
			}
		case hasMethod && hasID:
			// Server -> Client JSON-RPC request (e.g. session/request_permission).
			c.mu.Lock()
			c.cancelSettleLocked()
			c.mu.Unlock()
			idKey := jsonIDKey(rawID)
			c.Opened(idKey, seq)
			go c.handleServerRequest(rawID, idKey, method, m["params"])
			c.Note(seq, false)
			c.Ack()
		case hasMethod:
			// Server -> Client notification (session/update).
			queued := c.handleNotification(method, m["params"], seq)
			if !queued {
				c.Note(seq, false)
				c.Ack()
			}
		default:
			c.Note(seq, false)
			c.Ack()
		}
	}
	c.err = c.Pipe().Wait()
	close(c.readDone)
	c.pmu.Lock()
	for id, pr := range c.pending {
		close(pr.ch)
		delete(c.pending, id)
	}
	c.pmu.Unlock()
	// Cancel any open permission requests when the process exits.
	c.mu.Lock()
	c.inputClosed = true
	openIDs := make([]string, 0, len(c.openPerms))
	for id := range c.openPerms {
		openIDs = append(openIDs, id)
	}
	c.openPerms = map[string]bool{}
	c.mu.Unlock()
	if c.h.Cancel != nil {
		for _, id := range openIDs {
			c.h.Cancel(id)
		}
	}
	select {
	case c.qCh <- struct{}{}:
	default:
	}
	<-c.workerDone
	close(c.out)
	if c.cleanup != nil {
		c.cleanup()
	}
	close(c.done)
}

func jsonIDKey(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
}
