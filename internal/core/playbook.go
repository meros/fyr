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

// The playbook's slots. A stage has a name (the environment); every other
// slot holds one entry.
const (
	SlotStage    = "stage"    // an environment a merged change reaches
	SlotVerify   = "verify"   // where and how a change is looked at, and who checks
	SlotPromote  = "promote"  // the manual step to the next stage
	SlotIssue    = "issue"    // where issues live and how one is filed
	SlotRun      = "run"      // how the app is started locally to show it
	SlotRollback = "rollback" // how a deploy is undone
)

// PlaybookSlots are the slots in the order they are shown and said.
var PlaybookSlots = []string{SlotStage, SlotVerify, SlotPromote, SlotIssue, SlotRun, SlotRollback}

// The playbook's limits: it stays a handful of short entries.
const (
	MaxStages       = 6
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
	// By is who wrote it, ByOwner or ByAgent; Thread is where it was learned
	// and Evidence what it rests on (a file, what the owner said).
	By       string    `json:"by"`
	Thread   string    `json:"thread,omitempty"`
	Evidence string    `json:"evidence,omitempty"`
	At       time.Time `json:"at"`
}

// Key names the entry in its playbook: the slot, or stage:<name>.
func (e PlaybookEntry) Key() string { return PlaybookKey(e.Slot, e.Name) }

// PlaybookKey is the key of slot's entry; a stage's has its name.
func PlaybookKey(slot, name string) string {
	if slot == SlotStage {
		return SlotStage + ":" + name
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

// ProbeScript is a stage's probe for one merge commit: {sha} replaced. ""
// when the commit id is not one (it would be read by a shell).
func ProbeScript(probe, sha string) string {
	if !ValidSHA(sha) || strings.TrimSpace(probe) == "" {
		return ""
	}
	return strings.ReplaceAll(probe, "{sha}", sha)
}

// Kinds of the playbook (V0.project-playbook).
const (
	// KindProjectLearn writes one playbook entry: args {id, slot, name?,
	// how?, url?, probe?, live?, evidence?, thread?} set it, {id, slot,
	// name?, remove: true} removes it, and the owner's {id, slot, name?,
	// allow: bool} allows or stops its probe. An agent never changes an
	// entry the owner wrote and never allows a probe.
	KindProjectLearn = "project.learn"
	// KindProjectSharedPlaybook is a linked project's playbook as another
	// member has it: args {id, link, at, playbook}. An older at changes
	// nothing.
	KindProjectSharedPlaybook = "project.sharedPlaybook"
	// KindProjectPlaybookRun starts the run that learns the playbook from the
	// repository: args {id}. The engine starts the thread; the record does
	// not change.
	KindProjectPlaybookRun = "project.playbookRun"
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
	RegisterKind(&InputKind{Name: KindProjectSharedPlaybook, On: RecProject, From: ownerOnly, Reduce: reduceProjectSharedPlaybook})
}

// entryName names an entry in a sentence: "the dev stage", "the verify entry".
func entryName(slot, name string) string {
	if slot == SlotStage {
		return "the " + name + " stage"
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
	if slot != SlotStage {
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
	if at >= 0 {
		if err := CanWrite(FieldProjectPlaybook, by, old.By, what); err != nil {
			return Result{}, err
		}
	}
	if rm, _ := a["remove"].(bool); rm {
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
		e.Live, _ = a["live"].(bool)
		// The owner's yes is for one command: a changed probe waits again.
		e.ProbeOK = e.Probe != "" && e.Probe == old.Probe && old.ProbeOK
	} else if a.Str("probe") != "" || a.Str("url") != "" {
		return Result{}, reject(RejectBadArgs, "only a stage has a probe or a url; put it in how")
	}
	if e.How == "" && e.Probe == "" && e.URL == "" {
		return Result{}, reject(RejectBadArgs, "give how: %s in a sentence or two", what)
	}
	if at >= 0 {
		p.Playbook[at] = e
	} else {
		if slot == SlotStage && len(p.Stages()) >= MaxStages {
			return Result{}, reject(RejectBadArgs, "a playbook has at most %d stages; remove one first", MaxStages)
		}
		p.Playbook = append(p.Playbook, e)
	}
	stampPlaybook(p, now)
	return Result{Events: event("set"), Reply: map[string]any{"id": p.ID, "key": key, "needsAllow": e.Probe != "" && !e.ProbeOK}}, nil
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
		ok := slices.Contains(PlaybookSlots, e.Slot) && (e.Slot == SlotStage) == (e.Name != "") && !seen[e.Key()] &&
			len(e.How) <= maxPlaybookHow && len(e.Probe) <= maxPlaybookCmd && len(e.URL) <= maxPlaybookURL
		if !ok || (e.Slot == SlotStage && !stageName.MatchString(e.Name)) || len(es) > MaxStages+len(PlaybookSlots) {
			return Result{}, reject(RejectBadArgs, "playbook has an entry Fyr cannot keep (%s)", e.Key())
		}
		seen[e.Key()] = true
	}
	p.Playbook, p.PlaybookAt = es, at
	return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}, nil
}

// PlaybookLearnPrompt is the line every agent of a project gets
// (V0.project-playbook): when to write the playbook, and how to act when a
// change reaches a stage.
const PlaybookLearnPrompt = `This thread's project keeps a playbook in Fyr: the stages a merged change reaches (dev, prod) with how it gets there and how Fyr can tell, how a change is verified, promoted and rolled back, where issues are filed, and how the app is run. When the owner tells you one of these or corrects you, or you had to work one out, and the playbook does not say it, record it with mcp__fyr__project_learn so the next thread knows (mcp__fyr__project_learn {} reads the playbook). Do not copy what CLAUDE.md or the README already says, unless Fyr needs it: a stage's probe and url. A probe is a shell command Fyr runs itself after each merge, so nobody has to ask where a change is; Fyr tells you in a [Fyr] line when a merged change reaches a stage. To act at that moment (verify it there, tell the owner it can be tested), arm mcp__fyr__watch {when: {kind: "condition", change, type: "deploy:<stage>", status: "ok"}}, or {kind: "changeReached", change, phase: "live"} for the live stage.`

// PlaybookPrompt is the project's playbook for an agent's prompt: one line
// per filled slot, "" for an empty playbook.
func PlaybookPrompt(p *Project) string {
	if p == nil || len(p.Playbook) == 0 {
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
		live := ""
		if e.IsLive() {
			live = ", live"
		}
		fmt.Fprintf(&b, "\n  Stage %s%s: %s.", e.Name, live, strings.Join(parts, "; "))
	}
	for _, slot := range PlaybookSlots[1:] {
		for _, e := range p.Playbook {
			if e.Slot == slot {
				fmt.Fprintf(&b, "\n  %s: %s", strings.ToUpper(slot[:1])+slot[1:], e.How)
			}
		}
	}
	return b.String()
}
