package core

import (
	"encoding/json"
	"hash/fnv"
	"path"
	"strings"
	"time"
)

// Project is a name, a colour and one folder on its host (SPEC 0.5 V0.55,
// decision 0017), with defaults and quiet hours.
type Project struct {
	ID string `json:"id"`
	// Folder is a full path on this host; threads that run in it or below
	// land in the project. "" means threads are only filed by hand.
	Folder  string `json:"folder,omitempty"`
	Edited  bool   `json:"edited,omitempty"` // the user changed it
	Name    string `json:"name"`
	Color   string `json:"color"` // a name from Palette
	Icon    Icon   `json:"icon"`
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
	// Effort is the default effort of a new thread here, when its model takes
	// one (V0.model-choice); "" leaves it to the provider.
	Effort string     `json:"effort,omitempty"`
	Quiet  QuietHours `json:"quiet,omitempty"` // quiet hours (V0.46): a list of windows
	// QuietNow is the end of a quiet stretch the owner started by hand
	// (V0.quiet-now): the project is quiet from then until this time, joined
	// with the windows of Quiet that hold or follow on. A time passed is off.
	QuietNow *time.Time `json:"quietNow,omitempty"`
	// QuietLift is the end of a pause of the quiet the owner held for on
	// the project page (V0.quiet-lift): until then the project is not quiet,
	// whatever its windows and QuietNow say, on every device and every host
	// of a linked project. A time passed is off.
	QuietLift *time.Time `json:"quietLift,omitempty"`
	// Priority is the owner's say in how much the project's threads get of
	// the host (V0.project-priority): PriorityHigh, PriorityLow, or "" for
	// normal. It orders new waiters in the slot queue and sets the threads'
	// CPU weight; it changes no list and no notification.
	Priority string `json:"priority,omitempty"`
	// AgentSettle is what an agent's settle_request does on the project's
	// threads (V0.settle-request): AgentSettleAuto settles the thread once
	// nothing waits; "" (ask) shows the owner a Settle chip.
	AgentSettle string `json:"agentSettle,omitempty"`
	// ToolApproval is how agent tool approval prompts run on this project's
	// threads (V0.tool-approval-judge): ToolApprovalAuto lets the approval
	// judge allow routine calls and escalate uncertain ones; "" (ask) asks
	// the owner directly.
	ToolApproval string    `json:"toolApproval,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	// Image is the version of the project's image icon (V0.116), served at
	// /api/projects/<id>/icon; "" draws Icon, the glyph, in Color.
	Image string `json:"image,omitempty"`
	// ImageAt is when Image was set: a linked project shows its newest
	// member's image (V0.project-icons-on-demand).
	ImageAt time.Time `json:"imageAt,omitzero"`
	// By says who set name, color and image: ByOwner or ByAgent; "" is a
	// placeholder that the setup agent may fill (V0.116). SetBy derives a
	// missing key for projects from before.
	By map[string]string `json:"by,omitempty"`
	// Setup is the last project setup run (V0.116).
	Setup *ProjectSetup `json:"setup,omitempty"`
	// Link joins this record to its counterparts on other hosts (decision
	// 0034): records with one link are one project to the owner. "" is an
	// ordinary project. Each record stays its own host's.
	Link string `json:"link,omitempty"`
	// SharedAt stamps the values the members share: name, colour, glyph icon,
	// quiet hours, priority, agentSettle and toolApproval. The newest stamp in a link wins (decision 0034).
	SharedAt time.Time `json:"sharedAt,omitzero"`
	// ArchivedAt is when the owner archived the finished project
	// (V0.project-archive, decision 0017): it leaves the pickers and the
	// filter, and its threads leave the inbox. nil is an active project.
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	// Playbook is what the project does around and after a pull request
	// (V0.project-playbook, playbook.go): its stages after a merge and how
	// Fyr can tell, how a change is verified, where issues are filed.
	Playbook []PlaybookEntry `json:"playbook,omitempty"`
	// PlaybookAt stamps the playbook: a linked project's members take the
	// newest (decision 0034).
	PlaybookAt time.Time `json:"playbookAt,omitzero"`
}

// A project's priority (V0.project-priority); "" is normal.
const (
	PriorityHigh = "high"
	PriorityLow  = "low"
)

// ValidPriority reports whether s is a project priority: high, low, or ""
// (normal; "normal" is accepted and stored as "").
func ValidPriority(s string) bool {
	return s == "" || s == "normal" || s == PriorityHigh || s == PriorityLow
}

// What an agent's settle_request does (V0.settle-request); "" is ask.
const (
	AgentSettleAsk  = string(DecisionAsk) // the same word as a question
	AgentSettleAuto = "auto"
)

// SettlesOnRequest reports that the project settles a thread when its agent
// asks (V0.settle-request): a nil project asks the owner.
func (p *Project) SettlesOnRequest() bool { return p != nil && p.AgentSettle == AgentSettleAuto }

// How agent tool approval prompts are handled (V0.tool-approval-judge); "" is ask.
const (
	ToolApprovalAsk  = string(DecisionAsk)
	ToolApprovalAuto = "auto"
)

// AutoJudgesApprovals reports that the project runs the approval judge before
// asking the owner on tool approvals (V0.tool-approval-judge): a nil project asks.
func (p *Project) AutoJudgesApprovals() bool { return p != nil && p.ToolApproval == ToolApprovalAuto }

// Rank is the priority as a number: 1 high, 0 normal, -1 low.
func (p *Project) Rank() int {
	if p == nil {
		return 0
	}
	switch p.Priority {
	case PriorityHigh:
		return 1
	case PriorityLow:
		return -1
	}
	return 0
}

// ProjectSetup is a project setup run (V0.116): the thread it runs in, when
// it started, or why it could not start.
type ProjectSetup struct {
	Thread string    `json:"thread,omitempty"`
	At     time.Time `json:"at"`
	Error  string    `json:"error,omitempty"`
	// All: the owner asked for this run (New project, Set up automatically),
	// so it sets every field, the owner's too (V0.linked-values-sync). The
	// pass for projects from before keeps the owner's.
	All bool `json:"all,omitempty"`
}

// The fields the setup agent fills (V0.116); the keys of Project.By.
const (
	SetupName  = "name"
	SetupColor = "color"
	SetupImage = "image"
)

// SetupFields are the fields the setup agent fills, in order.
var SetupFields = []string{SetupName, SetupColor, SetupImage}

// SetBy is who set field f (SetupName, SetupColor, SetupImage): ByOwner,
// ByAgent, or "" for a placeholder. A project from before V0.116 has no
// record: a name equal to its folder's last element and the colour picked
// from the name are placeholders, anything else is the owner's.
func (p *Project) SetBy(f string) string {
	if by, ok := p.By[f]; ok {
		return by
	}
	switch f {
	case SetupName:
		if p.Name == "" || (p.Folder != "" && p.Name == path.Base(p.Folder)) {
			return ""
		}
		return ByOwner
	case SetupColor:
		if p.Color == "" || p.Color == DefaultColor(p.Name) {
			return ""
		}
		return ByOwner
	case SetupImage:
		if p.Image == "" {
			return ""
		}
		return ByOwner
	}
	return ""
}

// fillBy writes down who set each field before a reduce changes any of
// them, so a derived placeholder stays one after the name changes.
func (p *Project) fillBy() {
	by := map[string]string{}
	for _, f := range SetupFields {
		by[f] = p.SetBy(f)
	}
	p.By = by
}

// setupField maps a setup field to its row in Rights.
var setupField = map[string]Field{SetupName: FieldProjectName, SetupColor: FieldProjectColor, SetupImage: FieldProjectImage}

// Covers reports whether a thread in dir lands in a project with folder:
// the folder itself and every folder below it.
func Covers(folder, dir string) bool {
	if folder == "" || dir == "" {
		return false
	}
	folder, dir = path.Clean(folder), path.Clean(dir)
	return dir == folder || folder == "/" || strings.HasPrefix(dir, folder+"/")
}

// MatchFolder is the one matching rule (SPEC 0.5 V0.55): the project whose
// folder is dir or contains it, the longest folder first, ties by id; ""
// when none does.
func MatchFolder(ps map[string]*Project, dir string) string {
	best, bestLen := "", -1
	for _, p := range ps {
		if !Covers(p.Folder, dir) {
			continue
		}
		n := len(path.Clean(p.Folder))
		if n > bestLen || (n == bestLen && p.ID < best) {
			best, bestLen = p.ID, n
		}
	}
	return best
}

// Filed is the project a thread lands in among ps (V0.55, V0.58,
// V0.review-correctness (2)): a thread whose project is gone loses it and the
// filed-by-hand mark; a thread not filed by hand moves to the project whose
// folder covers its folder (MatchFolder), and one that nothing covers keeps
// its project.
func Filed(t *Thread, ps map[string]*Project) (id string, manual bool) {
	id, manual = t.ProjectID, t.ProjectManual
	if id != "" && ps[id] == nil {
		id, manual = "", false
	}
	if !manual {
		if m := MatchFolder(ps, t.Cwd); m != "" {
			id = m
		}
	}
	return id, manual
}

// Env is what a thread's reduce reads beside its own records.
type Env struct {
	// Projects are the host's projects; the reduce files the thread among
	// them (Filed), so the rules and the invariants see where it lands. nil
	// leaves the filing as it is (a reduce without the host, in tests).
	Projects map[string]*Project
}

// file files t among env's projects.
func (env Env) file(t *Thread) {
	if env.Projects != nil {
		t.ProjectID, t.ProjectManual = Filed(t, env.Projects)
	}
}

// Icon is a monogram (Value empty: derived from the name), an emoji, or a
// line icon from the client's curated set.
type Icon struct {
	Kind  string `json:"kind"` // monogram, emoji, line
	Value string `json:"value,omitempty"`
}

// Palette is the project hues. They work in light and dark themes; amber is
// left out because the beam marks only what needs the user.
var Palette = []string{"slate", "blue", "sky", "teal", "green", "olive", "violet", "indigo", "rose", "brown"}

func ValidColor(c string) bool {
	for _, p := range Palette {
		if p == c {
			return true
		}
	}
	return false
}

// DefaultColor picks a palette hue from the name, so it is stable.
func DefaultColor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return Palette[int(h.Sum32()%uint32(len(Palette)))]
}

// The project kinds (#48, SPEC 3.15): the owner's project commands are Input
// kinds of the project record on the one path. The engine checks a folder
// on the host and puts the clean path in args.folder before the reduce;
// threads land in projects by their folders (MatchFolder), which the engine
// works out for every thread after each project Input.
const (
	KindProjectCreate = "project.create"
	KindProjectUpdate = "project.update"
	KindProjectDelete = "project.delete"
	// KindProjectMatched files a thread by its folder, not by hand (V0.55):
	// args ProjectMatch.
	KindProjectMatched = "project.matched"
	// KindProjectSetup is the setup agent's fields (V0.116): args {id,
	// name?, color?, image?}; it fills only fields the owner has not set.
	KindProjectSetup = "project.setup"
	// KindProjectSetupRun records a setup run (V0.116): args {id, thread?,
	// error?}. The engine starts the thread first.
	KindProjectSetupRun = "project.setupRun"
	// KindProjectLink sets or clears the record's link (decision 0034): args
	// {id, link}; "" unlinks. It is a kind of its own so that a host too old
	// to keep a link refuses it, where project.update would ignore the field.
	KindProjectLink = "project.link"
	// KindProjectShared stores the values a link's members share: args {id,
	// link, at, name, color, icon, quiet, priority}. An older stamp changes nothing.
	KindProjectShared = "project.shared"
	// KindProjectSharedImage is a linked project's image as another member set
	// it (V0.linked-values-sync): args {id, link, at, image, by?}; the engine
	// stores the picture (png, base64) first. It is stamped by imageAt, not by
	// sharedAt, so a name edit never copies a picture: an older at changes
	// nothing, image "" removes it.
	KindProjectSharedImage = "project.sharedImage"
)

// ValidLink reports whether s is a link id: l_ and 16 to 32 hex digits.
func ValidLink(s string) bool {
	h, ok := strings.CutPrefix(s, "l_")
	if !ok || len(h) < 16 || len(h) > 32 {
		return false
	}
	return strings.Trim(h, "0123456789abcdef") == ""
}

// ProjectMatch is the project a thread's folder lands it in, "" for none.
type ProjectMatch struct{ ID string }

func init() {
	for _, k := range []*InputKind{
		{Name: KindProjectCreate, On: RecProject, Makes: true, From: ownerOnly, Reduce: reduceProjectCreate},
		{Name: KindProjectUpdate, On: RecProject, From: ownerOnly, Reduce: reduceProjectUpdate},
		{Name: KindProjectDelete, On: RecProject, From: ownerOnly, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			id := r.P.ID
			*r.P = Project{}
			return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": id}}, nil
		}},
		{Name: KindProjectSetup, On: RecProject, From: agentOnly, Reduce: reduceProjectSetup},
		{Name: KindProjectSetupRun, On: RecProject, From: []string{ByOwner, ByObserved}, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a := mapArgs(in)
			all, _ := a["all"].(bool)
			r.P.Setup = &ProjectSetup{Thread: a.Str("thread"), At: now, Error: a.Str("error"), All: all}
			return Result{Events: []Event{{"project.setupRun", map[string]any{"thread": a.Str("thread"), "error": a.Str("error")}}}, Reply: map[string]any{"id": r.P.ID, "thread": a.Str("thread")}}, nil
		}},
		{Name: KindProjectLink, On: RecProject, From: ownerOnly, Reduce: reduceProjectLink},
		{Name: KindProjectShared, On: RecProject, From: ownerOnly, Reduce: reduceProjectShared},
		{Name: KindProjectSharedImage, On: RecProject, From: ownerOnly, Reduce: reduceProjectSharedImage},
		{Name: KindProjectMatched, From: observed, Untouched: true, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[ProjectMatch](in)
			if err == nil {
				r.T.ProjectID, r.T.ProjectManual = a.ID, false
			}
			return Result{}, err
		}},
	} {
		RegisterKind(k)
	}
}

// reduceProjectCreate makes a project (SPEC 0.5 V0.23, V0.55) and files the
// thread it was made from by hand (V0.30).
func reduceProjectCreate(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	p.Folder = a.Str("folder")
	p.Name = a.trimmed("name")
	if p.Name == "" && p.Folder != "" {
		p.Name = path.Base(p.Folder)
	}
	if p.Name == "" {
		return Result{}, reject(RejectBadArgs, "a project needs a name or a folder")
	}
	p.Color, p.Icon, p.CreatedAt, p.Edited = DefaultColor(p.Name), Icon{Kind: "monogram"}, now, true
	p.By = map[string]string{SetupColor: "", SetupImage: ""}
	p.By[SetupName] = p.SetBy(SetupName)   // V0.116: a folder's name is a placeholder
	if link := a.Str("link"); link != "" { // decision 0034: made as a member of a link
		if !ValidLink(link) {
			return Result{}, reject(RejectBadArgs, "link must be l_ and 16 to 32 hex digits")
		}
		p.Link = link
		for _, f := range SetupFields {
			p.By[f] = ByOwner
		}
	}
	res := Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}
	if tid := a.Str("threadId"); tid != "" {
		res.Follow = []Input{{Source: in.Source, Kind: "thread.setProject", Thread: tid, Args: Args{"projectId": p.ID}}}
	}
	return res, nil
}

// reduceProjectUpdate changes the fields args name; quiet hours are a list
// of windows (decision 0014 addendum), and null or an empty list clears
// them (V0.46).
func reduceProjectUpdate(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	p.fillBy() // V0.116: before the name changes
	if _, ok := a["folder"]; ok {
		p.Folder = a.Str("folder")
	}
	// V0.116: a value the owner changes is the owner's; saving it unchanged
	// leaves who set it.
	if n := a.trimmed("name"); n != "" && n != p.Name {
		p.Name, p.By[SetupName] = n, ByOwner
	}
	if col := a.Str("color"); col != "" {
		if !ValidColor(col) {
			return Result{}, reject(RejectBadArgs, "color must be one of %v", Palette)
		}
		if col != p.Color {
			p.Color, p.By[SetupColor] = col, ByOwner
		}
	}
	// The owner's upload sets image to its version; "" removes it, and the
	// agent does not put one back (V0.116).
	if v, ok := a["image"].(string); ok {
		p.Image, p.ImageAt, p.By[SetupImage] = v, now, ByOwner
	}
	if raw, ok := a["icon"]; ok {
		var ic Icon
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &ic)
		if ic.Kind != "monogram" && ic.Kind != "emoji" && ic.Kind != "line" {
			return Result{}, reject(RejectBadArgs, "icon kind must be monogram, emoji or line")
		}
		p.Icon = ic
	}
	if v, ok := a["profile"].(string); ok {
		p.Profile = v
	}
	if v, ok := a["model"].(string); ok {
		p.Model = v
	}
	if v, ok := a["effort"].(string); ok {
		p.Effort = v
	}
	if raw, ok := a["quiet"]; ok {
		var qs QuietHours
		if raw != nil {
			b, _ := json.Marshal(raw)
			if err := json.Unmarshal(b, &qs); err != nil {
				return Result{}, reject(RejectBadArgs, "quiet must be a list of {from, to, days?}")
			}
			if err := qs.Validate(); err != nil {
				return Result{}, reject(RejectBadArgs, "%s", err.Error())
			}
		}
		if len(qs) == 0 {
			qs = nil
		}
		p.Quiet = qs
	}
	if raw, ok := a["quietNow"]; ok { // V0.quiet-now; null ends it
		t, err := ParseQuietNow(raw, now)
		if err != nil {
			return Result{}, reject(RejectBadArgs, "%s", err.Error())
		}
		p.QuietNow = t
		if _, lift := a["quietLift"]; t != nil && !lift {
			p.QuietLift = nil // V0.quiet-lift: quiet started by hand ends a lift
		}
	}
	if raw, ok := a["quietLift"]; ok { // V0.quiet-lift; null ends it
		t, err := ParseQuietLift(raw, now)
		if err != nil {
			return Result{}, reject(RejectBadArgs, "%s", err.Error())
		}
		p.QuietLift = t
	}
	if raw, ok := a["priority"]; ok { // V0.project-priority; null is normal
		v, _ := raw.(string)
		if raw != nil && !ValidPriority(v) {
			return Result{}, reject(RejectBadArgs, "priority must be high, normal or low")
		}
		if v == "normal" {
			v = ""
		}
		p.Priority = v
	}
	if raw, ok := a["agentSettle"]; ok { // V0.settle-request; null is ask
		v, _ := raw.(string)
		if raw != nil && v != AgentSettleAsk && v != AgentSettleAuto {
			return Result{}, reject(RejectBadArgs, "agentSettle must be ask or auto")
		}
		if v == AgentSettleAsk {
			v = ""
		}
		p.AgentSettle = v
	}
	if raw, ok := a["toolApproval"]; ok { // V0.tool-approval-judge; null is ask
		v, _ := raw.(string)
		if raw != nil && v != ToolApprovalAsk && v != ToolApprovalAuto {
			return Result{}, reject(RejectBadArgs, "toolApproval must be ask or auto")
		}
		if v == ToolApprovalAsk {
			v = ""
		}
		p.ToolApproval = v
	}
	p.Edited = true
	return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}, nil
}

// reduceProjectLink sets the record's link. Linking makes the fields the
// members show together the owner's, so that an agent on one host cannot
// change what the owner sees for the others (decision 0034).
func reduceProjectLink(r Rec, in Input, _ time.Time) (Result, error) {
	a := mapArgs(in)
	p, link := r.P, a.Str("link")
	if link != "" && !ValidLink(link) {
		return Result{}, reject(RejectBadArgs, "link must be l_ and 16 to 32 hex digits")
	}
	p.fillBy()
	if link != "" {
		for _, f := range SetupFields {
			p.By[f] = ByOwner
		}
	}
	p.Link = link
	return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID, "link": link}}, nil
}

// reduceProjectShared takes a link's shared values when they are newer than
// the record's. Only those four fields can change here.
func reduceProjectShared(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	if l := a.Str("link"); l == "" || l != p.Link {
		return Result{}, reject(RejectBadArgs, "project %s is not in link %s", p.ID, l)
	}
	at, err := time.Parse(time.RFC3339Nano, a.Str("at"))
	if err != nil {
		return Result{}, reject(RejectBadArgs, "at must be a time")
	}
	if !at.After(p.SharedAt) {
		return Result{Reply: map[string]any{"id": p.ID, "kept": true}}, nil
	}
	shared := Args{}
	for _, k := range []string{"name", "color", "icon", "quiet", "quietNow", "quietLift", "priority", "agentSettle", "toolApproval"} { // V0.linked-agent-settle, V0.tool-approval-judge, V0.quiet-now, V0.quiet-lift
		if v, ok := a[k]; ok {
			shared[k] = v
		}
	}
	in.Args = shared
	res, err := reduceProjectUpdate(r, in, now)
	if err != nil {
		return Result{}, err
	}
	p.SharedAt = at
	return res, nil
}

// reduceProjectSharedImage takes another member's image when it was set later
// than this record's (V0.linked-values-sync).
func reduceProjectSharedImage(r Rec, in Input, _ time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	if l := a.Str("link"); l == "" || l != p.Link {
		return Result{}, reject(RejectBadArgs, "project %s is not in link %s", p.ID, l)
	}
	at, err := time.Parse(time.RFC3339Nano, a.Str("at"))
	if err != nil {
		return Result{}, reject(RejectBadArgs, "at must be a time")
	}
	if !at.After(p.ImageAt) {
		return Result{Reply: map[string]any{"id": p.ID, "kept": true}}, nil
	}
	p.fillBy()
	by := a.Str("by")
	if by != ByAgent && by != "" {
		by = ByOwner
	}
	p.Image, p.ImageAt, p.By[SetupImage] = a.Str("image"), at, by
	return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID, "image": p.Image}}, nil
}

// reduceProjectSetup is the setup agent's fields (V0.116): each one the
// owner has not set is filled and marked the agent's; the owner's are kept
// and named in the reply.
func reduceProjectSetup(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	p := r.P
	p.fillBy()
	if col := a.Str(SetupColor); col != "" && !ValidColor(col) {
		return Result{}, reject(RejectBadArgs, "color must be one of %v", Palette)
	}
	set, kept := []string{}, []string{}
	for _, f := range SetupFields {
		v := strings.TrimSpace(a.Str(f))
		if f == SetupName {
			v = strings.Join(strings.Fields(v), " ")
		}
		if v == "" {
			continue
		}
		if CanWrite(setupField[f], in.Source.Principal(), p.By[f], "it") != nil {
			kept = append(kept, f)
			continue
		}
		switch f {
		case SetupName:
			p.Name = v
		case SetupColor:
			p.Color = v
		case SetupImage:
			p.Image, p.ImageAt = v, now
		}
		p.By[f] = ByAgent
		set = append(set, f)
	}
	return Result{Events: []Event{{"project.setup", map[string]any{"set": set, "kept": kept}}}, Reply: map[string]any{"id": p.ID, "set": set, "kept": kept}}, nil
}
