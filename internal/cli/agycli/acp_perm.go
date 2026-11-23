package agycli

import (
	"fmt"

	"github.com/meros/fyr/internal/cli"
)

type acpPermKind string

const (
	permAllowOnce    acpPermKind = "allow_once"
	permAllowAlways  acpPermKind = "allow_always"
	permRejectOnce   acpPermKind = "reject_once"
	permRejectAlways acpPermKind = "reject_always"
)

func (c *acpSession) handleServerRequest(rawID any, idKey, method string, rawParams any) {
	if method != "session/request_permission" {
		_ = c.Write(cli.Msg{
			"jsonrpc": "2.0",
			"id":      rawID,
			"error":   cli.Msg{"code": -32601, "message": "unsupported method: " + method},
		})
		c.Answered(idKey)
		return
	}
	params, _ := rawParams.(map[string]any)
	if sid, _ := params["sessionId"].(string); sid != "" {
		c.mu.Lock()
		if c.sessionID == "" {
			c.sessionID = sid
		}
		c.mu.Unlock()
	}
	tc, _ := params["toolCall"].(map[string]any)
	rawName, _ := tc["name"].(string)
	if rawName == "" {
		rawName, _ = tc["title"].(string)
	}
	callID, _ := tc["toolCallId"].(string)
	args, _ := tc["args"].(map[string]any)
	if args == nil {
		args, _ = tc["rawInput"].(map[string]any)
	}
	c.mu.Lock()
	mcpNames := c.mcpNames
	c.mu.Unlock()
	mappedName, mappedArgs := translateToolCall(rawName, args, mcpNames)
	options, _ := params["options"].([]any)

	allowID, denyID := pickPermissionOptions(options)

	// 1. Check Fyr's PreToolUse gate hook (merge gate, sudo gate, heavy-job slots).
	if mappedName == "Bash" && c.h.Hook != nil {
		gateResp := c.h.Hook("gate", cli.Msg{
			"tool_name":   mappedName,
			"tool_input":  mappedArgs,
			"tool_use_id": callID,
		})
		if gateDenied(gateResp) {
			c.replyPermission(rawID, idKey, denyID)
			return
		}
	}

	// 2. Enforce disabled CLI timers (schedule) when Fyr's watch tool is active.
	if isTimerBlocked(rawName, c.spec.DisallowedTools) {
		c.replyPermission(rawID, idKey, denyID)
		return
	}

	// 3. Auto-allow Fyr's own MCP tools (e.g. mcp__fyr__*).
	if matchesToolPattern(mappedName, c.spec.AllowedTools) {
		c.replyPermission(rawID, idKey, allowID)
		return
	}

	// 4. Ask the owner through Fyr's CanUseTool handler (blocks until answered).
	if c.h.CanUseTool == nil {
		c.replyPermission(rawID, idKey, allowID)
		return
	}
	c.mu.Lock()
	c.cancelSettleLocked()
	c.openPerms[idKey] = true
	c.mu.Unlock()
	ans := c.h.CanUseTool(cli.Msg{
		"request_id":  idKey,
		"tool_name":   mappedName,
		"input":       mappedArgs,
		"tool_use_id": callID,
	})
	c.mu.Lock()
	delete(c.openPerms, idKey)
	c.mu.Unlock()

	if ans["behavior"] == "allow" {
		c.replyPermission(rawID, idKey, allowID)
	} else {
		c.replyPermission(rawID, idKey, denyID)
	}
}

func (c *acpSession) replyPermission(rawID any, idKey, optionID string) {
	var outcome cli.Msg
	if optionID != "" {
		outcome = cli.Msg{"outcome": "selected", "optionId": optionID}
	} else {
		outcome = cli.Msg{"outcome": "cancelled"}
	}
	if c.Write(cli.Msg{"jsonrpc": "2.0", "id": rawID, "result": cli.Msg{"outcome": outcome}}) == nil {
		c.Answered(idKey)
	}
}

func pickPermissionOptions(options []any) (allowID, denyID string) {
	for _, x := range options {
		m, _ := x.(map[string]any)
		if m == nil {
			continue
		}
		id, _ := m["optionId"].(string)
		kind := acpPermKind(fmt.Sprint(m["kind"]))
		switch kind {
		case permAllowOnce:
			if allowID == "" {
				allowID = id
			}
		case permAllowAlways:
			if allowID == "" {
				allowID = id
			}
		case permRejectOnce:
			if denyID == "" {
				denyID = id
			}
		case permRejectAlways:
			if denyID == "" {
				denyID = id
			}
		}
	}
	return allowID, denyID
}

func gateDenied(resp cli.Msg) bool {
	if resp == nil {
		return false
	}
	inner, _ := resp["hookSpecificOutput"].(cli.Msg)
	if inner == nil {
		if m, ok := resp["hookSpecificOutput"].(map[string]any); ok {
			inner = m
		}
	}
	if inner == nil {
		return false
	}
	return inner["permissionDecision"] == "deny"
}
