package engine

import (
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// awaited runs one effect of an Await kind and gives the reply its outcome.
func (e *Engine) awaited(in core.Input, ef core.Effect, reply api.Reply) api.Reply {
	fail := func(status int, msg string) api.Reply { return api.Reply{Error: msg, Status: status} }
	switch ef.Kind {
	case core.EffectCancelMessage:
		if e.Agents == nil {
			return fail(409, "No agent runs this thread")
		}
		if err := e.Agents.Do(in.Thread, ef); err != nil {
			return fail(409, err.Error())
		}
	case core.EffectDeployNow:
		if e.DeployNow == nil {
			return fail(409, "This host runs no deploy")
		}
		if err := e.DeployNow(); err != nil {
			return fail(409, err.Error())
		}
	case core.EffectUpdateNow:
		if e.UpdateNow == nil {
			return fail(409, "This host has no release source")
		}
		if err := e.UpdateNow(); err != nil {
			return fail(409, err.Error())
		}
	case core.EffectHostSetupFix:
		if e.SetupFix == nil {
			return fail(409, "This host does not run setup fixes")
		}
		action, _ := ef.Request.Args["action"].(string)
		res, err := e.SetupFix(action, ef.Request.Args)
		if err != nil {
			return fail(400, err.Error())
		}
		reply.Result = res
	case core.EffectInvariantFix: // V0.load-invariants: the owner's Fix it
		id, _ := ef.Request.Args["id"].(string)
		r := e.invariantFix(id)
		if !r.OK {
			return r
		}
		reply.ThreadID, reply.Result = r.ThreadID, r.Result
	case core.EffectInvariantDismiss:
		id, _ := ef.Request.Args["id"].(string)
		if err := e.invariantDismiss(id); err != nil {
			return fail(404, err.Error())
		}
		reply.Result = map[string]any{"id": id}
	case core.EffectAutoMergeSet: // V0.102: the owner's setting
		repo, _ := ef.Request.Args["repo"].(string)
		on, _ := ef.Request.Args["on"].(bool)
		if err := e.setAutoMerge(repo, on); err != nil {
			return fail(500, err.Error())
		}
		reply.Result = e.AutoMergeRepos()
	case core.EffectAutoDeploySet: // V0.111: the owner's setting
		on, _ := ef.Request.Args["on"].(bool)
		if err := e.setAutoDeploy(on); err != nil {
			return fail(500, err.Error())
		}
		reply.Result = e.AutoDeploy()
	case core.EffectHostOrderSet: // V0.132: the owner's host order, newest wins
		var o HostOrder
		for _, x := range ef.Request.Args["names"].([]any) {
			o.Names = append(o.Names, x.(string))
		}
		if ids, ok := ef.Request.Args["ids"].([]any); ok { // V0.host-ids
			for _, x := range ids {
				o.IDs = append(o.IDs, x.(string))
			}
		}
		o.At, _ = time.Parse(time.RFC3339Nano, ef.Request.Args["at"].(string))
		kept, err := e.setHostOrder(o)
		if err != nil {
			return fail(500, err.Error())
		}
		reply.Result = kept
	case core.EffectHostMarksSet: // V0.171: the owner's host marks, newest wins
		kept, err := e.setHostMarks(hostMarksOf(ef.Request.Args))
		if err != nil {
			return fail(500, err.Error())
		}
		reply.Result = kept
	case core.EffectJobReset: // decision 0029 S3: the owner's Reset of a learned class
		c, _ := ef.Request.Args["class"].(string)
		if e.JobReset == nil || !e.JobReset(c) {
			return fail(404, "No learned job class "+c)
		}
		reply.Result = map[string]any{"class": c}
	case core.EffectLeftoverEnd: // V0.176
		id, _ := ef.Request.Args["id"].(string)
		if e.LeftoverEnd == nil {
			return fail(409, "This host does not track leftover processes")
		}
		if err := e.LeftoverEnd(id); err != nil {
			return fail(404, err.Error())
		}
		reply.Result = map[string]any{"id": id}
	case core.EffectLeftoverKeep:
		id, _ := ef.Request.Args["id"].(string)
		h, _ := ef.Request.Args["hours"].(float64)
		if e.LeftoverKeep == nil {
			return fail(409, "This host does not track leftover processes")
		}
		if err := e.LeftoverKeep(id, time.Duration(h*float64(time.Hour))); err != nil {
			return fail(404, err.Error())
		}
		reply.Result = map[string]any{"id": id, "hours": h}
	case core.EffectTempRemove:
		p, _ := ef.Request.Args["path"].(string)
		if e.TempRemove == nil {
			return fail(409, "This host lists no temporary folders")
		}
		if err := e.TempRemove(p); err != nil {
			return fail(409, err.Error())
		}
		reply.Result = map[string]any{"path": p}
	case core.EffectHeavyRemove:
		p, _ := ef.Request.Args["pattern"].(string)
		if e.HeavyRemove == nil || !e.HeavyRemove(p) {
			return fail(404, "No such heavy command")
		}
		reply.Result = map[string]any{"pattern": p}
	case core.EffectServicePatterns: // V0.services-not-jobs
		if e.ServicePatterns == nil {
			return fail(409, "This host keeps no service patterns")
		}
		add, _ := ef.Request.Args["add"].(string)
		remove, _ := ef.Request.Args["remove"].(string)
		if err := e.ServicePatterns(add, remove); err != nil {
			return fail(400, err.Error())
		}
		reply.Result = ef.Request.Args
	case core.EffectUndeclaredSettings:
		if e.UndeclaredSettings == nil {
			return fail(409, "This host cannot change these settings")
		}
		if err := e.UndeclaredSettings(ef.Request.Args); err != nil {
			return fail(400, err.Error())
		}
		reply.Result = ef.Request.Args
	case core.EffectRerunClasses: // V0.rerun-classes
		if e.RerunSet == nil {
			return fail(409, "This host cannot kill and rerun jobs")
		}
		if err := e.RerunSet(ef.Request.Args); err != nil {
			return fail(400, err.Error())
		}
		reply.Result = ef.Request.Args
	case core.EffectSlotOrderSet: // V0.137: the owner's order of the slot queue
		if e.Slots == nil || e.Slots.Off() {
			return fail(409, "This host has no slots for heavy jobs")
		}
		var owners []string
		for _, x := range ef.Request.Args["owners"].([]any) {
			owners = append(owners, x.(string))
		}
		reply.Result = e.Slots.Reorder(owners)
	case core.EffectSystemAgentsSet: // V0.115: the owner's setting
		a := ef.Request.Args
		job, _ := a["job"].(string)
		clr, _ := a["clear"].(bool)
		p, _ := a["profile"].(string)
		m, _ := a["model"].(string)
		if err := e.setSystemAgents(job, core.SystemAgentChoice{Profile: p, Model: m}, clr); err != nil {
			return fail(400, err.Error())
		}
		reply.Result = e.SystemAgentsView()
	case core.EffectProvider:
		if e.Providers == nil {
			return fail(404, "no providers on this host")
		}
		r := e.Providers.Run(api.Cmd{Action: ef.Request.Action, Args: core.Args(ef.Request.Args)})
		if r.Status == 0 {
			r.Status = 200
		}
		return r
	}
	return reply
}
