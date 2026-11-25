package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The project playbook (V0.project-playbook): what a project does around and
// after a pull request, in a fixed set of slots. Agents learn it
// (project.learn through the tool project_learn), the owner corrects it, and
// Fyr acts on the part it can check: a stage's probe is a shell command Fyr
// runs itself after a merge, so that no model is asked where a change is.

// The playbook's slots. A stage has a name (the environment), and so does a
// lesson; every other slot holds one entry.
const (
	SlotStage    = "stage"    // an environment a merged change reaches
	SlotVerify   = "verify"   // where and how a change is looked at, and who checks
	SlotPromote  = "promote"  // the manual step to the next stage
	SlotIssue    = "issue"    // where issues live and how one is filed
	SlotRun      = "run"      // how the app is started locally to show it
	SlotRollback = "rollback" // how a deploy is undone
	// SlotLesson is what an agent here should do differently, learned from an
	// earlier thread (V0.playbook-lessons). An agent only suggests one: it
	// reaches agents once the owner took it.
	SlotLesson = "lesson"
)

// PlaybookSlots are the slots in the order they are shown and said.
var PlaybookSlots = []string{SlotStage, SlotVerify, SlotPromote, SlotIssue, SlotRun, SlotRollback, SlotLesson}

// namedSlot reports a slot that holds several entries, each with a name.
func namedSlot(slot string) bool { return slot == SlotStage || slot == SlotLesson }

// The playbook's limits: it stays a handful of short entries.
const (
	MaxStages       = 6
	MaxLessons      = 12
	maxPlaybookHow  = 600
	maxPlaybookCmd  = 500
	maxPlaybookURL  = 300
	maxPlaybookNote = 200
)

// PlaybookEntry is one slot's entry, or one stage.
type PlaybookEntry struct {
	Slot string `json:"slot"`
	// Name is a stage's environment ("dev", "prod"); "" in every other slot.
	Name string `json:"name,omitempty"`
	// How is the entry in a sentence or two, for the agent and the owner.
	How string `json:"how,omitempty"`
	// URL is where a stage is looked at.
	URL string `json:"url,omitempty"`
	// Probe is a stage's shell command: {sha} is the merge commit, and exit 0
	// says the stage holds it. Fyr runs it only once the owner allowed it.
	Probe   string `json:"probe,omitempty"`
	ProbeOK bool   `json:"probeOk,omitempty"`
	// Live: a change that reaches this stage is live. A stage named prod or
	// production is live without it.
	Live bool `json:"live,omitempty"`
	// OwnerChecks: when a merged change reaches this stage, Fyr asks the
	// owner on a question card whether it looks right there
	// (V0.playbook-moments).
	OwnerChecks bool `json:"ownerChecks,omitempty"`
	// Deploy is the shell command that puts a merged change on this stage by
	// hand ({sha} is the merge commit): Fyr puts it on a Run card when the
	// stage before it holds the change. The owner's press runs it.
	Deploy string `json:"deployCmd,omitempty"`
	// Missed counts the merges in a row the probe did not see here within
	// the watch's day; the next one it sees clears it.
	Missed int `json:"missed,omitempty"`
	// Last is what the probe last said on this host, written when its answer
	// changes (V0.playbook-probe-health). Like Missed it is this host's.
	Last *ProbeResult `json:"last,omitempty"`
	// Suggest is what an agent found that differs from the owner's entry:
	// the owner takes it or dismisses it.
	Suggest *PlaybookSuggestion `json:"suggest,omitempty"`
	// By is who wrote it, ByOwner or ByAgent; Thread is where it was learned
	// and Evidence what it rests on (a file, what the owner said).
	By       string    `json:"by"`
	Thread   string    `json:"thread,omitempty"`
	Evidence string    `json:"evidence,omitempty"`
	At       time.Time `json:"at"`
}

// PlaybookSuggestion is an agent's finding for an entry the owner wrote.
type PlaybookSuggestion struct {
	How         string    `json:"how,omitempty"`
	URL         string    `json:"url,omitempty"`
	Probe       string    `json:"probe,omitempty"`
	Deploy      string    `json:"deployCmd,omitempty"`
	Live        bool      `json:"live,omitempty"`
	OwnerChecks bool      `json:"ownerChecks,omitempty"`
	Evidence    string    `json:"evidence,omitempty"`
	Thread      string    `json:"thread,omitempty"`
	At          time.Time `json:"at"`
}

// A probe's answer.
const (
	ProbeReached = "reached" // exit 0: the stage holds the commit
	ProbeWaiting = "waiting" // another exit: not there yet
	ProbeTimeout = "timeout" // stopped at the time limit
	ProbeError   = "error"   // it could not run: no folder, no such command
)

// ProbeResult is the last answer of a stage's probe: State, the exit code,
// the last line it printed, and since when it has answered so.
type ProbeResult struct {
	State string    `json:"state"`
	Exit  int       `json:"exit,omitempty"`
	Note  string    `json:"note,omitempty"`
	At    time.Time `json:"at"`
}

// ProbeState reads one run of a probe: a run stopped at the limit is a
// timeout, an exit a shell gives a command it cannot run (126, 127) or a run
// that never started is an error, exit 0 is reached, and any other exit says
// the commit is not there yet.
func ProbeState(exit int, timedOut bool) string {
	switch {
	case timedOut:
		return ProbeTimeout
	case exit == 0:
		return ProbeReached
	case exit < 0 || exit == 126 || exit == 127:
		return ProbeError
	}
	return ProbeWaiting
}

// Broken reports a stage whose probe Fyr runs and that cannot answer: it
// timed out or could not run the last time. No change reaches the stage
// while it is so.
func (e PlaybookEntry) Broken() bool {
	return e.Runs() && e.Last != nil && (e.Last.State == ProbeTimeout || e.Last.State == ProbeError)
}

// DoubtAfter is how many merges in a row a probe may miss before the entry
// is doubted.
const DoubtAfter = 2

// Doubted reports a stage whose probe did not see the last merges.
func (e PlaybookEntry) Doubted() bool { return e.Missed >= DoubtAfter }

// Key names the entry in its playbook: the slot, or stage:<name> and
// lesson:<name>.
func (e PlaybookEntry) Key() string { return PlaybookKey(e.Slot, e.Name) }

// PlaybookKey is the key of slot's entry; a stage's and a lesson's have
// their name.
func PlaybookKey(slot, name string) string {
	if namedSlot(slot) {
		return slot + ":" + name
	}
	return slot
}

// IsLive reports a stage that makes a change live.
func (e PlaybookEntry) IsLive() bool {
	return e.Slot == SlotStage && (e.Live || IsProduction(e.Name, false))
}

// Runs reports a stage whose probe Fyr runs: it has one and the owner
// allowed it.
func (e PlaybookEntry) Runs() bool { return e.Slot == SlotStage && e.Probe != "" && e.ProbeOK }

// Stages are the playbook's stages in the order a change reaches them: the
// order they were learned in, the live ones last.
func (p *Project) Stages() []PlaybookEntry {
	if p == nil {
		return nil
	}
	var out []PlaybookEntry
	for _, live := range []bool{false, true} {
		for _, e := range p.Playbook {
			if e.Slot == SlotStage && e.IsLive() == live {
				out = append(out, e)
			}
		}
	}
	return out
}

// ProbeStages are the stages whose probe Fyr runs.
func (p *Project) ProbeStages() []PlaybookEntry {
	var out []PlaybookEntry
	for _, e := range p.Stages() {
		if e.Runs() {
			out = append(out, e)
		}
	}
	return out
}

var (
	stageName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,23}$`)
	commitSHA = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// StageReached reports that change c holds stage e: its deploy condition is
// ok (the production one for a live stage).
func StageReached(c *Change, e PlaybookEntry) bool {
	typ := CondDeployTo(e.Name)
	if e.IsLive() {
		typ = CondDeploy
	}
	k := condition(c, typ)
	return k != nil && k.Status == CondOK
}

// ValidSHA reports a commit id that may stand in a probe's command.
func ValidSHA(s string) bool { return commitSHA.MatchString(s) }

// ProbeScript is a stage's probe for one merge commit: {sha} replaced, and
// {repo} by the pull request's repository (owner/name), so a project of
// several repositories can tell them apart (V0.playbook-probe-health). ""
// when the commit id is not one, or the command names {repo} and the
// repository is not one (either would be read by a shell).
func ProbeScript(probe, sha, repo string) string {
	if !ValidSHA(sha) || strings.TrimSpace(probe) == "" {
		return ""
	}
	if strings.Contains(probe, "{repo}") {
		if !repoName.MatchString(repo) {
			return ""
		}
		probe = strings.ReplaceAll(probe, "{repo}", repo)
	}
	return strings.ReplaceAll(probe, "{sha}", sha)
}

// Kinds of the playbook (V0.project-playbook).
const (
	// KindProjectLearn writes one playbook entry: args {id, slot, name?,
	// how?, url?, probe?, live?, evidence?, thread?} set it, {id, slot,
	// name?, remove: true} removes it, and the owner's {id, slot, name?,
	// allow: bool} allows or stops its probe. An agent never changes an
	// entry the owner wrote and never allows a probe: what it writes there
	// is kept as the entry's suggestion, which the owner's {take: true}
	// makes the entry and {dismiss: true} drops (V0.playbook-moments).
	KindProjectLearn = "project.learn"
	// KindProjectSharedPlaybook is a linked project's playbook as another
	// member has it: args {id, link, at, playbook}. An older at changes
	// nothing.
	KindProjectSharedPlaybook = "project.sharedPlaybook"
	// KindProjectPlaybookRun starts the run that learns the playbook from the
	// repository: args {id}. The engine starts the thread; the record does
	// not change.
	KindProjectPlaybookRun = "project.playbookRun"
	// KindProjectProbed counts a merge a stage's probe missed or saw
	// (V0.playbook-moments): args {id, name, ok}. With state it writes the
	// probe's last answer instead (V0.playbook-probe-health): args {id, name,
	// probe, state, exit, note}, kept only for the command it ran. It moves
	// no stamp: both are this host's.
	KindProjectProbed = "project.probed"
)

// FieldProjectPlaybook is a playbook entry: the owner's is kept.
const FieldProjectPlaybook Field = "project.playbook"

func init() {
	Rights = append(Rights, Right{Field: FieldProjectPlaybook, By: []string{ByOwner, ByAgent}, Kept: ownerOnly, Over: agentOnly,
		Refuse: "the owner wrote %s in the playbook; tell them what you found instead"})
	RegisterKind(&InputKind{Name: KindProjectLearn, On: RecProject, From: []string{ByOwner, ByAgent}, Reduce: reduceProjectLearn})
	RegisterKind(&InputKind{Name: KindProjectPlaybookRun, On: RecProject, From: ownerOnly, Reduce: func(r Rec, _ Input, _ time.Time) (Result, error) {
		return Result{Reply: map[string]any{"id": r.P.ID}}, nil
	}})
	RegisterKind(&InputKind{Name: KindProjectProbed, On: RecProject, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
		a := mapArgs(in)
		ok, _ := a["ok"].(bool)
		for i := range r.P.Playbook {
			if e := &r.P.Playbook[i]; e.Slot == SlotStage && e.Name == a.Str("name") {
				if st := a.Str("state"); st != "" {
					if e.Probe == a.Str("probe") {
						exit, _ := a["exit"].(int)
						if f, ok := a["exit"].(float64); ok { // an Input read back from JSON
							exit = int(f)
						}
						note := a.trimmed("note")
						if len(note) > maxPlaybookNote {
							note = note[:maxPlaybookNote]
						}
						e.Last = &ProbeResult{State: st, Exit: exit, Note: note, At: now}
					}
					continue
				}
				if ok {
					e.Missed = 0
				} else {
					e.Missed++
				}
			}
		}
		return Result{Reply: map[string]any{"id": r.P.ID}}, nil
	}})
	RegisterKind(&InputKind{Name: KindProjectSharedPlaybook, On: RecProject, From: ownerOnly, Reduce: reduceProjectSharedPlaybook})
}

// entryName names an entry in a sentence: "the dev stage", "the lesson
// e2e-first", "the verify entry".
func entryName(slot, name string) string {
	switch slot {
	case SlotStage:
		return "the " + name + " stage"
	case SlotLesson:
		return "the lesson " + name
	}
	return "the " + slot + " entry"
}

func clip(s string, n int, what string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "", reject(RejectBadArgs, "%s is at most %d characters; say it shorter", what, n)
	}
	return s, nil
}

// stampPlaybook moves the playbook's stamp to now, never back: the newest
// stamp in a link wins.
func stampPlaybook(p *Project, now time.Time) {
	if !now.After(p.PlaybookAt) {
		now = p.PlaybookAt.Add(time.Millisecond)
	}
	p.PlaybookAt = now
}

func reduceProjectLearn(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	p, by := r.P, in.Source.Principal()
	slot, name := a.Str("slot"), strings.ToLower(a.trimmed("name"))
	if !slices.Contains(PlaybookSlots, slot) {
		return Result{}, reject(RejectBadArgs, "slot must be one of %s", strings.Join(PlaybookSlots, ", "))
	}
	if slot == SlotStage && !stageName.MatchString(name) {
		return Result{}, reject(RejectBadArgs, "a stage needs a name: the environment in lower case, such as dev or prod")
	}
	if slot == SlotLesson && !stageName.MatchString(name) {
		return Result{}, reject(RejectBadArgs, "a lesson needs a name: a short one in lower case with dashes, such as e2e-before-done")
	}
	if !namedSlot(slot) {
		name = ""
	}
	key, what := PlaybookKey(slot, name), entryName(slot, name)
	at := slices.IndexFunc(p.Playbook, func(e PlaybookEntry) bool { return e.Key() == key })
	var old PlaybookEntry
	if at >= 0 {
		old = p.Playbook[at]
	}
	event := func(op string) []Event {
		return []Event{{"project.learn", map[string]any{"key": key, "op": op, "by": by, "thread": a.Str("thread")}}}
	}
	if v, ok := a["allow"].(bool); ok { // the owner's say over a probe
		if by != ByOwner {
			return Result{}, reject(RejectRights, "only the owner allows a probe")
		}
		if at < 0 || old.Probe == "" {
			return Result{}, reject(RejectNotFound, "%s has no probe", what)
		}
		p.Playbook[at].ProbeOK = v
		stampPlaybook(p, now)
		return Result{Events: event("allow"), Reply: map[string]any{"id": p.ID, "key": key, "probeOk": v}}, nil
	}
	take, _ := a["take"].(bool)
	dismiss, _ := a["dismiss"].(bool)
	if take || dismiss { // the owner's answer to an agent's suggestion
		if by != ByOwner {
			return Result{}, reject(RejectRights, "only the owner takes or dismisses a suggestion")
		}
		if at < 0 || old.Suggest == nil {
			return Result{}, reject(RejectNotFound, "%s has no suggestion", what)
		}
		s := old.Suggest
		e := &p.Playbook[at]
		e.Suggest = nil
		if dismiss && old.Slot == SlotLesson && old.How == "" {
			// A lesson an agent only suggested is gone with its suggestion.
			p.Playbook = slices.Delete(p.Playbook, at, at+1)
		}
		if take {
			// The owner's yes for a probe is for one command: another waits.
			e.ProbeOK = e.ProbeOK && s.Probe == e.Probe
			if s.Probe != e.Probe {
				e.Last = nil
			}
			e.How, e.URL, e.Probe, e.Deploy, e.Live, e.OwnerChecks, e.Evidence, e.Thread, e.At, e.Missed = s.How, s.URL, s.Probe, s.Deploy, s.Live, s.OwnerChecks, s.Evidence, s.Thread, now, 0
			// What the owner took is theirs: an agent's later finding waits beside it.
			e.By = ByOwner
		}
		stampPlaybook(p, now)
		return Result{Events: event("suggestion"), Reply: map[string]any{"id": p.ID, "key": key, "taken": take}}, nil
	}
	kept := at >= 0 && CanWrite(FieldProjectPlaybook, by, old.By, what) != nil
	if rm, _ := a["remove"].(bool); rm {
		if kept {
			return Result{}, CanWrite(FieldProjectPlaybook, by, old.By, what)
		}
		if at < 0 {
			return Result{Reply: map[string]any{"id": p.ID, "key": key, "removed": false}}, nil
		}
		p.Playbook = slices.Delete(p.Playbook, at, at+1)
		stampPlaybook(p, now)
		return Result{Events: event("remove"), Reply: map[string]any{"id": p.ID, "key": key, "removed": true}}, nil
	}
	e := PlaybookEntry{Slot: slot, Name: name, By: by, Thread: a.Str("thread"), At: now}
	var err error
	if e.How, err = clip(a.Str("how"), maxPlaybookHow, "how"); err != nil {
		return Result{}, err
	}
	if e.Evidence, err = clip(a.Str("evidence"), maxPlaybookNote, "evidence"); err != nil {
		return Result{}, err
	}
	if slot == SlotStage {
		if e.Probe, err = clip(a.Str("probe"), maxPlaybookCmd, "probe"); err != nil {
			return Result{}, err
		}
		if e.URL, err = clip(a.Str("url"), maxPlaybookURL, "url"); err != nil {
			return Result{}, err
		}
		if e.URL != "" && !strings.HasPrefix(e.URL, "https://") && !strings.HasPrefix(e.URL, "http://") {
			return Result{}, reject(RejectBadArgs, "url must start with https:// or http://")
		}
		if e.Deploy, err = clip(a.Str("deployCmd"), maxPlaybookCmd, "deployCmd"); err != nil {
			return Result{}, err
		}
		e.Live, _ = a["live"].(bool)
		e.OwnerChecks, _ = a["ownerChecks"].(bool)
		// The owner's yes is for one command: a changed probe waits again.
		e.ProbeOK = e.Probe != "" && e.Probe == old.Probe && old.ProbeOK
	} else if a.Str("probe") != "" || a.Str("url") != "" || a.Str("deployCmd") != "" {
		return Result{}, reject(RejectBadArgs, "only a stage has a probe, a url or a deploy command; put it in how")
	}
	if e.How == "" && e.Probe == "" && e.URL == "" && e.Deploy == "" {
		return Result{}, reject(RejectBadArgs, "give how: %s in a sentence or two", what)
	}
	if slot == SlotLesson && by != ByOwner && !kept {
		// An agent only suggests a lesson (V0.playbook-lessons): it reaches
		// agents once the owner took it, so a lesson an agent wrote waits
		// with no text of its own.
		sug := &PlaybookSuggestion{How: e.How, Evidence: e.Evidence, Thread: e.Thread, At: now}
		if at >= 0 {
			p.Playbook[at].Suggest = sug
		} else {
			if lessons(p) >= MaxLessons {
				return Result{}, reject(RejectBadArgs, "a playbook has at most %d lessons; tell the owner which one yours would replace", MaxLessons)
			}
			p.Playbook = append(p.Playbook, PlaybookEntry{Slot: slot, Name: name, By: by, Thread: e.Thread, At: now, Suggest: sug})
		}
		stampPlaybook(p, now)
		return Result{Events: event("suggest"), Reply: map[string]any{"id": p.ID, "key": key, "suggested": true}}, nil
	}
	if kept {
		// The owner's entry stays; what the agent found waits beside it.
		p.Playbook[at].Suggest = &PlaybookSuggestion{How: e.How, URL: e.URL, Probe: e.Probe, Deploy: e.Deploy, Live: e.Live, OwnerChecks: e.OwnerChecks, Evidence: e.Evidence, Thread: e.Thread, At: now}
		stampPlaybook(p, now)
		return Result{Events: event("suggest"), Reply: map[string]any{"id": p.ID, "key": key, "suggested": true}}, nil
	}
	if at >= 0 {
		if e.Probe == old.Probe {
			e.Missed, e.Last = old.Missed, old.Last
		}
		p.Playbook[at] = e
	} else {
		if slot == SlotStage && len(p.Stages()) >= MaxStages {
			return Result{}, reject(RejectBadArgs, "a playbook has at most %d stages; remove one first", MaxStages)
		}
		if slot == SlotLesson && lessons(p) >= MaxLessons {
			return Result{}, reject(RejectBadArgs, "a playbook has at most %d lessons; remove one first", MaxLessons)
		}
		p.Playbook = append(p.Playbook, e)
	}
	stampPlaybook(p, now)
	return Result{Events: event("set"), Reply: map[string]any{"id": p.ID, "key": key, "needsAllow": e.Probe != "" && !e.ProbeOK}}, nil
}

// lessons counts the playbook's lessons, taken or suggested.
func lessons(p *Project) int {
	n := 0
	for _, e := range p.Playbook {
		if e.Slot == SlotLesson {
			n++
		}
	}
	return n
}

// Lessons are the lessons the owner took or wrote, as agents are told them.
func (p *Project) Lessons() []PlaybookEntry {
	if p == nil {
		return nil
	}
	var out []PlaybookEntry
	for _, e := range p.Playbook {
		if e.Slot == SlotLesson && e.How != "" {
			out = append(out, e)
		}
	}
	return out
}

// reduceProjectSharedPlaybook takes a link's playbook when it is newer than
// the record's.
func reduceProjectSharedPlaybook(r Rec, in Input, _ time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	if l := a.Str("link"); l == "" || l != p.Link {
		return Result{}, reject(RejectBadArgs, "project %s is not in link %s", p.ID, l)
	}
	at, err := time.Parse(time.RFC3339Nano, a.Str("at"))
	if err != nil {
		return Result{}, reject(RejectBadArgs, "at must be a time")
	}
	if !at.After(p.PlaybookAt) {
		return Result{Reply: map[string]any{"id": p.ID, "kept": true}}, nil
	}
	var es []PlaybookEntry
	if raw := a["playbook"]; raw != nil {
		b, _ := json.Marshal(raw)
		if err := json.Unmarshal(b, &es); err != nil {
			return Result{}, reject(RejectBadArgs, "playbook must be a list of entries")
		}
	}
	seen := map[string]bool{}
	for _, e := range es {
		ok := slices.Contains(PlaybookSlots, e.Slot) && namedSlot(e.Slot) == (e.Name != "") && !seen[e.Key()] &&
			len(e.How) <= maxPlaybookHow && len(e.Probe) <= maxPlaybookCmd && len(e.Deploy) <= maxPlaybookCmd && len(e.URL) <= maxPlaybookURL
		if !ok || (namedSlot(e.Slot) && !stageName.MatchString(e.Name)) || len(es) > MaxStages+MaxLessons+len(PlaybookSlots) {
			return Result{}, reject(RejectBadArgs, "playbook has an entry Fyr cannot keep (%s)", e.Key())
		}
		seen[e.Key()] = true
	}
	// What a probe did is each host's own: it stays with the same command.
	for i := range es {
		es[i].Missed, es[i].Last = 0, nil
		for _, old := range p.Playbook {
			if old.Key() == es[i].Key() && old.Probe == es[i].Probe {
				es[i].Missed, es[i].Last = old.Missed, old.Last
			}
		}
	}
	p.Playbook, p.PlaybookAt = es, at
	return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}, nil
}

// PlaybookLearnPrompt is the line every agent of a project gets
// (V0.project-playbook): when to write the playbook, and how to act when a
// change reaches a stage.
const PlaybookLearnPrompt = `This thread's project keeps a playbook in Fyr: the stages a merged change reaches (dev, prod) with how it gets there and how Fyr can tell, how a change is verified, promoted and rolled back, where issues are filed, and how the app is run. When the owner tells you one of these or corrects you, or you had to work one out, and the playbook does not say it, record it with mcp__fyr__project_learn so the next thread knows (mcp__fyr__project_learn {} reads the playbook). Do not copy what CLAUDE.md or the README already says, unless Fyr needs it: a stage's probe and url. A probe is a shell command Fyr runs itself after each merge ({sha} is the merge commit, {repo} the pull request's owner/name), so nobody has to ask where a change is; it answers at once and never waits for a deploy, because Fyr runs it again each minute and stops it after 30 s; Fyr tells you in a [Fyr] line when a merged change reaches a stage. To act at that moment (verify it there, tell the owner it can be tested), arm mcp__fyr__watch {when: {kind: "condition", change, type: "deploy:<stage>", status: "ok"}}, or {kind: "changeReached", change, phase: "live"} for the live stage. When the owner corrects you on something every later thread here should do differently (what to run before saying done, a check you missed), suggest a lesson: mcp__fyr__project_learn {slot: "lesson", name: "<short-name>", how: "<what to do, in a sentence>", evidence: "<what happened>"}; it reaches later agents once the owner takes it.`

// PlaybookPrompt is the project's playbook for an agent's prompt: one line
// per filled slot and per lesson the owner took, "" for an empty playbook (a
// lesson an agent only suggested is not said).
func PlaybookPrompt(p *Project) string {
	if p == nil || !slices.ContainsFunc(p.Playbook, func(e PlaybookEntry) bool { return e.Slot != SlotLesson || e.How != "" }) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The playbook of the project %q:", p.Name)
	for _, e := range p.Stages() {
		parts := []string{}
		if e.How != "" {
			parts = append(parts, e.How)
		}
		if e.URL != "" {
			parts = append(parts, "at "+e.URL)
		}
		switch {
		case e.Runs():
			parts = append(parts, "Fyr checks it with a probe")
		case e.Probe != "":
			parts = append(parts, "its probe waits for the owner's yes, so Fyr does not check it yet")
		default:
			parts = append(parts, "no probe: Fyr cannot tell when a change is there")
		}
		if e.Broken() {
			why := fmt.Sprintf("could not run (exit %d)", e.Last.Exit)
			if e.Last.State == ProbeTimeout {
				why = "is stopped at Fyr's 30 s limit"
			}
			if e.Last.Note != "" {
				why += ": " + e.Last.Note
			}
			parts = append(parts, "its probe "+why+", so no change is seen here: find out why and record one that answers at once")
		}
		if e.Doubted() {
			parts = append(parts, fmt.Sprintf("its probe did not see the last %d merges within a day, so it may be wrong: check it and record a better one", e.Missed))
		}
		if e.Deploy != "" {
			parts = append(parts, "a change gets here by a command the owner runs from a Run card Fyr shows when the stage before holds it")
		}
		if e.OwnerChecks {
			parts = append(parts, "Fyr asks the owner to check a change here, and you hear the answer")
		}
		live := ""
		if e.IsLive() {
			live = ", live"
		}
		fmt.Fprintf(&b, "\n  Stage %s%s: %s.", e.Name, live, strings.Join(parts, "; "))
	}
	for _, slot := range PlaybookSlots[1:] {
		for _, e := range p.Playbook {
			if e.Slot == slot && slot != SlotLesson {
				fmt.Fprintf(&b, "\n  %s: %s", strings.ToUpper(slot[:1])+slot[1:], e.How)
			}
		}
	}
	if ls := p.Lessons(); len(ls) > 0 {
		b.WriteString("\n  Lessons from earlier threads here, which the owner took; follow them:")
		for _, e := range ls {
			fmt.Fprintf(&b, "\n    %s: %s", e.Name, e.How)
		}
	}
	return b.String()
}
