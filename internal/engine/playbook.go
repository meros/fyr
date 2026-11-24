package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/mcp"
)

// The project playbook on the host (V0.project-playbook): the tool an agent
// writes it with, the lines an agent's prompt gets from it, the run that
// learns it from the repository, and the probes Fyr runs after a merge.

// ProjectPrompt is what the prompt of an agent in thread id says about its
// project: when to write the playbook, and the playbook. "" for a thread
// without a project.
func (e *Engine) ProjectPrompt(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[id]
	if t == nil || t.ProjectID == "" || e.projects[t.ProjectID] == nil {
		return ""
	}
	out := core.PlaybookLearnPrompt
	if pb := core.PlaybookPrompt(e.projects[t.ProjectID]); pb != "" {
		out += "\n- " + pb
	}
	return out
}

// playbookBrief is the first message of the run that learns a project's
// playbook from its repository.
func playbookBrief(p core.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fyr playbook run for the project %q (id %s), folder %s.\n\n", p.Name, p.ID, p.Folder)
	b.WriteString("Find out what this project does around and after a pull request, and record it with the tool project_learn {slot, name?, how, url?, probe?, live?, evidence}. ")
	b.WriteString("Do not ask the owner anything, change no file, deploy nothing, and finish in a few minutes. Record only what you can back with evidence (a file and what it says, a workflow, what the last merges did); leave a slot empty when the repository does not say. A library with no deploy has no stages: that is an answer.\n\n")
	b.WriteString("1. Read CLAUDE.md, AGENTS.md, the README, the CI and deploy workflows (.github/workflows, a Makefile, package.json scripts, deploy scripts, infrastructure folders), and what followed the last few merges to the default branch (gh run list, gh api repos/<owner>/<repo>/deployments, git tag).\n")
	b.WriteString("2. Stages (slot stage, name the environment: dev, staging, prod). For each environment a merged change reaches: how it gets there (automatic on merge and roughly how long, a command, a release tag, a schedule), its url, and live: true for the one that is production. ")
	b.WriteString("A stage GitHub Deployments already reports needs no probe: Fyr reads those itself. Otherwise give probe when a command can tell: a shell command where {sha} is the merge commit and exit 0 means the stage holds that commit, for example comparing a version endpoint with git (git merge-base --is-ancestor {sha} \"$(curl -fsS https://dev.example.com/version)\"). It must only read, end within 30 s and need no input. Fyr runs it after each merge once the owner allowed it. A stage a change reaches only by hand gets deployCmd: the command that puts the merge commit there ({sha}); Fyr shows it to the owner on a Run card and never runs it itself. Set ownerChecks only when the docs say the owner looks at each change on that stage.\n")
	b.WriteString("3. The other slots, one or two sentences each: verify (where and how a change is looked at, with which login, and whether the owner or the agent checks), promote (the manual step to the next stage), issue (where issues live and how one is filed), run (how the app is started locally to show it), rollback (how a deploy is undone).\n")
	b.WriteString("4. Do not copy general advice or what a reader of CLAUDE.md already knows unless Fyr needs it (a stage's probe and url).\n\n")
	b.WriteString("End with a short list of what you recorded, what you left empty and why, and name each probe that waits for the owner's yes (Settings → Projects → this project → Playbook).")
	return b.String()
}

// startPlaybookRun starts the run that learns project id's playbook: a
// thread in its folder, filed in it, as the owner's.
func (e *Engine) startPlaybookRun(id, cmdID string) api.Reply {
	e.mu.Lock()
	cur := e.projects[id]
	var p core.Project
	if cur != nil {
		p = *cur
	}
	e.mu.Unlock()
	if cur == nil {
		return api.Reply{Error: "no project " + id, Status: 404}
	}
	if p.Folder == "" {
		return api.Reply{Error: "a project needs a folder for Fyr to learn its playbook", Status: 400}
	}
	args := core.Args{"cwd": p.Folder, "projectId": id, "title": "Learn how " + p.Name + " works", "text": playbookBrief(p)}
	t := e.submit(core.Input{Source: core.FromOwner, Kind: "thread.create", Args: args, CmdID: cmdID + "/thread"})
	if !t.OK {
		return t
	}
	return api.Reply{OK: true, Status: 200, ThreadID: t.ThreadID, Result: map[string]any{"id": id, "thread": t.ThreadID}}
}

// playbookText is a project's playbook for a tool's answer.
func playbookText(p core.Project) string {
	if len(p.Playbook) == 0 {
		return fmt.Sprintf("The playbook of %q is empty.", p.Name)
	}
	var b strings.Builder
	b.WriteString(core.PlaybookPrompt(&p))
	for _, e := range p.Stages() {
		if e.Probe != "" {
			fmt.Fprintf(&b, "\n  Probe of %s: %s", e.Name, e.Probe)
		}
		if e.Deploy != "" {
			fmt.Fprintf(&b, "\n  Deploy command of %s: %s", e.Name, e.Deploy)
		}
	}
	return b.String()
}

func (e *Engine) playbookTools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "project_learn",
		Description: "Read or write the playbook of a Fyr project: what the project does around and after a pull request, kept for every later thread. Without slot it returns the playbook. " +
			"Slots: stage (an environment a merged change reaches; name it: dev, prod), verify (where and how a change is looked at, and who checks), promote (the manual step to the next stage), issue (where issues live and how one is filed), run (how the app is started locally), rollback (how a deploy is undone). " +
			"Write an entry when the owner tells you how the project works or corrects you, or when you had to work it out; how is a sentence or two, evidence what it rests on. " +
			"A stage may carry url (where to look at it), live: true (reaching it makes a change live) and probe: a shell command Fyr runs by itself after each merge, in the project's folder, where {sha} is the merge commit and exit 0 means the stage holds that commit. A probe only reads, ends within 30 s and needs no input. Fyr runs it only once the owner allowed it. " +
			"A stage may also carry deployCmd (the command that puts a change there by hand: Fyr shows it to the owner on a Run card when the stage before holds the change) and ownerChecks (Fyr asks the owner to look at each change there). " +
			"An entry the owner wrote is kept: what you write there is stored beside it as a suggestion the owner takes or dismisses. Pass ownerAsked when the owner's newest message asks for exactly this entry; it is then theirs, its probe allowed. A linked project's playbook is shared by all its hosts.",
		Schema: obj(map[string]any{
			"project":     str("The project's id or name; default this thread's project."),
			"slot":        map[string]any{"type": "string", "enum": core.PlaybookSlots, "description": "The slot to write. Leave it out to read the playbook."},
			"name":        str("A stage's environment in lower case: dev, staging, prod."),
			"how":         str("The entry in a sentence or two. For a stage: how a merged change gets there and roughly how long it takes."),
			"url":         str("A stage's address, where a change is looked at."),
			"probe":       str("A stage's check: a shell command with {sha}; exit 0 says the stage holds that commit."),
			"live":        map[string]any{"type": "boolean", "description": "A stage only: a change that reaches it is live (production)."},
			"deployCmd":   str("A stage reached by hand only: the shell command that puts a merged change there ({sha} is the merge commit). Fyr puts it on a Run card for the owner when the stage before holds the change."),
			"ownerChecks": map[string]any{"type": "boolean", "description": "A stage only: the owner looks at each change there. Fyr asks them on a question card when a change reaches it, and you hear the answer."},
			"evidence":    str("What the entry rests on: a file, a workflow, what the owner said."),
			"remove":      map[string]any{"type": "boolean", "description": "Remove the entry."},
			"ownerAsked":  map[string]any{"type": "boolean", "description": "The owner's newest message in this thread asks for exactly this entry. It becomes theirs and its probe is allowed. Refused when the newest message is not the owner's own."},
		}),
		Call: func(_ context.Context, self string, a map[string]any) (string, error) {
			th, inThread := e.Thread(self)
			key := sArg(a, "project")
			if key == "" && inThread {
				key = th.ProjectID
			}
			if key == "" {
				return "", errors.New("this thread has no project; name one with project")
			}
			p, ok := e.findProject(key)
			if !ok {
				return "", fmt.Errorf("no project %q on this host", key)
			}
			slot := sArg(a, "slot")
			if slot == "" {
				return playbookText(p), nil
			}
			asked, _ := a["ownerAsked"].(bool)
			if asked && (!inThread || th.LastAsker == "") {
				return "", errors.New("ownerAsked needs the newest message in this thread to be the owner's own; it is not, so ask the owner")
			}
			in := core.Input{Source: core.FromAgent, Kind: core.KindProjectLearn, CmdID: NewID("agent_")}
			if asked {
				in.Source, in.Actor = core.FromOwner, th.LastAsker
			}
			args := core.Args{"id": p.ID, "slot": slot, "thread": self}
			if v, _ := a["ownerChecks"].(bool); v {
				args["ownerChecks"] = true
			}
			for _, k := range []string{"name", "how", "url", "probe", "deployCmd", "evidence"} {
				if v := sArg(a, k); v != "" {
					args[k] = v
				}
			}
			if v, _ := a["live"].(bool); v {
				args["live"] = true
			}
			if v, _ := a["remove"].(bool); v {
				args["remove"] = true
			}
			in.Args = args
			r := e.applyProject(in)
			if !r.OK {
				return "", errors.New(r.Error)
			}
			res, _ := r.Result.(map[string]any)
			what := "the " + slot + " entry"
			if slot == core.SlotStage {
				what = "the " + strings.ToLower(sArg(a, "name")) + " stage"
			}
			if _, rm := args["remove"]; rm {
				if removed, _ := res["removed"].(bool); !removed {
					return fmt.Sprintf("The playbook of %q has no such entry.", p.Name), nil
				}
				return fmt.Sprintf("Removed %s from the playbook of %q.", what, p.Name), nil
			}
			if s, _ := res["suggested"].(bool); s {
				return fmt.Sprintf("The owner wrote %s in the playbook of %q, so it stands. What you found is kept beside it as a suggestion on the project's page, for the owner to take or dismiss; tell them in a sentence what differs.", what, p.Name), nil
			}
			out := fmt.Sprintf("Recorded %s in the playbook of %q.", what, p.Name)
			if needs, _ := res["needsAllow"].(bool); needs {
				if asked { // the owner asked for this entry: its probe is theirs
					k, _ := res["key"].(string)
					slot, name, _ := strings.Cut(k, ":")
					al := e.applyProject(core.Input{Source: core.FromOwner, Actor: th.LastAsker, Kind: core.KindProjectLearn, CmdID: NewID("agent_"),
						Args: core.Args{"id": p.ID, "slot": slot, "name": name, "allow": true}})
					if !al.OK {
						return "", errors.New(al.Error)
					}
					out += " Fyr runs its probe after each merge."
				} else {
					out += fmt.Sprintf(" Fyr does not run its probe until the owner allows it: tell them the command and that they allow it in Settings → Projects → %s → Playbook, or by saying so here (then call project_learn again with the same values and ownerAsked: true).", p.Name)
				}
			} else if sArg(a, "probe") != "" {
				out += " Fyr runs its probe after each merge."
			}
			if p.Link != "" {
				out += " This project is linked across hosts: its other hosts get the playbook when Fyr's front next hears from them."
			}
			return out, nil
		}})
}

// A stage probe (V0.project-playbook): after a merge Fyr runs the probes of
// the project's stages for the merge commit, by itself and without a model,
// until each stage holds the commit or the deploy watch ends.

const (
	probeTimeout = 30 * time.Second
	// ProbeEvery is how often the host looks for due probes; a watch older
	// than an hour is probed less often (github.DeployEvery).
	ProbeEvery = time.Minute
)

type probeJob struct {
	thread, change, sha, dir, project string
	since                             time.Time
	stage                             core.PlaybookEntry
}

// probes keeps when each probe last ran.
type probes struct {
	mu sync.Mutex
	at map[string]time.Time
	// watched are the probes of the last pass: one that is gone without its
	// stage reached is a merge the probe missed (V0.playbook-moments).
	watched map[string]probeJob
}

// dueProbes are the probes to run now: every stage Fyr may probe that a
// landed change of a thread has not reached, by the watch's age.
func (e *Engine) dueProbes(now time.Time) []probeJob {
	e.mu.Lock()
	var jobs []probeJob
	for _, t := range e.threads {
		if t.ArchivedAt != nil || t.ProjectID == "" {
			continue
		}
		p := e.projects[t.ProjectID]
		stages := p.ProbeStages()
		if len(stages) == 0 {
			continue
		}
		dir := p.Folder
		if dir == "" {
			dir = t.Cwd
		}
		for _, d := range core.ProbeWatches(t, now) {
			c := core.FindChange(t, d.Change)
			if c == nil || !core.ValidSHA(d.SHA) {
				continue
			}
			for _, st := range stages {
				if !core.StageReached(c, st) {
					jobs = append(jobs, probeJob{project: p.ID, thread: t.ID, change: c.ID, sha: d.SHA, dir: dir, since: d.Since, stage: st})
				}
			}
		}
	}
	e.mu.Unlock()
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].thread+jobs[i].change+jobs[i].stage.Name < jobs[j].thread+jobs[j].change+jobs[j].stage.Name
	})
	e.probes.mu.Lock()
	defer e.probes.mu.Unlock()
	if e.probes.at == nil {
		e.probes.at = map[string]time.Time{}
	}
	live := map[string]bool{}
	var due []probeJob
	for _, j := range jobs {
		key := j.change + "/" + j.stage.Name
		live[key] = true
		if last, ok := e.probes.at[key]; ok && now.Sub(last) < max(github.DeployEvery(now.Sub(j.since)), ProbeEvery/2) {
			continue
		}
		e.probes.at[key] = now
		due = append(due, j)
	}
	for k := range e.probes.at {
		if !live[k] {
			delete(e.probes.at, k)
		}
	}
	gone := e.probes.watched
	e.probes.watched = map[string]probeJob{}
	for _, j := range jobs {
		k := j.change + "/" + j.stage.Name
		e.probes.watched[k] = j
		delete(gone, k)
	}
	for _, j := range gone {
		go e.probeMissed(j, now)
	}
	return due
}

// probeMissed counts a merge the probe of j's stage did not see: its watch
// ended (the day passed) while the change is still landed and not on the
// stage. A change that ended, or a probe that no longer runs, counts
// nothing.
func (e *Engine) probeMissed(j probeJob, now time.Time) {
	e.mu.Lock()
	t := e.threads[j.thread]
	missed := false
	var pid string
	if t != nil && t.ArchivedAt == nil {
		pid = t.ProjectID
		c := core.FindChange(t, j.change)
		still := false
		for _, st := range e.projects[pid].ProbeStages() {
			still = still || (st.Name == j.stage.Name && st.Probe == j.stage.Probe)
		}
		watched := false
		for _, d := range core.ProbeWatches(t, now) {
			watched = watched || d.Change == j.change
		}
		missed = c != nil && still && !watched && c.Phase == core.PhaseLanded && !core.StageReached(c, j.stage)
	}
	e.mu.Unlock()
	if missed {
		e.applyProject(core.Input{Source: core.Observer(core.ObsDeploy), Kind: core.KindProjectProbed, CmdID: NewID("probe_"), Args: core.Args{"id": pid, "name": j.stage.Name, "ok": false}})
	}
}

// stageMoments opens the moments that are due (core.StageMoments): the
// owner's check of a stage a change reached, and the Run card of a stage's
// deploy command. It returns how many it opened.
func (e *Engine) stageMoments(now time.Time) int {
	type due struct {
		thread string
		a      core.StageMomentArgs
	}
	var ms []due
	e.mu.Lock()
	for _, t := range e.threads {
		if t.ArchivedAt != nil || t.ProjectID == "" {
			continue
		}
		p := e.projects[t.ProjectID]
		stages := p.Stages()
		if !slices.ContainsFunc(stages, func(s core.PlaybookEntry) bool { return s.OwnerChecks || s.Deploy != "" }) {
			continue
		}
		dir := p.Folder
		if dir == "" {
			dir = t.Cwd
		}
		for i := range t.Changes {
			for _, a := range core.StageMoments(t, &t.Changes[i], stages, now) {
				a.Cwd = dir
				ms = append(ms, due{t.ID, a})
			}
		}
	}
	e.mu.Unlock()
	sort.Slice(ms, func(i, j int) bool {
		return ms[i].thread+ms[i].a.Change+ms[i].a.Stage < ms[j].thread+ms[j].a.Change+ms[j].a.Stage
	})
	for _, m := range ms {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsDeploy), Kind: core.KindStageMoment, Thread: m.thread, Args: m.a}); err != nil {
			log.Printf("stage moment %s of %s: %v", m.a.Stage, m.a.Change, err)
		}
	}
	return len(ms)
}

// runProbe runs one probe: true when the stage holds the commit (exit 0).
func (e *Engine) runProbe(ctx context.Context, j probeJob) bool {
	script := core.ProbeScript(j.stage.Probe, j.sha)
	if script == "" {
		return false
	}
	run := e.RunProbe
	if run == nil {
		run = func(ctx context.Context, dir, script string) (int, error) {
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				return -1, fmt.Errorf("no folder %s", dir)
			}
			argv := []string{"sh", "-c", script}
			if e.FolderEnv != nil { // the folder's environment, as a Run card has
				argv = e.FolderEnv(dir, argv)
			}
			_, exit, err := runScriptLive(ctx, dir, argv, nil)
			return exit, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	exit, err := run(ctx, j.dir, script)
	if exit < 0 && err != nil {
		log.Printf("probe %s of %s: %v", j.stage.Name, j.change, err)
	}
	return exit == 0 && ctx.Err() == nil
}

// ProbeOnce runs the probes that are due and writes each stage that holds
// its commit into the change, as an observation of the deploy observer. It
// returns how many probes ran.
func (e *Engine) ProbeOnce(ctx context.Context) int {
	due := e.dueProbes(e.Now())
	type key struct{ thread, change string }
	reached := map[key][]core.Deployment{}
	for _, j := range due {
		if ctx.Err() != nil {
			break
		}
		if e.runProbe(ctx, j) {
			if j.stage.Missed > 0 { // the probe works again
				e.applyProject(core.Input{Source: core.Observer(core.ObsDeploy), Kind: core.KindProjectProbed, CmdID: NewID("probe_"), Args: core.Args{"id": j.project, "name": j.stage.Name, "ok": true}})
			}
			k := key{j.thread, j.change}
			reached[k] = append(reached[k], core.Deployment{Env: j.stage.Name, Production: j.stage.IsLive(), State: "success", URL: j.stage.URL})
		}
	}
	for k, ds := range reached {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsDeploy), Kind: core.KindDeployObserved, Thread: k.thread,
			Args: core.DeployObservedArgs{Change: k.change, Obs: core.DeployObs{Probe: true, Deployments: ds}, Probes: true}}); err != nil {
			log.Printf("probe: change %s: %v", k.change, err)
		}
	}
	e.stageMoments(e.Now())
	return len(due)
}

// ProbeLoop runs ProbeOnce every ProbeEvery until ctx ends.
func (e *Engine) ProbeLoop(ctx context.Context) {
	t := time.NewTicker(ProbeEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.ProbeOnce(ctx)
		}
	}
}

// hasProbes reports that the project of thread id has a stage Fyr probes.
func (e *Engine) hasProbes(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[id]
	return t != nil && t.ProjectID != "" && len(e.projects[t.ProjectID].ProbeStages()) > 0
}
