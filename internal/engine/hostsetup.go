package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/stream"
)

// Host setup (SPEC 0.5 V0.208): the project setup of V0.116 for a host's mark.
// An agent run on the host picks the icon and colour the owner has not set
// with the tool host_setup. The host keeps its agent's choice itself, apart
// from the owner's marks (see core/hostmarks.go); the fronts show the owner's
// value first.

const agentMarkKey = "hostAgentMark"

// AgentMark is what this host's agent set and its last setup run.
type AgentMark struct {
	Icon  string             `json:"icon,omitempty"`
	Color string             `json:"color,omitempty"`
	Setup *core.ProjectSetup `json:"setup,omitempty"`
}

// loadAgentMark reads the stored mark. Call it with e.mu held.
func (e *Engine) loadAgentMark() {
	raw, _ := e.st.GetKV(agentMarkKey)
	if raw == "" {
		return
	}
	var m AgentMark
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		log.Printf("host setup: %v", err)
		return
	}
	e.agentMark = m
}

// AgentMark is what this host's agent set (none: empty).
func (e *Engine) AgentMark() AgentMark {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agentMark
}

// changeAgentMark changes the stored mark and tells the fronts.
func (e *Engine) changeAgentMark(change func(*AgentMark)) error {
	e.mu.Lock()
	m := e.agentMark
	change(&m)
	b, _ := json.Marshal(m)
	err := e.st.SetKV(agentMarkKey, string(b))
	if err == nil {
		e.agentMark = m
	}
	e.mu.Unlock()
	if err == nil && e.Hub != nil {
		e.Hub.Send(stream.FrameAgentMark, m)
	}
	return err
}

// ownerMarkBy is who set this host's mark in the owner's marks it keeps.
func (e *Engine) ownerMarkBy() core.HostMarkBy {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.hostMarks.Marks[e.InstallID]
	return core.HostMarkOwned(m.Icon, m.Color, m.By)
}

func (e *Engine) hostSetupBrief() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fyr host setup for this machine (%q).\n\n", e.HostName)
	b.WriteString("Give this host its mark in Fyr with the tool host_setup {icon?, color?}. Fyr keeps every field the owner set and tells you which; do not try again for those. ")
	b.WriteString("Do not ask the owner anything, do not change anything on this machine, and finish in a minute or two.\n\n")
	b.WriteString("1. Look at what this machine is, read-only: its hostname, hostnamectl, /etc/os-release, /sys/class/dmi/id/product_name, and what it mainly runs.\n")
	b.WriteString("2. Icon: the glyph that fits best (" + strings.Join(core.HostIcons, ", ") + "), or one emoji when the hostname suggests one clearly.\n")
	b.WriteString("3. Colour: one of " + strings.Join(core.Palette, ", ") + ", one that suits the machine or its name.\n\n")
	b.WriteString("End with one line saying what you set and why.")
	return b.String()
}

// startHostSetup starts a setup run: a thread in the home folder on the system
// agent's provider and model, then the record. A system agent that cannot run
// is recorded (the host's page shows it) and answered as 409.
func (e *Engine) startHostSetup(cmdID string) api.Reply {
	record := func(s core.ProjectSetup) {
		s.At = e.Now()
		if err := e.changeAgentMark(func(m *AgentMark) { m.Setup = &s }); err != nil {
			log.Printf("host setup: %v", err)
		}
	}
	fail := func(why string) api.Reply {
		record(core.ProjectSetup{Error: why})
		return api.Reply{Error: "Host setup cannot start: " + why, Status: 409}
	}
	if e.SystemAgentResolve == nil {
		return fail("this host runs no system agents")
	}
	profile, model, err := e.SystemAgentResolve(core.JobSetup)
	if err != nil {
		return fail(err.Error())
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err.Error())
	}
	args := core.Args{"cwd": home, "title": "Set up this host's mark", "text": e.hostSetupBrief()}
	if profile != "" {
		args["profile"] = profile
	}
	if model != "" {
		args["model"] = model
	}
	t := e.submit(core.Input{Source: core.FromOwner, Kind: "thread.create", Args: args, CmdID: cmdID + "/thread"})
	if !t.OK {
		return fail(t.Error)
	}
	record(core.ProjectSetup{Thread: t.ThreadID})
	return api.Reply{OK: true, Status: 200, ThreadID: t.ThreadID, Result: map[string]any{"thread": t.ThreadID}}
}

// SetupHostOnce starts this host's one automatic run: not when the owner set
// both fields or a run has started; a run that never started gets another try.
func (e *Engine) SetupHostOnce() bool {
	by := e.ownerMarkBy()
	s := e.AgentMark().Setup
	if e.InstallID == "" || (by.Icon != "" && by.Color != "") || (s != nil && s.Error == "") {
		return false
	}
	r := e.startHostSetup(NewID("setup_"))
	if !r.OK {
		log.Printf("host setup: %s", r.Error)
	}
	return r.OK
}

func (e *Engine) hostSetupTools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "host_setup",
		Description: "Set this host's mark in Fyr: its icon and colour. Fyr fills only the fields the owner has not set and says which it kept. " +
			"icon is a glyph name or one emoji.",
		Schema: obj(map[string]any{
			"icon":  str("One of " + strings.Join(core.HostIcons, ", ") + ", or one emoji."),
			"color": map[string]any{"type": "string", "enum": core.Palette, "description": "A palette colour."},
		}),
		Call: func(_ context.Context, _ string, a map[string]any) (string, error) {
			icon, color := strings.TrimSpace(sArg(a, "icon")), strings.TrimSpace(sArg(a, "color"))
			if icon == "" && color == "" {
				return "", errors.New("give icon or color")
			}
			if icon != "" && !core.HostIconOK(icon) {
				return "", fmt.Errorf("icon must be one of %s, or one emoji", strings.Join(core.HostIcons, ", "))
			}
			if color != "" && !core.ValidColor(color) {
				return "", fmt.Errorf("color must be one of %v", core.Palette)
			}
			by := e.ownerMarkBy()
			var set, kept []string
			if icon != "" {
				if core.CanWrite(core.FieldHostIcon, core.ByAgent, by.Icon, "it") != nil {
					icon, kept = "", append(kept, "icon")
				} else {
					set = append(set, fmt.Sprintf("icon (%s)", icon))
				}
			}
			if color != "" {
				if core.CanWrite(core.FieldHostColor, core.ByAgent, by.Color, "it") != nil {
					color, kept = "", append(kept, "colour")
				} else {
					set = append(set, fmt.Sprintf("colour (%s)", color))
				}
			}
			if len(set) > 0 {
				err := e.changeAgentMark(func(m *AgentMark) {
					if icon != "" {
						m.Icon = icon
					}
					if color != "" {
						m.Color = color
					}
				})
				if err != nil {
					return "", err
				}
			}
			out := ""
			if len(set) > 0 {
				out = "Set the " + strings.Join(set, ", ") + "."
			}
			if len(kept) > 0 {
				out = strings.TrimSpace(out + " Kept the " + strings.Join(kept, ", ") + ": the owner set it.")
			}
			return out, nil
		}})
}
