package agycli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// defaultUnsolicitedSettle is how long an unsolicited (reactive-wakeup) ACP
// turn waits after a text step's usage_update before flushing buffered text
// and closing the turn with a synthetic result (V0.agy-reactive-wakeup).
// Live agy sessions emit any subsequent tool_call or JSON-RPC stopReason
// response within ~250ms of usage_update; 1.5s leaves generous headroom while
// settling reactive wakeups promptly.
const defaultUnsolicitedSettle = 1500 * time.Millisecond

type queuedPrompt struct {
	uuid    string
	content []cli.Msg
}

// SendUser enqueues a user message for the session's prompt worker and emits
// command_lifecycle ("queued") immediately when a turn is already running.
func (c *acpSession) SendUser(content []cli.Msg, uuid string) error {
	c.mu.Lock()
	if c.inputClosed {
		c.mu.Unlock()
		return fmt.Errorf("session input is closed")
	}
	busy := c.turnRunning || c.reattachActive || c.unsolicitedActive || len(c.queue) > 0
	c.queue = append(c.queue, queuedPrompt{uuid: uuid, content: content})
	c.mu.Unlock()
	if busy && uuid != "" {
		c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": uuid, "state": "queued"}, 0)
	}
	c.signalWorker()
	return nil
}

// CancelMessage withdraws a queued user message before it starts executing.
func (c *acpSession) CancelMessage(ctx context.Context, uuid string) (bool, error) {
	c.mu.Lock()
	for i, q := range c.queue {
		if q.uuid == uuid {
			c.queue = append(c.queue[:i], c.queue[i+1:]...)
			c.mu.Unlock()
			c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": uuid, "state": "cancelled"}, 0)
			return true, nil
		}
	}
	c.mu.Unlock()
	return false, nil
}

// Interrupt cancels the active turn via ACP `session/cancel`.
func (c *acpSession) Interrupt(ctx context.Context) error {
	c.mu.Lock()
	sid := c.sessionID
	wake := false
	if !c.turnRunning && (c.unsolicitedActive || c.reattachActive) {
		c.unsolicitedInterrupted = true
		c.armSettleLocked(c.settleDelay)
		wake = true
	}
	c.mu.Unlock()
	if wake {
		c.signalWorker()
	}
	if sid == "" {
		return nil
	}
	return c.notify("session/cancel", cli.Msg{"sessionId": sid})
}

// Close ends the session cleanly once any in-flight turn finishes.
// Closing runs on promptWorker so Close never blocks the Out() consumer.
func (c *acpSession) Close() error {
	c.mu.Lock()
	c.inputClosed = true
	c.mu.Unlock()
	select {
	case c.qCh <- struct{}{}:
	default:
	}
	return nil
}

// Kill immediately terminates the underlying process and closes stdout.
func (c *acpSession) Kill() {
	c.mu.Lock()
	c.inputClosed = true
	c.killed = true
	c.mu.Unlock()
	select {
	case c.qCh <- struct{}{}:
	default:
	}
	c.Pipe().Kill()
}

func (c *acpSession) signalWorker() {
	select {
	case c.qCh <- struct{}{}:
	default:
	}
}

// openToolLocked reports a tool of the current turn that has not finished:
// agy is waiting on it, not done (V0.agy-open-tool-settle).
func (c *acpSession) openToolLocked() bool {
	for id := range c.emittedTools {
		if !c.finishedTools[id] {
			return true
		}
	}
	return false
}

func (c *acpSession) armSettleLocked(d time.Duration) {
	c.settleGen++
	c.settleArmed = true
	c.settleDeadline = time.Now().Add(d)
}

func (c *acpSession) cancelSettleLocked() {
	if c.settleArmed {
		c.settleGen++
		c.settleArmed = false
		c.settleDeadline = time.Time{}
	}
}

func (c *acpSession) resetTurnMapsLocked() {
	c.emittedTools = map[string]bool{}
	c.finishedTools = map[string]bool{}
	c.rawNames = map[string]string{}
	c.toolNames = map[string]string{}
	c.toolDescs = map[string]bgTool{}
	for id, bt := range c.bgTools {
		c.emittedTools[id] = true
		c.toolDescs[id] = bt
	}
}

func (c *acpSession) promptWorker() {
	defer close(c.workerDone)
	var timer *time.Timer
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer = nil
		}
	}
	defer stopTimer()

	for {
		c.mu.Lock()
		if c.settleArmed {
			now := time.Now()
			if !now.Before(c.settleDeadline) {
				gen := c.settleGen
				c.mu.Unlock()
				stopTimer()
				c.finishUnsolicitedTurn(gen)
				continue
			}
			wait := time.Until(c.settleDeadline)
			c.mu.Unlock()
			stopTimer()
			timer = time.NewTimer(wait)
			select {
			case <-timer.C:
				continue
			case <-c.qCh:
				continue
			case <-c.readDone:
				return
			}
		}
		if c.reattachActive || c.unsolicitedActive {
			c.mu.Unlock()
			stopTimer()
			select {
			case <-c.qCh:
				continue
			case <-c.readDone:
				return
			}
		}
		if len(c.queue) == 0 {
			closed := c.inputClosed
			killed := c.killed
			sid := c.sessionID
			c.mu.Unlock()
			stopTimer()
			if closed {
				if !killed && sid != "" {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					_, _ = c.request(ctx, "session/close", cli.Msg{"sessionId": sid})
					cancel()
				}
				_ = c.Pipe().CloseInput()
				return
			}
			select {
			case <-c.qCh:
				continue
			case <-c.readDone:
				return
			}
		}
		if isClosed(c.readDone) {
			// The process is gone: a queued prompt never starts, and it gets
			// no made-up result either.
			c.mu.Unlock()
			return
		}
		item := c.queue[0]
		c.queue = c.queue[1:]
		c.turnRunning = true
		c.reattachActive = false
		c.unsolicitedActive = false
		c.unsolicitedInterrupted = false
		c.cancelSettleLocked()
		c.textBuf.Reset()
		c.turnText.Reset()
		c.resetTurnMapsLocked()
		sid := c.sessionID
		model := c.currentModel
		needInit := !c.initEmitted
		c.initEmitted = true
		c.mu.Unlock()
		stopTimer()

		var startedOnce sync.Once
		started := func() {
			startedOnce.Do(func() {
				if item.uuid != "" {
					c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": item.uuid, "state": "started"}, 0)
				}
				if needInit && sid != "" {
					c.emit(cli.Msg{"type": "system", "subtype": "init", "session_id": sid, "model": model}, 0)
				}
			})
		}
		start := time.Now()
		res, respSeq, err := c.requestOn(context.Background(), "session/prompt", cli.Msg{
			"sessionId": sid,
			"prompt":    convertPromptBlocks(item.content),
		}, true, started)
		durMs := time.Since(start).Milliseconds()
		if err != nil && isClosed(c.readDone) {
			// The process exited: read() closes Out and reports Err. A
			// result here would be a made-up one, and read() waits for this
			// worker before it closes Out (V0.review-hangs (9)).
			c.mu.Lock()
			c.turnRunning = false
			c.mu.Unlock()
			return
		}

		// Flush any trailing assistant text, promote any still-open background
		// tools, and emit result atomically under emitMu so a subsequent
		// reactive-wakeup notification cannot interleave before result.
		end := turnEnd{err: err, seq: respSeq, held: true, command: item.uuid, durMs: durMs, timed: true}
		if err == nil {
			end.stopReason, _ = res["stopReason"].(string)
			end.usage, _ = res["usage"].(map[string]any)
		}
		c.emitMu.Lock()
		c.mu.Lock()
		c.takeTurnLocked(&end)
		c.turnRunning = false
		shouldClose := c.inputClosed && len(c.queue) == 0
		c.mu.Unlock()
		c.closeTurn(end)
		c.emitMu.Unlock()
		if shouldClose {
			if sid != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, _ = c.request(ctx, "session/close", cli.Msg{"sessionId": sid})
				cancel()
			}
			_ = c.Pipe().CloseInput()
			return
		}
	}
}

func convertPromptBlocks(content []cli.Msg) []cli.Msg {
	out := make([]cli.Msg, 0, len(content))
	for _, blk := range content {
		switch blk["type"] {
		case "text":
			if t, _ := blk["text"].(string); t != "" {
				out = append(out, cli.Msg{"type": "text", "text": t})
			}
		case "image":
			if src, _ := blk["source"].(cli.Msg); src != nil {
				mt, _ := src["media_type"].(string)
				data, _ := src["data"].(string)
				if mt != "" && data != "" {
					out = append(out, cli.Msg{"type": "image", "mimeType": mt, "data": data})
				}
			} else if mt, _ := blk["mimeType"].(string); mt != "" {
				if data, _ := blk["data"].(string); data != "" {
					out = append(out, cli.Msg{"type": "image", "mimeType": mt, "data": data})
				}
			}
		}
	}
	if len(out) == 0 {
		out = append(out, cli.Msg{"type": "text", "text": ""})
	}
	return out
}

func (c *acpSession) emitAssistantText(text string, seq int64) {
	c.emit(cli.Msg{
		"type": "assistant",
		"message": cli.Msg{
			"role":    "assistant",
			"content": []any{cli.Msg{"type": "text", "text": text}},
		},
	}, seq)
}

func (c *acpSession) flushAssistantText(seq int64) {
	c.mu.Lock()
	text := c.textBuf.String()
	c.textBuf.Reset()
	c.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return
	}
	c.emitAssistantText(text, seq)
}

// finishUnsolicitedTurn closes a reactive-wakeup turn (or a reattached
// reactive-wakeup turn) after its settle timer fires with no further ACP
// notifications or JSON-RPC stopReason response (V0.agy-reactive-wakeup).
func (c *acpSession) finishUnsolicitedTurn(gen uint64) {
	c.emitMu.Lock()
	c.mu.Lock()
	if c.turnRunning || !c.settleArmed || c.settleGen != gen || len(c.openPerms) > 0 {
		c.mu.Unlock()
		c.emitMu.Unlock()
		return
	}
	c.settleArmed = false
	c.settleDeadline = time.Time{}
	var end turnEnd
	if c.unsolicitedInterrupted {
		end.stopReason = "cancelled"
	}
	c.unsolicitedActive = false
	c.unsolicitedInterrupted = false
	c.reattachActive = false
	c.takeTurnLocked(&end)
	c.resetTurnMapsLocked()
	c.mu.Unlock()
	c.closeTurn(end)
	c.emitMu.Unlock()
	c.signalWorker()
}

// finishReattachedTurn completes an in-flight session/prompt that was started
// before fyrd restarted (V0.90) when its JSON-RPC response arrives on a
// reattached pipe with no local waiter in c.pending.
func (c *acpSession) finishReattachedTurn(resp rpcResp, seq int64) bool {
	stopReason, hasStop := resp.result["stopReason"].(string)
	c.emitMu.Lock()
	c.mu.Lock()
	active := (c.reattachActive || c.unsolicitedActive) && !c.turnRunning
	if !hasStop && !(resp.err != nil && active) {
		c.mu.Unlock()
		c.emitMu.Unlock()
		return false
	}
	c.cancelSettleLocked()
	end := turnEnd{stopReason: stopReason, seq: seq}
	if resp.err != nil {
		end.err = resp.err
	}
	end.usage, _ = resp.result["usage"].(map[string]any)
	c.takeTurnLocked(&end)
	c.resetTurnMapsLocked()
	c.unsolicitedActive = false
	c.unsolicitedInterrupted = false
	c.mu.Unlock()
	c.closeTurn(end)
	// Release promptWorker only after the result is on c.out, so any queued
	// follow-up turn starts strictly after it (V0.agy-reattach-queue).
	c.mu.Lock()
	c.reattachActive = false
	c.mu.Unlock()
	c.emitMu.Unlock()
	c.signalWorker()
	return true
}

// turnEnd is how a turn ended and what closing it emits: the prompt's
// answer (stopReason, usage) or err, the runner seq the result carries
// (held: read() already noted it), the queued command it completes, and
// its duration when timed. The three ways a turn ends (its prompt
// answered, an unsolicited turn settled, a turn begun before a restart
// answered on a reattached pipe) all close it with closeTurn.
type turnEnd struct {
	stopReason string
	usage      map[string]any
	err        error
	seq        int64
	held       bool
	command    string
	durMs      int64
	timed      bool

	// Filled by takeTurnLocked.
	text, full string
	added, all []bgTool
}

// cancelled: the agent ended the turn because it was interrupted.
func (e turnEnd) cancelled() bool {
	return e.err == nil && (e.stopReason == "cancelled" || e.stopReason == "canceled")
}

// takeTurnLocked takes the turn's text not yet emitted and its whole text
// and, unless the turn failed or was cancelled, promotes its still-open
// tools to background tools. c.mu is held.
func (c *acpSession) takeTurnLocked(e *turnEnd) {
	e.text = c.textBuf.String()
	c.textBuf.Reset()
	e.full = c.turnText.String()
	c.turnText.Reset()
	if e.err == nil && !e.cancelled() {
		e.added, e.all = c.promoteOpenToolsLocked()
	}
}

// closeTurn emits the turn's trailing text, its new background tools, the
// completion of its command and its result. c.emitMu is held, so a
// reactive-wakeup notification cannot come between them.
func (c *acpSession) closeTurn(e turnEnd) {
	if strings.TrimSpace(e.text) != "" {
		c.emitAssistantText(e.text, 0)
	}
	if len(e.added) > 0 {
		c.emitBackgroundStarted(e.added, e.all)
	}
	if e.command != "" {
		c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": e.command, "state": "completed"}, 0)
	}
	res := cli.Msg{"type": "result", "subtype": "success", "is_error": false, "result": e.full}
	switch {
	case e.err != nil:
		res["subtype"], res["is_error"], res["result"] = "error", true, e.err.Error()
	case e.cancelled():
		res["subtype"] = "interrupted"
	}
	if e.usage != nil && e.err == nil {
		res["usage"] = e.usage
	}
	if e.timed {
		res["duration_ms"] = float64(e.durMs)
	}
	if e.held {
		c.emitHeld(res, e.seq)
	} else {
		c.emit(res, e.seq)
	}
}
