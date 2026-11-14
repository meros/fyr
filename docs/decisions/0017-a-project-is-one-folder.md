# 0017 A project is one folder

Date: 2026-11-14 · Status: accepted (owner, walk-through) · Amends 0008 and 0012 · Fits 0016

**Context.** The owner asked: "Take a critical look at what a project really is... Maybe we are overthinking it?" The live data on 14 Nov: 7 projects, each with exactly one folder; 0 with instructions or actions; 1 with quiet hours; 9 of 17 threads with no project, 6 of them in the home folder. What a project did in practice: filter and colour the list, give the start folder, hold quiet hours. Comparable tools (the Codex app, Conductor, T3 Code) treat a project as one codebase folder. The model from 0008/0012 (sources, refs, `covers` matching, several folders with a main star, cross-host repo matching, a place "provider" interface) served none of the live use, clashed with the word provider from 0015, and made the multi-host decision harder.

**Decisions.**

| Question | Decision |
|---|---|
| What a project is | **A name, a colour and one folder on one host**, plus quiet hours (0014), a default provider + model + effort (0015), a project page with the open PRs of its repo, and archive. |
| Which threads land in it | Threads that run in its folder or below it, by default (the longest folder wins). |
| Topic filing | **By hand only.** The owner, or the agent with `set_project`, can file a thread in a project whose folder it does not run in. A filed thread keeps its project; nothing matches it again. |
| Project objects at all | **Kept, small.** Folders never named stay "No project". |
| Dropped | **Agent instructions** (CLAUDE.md in the folder does this) and **project actions** (deploys stay effect requests the owner approves). Also dropped: several folders, the main star, the automatic repo line and cross-host repo matching, the sources/refs/`covers` model and the place "provider" interface. |

**Consequences.** Existing projects keep their one folder; a project with several folders keeps its first (main) one and the rest are dropped in a migration, logged. The project screen shows name, colour, folder (Change… through the folder picker), quiet hours, default model, archive. The folder picker (0012) stays for choosing the folder and starting threads; scan roots stay. A new thread with no project still starts in a scratch folder. With 0016 a project lives on the host of its folder, which is now the only choice. The repo link for the PR page comes from the folder's git origin when needed, not stored.

**Later, if multi-host needs it** (owner: "we could tag projects with project types or something"): a tag on projects, for example `fyr` on the laptop's and the server's fyr projects, lets the list and filters group them. A tag is a label with no rules, so it adds no owner to a record and does not reopen 0016.
