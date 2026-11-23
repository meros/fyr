package engine

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// Command applies one owner command exactly once per CommandID (I7): it
// builds the owner's Input and submits it.
func (e *Engine) Command(c api.Cmd) api.Reply { return e.CommandAs(c, "", "") }

// CommandAs is Command with who acted (decision 0030 3.3): actor is
// "device:<id>", "local:owner" or "install:<id>", front the install whose front
// forwarded it. The command's event records both.
func (e *Engine) CommandAs(c api.Cmd, actor, front string) api.Reply {
	if c.CommandID == "" {
		return api.Reply{Error: "commandId is required", Status: 400}
	}
	return e.submit(core.Input{Source: core.FromOwner, Kind: c.Action, Thread: c.ThreadID, Args: c.Args, CmdID: c.CommandID, Actor: actor, Front: front})
}

// submit sends a command's Input down the one path: the owner's commands
// and the agent's thread_send and thread_new. It checks what only the host
// can know first (a project, a profile, an attachment, a folder on disk),
// makes the record of a kind that makes one, and takes the path of the
// kind's record: a thread, a project, or the host for a kind whose reply
// waits for its effects.
func (e *Engine) submit(in core.Input) api.Reply {
	k := core.Kinds[in.Kind]
	if k == nil || !slices.Contains(k.From, in.Source.Principal()) {
		return api.Reply{Error: "unknown action " + in.Kind, Status: 400}
	}
	var a core.Args
	switch x := in.Args.(type) {
	case core.Args:
		a = x
	case map[string]any:
		a = x
	}
	if a == nil {
		a = core.Args{}
	}
	in.Args = a
	if kind, ok := recordKinds[in.Kind]; ok {
		// V0.98 (4) Done and V0.101 Abandon on a host record.
		if r, ok := e.recordOwner(in, a, kind); ok {
			return r
		}
	}
	if k.Await {
		return e.await(in, k)
	}
	if in.Kind == core.KindHostSetupRun { // V0.208: the thread first
		return e.startHostSetup(in.CmdID)
	}
	if in.Kind == core.KindProjectSetupRun { // V0.116: the thread first
		return e.startSetup(a.Str("id"), in.Source, in.CmdID, true) // V0.linked-values-sync: the owner asked
	}
	if in.Kind == core.KindProjectSharedImage { // V0.linked-values-sync: the picture first
		if r, ok := e.storeSharedImage(a); !ok {
			return r
		}
	}
	if k.On == core.RecProject {
		r := e.applyProject(in)
		if r.OK {
			e.afterProject(in, r)
		}
		return r
	}
	if in.Kind == "thread.setProject" && a.Str("projectId") != "" {
		e.mu.Lock()
		ok := e.projects[a.Str("projectId")] != nil
		e.mu.Unlock()
		if !ok {
			return api.Reply{Error: "no project " + a.Str("projectId"), Status: 400}
		}
	}
	if in.Kind == "thread.handover" && e.ProfileExists != nil && a.Str("profile") != "" && !e.ProfileExists(a.Str("profile")) {
		return api.Reply{Error: "no profile " + a.Str("profile"), Status: 400}
	}
	if in.Kind == core.KindWakeOnQuota {
		prov, win, at, err := e.resolveQuotaReset(context.Background(), in.Thread, a.Str("provider"), a.Str("window"))
		if err != nil {
			status := 409
			if errors.Is(err, core.ErrBadArgs) {
				status = 400
			}
			return api.Reply{Error: err.Error(), Status: status}
		}
		args := maps.Clone(a)
		args["provider"] = prov
		args["window"] = win
		args["at"] = at.UTC().Format(time.RFC3339)
		if args.Str("id") == "" {
			args["id"] = NewID("w_")
		}
		in.Args = args
	}
	if in.Kind == "thread.create" || in.Kind == "thread.send" {
		for _, id := range core.AttachmentIDs(a) {
			if e.AttachmentExists == nil || !e.AttachmentExists(id) {
				return api.Reply{Error: "unknown attachment " + id, Status: 400}
			}
		}
	}
	var fresh *core.Thread
	var scratch string
	if in.Kind == "thread.create" {
		if r, done := e.replay(in.CmdID); done {
			return r
		}
		now := e.Now()
		fresh = &core.Thread{ID: NewID("t_"), Owner: core.Owner, Visibility: "private", CreatedAt: now}
		// V0.42: thread.create files into projectId; without a cwd it starts
		// in the project's folder (V0.55), else in a fresh scratch folder.
		args := maps.Clone(a)
		if pid := args.Str("projectId"); pid != "" {
			e.mu.Lock()
			pr := e.projects[pid]
			e.mu.Unlock()
			if pr == nil {
				return api.Reply{Error: "no project " + pid, Status: 400}
			}
			if strings.TrimSpace(args.Str("cwd")) == "" && pr.Folder != "" {
				args["cwd"] = pr.Folder
			}
		}
		if strings.TrimSpace(args.Str("cwd")) == "" {
			dir, err := e.makeScratch(args.Str("text"), now)
			if err != nil {
				return api.Reply{Error: "could not make a scratch folder: " + err.Error(), Status: 500}
			}
			scratch = dir
			args["cwd"] = dir
		}
		in.Args = args
	}
	p, err := e.apply(in, fresh)
	if scratch != "" && (err != nil || p.replayed) {
		os.Remove(scratch)
	}
	if err != nil {
		return rejected(err)
	}
	e.publish(p)
	return p.reply
}

// await is the path of a kind whose reply waits for its effects
// (core.InputKind.Await): the reduce, then its effects in order, then the
// receipt with the outcome. press makes the receipt check, the work and the
// receipt one step. A failure that may pass on a retry (409, 5xx) stores no
// receipt (A4).
func (e *Engine) await(in core.Input, k *core.InputKind) api.Reply {
	e.press.Lock()
	defer e.press.Unlock()
	if r, done := e.replay(in.CmdID); done {
		return r
	}
	id := in.CmdID
	in.CmdID = "" // the receipt waits for the outcome
	var effs []core.Effect
	var reply api.Reply
	if k.On == core.RecHost {
		res, err := core.ReduceHost(in, e.Now())
		if err != nil {
			return rejected(err)
		}
		effs, reply = res.Effects, api.Reply{OK: true, Status: 200, Result: res.Reply}
	} else {
		p, err := e.apply(in, nil)
		if err != nil {
			return rejected(err)
		}
		effs, p.res.Effects = p.res.Effects, nil
		e.publish(p)
		reply = p.reply
	}
	for _, ef := range effs {
		if reply = e.awaited(in, ef, reply); !reply.OK {
			break
		}
	}
	if reply.Status != 409 && reply.Status < 500 {
		e.st.Write(func(tx *store.Tx) error { return tx.PutReceipt(id, reply, e.Now()) })
	}
	return reply
}

// rejected is a command's reply for an Input that did not apply.
func rejected(err error) api.Reply {
	var r *core.Rejection
	if !errors.As(err, &r) {
		return api.Reply{Error: err.Error(), Status: 500}
	}
	status := map[core.RejectCode]int{core.RejectBadArgs: 400, core.RejectNotFound: 404, core.RejectRights: 403}[r.Code]
	if status == 0 {
		status = 409 // not now; a transient rejection gets no receipt (A4)
	}
	return api.Reply{Error: r.Reason, Status: status}
}

// replay returns the stored reply of a command id that already ran (I7),
// as a 200, and done; a store error is a 500, also done. Call it under the
// lock that also covers writing the command's receipt, so a command sent
// twice at once runs once.
func (e *Engine) replay(cmdID string) (api.Reply, bool) {
	prev, err := e.st.Receipt(cmdID)
	if err != nil {
		return api.Reply{Error: err.Error(), Status: 500}, true
	}
	if prev == nil {
		return api.Reply{}, false
	}
	var r api.Reply
	json.Unmarshal(prev, &r)
	r.Status = 200
	return r, true
}
