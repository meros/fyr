package agycli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/cli"
)

type acpToolStatus string

const (
	toolStatusPending    acpToolStatus = "pending"
	toolStatusInProgress acpToolStatus = "in_progress"
	toolStatusCompleted  acpToolStatus = "completed"
	toolStatusFailed     acpToolStatus = "failed"
	toolStatusCancelled  acpToolStatus = "cancelled"
)

type bgTool struct {
	id       string
	taskType string
	desc     string
}

func (c *acpSession) promoteOpenToolsLocked() (added, all []bgTool) {
	for id := range c.emittedTools {
		if c.finishedTools[id] {
			continue
		}
		if _, exists := c.bgTools[id]; exists {
			continue
		}
		bt, ok := c.toolDescs[id]
		if !ok {
			name := c.toolNames[id]
			if name == "" {
				name = c.rawNames[id]
			}
			taskType := "local_bash"
			if name == "Agent" || name == "Task" {
				taskType = "local_agent"
			}
			bt = bgTool{id: id, taskType: taskType, desc: name}
		}
		c.bgTools[id] = bt
		added = append(added, bt)
	}
	if len(added) == 0 {
		return nil, nil
	}
	sort.Slice(added, func(i, j int) bool { return added[i].id < added[j].id })
	all = c.sortedBGToolsLocked()
	return added, all
}

func (c *acpSession) sortedBGToolsLocked() []bgTool {
	if len(c.bgTools) == 0 {
		return nil
	}
	all := make([]bgTool, 0, len(c.bgTools))
	for _, bt := range c.bgTools {
		all = append(all, bt)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	return all
}

func (c *acpSession) emitBackgroundChanged(all []bgTool) {
	tasks := make([]any, 0, len(all))
	for _, bt := range all {
		tasks = append(tasks, cli.Msg{
			"task_id":     bt.id,
			"task_type":   bt.taskType,
			"description": bt.desc,
		})
	}
	c.emit(cli.Msg{
		"type":    "system",
		"subtype": "background_tasks_changed",
		"tasks":   tasks,
	}, 0)
}

func (c *acpSession) emitBackgroundStarted(added, all []bgTool) {
	c.emitBackgroundChanged(all)
	for _, bt := range added {
		c.emit(cli.Msg{
			"type":            "system",
			"subtype":         "task_started",
			"task_id":         bt.id,
			"tool_use_id":     bt.id,
			"task_type":       bt.taskType,
			"description":     bt.desc,
			"is_backgrounded": true,
		}, 0)
	}
}

func makeBGTool(callID, mappedName string, mappedArgs map[string]any) bgTool {
	taskType := "local_bash"
	if mappedName == "Agent" || mappedName == "Task" {
		taskType = "local_agent"
	}
	desc := ""
	for _, key := range []string{"description", "command", "prompt", "file_path", "pattern", "query"} {
		if v, ok := mappedArgs[key].(string); ok {
			if line := firstNonEmptyLine(v); line != "" {
				desc = line
				break
			}
		}
	}
	if desc == "" {
		desc = mappedName
	}
	return bgTool{id: callID, taskType: taskType, desc: desc}
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if r := []rune(line); len(r) > 120 {
				return string(r[:119]) + "…"
			}
			return line
		}
	}
	return ""
}

func (c *acpSession) handleNotification(method string, rawParams any, seq int64) bool {
	if method != "session/update" {
		return false
	}
	params, _ := rawParams.(map[string]any)
	if params == nil {
		return false
	}
	if sid, _ := params["sessionId"].(string); sid != "" {
		c.mu.Lock()
		if c.sessionID == "" {
			c.sessionID = sid
		}
		c.mu.Unlock()
	}
	upd, _ := params["update"].(map[string]any)
	if upd == nil {
		return false
	}
	kind, _ := upd["sessionUpdate"].(string)
	switch kind {
	case "agent_message_chunk":
		if content, _ := upd["content"].(map[string]any); content != nil {
			if t, _ := content["text"].(string); t != "" {
				wake := false
				c.mu.Lock()
				c.textBuf.WriteString(t)
				c.turnText.WriteString(t)
				if !c.turnRunning {
					if c.reattachActive {
						c.cancelSettleLocked()
					} else {
						c.unsolicitedActive = true
						// Fallback settle window in case agy omits usage_update
						// (e.g. quota warnings); usage_update tightens this to settleDelay.
						// Not while a tool is open: the model wrote this note and
						// then waited on the tool, it did not end its turn.
						if len(c.openPerms) == 0 && !c.openToolLocked() {
							c.armSettleLocked(3 * c.settleDelay)
							wake = true
						}
					}
				}
				c.mu.Unlock()
				if wake {
					c.signalWorker()
				}
			}
		}
		return false
	case "agent_thought_chunk":
		c.emitMu.Lock()
		c.mu.Lock()
		c.cancelSettleLocked()
		startUnsolicited := !c.turnRunning && !c.reattachActive && !c.unsolicitedActive
		if startUnsolicited {
			c.unsolicitedActive = true
		}
		c.mu.Unlock()
		if startUnsolicited {
			// V0.agy-reactive-wakeup: open the run immediately as "Thinking"
			// when a reactive wakeup starts thinking before its first tool call.
			c.emit(cli.Msg{
				"type": "assistant",
				"message": cli.Msg{
					"role":    "assistant",
					"content": []any{cli.Msg{"type": "thinking", "thinking": ""}},
				},
			}, seq)
			c.emitMu.Unlock()
			return true
		}
		c.emitMu.Unlock()
		return false
	case "usage_update":
		wake := false
		c.mu.Lock()
		if !c.turnRunning && (c.unsolicitedActive || c.reattachActive) &&
			strings.TrimSpace(c.textBuf.String()) != "" && len(c.openPerms) == 0 && !c.openToolLocked() {
			c.armSettleLocked(c.settleDelay)
			wake = true
		}
		c.mu.Unlock()
		if wake {
			c.signalWorker()
		}
		return false
	case "available_commands_update":
		if rawCmds, _ := upd["availableCommands"].([]any); len(rawCmds) > 0 {
			cmds := make([]cli.Msg, 0, len(rawCmds))
			for _, x := range rawCmds {
				if m, ok := x.(map[string]any); ok {
					cmds = append(cmds, m)
				}
			}
			c.mu.Lock()
			c.commands = cmds
			c.mu.Unlock()
		}
		return false
	case "config_option_update":
		c.mu.Lock()
		c.updateConfigOptionsLocked(upd["configOptions"])
		c.mu.Unlock()
		return false
	case "tool_call", "tool_call_update":
		return c.handleToolUpdate(upd, seq)
	}
	return false
}

func (c *acpSession) handleToolUpdate(upd map[string]any, seq int64) bool {
	callID, _ := upd["toolCallId"].(string)
	rawName, _ := upd["name"].(string)
	if callID == "" {
		return false
	}
	args, _ := upd["args"].(map[string]any)
	if args == nil {
		args, _ = upd["rawInput"].(map[string]any)
	}
	status := acpToolStatus(fmt.Sprint(upd["status"]))
	isTerminal := status == toolStatusCompleted || status == toolStatusFailed || status == toolStatusCancelled

	c.emitMu.Lock()
	defer c.emitMu.Unlock()

	c.mu.Lock()
	c.cancelSettleLocked()
	if rawName == "" {
		rawName = c.rawNames[callID]
	}
	if rawName == "" {
		rawName, _ = upd["title"].(string)
	}
	if rawName != "" {
		c.rawNames[callID] = rawName
	}
	alreadyEmitted := c.emittedTools[callID]
	alreadyFinished := c.finishedTools[callID]
	mcpNames := c.mcpNames
	if !c.turnRunning && !c.reattachActive && !isTerminal {
		c.unsolicitedActive = true
	}
	c.mu.Unlock()

	queued := false
	if !alreadyEmitted && rawName != "" {
		c.flushAssistantText(0)
		mappedName, mappedArgs := translateToolCall(rawName, args, mcpNames)
		bt := makeBGTool(callID, mappedName, mappedArgs)
		c.mu.Lock()
		c.emittedTools[callID] = true
		c.toolNames[callID] = mappedName
		c.toolDescs[callID] = bt
		c.mu.Unlock()
		c.emit(cli.Msg{
			"type": "assistant",
			"message": cli.Msg{
				"role": "assistant",
				"content": []any{cli.Msg{
					"type":  "tool_use",
					"id":    callID,
					"name":  mappedName,
					"input": mappedArgs,
				}},
			},
		}, seq)
		queued = true
	}

	if !alreadyFinished && isTerminal {
		if !queued {
			// Flush any interim assistant text written before this tool finished
			// (e.g. a background command completing right after a status note).
			c.flushAssistantText(0)
		}
		var wasBG bool
		var remainingBG []bgTool
		rearm := false
		c.mu.Lock()
		c.finishedTools[callID] = true
		if _, ok := c.bgTools[callID]; ok {
			wasBG = true
			delete(c.bgTools, callID)
			remainingBG = c.sortedBGToolsLocked()
		}
		// V0.agy-settle-final-text (amends V0.agy-open-tool-settle): a tool
		// ending is not a turn ending. The live streams complete a tool and
		// then think for seconds (sometimes minutes) before the next tool
		// batch or the final answer, so arming here closed a turn the model
		// was still answering — one continuous answer became dozens of runs.
		// Only an interrupted turn is armed here: its cancel already ended
		// the stream and the open tool is the last thing it waits on. A quiet
		// turn is settled by its last assistant text, which arms on arrival.
		if c.unsolicitedInterrupted && !c.turnRunning && !c.reattachActive &&
			len(c.openPerms) == 0 && !c.openToolLocked() {
			c.armSettleLocked(c.settleDelay)
			rearm = true
		}
		c.mu.Unlock()
		resultText := extractToolResult(upd)
		resultSeq := seq
		if queued {
			resultSeq = 0 // already tracked seq on the tool_use message
		}
		c.emit(cli.Msg{
			"type": "user",
			"message": cli.Msg{
				"role": "user",
				"content": []any{cli.Msg{
					"type":        "tool_result",
					"tool_use_id": callID,
					"content":     resultText,
					"is_error":    status == toolStatusFailed || status == toolStatusCancelled,
				}},
			},
		}, resultSeq)
		if wasBG {
			taskStat := "completed"
			switch status {
			case toolStatusFailed:
				taskStat = "failed"
			case toolStatusCancelled:
				taskStat = "stopped"
			}
			c.emit(cli.Msg{
				"type":    "system",
				"subtype": "task_notification",
				"task_id": callID,
				"status":  taskStat,
				"summary": firstNonEmptyLine(resultText),
			}, 0)
			c.emitBackgroundChanged(remainingBG)
		}
		if rearm {
			c.signalWorker()
		}
		queued = true
	}
	return queued
}

func extractToolResult(upd map[string]any) string {
	if rawContent, _ := upd["content"].([]any); len(rawContent) > 0 {
		var sb strings.Builder
		for _, x := range rawContent {
			m, _ := x.(map[string]any)
			if m == nil {
				continue
			}
			if inner, _ := m["content"].(map[string]any); inner != nil {
				if t, _ := inner["text"].(string); t != "" {
					if sb.Len() > 0 {
						sb.WriteByte('\n')
					}
					sb.WriteString(t)
					continue
				}
			}
			if t, _ := m["text"].(string); t != "" {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(t)
			}
		}
		if sb.Len() > 0 {
			return unwrapToolResult(sb.String())
		}
	}
	if rawOut, _ := upd["rawOutput"].(map[string]any); rawOut != nil {
		if s, _ := rawOut["output"].(string); s != "" {
			return unwrapToolResult(s)
		}
		if b, err := json.Marshal(rawOut); err == nil {
			return unwrapToolResult(string(b))
		}
	}
	if s, _ := upd["rawOutput"].(string); s != "" {
		return unwrapToolResult(s)
	}
	return ""
}
