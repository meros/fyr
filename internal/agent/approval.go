package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/meros/fyr/internal/core"
)

// ApprovalContext is the trusted project, thread, and owner-message context
// fed to the tool approval judge (V0.tool-approval-judge).
type ApprovalContext struct {
	AutoJudge     bool
	ProjectName   string
	ProjectFolder string
	ThreadTitle   string
	ThreadBranch  string
	ThreadProfile string
	ThreadModel   string
	Messages      []core.ApprovalMessage
}

// judgeApproval runs the approval judge for one tool permission request.
// It returns allow=true ONLY when the judge explicitly returns JSON
// {"decision":"allow",...} (or an identical tool+input call was already
// approved in this session). Any error, timeout, or ambiguous output fails
// closed to allow=false so the owner sees the normal approval card.
func (m *Manager) judgeApproval(ctx context.Context, s *session, tool string, input map[string]any, agentID string, ac ApprovalContext) (allow bool, reason, model string, dur time.Duration, cached bool) {
	key := core.CanonicalApprovalKey(tool, input)
	if key != "" {
		m.mu.Lock()
		if r, ok := s.approved[key]; ok {
			m.mu.Unlock()
			return true, r, "", 0, true
		}
		m.mu.Unlock()
	}

	sa, err := m.ResolveApprovalJudge(ac.ThreadProfile, ac.ThreadModel)
	if err != nil {
		log.Printf("thread %s: approval judge resolve: %v", s.thread, err)
		return false, core.CleanJudgeReason("Approval judge unavailable: " + err.Error()), "", 0, false
	}

	select {
	case m.approvalSlots <- struct{}{}:
		defer func() { <-m.approvalSlots }()
	case <-ctx.Done():
		return false, "", sa.Model, 0, false
	}

	m.observe(s.thread, core.KindDigest, core.DigestArgs{Activity: "Checking " + tool + " approval…"})

	prompt := core.FormatApprovalPrompt(core.ApprovalPromptInput{
		ProjectName:   ac.ProjectName,
		ProjectFolder: ac.ProjectFolder,
		ThreadTitle:   ac.ThreadTitle,
		ThreadBranch:  ac.ThreadBranch,
		Tool:          tool,
		Input:         input,
		Subagent:      agentID != "",
		Messages:      ac.Messages,
	})

	start := time.Now()
	jctx, cancel := context.WithTimeout(ctx, m.cfg.ApprovalTimeout)
	defer cancel()

	out, err := m.printOn(jctx, core.JobApproval, s.thread, sa, core.ApprovalJudgeSystem, prompt)
	dur = time.Since(start)
	if ctx.Err() != nil {
		return false, "", sa.Model, dur, false
	}
	if err != nil {
		if jctx.Err() != nil {
			log.Printf("thread %s: approval judge timed out after %s (%s)", s.thread, dur.Round(time.Millisecond), sa.Model)
			return false, "Approval judge timed out", sa.Model, dur, false
		}
		log.Printf("thread %s: approval judge error (%s): %v", s.thread, sa.Model, err)
		return false, core.CleanJudgeReason(fmt.Sprintf("Approval judge failed: %v", err)), sa.Model, dur, false
	}

	okAllow, why, ok := core.ParseApprovalJudge(out)
	if !ok {
		log.Printf("thread %s: approval judge unclear reply (%s): %q", s.thread, sa.Model, core.TruncHead(out, 200))
		return false, "Approval judge gave an unclear reply", sa.Model, dur, false
	}
	if okAllow {
		if key != "" {
			m.mu.Lock()
			if s.approved == nil {
				s.approved = map[string]string{}
			}
			s.approved[key] = why
			m.mu.Unlock()
		}
		return true, why, sa.Model, dur, false
	}
	if why == "" {
		why = "Escalated for your review"
	}
	return false, why, sa.Model, dur, false
}
