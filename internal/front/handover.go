package front

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/core"
)

// Trans-host handover orchestration on the front (decision 0039,
// V0.trans-host-handover): exports context from the source host, imports it as
// a linked new thread on the target host (running the 3-rung ladder there,
// requesting a rung-3 Brief from the source only if needed), and settles the
// source thread with a link forward once the target succeeds.

const KindThreadHandover = "front.threadHandover"

type handoverExportRes struct {
	Thread         map[string]any  `json:"thread"`
	CLI            string          `json:"cli"`
	Git            json.RawMessage `json:"git"`
	Conversation   string          `json:"conversation"`
	SessionArchive json.RawMessage `json:"sessionArchive"`
	SessionNote    string          `json:"sessionNote"`
	OpenChanges    string          `json:"openChanges"`
	Brief          string          `json:"brief"`
	Error          string          `json:"error"`
}

type handoverImportRes struct {
	OK        bool   `json:"ok"`
	ThreadID  string `json:"threadId"`
	Rung      string `json:"rung"`
	Note      string `json:"note"`
	Cwd       string `json:"cwd"`
	ProjectID string `json:"projectId"`
	NeedBrief bool   `json:"needBrief"`
	Error     string `json:"error"`
}

func (f *Front) postHostJSON(ctx context.Context, l *link, actor authn.Principal, origin, path string, body any, timeout time.Duration, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 500, err
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodPost, l.base.String()+path, bytes.NewReader(raw))
	if err != nil {
		return 500, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fyr", "1")
	setActor(req, actor, f.opt.ID, origin)
	resp, err := l.client.Do(req)
	if err != nil {
		return 502, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	if err != nil {
		return 502, err
	}
	if out != nil && len(data) > 0 {
		_ = json.Unmarshal(data, out)
	}
	return resp.StatusCode, nil
}

// threadHandoverCommand runs front.threadHandover {fromHost, threadId, toHost,
// profile, model?, effort?, cwd?, projectId?, useWorktree?}.
func (f *Front) threadHandoverCommand(w http.ResponseWriter, r *http.Request, commandID string, args map[string]any) {
	actor, origin := principalOf(r), originOf(r)
	f.frontCommand(w, commandID, func() (int, api.Reply) {
		strArg := func(k string) string { s, _ := args[k].(string); return strings.TrimSpace(s) }
		fromHost := strArg("fromHost")
		toHost := strArg("toHost")
		threadID := strArg("threadId")
		profile := strArg("profile")
		model := strArg("model")
		cwd := strArg("cwd")
		projectID := strArg("projectId")
		useWorktree, _ := args["useWorktree"].(bool)
		var effort *string
		if _, has := args["effort"]; has {
			e := strArg("effort")
			effort = &e
		}
		if fromHost == "" || toHost == "" || threadID == "" || profile == "" {
			return 400, api.Reply{Error: "fromHost, toHost, threadId and profile are required", Status: 400}
		}
		if fromHost == toHost {
			return 400, api.Reply{Error: "fromHost and toHost must be different hosts", Status: 400}
		}
		// Strip host ref prefix if the caller passed a ref.
		threadID = strings.TrimPrefix(threadID, fromHost+"/")
		projectID = strings.TrimPrefix(projectID, toHost+"/")

		f.mu.Lock()
		from := f.byIDLocked(fromHost)
		to := f.byIDLocked(toHost)
		var fromName, toName string
		var fromOn, toOn bool
		if from != nil {
			fromName, fromOn = from.name(), from.online
		}
		if to != nil {
			toName, toOn = to.name(), to.online
		}
		f.mu.Unlock()
		switch {
		case from == nil:
			return 404, api.Reply{Error: "this front follows no host " + fromHost, Status: 404}
		case to == nil:
			return 404, api.Reply{Error: "this front follows no host " + toHost, Status: 404}
		case !fromOn:
			return 409, api.Reply{Error: fromName + " is offline", Status: 409}
		case !toOn:
			return 409, api.Reply{Error: toName + " is offline", Status: 409}
		}

		ctx := context.Background()
		exportPath := "/api/threads/" + url.PathEscape(threadID) + "/handover/export"

		// Step 1: export bundle from source host.
		var exp handoverExportRes
		code, err := f.postHostJSON(ctx, from, actor, origin, exportPath, map[string]any{}, 45*time.Second, &exp)
		if err != nil {
			return 502, api.Reply{Error: fmt.Sprintf("%s: %v", fromName, err), Status: 502}
		}
		if code == 404 || code == 501 {
			msg := exp.Error
			if msg == "" {
				msg = fromName + " needs an update of Fyr for cross-host handover"
			}
			return code, api.Reply{Error: msg, Status: code}
		}
		if code != 200 || exp.Thread == nil {
			msg := exp.Error
			if msg == "" {
				msg = fmt.Sprintf("%s answered HTTP %d", fromName, code)
			}
			return code, api.Reply{Error: msg, Status: code}
		}

		buildImport := func(brief string) map[string]any {
			m := map[string]any{
				"fromHostId":   fromHost,
				"fromHostName": fromName,
				"fromThreadId": threadID,
				"fromProfile":  exp.Thread["profile"],
				"fromCwd":      exp.Thread["cwd"],
				"fromCli":      exp.CLI,
				"title":        exp.Thread["title"],
				"titleBy":      exp.Thread["titleBy"],
				"cwd":          cwd,
				"projectId":    projectID,
				"useWorktree":  useWorktree,
				"profile":      profile,
				"model":        model,
				"sessionId":    exp.Thread["sessionId"],
				"sessionNote":  exp.SessionNote,
				"conversation": exp.Conversation,
				"openChanges":  exp.OpenChanges,
				"summary":      exp.Thread["summary"],
				"liveView":     exp.Thread["liveView"],
				"pr":           exp.Thread["pr"],
				"events":       exp.Thread["events"],
			}
			if effort != nil {
				m["effort"] = *effort
			}
			if len(exp.SessionArchive) > 0 && string(exp.SessionArchive) != "null" {
				m["sessionArchive"] = exp.SessionArchive
			}
			if len(exp.Git) > 0 && string(exp.Git) != "null" {
				m["git"] = exp.Git
			}
			if brief != "" {
				m["brief"] = brief
			}
			return m
		}

		// Step 2: import on target host (rungs 1 & 2, falling back to rung 3 Brief if requested).
		var imp handoverImportRes
		code, err = f.postHostJSON(ctx, to, actor, origin, "/api/threads/handover/import", buildImport(""), 90*time.Second, &imp)
		if err != nil {
			return 502, api.Reply{Error: fmt.Sprintf("%s: %v", toName, err), Status: 502}
		}
		if code == 404 || code == 501 {
			msg := imp.Error
			if msg == "" {
				msg = toName + " needs an update of Fyr for cross-host handover"
			}
			return code, api.Reply{Error: msg, Status: code}
		}
		if code == 200 && imp.NeedBrief {
			// Rung 3: ask source host to produce a handover Brief, then retry import.
			var briefRes handoverExportRes
			bcode, berr := f.postHostJSON(ctx, from, actor, origin, exportPath, map[string]any{
				"wantBrief":     true,
				"targetHost":    toName,
				"targetProfile": profile,
			}, 11*time.Minute, &briefRes)
			if berr != nil {
				return 502, api.Reply{Error: fmt.Sprintf("%s: %v", fromName, berr), Status: 502}
			}
			if bcode != 200 || strings.TrimSpace(briefRes.Brief) == "" {
				msg := briefRes.Error
				if msg == "" {
					msg = fmt.Sprintf("%s could not write a handover brief (HTTP %d)", fromName, bcode)
				}
				return 502, api.Reply{Error: msg, Status: 502}
			}
			imp = handoverImportRes{}
			code, err = f.postHostJSON(ctx, to, actor, origin, "/api/threads/handover/import", buildImport(briefRes.Brief), 90*time.Second, &imp)
			if err != nil {
				return 502, api.Reply{Error: fmt.Sprintf("%s: %v", toName, err), Status: 502}
			}
		}
		if code != 200 || !imp.OK || imp.ThreadID == "" {
			msg := imp.Error
			if msg == "" {
				msg = fmt.Sprintf("%s answered HTTP %d", toName, code)
			}
			return code, api.Reply{Error: msg, Status: code}
		}

		// Step 3: settle source thread and record the link forward.
		srcCmd := map[string]any{
			"commandId": commandID + ":source",
			"action":    core.KindThreadHandedOver,
			"threadId":  threadID,
			"args": map[string]any{
				"fromHostId":   fromHost,
				"fromHostName": fromName,
				"toHostId":     toHost,
				"toHostName":   toName,
				"toThreadId":   imp.ThreadID,
				"toProfile":    profile,
				"toModel":      model,
				"toCwd":        imp.Cwd,
				"rung":         imp.Rung,
				"note":         imp.Note,
			},
		}
		var srcRep struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		scode, serr := f.postHostJSON(ctx, from, actor, origin, "/api/command", srcCmd, 15*time.Second, &srcRep)
		sourceWarning := ""
		if serr != nil {
			sourceWarning = fmt.Sprintf("could not settle source on %s: %v", fromName, serr)
		} else if scode != 200 || !srcRep.OK {
			sourceWarning = fmt.Sprintf("could not settle source on %s: %s", fromName, srcRep.Error)
		}

		res := map[string]any{
			"host":     toHost,
			"threadId": toHost + "/" + imp.ThreadID,
			"rid":      imp.ThreadID,
			"rung":     imp.Rung,
			"cwd":      imp.Cwd,
		}
		if imp.Note != "" {
			res["note"] = imp.Note
		}
		if sourceWarning != "" {
			res["sourceWarning"] = sourceWarning
		}
		return 200, api.Reply{OK: true, ThreadID: toHost + "/" + imp.ThreadID, Status: 200, Result: res}
	})
}
