package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/projicon"
)

// Project setup (SPEC 0.5 V0.116): an agent run fills the fields of a
// project the owner has not set (the image icon, the colour from it, the
// name) with the tool project_setup. It runs once when a project is made
// with a folder, once for each project from before, and when the owner
// presses "Set up automatically".

// setupBrief is the setup run's first message.
func setupBrief(p core.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fyr project setup for the project %q (id %s), folder %s.\n\n", p.Name, p.ID, p.Folder)
	b.WriteString("Fill in the project's identity in Fyr with the tool project_setup {project: \"" + p.ID + "\", icon?, name?, color?}. ")
	b.WriteString("Fyr keeps every field the owner set and tells you which; do not try again for those. Do not ask the owner anything, do not change files in the folder, and finish in a few minutes.\n\n")
	b.WriteString("1. Icon. Look in the folder for the project's own image: a favicon or apple-touch-icon, logo files, public/, static/, assets/, the web manifest's icons, an image at the top of the README. ")
	b.WriteString("Prefer a square logo mark of at least 64 px over a wide wordmark or a screenshot. Pass its full path as icon.\n")
	b.WriteString("2. If the folder has none, fetch one online, but only the project's own branding: its own website's icon or the avatar of its GitHub owner (https://github.com/<owner>.png) from the folder's git remote. ")
	b.WriteString("No stock images, no pictures of other projects, nothing whose licence does not allow it. Pass the https URL as icon. If nothing fits, set no icon: the glyph stays.\n")
	b.WriteString("3. Fyr reads PNG, JPEG and GIF up to 2 MB and makes it square. Convert an SVG, WebP or ICO to a PNG first in a temp folder (rsvg-convert, magick or similar), outside the project folder.\n")
	b.WriteString("4. Name. If the name is only the folder's name, give the project's real name from package.json, go.mod, Cargo.toml, pyproject.toml, the README title or the repo name; keep it short. ")
	b.WriteString("Leave the colour out: Fyr takes it from the icon.\n\n")
	b.WriteString("End with one line saying what you set and where the icon came from.")
	return b.String()
}

// afterProject runs after a project Input applied: the icon files of a
// removed image or a deleted project go, and a new project with a folder
// gets its setup run (AutoSetup).
func (e *Engine) afterProject(in core.Input, r api.Reply) {
	a, _ := in.Args.(core.Args)
	switch in.Kind {
	case core.KindProjectUpdate:
		if v, ok := a["image"].(string); ok && v == "" && e.Icons != nil {
			e.Icons.Remove(a.Str("id"))
		}
	case core.KindProjectDelete:
		if e.Icons != nil {
			e.Icons.Remove(a.Str("id"))
		}
	case core.KindProjectCreate:
		id, _ := r.Result.(map[string]any)["id"].(string)
		// Decision 0034: a member of a link has the link's name and look.
		if e.AutoSetup && id != "" && a.Str("folder") != "" && a.Str("link") == "" {
			if r := e.startSetup(id, core.Observer(core.ObsHost), NewID("setup_")); !r.OK {
				log.Printf("project setup %s: %s", id, r.Error)
			}
		}
	}
}

// startSetup starts a setup run for project id: a thread in its folder,
// filed in it, on the system agent's provider and model, then the record
// on the project. A system agent that cannot run is recorded on the project
// (the project screen shows it) and answered as 409.
func (e *Engine) startSetup(id string, src core.Source, cmdID string) api.Reply {
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
		return api.Reply{Error: "a project needs a folder to be set up", Status: 400}
	}
	if r, done := e.replay(cmdID); done {
		return r
	}
	record := func(args core.Args, cmd string) api.Reply {
		args["id"] = id
		return e.applyProject(core.Input{Source: src, Kind: core.KindProjectSetupRun, Args: args, CmdID: cmd})
	}
	fail := func(why string) api.Reply {
		record(core.Args{"error": why}, cmdID+"/error")
		return api.Reply{Error: "Project setup cannot start: " + why, Status: 409}
	}
	if e.SystemAgentResolve == nil {
		return fail("this host runs no system agents")
	}
	// V0.115: the owner's System agents setting for "Project setup".
	profile, model, err := e.SystemAgentResolve(core.JobSetup)
	if err != nil {
		return fail(err.Error())
	}
	args := core.Args{"cwd": p.Folder, "projectId": id, "title": "Set up " + p.Name, "text": setupBrief(p)}
	if profile != "" {
		args["profile"] = profile
	}
	if model != "" {
		args["model"] = model
	}
	// The owner's standing choice (V0.116) starts the thread: as the owner,
	// so it is filed in the project.
	t := e.submit(core.Input{Source: core.FromOwner, Kind: "thread.create", Args: args, CmdID: cmdID + "/thread"})
	if !t.OK {
		return fail(t.Error)
	}
	return record(core.Args{"thread": t.ThreadID}, cmdID)
}

// SetupExisting starts one setup run for each project with a folder, no
// image and no run yet (V0.116: once for the projects from before), gap
// apart, and returns how many threads it started. A run that never started
// (an error, no thread) gets another try (V0.117); one that started is
// never repeated.
func (e *Engine) SetupExisting(gap time.Duration) int {
	e.mu.Lock()
	var ids []string
	for id, p := range e.projects {
		if p.Folder != "" && p.Image == "" && (p.Setup == nil || p.Setup.Thread == "" && p.Setup.Error != "") {
			ids = append(ids, id)
		}
	}
	e.mu.Unlock()
	sort.Strings(ids)
	n := 0
	for i, id := range ids {
		if i > 0 && gap > 0 {
			time.Sleep(gap)
		}
		r := e.startSetup(id, core.Observer(core.ObsHost), NewID("setup_"))
		if !r.OK {
			log.Printf("project setup %s: %s", id, r.Error)
			continue
		}
		n++
	}
	return n
}

// ProjectIconPath is the stored icon of project id at size n, or "".
func (e *Engine) ProjectIconPath(id string, n int) string {
	if e.Icons == nil {
		return ""
	}
	return e.Icons.Path(id, n)
}

// UploadProjectImage is the owner's upload (V0.116): the picture becomes
// the project's icon, the owner's.
func (e *Engine) UploadProjectImage(id string, b []byte) api.Reply {
	e.mu.Lock()
	ok := e.projects[id] != nil
	e.mu.Unlock()
	if !ok || e.Icons == nil {
		return api.Reply{Error: "no project " + id, Status: 404}
	}
	img, err := projicon.Decode(b)
	if err != nil {
		return api.Reply{Error: err.Error(), Status: 400}
	}
	v, err := e.Icons.Save(id, img)
	if err != nil {
		return api.Reply{Error: err.Error(), Status: 500}
	}
	return e.applyProject(core.Input{Source: core.FromOwner, Kind: core.KindProjectUpdate, Args: core.Args{"id": id, "image": v}, CmdID: NewID("icon_")})
}

// findProject is a project by id, else by name (any case).
func (e *Engine) findProject(key string) (core.Project, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p := e.projects[key]; p != nil {
		return *p, true
	}
	for _, p := range e.projects {
		if strings.EqualFold(p.Name, strings.TrimSpace(key)) {
			return *p, true
		}
	}
	return core.Project{}, false
}

// loadIcon reads an icon source: an http(s) URL or a full path.
func (e *Engine) loadIcon(ctx context.Context, src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		fetch := e.FetchIcon
		if fetch == nil {
			fetch = projicon.Fetch
		}
		return fetch(ctx, src)
	}
	return projicon.ReadFile(src)
}

var setupWords = map[string]string{core.SetupName: "name", core.SetupColor: "colour", core.SetupImage: "icon"}

func (e *Engine) setupTools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "project_setup",
		Description: "Set a Fyr project's identity from any thread: its icon image, colour and name. Use it when the owner asks (\"change the icon to this one\") and pass ownerAsked. Without ownerAsked Fyr fills only the fields the owner has not set and says which it kept. " +
			"icon is a full path to an image file (an attachment's path works) or an https URL (PNG, JPEG or GIF, at most 2 MB; Fyr makes it square, scales it and strips its metadata; local network addresses are refused). " +
			"Without color Fyr takes the colour from the icon, unless the owner chose one. project is the id or name of a project on this host; default this thread's project.",
		Schema: obj(map[string]any{
			"project":    str("The project's id or name; default this thread's project."),
			"icon":       str("A full path to an image file, or an https URL."),
			"name":       str("The project's real name, when it has only its folder's name, or the name the owner asked for."),
			"color":      map[string]any{"type": "string", "enum": core.Palette, "description": "A palette colour; default the icon's main colour."},
			"ownerAsked": map[string]any{"type": "boolean", "description": "The owner's newest message in this thread asks for exactly this change. The values then replace the owner's and become theirs. Refused when the newest message is not the owner's own."},
		}),
		Call: func(ctx context.Context, self string, a map[string]any) (string, error) {
			th, inThread := e.Thread(self)
			asked, _ := a["ownerAsked"].(bool)
			if asked && (!inThread || th.LastAsker == "") {
				return "", errors.New("ownerAsked needs the newest message in this thread to be the owner's own; it is not, so ask the owner")
			}
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
			// args go in as the agent's and keep the owner's values; owner are what the owner asked for
			// here; shared are those of a linked project, which the front passes to its other hosts.
			args, owner, shared := core.Args{"id": p.ID}, core.Args{"id": p.ID}, core.Args{"id": p.ID, "link": p.Link}
			var kept []string
			for _, k := range []string{"name", "color"} {
				v := sArg(a, k)
				switch {
				case v == "":
				case !asked:
					args[k] = v
				case p.Link != "":
					shared[k] = v
				default:
					owner[k] = v
				}
			}
			if src := sArg(a, "icon"); src != "" {
				if err := core.CanWrite(core.FieldProjectImage, core.ByAgent, p.SetBy(core.SetupImage), "its icon"); err != nil && !asked {
					kept = append(kept, core.SetupImage)
				} else {
					if e.Icons == nil {
						return "", errors.New("this host stores no project icons")
					}
					b, err := e.loadIcon(ctx, src)
					if err != nil {
						return "", err
					}
					img, err := projicon.Decode(b)
					if err != nil {
						return "", err
					}
					v, err := e.Icons.Save(p.ID, img)
					if err != nil {
						return "", err
					}
					if asked {
						owner["image"] = v
					} else {
						args["image"] = v
					}
					if sArg(a, "color") == "" && p.SetBy(core.SetupColor) != core.ByOwner {
						args["color"] = projicon.MainColor(img)
					}
				}
			}
			if len(args) == 1 && len(owner) == 1 && len(shared) == 2 && len(kept) == 0 {
				return "", errors.New("give icon, name or color")
			}
			var parts []string
			say := func(f string, vals core.Args) {
				w := setupWords[f]
				switch f {
				case core.SetupColor:
					w += " (" + vals.Str("color") + ")"
				case core.SetupName:
					w += fmt.Sprintf(" (%q)", vals.Str("name"))
				}
				parts = append(parts, w)
			}
			if len(owner) > 1 {
				r := e.applyProject(core.Input{Source: core.FromOwner, Kind: core.KindProjectUpdate, Args: owner, CmdID: NewID("agent_"), Actor: th.LastAsker})
				if !r.OK {
					return "", errors.New(r.Error)
				}
				for _, f := range core.SetupFields {
					if _, ok := owner[f]; ok {
						say(f, owner)
					}
				}
			}
			if len(shared) > 2 {
				// Decision 0034: the newest stamp in a link wins, and the front sends it to the other members.
				at := e.Now().UTC()
				if !at.After(p.SharedAt) {
					at = p.SharedAt.Add(time.Millisecond)
				}
				shared["at"] = at.Format(time.RFC3339Nano)
				r := e.applyProject(core.Input{Source: core.FromOwner, Kind: core.KindProjectShared, Args: shared, CmdID: NewID("agent_"), Actor: th.LastAsker})
				if !r.OK {
					return "", errors.New(r.Error)
				}
				for _, f := range core.SetupFields {
					if _, ok := shared[f]; ok {
						say(f, shared)
					}
				}
			}
			if len(args) > 1 {
				r := e.applyProject(core.Input{Source: core.FromAgent, Kind: core.KindProjectSetup, Args: args, CmdID: NewID("agent_")})
				if !r.OK {
					return "", errors.New(r.Error)
				}
				res, _ := r.Result.(map[string]any)
				set, _ := res["set"].([]string)
				for _, f := range set {
					say(f, args)
				}
				k, _ := res["kept"].([]string)
				kept = append(kept, k...)
			}
			out := ""
			if len(parts) > 0 {
				out = "Set the " + strings.Join(parts, ", ") + "."
			}
			if len(kept) > 0 {
				var ks []string
				for _, f := range kept {
					ks = append(ks, setupWords[f])
				}
				out = strings.TrimSpace(out + " Kept the " + strings.Join(ks, ", ") + ": the owner set it.")
			}
			if _, image := owner["image"]; p.Link != "" && (image || len(shared) > 2) {
				out += " This project is linked across hosts: its other hosts follow when Fyr's front next hears from them. The image stays on this host and shows while this host is online."
			}
			return out, nil
		}})
}
