# 0039 Trans-host handover: a linked new thread on the target host, the source settled, and the 3-rung ladder across hosts

Date: 2026-11-21 · Status: **accepted** (owner answered the four questions of #498 on 2026-11-21) · Builds on 0013 (handover ladder), 0016 (one owner per record), 0031 (client names the host), 0034 (linked projects) · Spec: `V0.trans-host-handover`

## Context

Decision 0013 (`V0.45`) added "Hand over…" to move a thread to another provider on the same host via a three-rung ladder (resume, adopt, brief). A thread runs on one host, and the host follows its folder (decision 0015). When another host has room, the laptop is closing, or the repo is checked out on a server, the owner wants to continue the thread on another host without waiting for the post-v1 mesh move protocol (SPEC 10.4).

## Decision

| Question | Decision |
|---|---|
| Thread model | **Linked new thread + settle source.** The target host creates a new thread with the prior conversation copied over and a `handedOverFrom` link back; the source thread records `handedOverTo` and settles immediately with a link forward. The client opens the new thread on the target host. Fits one-owner-per-record (0016) and `(hostId, rid)` addressing (0031), and the target thread stays readable even when the source host goes offline. |
| Context rung | **Same 3-rung ladder: resume → adopt → brief.** The source exports the CLI session archive (up to 20 MB, when the CLI family matches) together with the conversation Markdown export. The target tries to extract the session under its own project directory and `--resume`, falling back to Adopt (reading the exported Markdown), and finally requesting a Brief from the source agent if neither applies. |
| Git state | **Inform about limitations, inject information about the host move in the target thread.** Fyr never auto-commits, pushes, or blocks handover on a dirty or unpushed working tree. The handover dialog inspects source git status and warns about uncommitted edits, unpushed commits, and local state staying on the source host. The target thread always injects a `[Fyr] Host handover:` context notice (queued into `Feed` with urgency `next` on rung 1 so it prepends to the next turn without burning an unsolicited turn; included in the handover message on rungs 2 and 3) naming the old host, old folder, new host, new folder, branch, dirty/unpushed counts, and open changes. |
| Target folder | **Linked project / matching repo + picker.** Pre-select the target host's member of the source thread's linked project (decision 0034), else a project on the target host with the same normalized git `origin` remote, with the target host's folder picker always available to override (or a fresh scratch folder when no folder is chosen). When moving a non-default branch into a matching git repository, optionally create or reuse a worktree for that branch when it exists locally or on `origin`, falling back cleanly to the selected folder if the branch is not pushed. |

## Protocol and orchestration

Because a cross-host handover spans two hosts and each host owns only its own records (0016), the front orchestrates it as a front command (`POST /api/front/command`, action `front.threadHandover`), just as `front.projectLink` and `front.projectAdd` do (0034):

1. Both hosts must be online and distinct; source guards match single-host `thread.handover` (owner only; unavailable while running, asking, archived, unread outbox, or handover already in progress).
2. The front requests `POST /api/threads/{id}/handover/export` on the source host, receiving thread metadata, git facts, lean conversation events, Markdown conversation, and an optional tar.gz archive of the CLI session files.
3. The front sends `POST /api/threads/handover/import` to the target host with the bundle, target provider/model/effort, target folder/project, and worktree option. If neither resume nor adopt can run and no brief was supplied yet, the target answers `needBrief: true`, the front requests a Brief (`wantBrief: true`) from the source host, and retries import.
4. Only after the target host creates the new thread and starts/prepares its session does the front send `thread.handedOver` to the source host to record `handedOverTo`, append the handover divider, and settle the source thread. If import fails on the target host, the source thread stays untouched.

## Consequences

- Both desktop and phone share the updated `HandoverForm` (with host selector, target folder/worktree control, and git limitations callout when targeting another host) and `HandoverDivider` (with clickable link to the counterpart thread).
- SPEC 10.4 (fenced record move with epochs) remains post-v1 for the mesh; decision 0039 delivers trans-host handover cleanly within the front-and-hosts architecture.
