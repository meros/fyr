package main

import (
	"context"
	"io/fs"
	"net"
	"sync/atomic"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/setup"
	"github.com/meros/fyr/internal/slots"
)

// wiring holds what host.serve builds, so each wire* step reads what the steps before it made.
// The steps run in this order, each in its own file (see main.go):
//
//	wireQuota, wireAgents, wireProjects (wire_agents.go), wireHeavy (wire_heavy.go),
//	wireGuard (wire_guard.go), wireDeploy, wireBackground (wire_deploy.go), wireListeners (wire_listen.go).
//
// cfg, h, webFS, hostName, data, tree, rn, eng, reg, ln and att are made before the first step; ctx and stop
// after wireGuard and before wireDeploy. Every step's comment lists what it needs from the steps before it and
// what it makes: a field is written by one step only, and a step that reads one earlier in the order than its
// maker would see nil.
type wiring struct {
	cfg          config
	h            *host
	webFS        fs.FS
	hostName     string
	data         string
	tree         *cgroup.Tree
	rn           *kept
	att          *attach.Store
	book         *heavy.Book
	ctx          context.Context
	dep          *deploy.Deployer
	detector     *cgroup.Detector
	eng          *engine.Engine
	fe           *runningFront
	gh           github.Runner
	ghBudget     *github.Budget
	ghMeter      *github.Meter
	ghView       func() github.MeterView
	groups       *cgroup.JobGroups
	stops        *cgroup.ProcStops // V0.guard-finish: nil with the cgroup tree
	guardService func(th string) bool
	health       []deploy.Check
	heavyCmds    *heavy.Matcher
	jobs         *cgroup.Jobs
	ln           net.Listener
	mcpSrv       *mcp.Server
	mgr          *agent.Manager
	n            int
	old          *oldListeners
	peerEnvPtr   atomic.Pointer[peer.Env]
	pollGh       github.Runner
	pool         *slots.Pool
	prDetails    *github.Details
	quotas       *quota.Service
	reg          *provider.Registry
	rerun        *cgroup.RerunClasses
	sampler      *cgroup.Sampler
	services     *cgroup.Services
	setupChecker *setup.Checker
	srvs         *servers
	stop         context.CancelFunc
	svcBook      *heavy.Services
	usettings    *cgroup.UndeclaredSettings
}
