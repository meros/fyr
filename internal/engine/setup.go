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

// SetupJob is the system agent job of a setup run (the owner's System
// agents setting picks its provider and model).
const SetupJob = "setup"

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
		if e.AutoSetup && id != "" && a.Str("folder") != "" {
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
	if e.SystemAgent == nil {
		return fail("this host runs no system agents")
	}
	profile, model, err := e.SystemAgent(SetupJob)
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
// apart, and returns how many threads it started.
func (e *Engine) SetupExisting(gap time.Duration) int {
	e.mu.Lock()
	var ids []string
	for id, p := range e.projects {
		if p.Folder != "" && p.Image == "" && p.Setup == nil {
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
		Description: "Fill in a Fyr project's identity: its icon image, colour and name. Fyr fills only the fields the owner has not set and says which it kept. " +
			"icon is a full path to an image file or an https URL of the project's own branding (PNG, JPEG or GIF, at most 2 MB; Fyr makes it square, scales it and strips its metadata; local network addresses are refused). " +
			"Without color Fyr takes the colour from the icon. project is the project's id or name; default this thread's project.",
		Schema: obj(map[string]any{
			"project": str("The project's id or name; default this thread's project."),
			"icon":    str("A full path to an image file, or an https URL."),
			"name":    str("The project's real name, when it has only its folder's name."),
			"color":   map[string]any{"type": "string", "enum": core.Palette, "description": "A palette colour; default the icon's main colour."},
		}),
		Call: func(ctx context.Context, self string, a map[string]any) (string, error) {
			key := sArg(a, "project")
			if key == "" {
				if t, ok := e.Thread(self); ok {
					key = t.ProjectID
				}
			}
			if key == "" {
				return "", errors.New("this thread has no project; name one with project")
			}
			p, ok := e.findProject(key)
			if !ok {
				return "", fmt.Errorf("no project %q", key)
			}
			args := core.Args{"id": p.ID}
			for _, k := range []string{"name", "color"} {
				if v := sArg(a, k); v != "" {
					args[k] = v
				}
			}
			var kept []string
			if src := sArg(a, "icon"); src != "" {
				if err := core.CanWrite(core.FieldProjectImage, core.ByAgent, p.SetBy(core.SetupImage), "its icon"); err != nil {
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
					args["image"] = v
					if _, given := args["color"]; !given && p.SetBy(core.SetupColor) != core.ByOwner {
						args["color"] = projicon.MainColor(img)
					}
				}
			}
			if len(args) == 1 && len(kept) == 0 {
				return "", errors.New("give icon, name or color")
			}
			var set []string
			if len(args) > 1 {
				r := e.applyProject(core.Input{Source: core.FromAgent, Kind: core.KindProjectSetup, Args: args, CmdID: NewID("agent_")})
				if !r.OK {
					return "", errors.New(r.Error)
				}
				res, _ := r.Result.(map[string]any)
				set, _ = res["set"].([]string)
				k, _ := res["kept"].([]string)
				kept = append(kept, k...)
			}
			var parts []string
			for _, f := range set {
				w := setupWords[f]
				switch f {
				case core.SetupColor:
					w += " (" + args.Str("color") + ")"
				case core.SetupName:
					w += fmt.Sprintf(" (%q)", args.Str("name"))
				}
				parts = append(parts, w)
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
			return out, nil
		}})
}
