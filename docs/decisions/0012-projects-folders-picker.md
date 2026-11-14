# 0012 Projects, simplified: folders, an automatic repo link, a host folder picker

Date: 2026-11-14 · Status: accepted (owner, walk-through) · Spec: 0.5 V0.23, V0.28, V0.30, V0.42 · Builds on 0008

**Context.** After V0.30 the owner still did not understand the project picker, in particular how folders take part. Sources mixed folders and hand-typed repo URLs, and a new thread asked for a folder in free text.

**Decision.** The owner chose in a walk-through:

| Question | Decision |
|---|---|
| Folders per project | Several folders, as today. |
| Main folder | Each project marks one folder as **main** (the first by default; a star changes it). A new thread in the project starts there; "Change" offers the project's other folders first, then the folder picker. |
| The GitHub repo | Automatic, one line: found from a folder's git `origin` and shown read-only ("Also matches clones of github.com/meros/fyr"). Never typed. Repo matching across hosts keeps working. |
| A new thread with no project and no folder | Starts in a fresh scratch folder (`~/.local/share/fyr/scratch/<date>-<slug>`) with "No project". |
| The host folder picker | Offers recent folders (where threads ran, newest first), git repos found by a scan, browsing the tree from `~` with breadcrumbs and typed paths with completion, and creating a new folder. |
| Where the scan looks | The folders of the projects plus `~/git`, `~/src` and `~/locked`, two levels deep; the roots can be changed later in settings. |

**How the words read in the UI.** "Project" is a name with folders. "Threads started in these folders land here." "Main folder: new threads start here." "Also matches clones of <repo>." No word "source" in the UI.

**Consequences.** The project screen lists folders (with the main star and remove) and the automatic repo line; the "Add a folder or repo" text field becomes "Add folder…" through the picker. The host needs a read-only folder API (list, recent, repos found, create) limited to the owner's home folder. Hand-typed repo sources that exist stay valid and show as the same read-only line.
