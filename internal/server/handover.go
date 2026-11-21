package server

import (
	"encoding/json"
	"net/http"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/engine"
)

// Trans-host handover host-protocol endpoints (decision 0039, V0.trans-host-handover).

type handoverExportRequest struct {
	WantBrief     bool   `json:"wantBrief,omitempty"`
	TargetHost    string `json:"targetHost,omitempty"`
	TargetProfile string `json:"targetProfile,omitempty"`
}

type handoverExportResponse struct {
	Thread         *engine.ThreadExport `json:"thread,omitempty"`
	CLI            string               `json:"cli,omitempty"`
	Git            *agent.HandoverGit   `json:"git,omitempty"`
	Conversation   string               `json:"conversation,omitempty"`
	SessionArchive []byte               `json:"sessionArchive,omitempty"`
	SessionNote    string               `json:"sessionNote,omitempty"`
	OpenChanges    string               `json:"openChanges,omitempty"`
	Brief          string               `json:"brief,omitempty"`
}

func (s *Server) handoverExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req handoverExportRequest
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	}
	if s.HandoverExport == nil {
		api.WriteJSON(w, 501, map[string]any{"ok": false, "error": "this host does not support trans-host handover export"})
		return
	}
	if req.WantBrief {
		if _, code, why := s.Engine.CanHandover(id); code != 200 {
			api.WriteJSON(w, code, map[string]any{"ok": false, "error": why})
			return
		}
		targetLabel := req.TargetProfile
		if req.TargetHost != "" {
			if targetLabel != "" {
				targetLabel += " on " + req.TargetHost
			} else {
				targetLabel = req.TargetHost
			}
		}
		ag, err := s.HandoverExport(id, true, targetLabel)
		if err != nil {
			api.WriteJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, handoverExportResponse{Brief: ag.Brief})
		return
	}
	th, code, why := s.Engine.ExportThread(id)
	if code != 200 {
		api.WriteJSON(w, code, map[string]any{"ok": false, "error": why})
		return
	}
	ag, err := s.HandoverExport(id, false, "")
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, handoverExportResponse{
		Thread:         &th,
		CLI:            ag.CLI,
		Git:            ag.Git,
		Conversation:   ag.Conversation,
		SessionArchive: ag.SessionArchive,
		SessionNote:    ag.SessionNote,
		OpenChanges:    ag.OpenChanges,
	})
}

type handoverImportRequest struct {
	FromHostID     string                 `json:"fromHostId"`
	FromHostName   string                 `json:"fromHostName,omitempty"`
	FromThreadID   string                 `json:"fromThreadId"`
	FromProfile    string                 `json:"fromProfile,omitempty"`
	FromCwd        string                 `json:"fromCwd,omitempty"`
	FromCLI        string                 `json:"fromCli,omitempty"`
	Title          string                 `json:"title,omitempty"`
	TitleBy        string                 `json:"titleBy,omitempty"`
	Cwd            string                 `json:"cwd,omitempty"`
	ProjectID      string                 `json:"projectId,omitempty"`
	UseWorktree    bool                   `json:"useWorktree,omitempty"`
	Profile        string                 `json:"profile"`
	Model          string                 `json:"model,omitempty"`
	Effort         *string                `json:"effort,omitempty"`
	SessionID      string                 `json:"sessionId,omitempty"`
	SessionArchive []byte                 `json:"sessionArchive,omitempty"`
	SessionNote    string                 `json:"sessionNote,omitempty"`
	Conversation   string                 `json:"conversation,omitempty"`
	Brief          string                 `json:"brief,omitempty"`
	OpenChanges    string                 `json:"openChanges,omitempty"`
	Summary        string                 `json:"summary,omitempty"`
	LiveView       bool                   `json:"liveView,omitempty"`
	PR             *engine.ExportedPR     `json:"pr,omitempty"`
	Git            *agent.HandoverGit     `json:"git,omitempty"`
	Events         []engine.ExportedEvent `json:"events,omitempty"`
}

func (s *Server) handoverImport(w http.ResponseWriter, r *http.Request) {
	if s.HandoverImport == nil {
		api.WriteJSON(w, 501, map[string]any{"ok": false, "error": "this host does not support trans-host handover import"})
		return
	}
	// Allow up to 36 MB JSON body (covers 20 MB base64-encoded session archive + events).
	var req handoverImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 36<<20)).Decode(&req); err != nil {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON: " + err.Error()})
		return
	}
	if req.FromHostID == "" || req.FromThreadID == "" || req.Profile == "" {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "fromHostId, fromThreadId and profile are required"})
		return
	}
	effort := ""
	if req.Effort != nil {
		effort = *req.Effort
	}
	tid, actualCwd, pid, wtNote, code, why := s.Engine.PrepareHandIn(r.Context(), engine.HandInSpec{
		FromHostID:   req.FromHostID,
		FromHostName: req.FromHostName,
		FromThreadID: req.FromThreadID,
		Title:        req.Title,
		TitleBy:      req.TitleBy,
		Cwd:          req.Cwd,
		ProjectID:    req.ProjectID,
		UseWorktree:  req.UseWorktree,
		Profile:      req.Profile,
		Model:        req.Model,
		Effort:       effort,
		Summary:      req.Summary,
		LiveView:     req.LiveView,
		PR:           req.PR,
		Git:          req.Git,
		Events:       req.Events,
	})
	if code != 200 {
		api.WriteJSON(w, code, map[string]any{"ok": false, "error": why})
		return
	}
	res, err := s.HandoverImport(tid, agent.HandoverInReq{
		FromHostID:     req.FromHostID,
		FromHostName:   req.FromHostName,
		FromThreadID:   req.FromThreadID,
		FromProfile:    req.FromProfile,
		FromCwd:        req.FromCwd,
		FromCLI:        req.FromCLI,
		ToHostID:       s.ID,
		ToHostName:     s.hostName(),
		Profile:        req.Profile,
		Model:          req.Model,
		Effort:         req.Effort,
		SessionID:      req.SessionID,
		SessionArchive: req.SessionArchive,
		SessionNote:    req.SessionNote,
		Conversation:   req.Conversation,
		Brief:          req.Brief,
		Git:            req.Git,
		WorktreeNote:   wtNote,
		OpenChanges:    req.OpenChanges,
	})
	if err != nil {
		s.Engine.RollbackHandIn(tid)
		api.WriteJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if res.NeedBrief {
		s.Engine.RollbackHandIn(tid)
		api.WriteJSON(w, 200, map[string]any{"ok": false, "needBrief": true, "note": res.Note})
		return
	}
	api.WriteJSON(w, 200, map[string]any{
		"ok":        true,
		"threadId":  tid,
		"rung":      res.Rung,
		"note":      res.Note,
		"cwd":       actualCwd,
		"projectId": pid,
	})
}
