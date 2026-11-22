# 0034 Linked projects: a project can span hosts, and each host keeps its own record

Date: 2026-11-18 · Status: **accepted** (the owner answered the four questions of #295 on 18 Nov, each as recommended) · Builds on 0016 (one owner per record), 0017 (a project is one folder; its "later" note on tags), 0031 (refs) · Spec rows: one per slice, from V0.211

**Context.** 0016 kept projects on one host and left "a project across hosts" as a later decision. The live data has the same work on two hosts as two unrelated projects, with different names and colours. The folder differs per machine and providers exist per host, so one shared record does not fit.

**Decision.** A project stays a record on one host with one folder. A record may carry a `link`. Records with one link are the **members** of one project, and the client shows them as one. A project without a link does not change, and a link with one member behaves like an ordinary project. A thread still belongs to its host's record.

| Question | Decision |
|---|---|
| Direction | Linked records (#295 option 1). Rejected: a plain tag alone, groups kept on a front or in one replicated document, one record with a folder per host, automatic grouping by git remote |
| Per member | folder, default provider and model, setup run, threads |
| Shared | name, colour, glyph icon, quiet hours (slice 2). Each host reads quiet hours in its own time zone |
| Image | not copied: the first online member in the owner's host order that has one is shown. *Amended by V0.linked-values-sync: the image set last (`imageAt`) is copied to every member, a removal too.* |
| Members per host | at most one |
| New thread | stays host first (V0.139) |
| Who writes a link | the front, because a link spans hosts: `front.projectLink {refs}` and `front.projectUnlink {ref}`. Each host stores its own record's link with the host kind `project.link` |
| Agents | no agent can link. Linking makes the shown fields the owner's, so `project_setup` cannot change them. Agents see only their own host, as before |

`project.link` is a kind of its own because `project.update` ignores arguments it does not know: an older host would answer OK and store nothing. A new kind gets "unknown action", and the front reports that the host needs an update.

Every host a link command names must be online. A link stored on some members only is a valid state, and the reply names the hosts that stored it and those that failed. The front checks "one member per host" against the records it has seen, so two fronts acting at once can get past it; the client then shows both records as members.

**Slices.** (1) The link and one row in every list, shown from the first online member. (2) Shared name, colour, icon and quiet hours: `SharedAt`, newest wins, catch-up as with the host order (V0.132) and host marks (V0.171). (3) Add on another host, "Add this project here" in New thread, the same-repo suggestion.

**Not in scope.** Syncing code or worktrees, moving a thread between hosts, agents seeing other hosts' threads, merging change cards when two hosts work on one PR.
