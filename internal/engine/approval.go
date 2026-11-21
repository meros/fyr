package engine

import (
	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
)

const approvalContextEvents = 200

// ApprovalContext reads the project, thread metadata, and recent owner-authored
// messages for the tool approval judge (V0.tool-approval-judge). It releases
// e.mu before reading the event log (V0.147).
func (e *Engine) ApprovalContext(threadID string) agent.ApprovalContext {
	e.mu.Lock()
	t := e.threads[threadID]
	if t == nil {
		e.mu.Unlock()
		return agent.ApprovalContext{}
	}
	var p *core.Project
	if t.ProjectID != "" {
		p = e.projects[t.ProjectID]
	}
	ac := agent.ApprovalContext{
		AutoJudge:     p.AutoJudgesApprovals(),
		ThreadTitle:   t.Title,
		ThreadBranch:  t.Branch,
		ThreadProfile: t.Profile,
		ThreadModel:   t.Model,
	}
	if p != nil {
		ac.ProjectName = p.Name
		ac.ProjectFolder = p.Folder
	}
	parent := t.Parent
	e.mu.Unlock()

	if !ac.AutoJudge {
		return ac
	}
	evs, err := e.st.EventsFor(threadID, 0, approvalContextEvents, true)
	if err != nil {
		return ac
	}
	turns := readTurns(evs)
	parentIncluded := false
	for _, tr := range turns {
		if tr.Kind != turnUser {
			continue
		}
		if tr.From == "" {
			ac.Messages = append(ac.Messages, core.ApprovalMessage{Text: tr.Text})
		} else if parent != "" && !parentIncluded && tr.From == "thread:"+parent {
			parentIncluded = true
			ac.Messages = append(ac.Messages, core.ApprovalMessage{Text: tr.Text, FromParent: true})
		}
	}
	return ac
}
