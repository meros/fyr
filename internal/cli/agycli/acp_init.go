package agycli

import (
	"context"
	"fmt"

	"github.com/meros/fyr/internal/cli"
)

// Initialize performs the ACP handshake (`initialize`), opens or resumes the
// session (`session/new` or `session/resume`), applies the requested model,
// effort, and permission mode, and returns Fyr's initialize answer with
// discovered commands and models.
func (c *acpSession) Initialize(ctx context.Context, hooks cli.Msg) (cli.Msg, error) {
	if _, err := c.request(ctx, "initialize", cli.Msg{
		"protocolVersion":    1,
		"clientCapabilities": cli.Msg{},
	}); err != nil {
		return nil, err
	}
	mcpServers := c.spec.MCPServers
	if mcpServers == nil {
		mcpServers = []cli.Msg{}
	}
	params := cli.Msg{
		"cwd":        c.spec.Dir,
		"mcpServers": mcpServers,
	}
	if len(c.spec.AddDirs) > 0 {
		params["additionalDirectories"] = c.spec.AddDirs
	}
	if c.spec.SystemPrompt != "" {
		params["_meta"] = cli.Msg{
			"antigravity": cli.Msg{
				"systemPrompt": cli.Msg{"append": c.spec.SystemPrompt},
			},
		}
	}
	method := "session/new"
	if c.spec.Resume && c.spec.SessionID != "" {
		method = "session/resume"
		params["sessionId"] = c.spec.SessionID
	}
	res, err := c.request(ctx, method, params)
	if err != nil {
		return nil, err
	}
	sid, _ := res["sessionId"].(string)
	if sid == "" {
		sid = c.spec.SessionID
	}
	if sid == "" {
		return nil, fmt.Errorf("%s returned no sessionId", method)
	}
	models := ParseACPModels(res)
	c.mu.Lock()
	c.sessionID = sid
	c.availableModels = parseAvailableModels(res)
	c.updateConfigOptionsLocked(res["configOptions"])
	c.availableModes = parseAvailableModes(res)
	if mObj, _ := res["models"].(map[string]any); mObj != nil {
		if cur, _ := mObj["currentModelId"].(string); cur != "" && c.currentModel == "" {
			c.currentModel = cur
		}
	}
	c.mu.Unlock()

	if c.spec.Model != "" || c.spec.Effort != "" {
		if err := c.applyModelAndEffort(ctx, c.spec.Model, c.spec.Effort); err != nil {
			return nil, err
		}
	}
	if mode := mapPermissionMode(c.spec.PermissionMode); mode != "" {
		c.mu.Lock()
		modeOK := len(c.availableModes) == 0 || c.availableModes[mode]
		c.mu.Unlock()
		if modeOK {
			// Best-effort: some account tiers (e.g. enterprise) disallow `auto`.
			_, _ = c.request(ctx, "session/set_mode", cli.Msg{"sessionId": sid, "modeId": mode})
		}
	}

	c.mu.Lock()
	cmds := append([]cli.Msg(nil), c.commands...)
	c.mu.Unlock()
	resp := cli.Msg{"session_id": sid, "models": models}
	if len(cmds) > 0 {
		raw := make([]any, len(cmds))
		for i, x := range cmds {
			raw[i] = x
		}
		resp["commands"] = raw
	}
	return cli.Msg{"subtype": "success", "response": resp}, nil
}

func (c *acpSession) applyModelAndEffort(ctx context.Context, model, effort string) error {
	c.mu.Lock()
	sid := c.sessionID
	hasModelOpt := c.hasConfigValueLocked("model", model)
	avail := append([]availModel(nil), c.availableModels...)
	c.mu.Unlock()

	if model != "" {
		if hasModelOpt {
			res, err := c.request(ctx, "session/set_config_option", cli.Msg{
				"sessionId": sid, "configId": "model", "value": model,
			})
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.currentModel = model
			c.updateConfigOptionsLocked(res["configOptions"])
			c.mu.Unlock()
		} else {
			target := model
			if effort != "" {
				cand := model + "-" + effort
				for _, a := range avail {
					if a.id == cand {
						target = cand
						effort = "" // already encoded in modelId
						break
					}
				}
			}
			if _, err := c.request(ctx, "session/set_model", cli.Msg{
				"sessionId": sid, "modelId": target,
			}); err != nil {
				return err
			}
			c.mu.Lock()
			c.currentModel = target
			c.mu.Unlock()
		}
	}
	if effort != "" {
		c.mu.Lock()
		hasEff := c.hasConfigValueLocked("thought_level", effort)
		c.mu.Unlock()
		if hasEff {
			res, err := c.request(ctx, "session/set_config_option", cli.Msg{
				"sessionId": sid, "configId": "thought_level", "value": effort,
			})
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.currentEffort = effort
			c.updateConfigOptionsLocked(res["configOptions"])
			c.mu.Unlock()
		}
	}
	return nil
}

func (c *acpSession) updateConfigOptionsLocked(raw any) {
	opts, _ := raw.([]any)
	if len(opts) == 0 {
		return
	}
	out := make([]cli.Msg, 0, len(opts))
	for _, x := range opts {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
			if id, _ := m["id"].(string); id == "model" {
				if cur, _ := m["currentValue"].(string); cur != "" {
					c.currentModel = cur
				}
			}
			if id, _ := m["id"].(string); id == "thought_level" {
				if cur, _ := m["currentValue"].(string); cur != "" {
					c.currentEffort = cur
				}
			}
		}
	}
	c.configOptions = out
}

func (c *acpSession) hasConfigValueLocked(configID, value string) bool {
	if value == "" {
		return false
	}
	for _, o := range c.configOptions {
		if id, _ := o["id"].(string); id != configID {
			continue
		}
		rawOpts, _ := o["options"].([]any)
		for _, ro := range rawOpts {
			if m, _ := ro.(map[string]any); m["value"] == value {
				return true
			}
		}
	}
	return false
}

func parseAvailableModes(res cli.Msg) map[string]bool {
	out := map[string]bool{}
	modesObj, _ := res["modes"].(map[string]any)
	raw, _ := modesObj["availableModes"].([]any)
	for _, x := range raw {
		if m, _ := x.(map[string]any); m != nil {
			if id, _ := m["id"].(string); id != "" {
				out[id] = true
			}
		}
	}
	return out
}

// SetModel changes the active model mid-session (re-applying the current
// effort when supported by the new model).
func (c *acpSession) SetModel(ctx context.Context, model string) error {
	c.mu.Lock()
	eff := c.currentEffort
	c.mu.Unlock()
	return c.applyModelAndEffort(ctx, model, eff)
}
