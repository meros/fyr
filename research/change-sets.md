# Changes as first-class records (research 2026-11-15)

The owner's question: "a really good way, sota, way of managing and showing change-sets. We have PRs sure, but sometimes a change is not a git diff. And sometimes it's a git diff that is not a PR etc etc... We need a flexible and robust way for the agents to communicate changes and the status of those changes (in progress, under review, merged, deployed etc)."

The owner's follow-up: "git obviously is the most common case, and we should visualize that sota and best. But it's not the only thing we use agents to ask for things. Change-sets handling should have its own set of fyr tools the agent could and should use." Section 3.3 (the tool set) and 3.5 (git first) answer it.

Sources are linked per row. The web research ran from model knowledge and the canonical docs listed; a row marked "(unverified)" was not confirmed against a current page.

## 1. What Fyr has today

| Surface | What it shows | Where the state comes from | Code |
|---|---|---|---|
| PR | One PR per thread (`Thread.PR`): state OPEN/MERGED/CLOSED, checks pass/fail/pending/none, review decision, conflict | `gh pr create` URL in the output, or `pr_link`; `gh pr view` poll every 2 min | `internal/core/model.go` `PR`, `internal/github/github.go`, `internal/engine/tools.go` |
| PR tab | Checks, reviews, unresolved threads, Merge, Re-run | `gh` on demand, cached 30 s (V0.43) | `internal/github/detail.go`, `web/src/pr.ts`, `web/src/pr-ui.tsx` |
| Changes tab | The branch diff of the thread's folder against `origin/HEAD` | `git` on demand, no live updates (V0.25) | `internal/gitdiff`, `web/src/changes.ts`, `web/src/changes-ui.tsx` |
| Effects | A Decision card for `pr.merge` or `fyr.deploy`; the result as an `effect.result` line | `effect_request` (agent) or the PR tab (owner) | `internal/engine/tools.go`, `engine.go` `runEffect`, `web/src/effects.ts` |
| Deploy | A global banner with the phases build, test, drain, switch, done, failed | The deployer, kept 2 min (V0.39) | `internal/deploy`, `internal/engine/deploystate.go`, `web/src/deploy.ts` |
| Live view | A page plus a JSON state the agent patches; `headline` on the row | `view_publish`, `view_state` (V0.37) | `internal/engine/view.go` |
| Attention | `blocked` only from `PR.Blocking()`: CI failed, conflict, changes requested (V0.4, 12.5) | The PR snapshot | `internal/core/attention.go` |
| Settle | `settle_when merged` settles on a merged PR | The PR snapshot | `internal/core/observe.go` `PRObserved` |

**States that exist.** For a PR: open, merged, closed, plus three blocking reasons. For a deploy: six phases, global, not tied to a thread or a change. For an effect: requested, then ok, failed, declined. Nothing else has a state.

**Where it breaks.**

| Case | What happens today |
|---|---|
| Config change on a server (`kubectl apply`, `gcloud run deploy`) | Invisible. It shows only as Bash tool rows in the conversation. No state, no "applied" or "healthy", no attention when it fails later. |
| Figma, CAD (SolidCraft, Onshape) or a Google Doc | A `show` preview card at best. No record that a design is "under review" or "accepted". |
| Database migration run by hand | Tool rows only. Nothing records that it ran, where, or that it is irreversible. |
| NixOS rebuild | Tool rows only. The generation number and the host are not kept. |
| Change in another repo than the thread's folder | The Changes tab reads only the thread's folder. A PR in another repo can be linked, but it replaces the thread's one PR. |
| Local commit with no PR | The Changes tab shows the diff, but nothing says "committed, not pushed" and nothing asks the owner. |
| Stacked PRs | One PR per thread. `pr_link` switches to the new PR; the old one drops out of view. |
| Several repos at once | Same as above. |
| Merged but not deployed | The thread settles on merge. Nothing follows the change to production, except Fyr's own deploy, which is a banner. |

The gaps share one cause: Fyr has a hard-coded "PR of the thread" and a hard-coded "Fyr deploy". It has no general record of "a change, where it lands, and how far it got".

## 2. State of the art

### Code review systems

| System | Identity | Revisions | States | Evidence | Multi-change |
|---|---|---|---|---|---|
| **Gerrit** | `Change-Id` footer in the commit message; survives amend and rebase | Patch sets 1..n of one change | `NEW`, `MERGED`, `ABANDONED`; WIP and private flags | Labels with votes (`Code-Review` −2..+2, `Verified`), submit requirements | Topics ("submit whole topic" across repos), relation chains for stacks |
| **Phabricator Differential** | Revision `D123` | Diffs, each update a new diff on the same revision | Draft, Needs Review, Needs Revision, Changes Planned, Accepted, Closed, Abandoned | Reviewers, Herald rules, build status from Harbormaster | Parent/child revisions |
| **Graphite** | The branch | Commits on the branch; `gt submit` pushes each | Follows GitHub PR states | GitHub checks and reviews | Stacks: each branch records its parent; merge queue knows the stack |
| **Jujutsu (jj)** | Change ID, stable across rewrites; commit ID changes | Evolution log (`jj evolog`) of one change | None of its own | None | Any DAG of changes; a `change-id` commit header for Gerrit and others was proposed (unverified) |
| **Sapling** | Commit with hidden mutation history | Amends tracked by the mutation graph | Follows GitHub | GitHub | `sl pr` submits a stack as one PR per commit; ReviewStack shows it |
| **GitHub Deployments** | Deployment id per ref + environment | A new deployment per ref | Deployment status: `error`, `failure`, `inactive`, `in_progress`, `queued`, `pending`, `success` | `environment_url`, `log_url` | One deployment per environment |
| **GitHub commit statuses and checks** | Per commit SHA | n/a | Status: `error`, `failure`, `pending`, `success`. Check run status `queued`, `in_progress`, `completed` plus a conclusion | `details_url`, annotations | n/a |

Sources: Gerrit [concept-changes](https://gerrit-review.googlesource.com/Documentation/concept-changes.html), [Change-Id](https://gerrit-review.googlesource.com/Documentation/user-changeid.html), [topics](https://gerrit-review.googlesource.com/Documentation/cross-repository-changes.html); Phabricator [Differential](https://secure.phabricator.com/book/phabricator/article/differential/); Graphite [docs](https://graphite.dev/docs); jj [concepts](https://jj-vcs.github.io/jj/latest/glossary/); Sapling [stacks](https://sapling-scm.com/docs/git/github/); GitHub [deployments](https://docs.github.com/en/rest/deployments/deployments), [deployment statuses](https://docs.github.com/en/rest/deployments/statuses), [commit statuses](https://docs.github.com/en/rest/commits/statuses), [check runs](https://docs.github.com/en/rest/checks/runs).

**What they get right.** Gerrit and jj separate the identity of a change from its current content: one ID, many revisions. Gerrit's topic is the only built-in answer to "one change across several repos". GitHub separates "merged" (the source of truth changed) from "deployed" (an environment runs it), and gives each environment its own status. Checks and statuses attach to a revision (a SHA), never to the change as a whole, so a new revision resets them.

### Delivery and infrastructure

| Model | What to take |
|---|---|
| **CloudEvents** ([spec](https://github.com/cloudevents/spec/blob/main/cloudevents/spec.md)): `id`, `source`, `specversion`, `type`, optional `subject`, `time` | An event is unique by `source` + `id`, so a report sent twice is one event. `subject` names the thing inside the source. |
| **CDEvents** ([spec](https://cdevents.dev/docs/)): subjects `change`, `branch`, `pipelineRun`, `artifact`, `environment`, `service`; predicates such as `change.created`, `change.reviewed`, `change.merged`, `change.abandoned`, `service.deployed` | A shared vocabulary for the path from change to deploy. Take the names, not the wire format. |
| **OpenTelemetry** CI/CD and VCS semantic conventions ([cicd](https://opentelemetry.io/docs/specs/semconv/registry/attributes/cicd/), [vcs](https://opentelemetry.io/docs/specs/semconv/registry/attributes/vcs/), [deployment](https://opentelemetry.io/docs/specs/semconv/registry/attributes/deployment/)) | Attribute names such as `vcs.change.id`, `vcs.ref.head.revision`, `deployment.environment.name`. Useful as field names. |
| **in-toto / SLSA provenance v1** ([attestation](https://github.com/in-toto/attestation/blob/main/spec/README.md), [SLSA provenance](https://slsa.dev/spec/v1.0/provenance)) | A statement names its `subject` by name plus digest and says what produced it. A revision of a non-git change can be identified the same way: a SHA-256 of the plan, the file, the manifest. |
| **Argo CD** ([health](https://argo-cd.readthedocs.io/en/stable/operator-manual/health/), [sync](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/)) | Two axes: sync (`Synced`, `OutOfSync`, `Unknown`) and health (`Healthy`, `Progressing`, `Degraded`, `Suspended`, `Missing`, `Unknown`). "Applied" and "working" are different facts. |
| **Flux / kstatus** ([kstatus](https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md)) | `Current`, `InProgress`, `Failed`, from a `Ready` condition. |
| **Kubernetes conditions** ([API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties)) | A list of `{type, status: True\|False\|Unknown, reason, message, lastTransitionTime}`. Many independent facts without a state explosion. |
| **Terraform / HCP Terraform** ([run states](https://developer.hashicorp.com/terraform/cloud-docs/run/states)) | `planning`, `planned`, `confirmed`, `applying`, `applied`, `discarded`, `errored`, `canceled`. A saved plan is applied exactly; the approval binds to the plan. This is Fyr's D5 rule for infrastructure. |
| **Backstage catalog** ([entity refs](https://backstage.io/docs/features/software-catalog/references)) | A target named `kind:namespace/name`. One string form for "where a change lands". |
| **Linear and Jira** ([Linear workflows](https://linear.app/docs/configuring-workflows), [Jira status categories](https://support.atlassian.com/jira-cloud-administration/docs/what-are-issue-statuses-priorities-and-resolutions/)) | Free state names, each mapped to a fixed category (Linear: triage, backlog, unstarted, started, completed, canceled; Jira: To Do, In Progress, Done). The UI and the rules use the category; people read the name. |

### Agent tools

| Product | Unit | States shown | How state arrives | Non-git |
|---|---|---|---|---|
| **Devin** ([API sessions](https://docs.devin.ai/api-reference/sessions/retrieve-details-about-an-existing-session)) | Session, with PRs as outputs | Session status (working, blocked, finished, and similar); `pull_request` field | The product observes and the agent reports | Output files and messages only |
| **OpenHands** ([events](https://docs.all-hands.dev/usage/architecture/backend)) | Conversation; actions and observations in an event stream | Agent state (running, awaiting input, finished, error) | The event stream | Tool output only |
| **Cursor background agents** ([docs](https://docs.cursor.com/background-agent)) | Branch plus PR | Running, finished, PR open | The product owns the branch | No |
| **Codex cloud** ([docs](https://developers.openai.com/codex/cloud)) | Task with a diff, several attempts | Running, ready, PR created | The product owns the sandbox | No |
| **GitHub Copilot coding agent** ([docs](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent)) | A draft PR from the first push | PR states, plus session logs; the agent rewrites the PR body checklist | The agent pushes; GitHub is the record | No |
| **Claude Code** ([web](https://docs.claude.com/en/docs/claude-code/claude-code-on-the-web), [hooks](https://docs.claude.com/en/docs/claude-code/hooks)) | Session; PR on GitHub; todo list (`TodoWrite`) | Session states; PR badge | Hooks and the transcript | Todos only |
| **A2A protocol** ([spec](https://a2a-protocol.org/latest/specification/)) | Task with artifacts | `submitted`, `working`, `input-required`, `auth-required`, `completed`, `canceled`, `failed`, `rejected`, `unknown` | Push or stream of status updates | Artifacts are generic parts (text, file, data) |

**Patterns across agent products.** Every product uses the PR as the only change, because each owns a sandbox with one repo. None records a change outside git. None follows a change after merge. A2A has the cleanest task state machine, but a task is the agent's work; it is not the change the work produced. Fyr runs on the owner's own machines with real credentials, so its agents make non-git changes daily (kubectl, gcloud, NixOS, SolidCraft). This gap is Fyr's to fill.

## 3. Proposed model

### 3.1 The record

```ts
type Change = {
  id: string;                 // "c_…", made by Fyr
  key: string;                // the agent's or observer's stable key, unique per thread: "git:github.com/meros/fyr#v0", "k8s:prod/api", "doc:<url>"
  thread: string;             // the thread that made it
  kind: string;               // open: "code", "infra", "host", "data", "doc", "design", "other"
  title: string;              // one line for a person
  targets: Target[];          // where it lands; several for a cross-repo or multi-env change
  revisions: Revision[];      // newest last, at most 20 kept
  phase: Phase;               // the category, see 3.2
  label?: string;             // the kind's own word: "planned", "applied to staging", "patch set 3"
  conditions: Condition[];    // independent facts: checks, review, conflict, apply, health
  evidence: Evidence[];       // links: PR, check, deployment, plan, preview, log
  group?: string;             // a topic that ties changes into one set (Gerrit topic)
  after?: string[];           // change ids this one builds on (a stack)
  phaseBy: "agent" | "observed" | "owner";
  createdAt: string; updatedAt: string;
};
type Target = { ref: string; url?: string };          // Backstage-style "repo:github.com/meros/fyr@main", "k8s:gke-prod/api", "host:meros-server", "env:production"
type Revision = { id: string; at: string; summary?: string; url?: string; by: "agent" | "observed" }; // a commit SHA, a plan digest, a doc version, a file SHA-256
type Condition = { type: string; status: "ok" | "fail" | "pending"; reason?: string; at: string; source: "agent" | "observed" | "owner" };
type Evidence = { kind: string; url?: string; attachment?: string; title?: string; state?: string };
```

The PR is not a kind. A PR is evidence and conditions on a `code` change whose key is the repo plus branch. A local commit, a pushed branch, an open PR and a merged PR are the same change in four phases. A stack is several `code` changes with `after`. A cross-repo change is several changes with one `group`.

### 3.2 Phases and who moves them

Five phases plus two ends, the way Linear maps names to categories:

| Phase | Means | Examples |
|---|---|---|
| `working` | The agent is still making it | uncommitted edits; a draft PR; a design in progress |
| `review` | It waits for a review or an approval | an open PR; a Terraform plan; a doc the owner should read |
| `landed` | The source of truth has it | merged; `kubectl apply` done; migration ran; NixOS switched; doc published |
| `live` | It runs where it should and looks healthy | deployment `success`; rollout healthy; Fyr deploy `done` |
| `abandoned` / `reverted` | It stopped, or was undone | PR closed; plan discarded; rolled back |

Rules:

1. **Facts beat claims.** A phase or condition with `source: observed` cannot be overwritten by an agent report. The agent may report the phase of a change Fyr cannot see (a Figma file, a NixOS rebuild on another machine); the UI marks it "reported by the agent".
2. **The agent never sets `approved` or a review condition to `ok`.** Approval comes from the owner or from an observed review (GitHub). This keeps D5 intact.
3. **The owner may set any phase** (Accept, Mark live, Abandon), as an Action with a receipt.
4. **Landing through an effect.** When an effect names a change (`effect_request {action, args, change}`), its result moves the change: `pr.merge` ok moves it to `landed`, `fyr.deploy` done to `live`.
5. **Conditions carry failures**, not phases. A failed check, a conflict, a failed apply or a degraded rollout is a `fail` condition. Argo's two axes (sync and health) map to the phase and the conditions.

### 3.3 The change tools

Changes get their own family of Fyr tools, in the same MCP server as `effect_request` and `show`. The agent uses them for every change it makes or asks for. For git, Fyr already sees the commits, so the tools add what Fyr cannot see: the intent, the grouping, and the asks to the owner.

| Tool | Does | Replaces or extends |
|---|---|---|
| `change_open {key, kind, title, targets?, group?, after?, revision?}` | Starts a change, or returns the one with that `key` (idempotent). For git the key is `git:<repo>#<branch>` and Fyr fills targets and revisions itself. | `pr_link` becomes `change_open {kind: code}` with the PR URL as evidence |
| `change_revise {change, revision: {id, summary?, url?, digest?}}` | A new revision: a new commit range, a new plan, a new doc version, a new CAD file. Conditions bound to the old revision reset to pending. | new |
| `change_status {change, phase?, label?, conditions?}` | Moves the phase or reports a fact the agent knows ("applied to staging", "health ok"). Marked "reported"; an observed fact wins (rule 1). | new |
| `change_attach {change, evidence: [{kind, url?, attachment?, title?}]}` | Links proof: a plan output, a preview (`show` result), a log, a deployment URL, a design export. | `show` can take `change` to attach in one call |
| `change_request {change, ask, args?}` | Asks the owner: `review`, `approve`, `merge`, `apply`, `deploy`, `revert`. It shows as a Decision card. Fyr runs the effect where it can (`merge`, `deploy`), else the answer comes back to the agent as context. | `effect_request` becomes `change_request`; `pr.merge` is `ask: merge` |
| `change_list {scope?: thread\|project\|target\|group, id?}` | Reads changes with their observed state (CI, review, deployment), from this thread or others. The agent reads Fyr's facts instead of polling `gh`, and sees another thread's open change on the same target before it touches it. | replaces ad-hoc `gh pr view` loops |

Rules for the set:

1. **Six verbs, flat arguments.** Each tool does one thing, so the agent picks by name and the schema stays short (about 900 tokens for the six, against about 400 for one `change_report`). One catch-all tool was the first draft; it hides the asks inside a status update and makes "the agent asked for approval" hard to see.
2. **The agent never approves.** No tool sets an approval or a review to ok (D5). `change_request` is the only way to the owner.
3. **Idempotent by key and revision id.** A retried call never makes a second change or a second revision.
4. **The tool result says what Fyr shows**, in one line: "Change c_ab12 'Raise api replicas to 4' · landed (reported) · shown in the thread."
5. **The system prompt tells the agent when.** The Fyr block in `--append-system-prompt`: open a change for every change outside this folder's git (servers, other repos, databases, designs, documents) and before the first edit of it; use `change_request` for anything the owner must accept. The Stop hook of V0.37 can later ask once per run when Bash ran `kubectl`, `gcloud`, `terraform`, `nixos-rebuild` or `psql` without a change (opt-in, same budget rules).

`settle_when` takes `{change, phase}` next to `done` and `merged` (which becomes "every code change landed").

### 3.3a Actions: scripted and agent-driven

The owner's second follow-up: "Changes should include action. Scripted and llm-driven." A change carries buttons the owner presses. Each has one of three ways to run:

| Run | What happens on the press | For | Prior art |
|---|---|---|---|
| `fyr` | Fyr runs one of its own effects: merge a PR, re-run failed checks | Steps Fyr already knows | GitHub merge button, Gerrit submit |
| `script` | Fyr runs the declared `argv` exactly, no shell, in a folder, with a time limit; output kept; exit 0 moves the change (`onOk`), a failure blocks the thread | Exact, repeatable steps: apply a plan, run a migration, roll back | Terraform saved plan (apply binds to the plan), Argo CD sync button, Rundeck jobs |
| `agent` | The prompt goes to this thread's agent, or starts a new thread | Steps that need judgement: fix CI, address review, rebase, write notes | Copilot coding agent "fix this", Devin follow-ups |

The same model holds actions on the thread and on its live view (owner: "Dry/align the actions system with views and potentially other things"). A view page runs only an action the agent declared on the view, with `data-action="id"` or `fyr.run(id)`; its existing `data-send` stays a plain message. The owner's own registry (settle, snooze, archive) is a different thing: commands on the thread, not offers.

Safety follows D5. Only the owner presses. The press carries a digest of every field the owner saw; an action the agent changed since then is refused (409). A script bound to a revision runs only while that revision is the newest. Fyr derives the PR buttons (Merge, Re-run failed, Fix CI, Address review, Rebase) from what it observed, so the common case needs no declaration.

### 3.4 How Fyr observes

| Observer | Makes or moves | Cost |
|---|---|---|
| git, after each run (the thread's folder) | A `code` change keyed by repo plus branch when the branch has commits or edits against its base; `working`; a condition `pushed` ok/pending | 2 to 3 `git` calls, as V0.25 |
| GitHub poller (existing, 2 min) | PR as evidence; `review` while open; `landed` on merge; `abandoned` on close; conditions `checks`, `review`, `conflict` | Unchanged; PRs of every `code` change, not one per thread |
| GitHub Deployments (new, only for landed code changes in repos that use them) | `live` on a `success` deployment of the merge commit to the production environment; a `fail` condition on `failure` | One REST call per landed change per poll, stop after `live` or 24 h |
| Fyr deployer | `live` for the Fyr change with the build id | Free |
| Agent reports | Everything else | Free |

Probes for non-git targets (a command that checks a rollout, an HTTP health URL) are deferred: they run code on the owner's machines on the agent's say, so they need the D5 approval path first.

### 3.5 Git first: the best view of a code change

Most changes are git changes, so the `code` kind gets the richest view. Fyr observes it without any tool call; the tools only add intent. What the best review tools do, and what Fyr takes:

| Practice | Source | In Fyr |
|---|---|---|
| A stack as one vertical list, each layer with its own status | Graphite, Sapling ReviewStack ([reviewstack.dev](https://reviewstack.dev)) | `after` links render as a stack in the Changes tab: base at the bottom, each layer with phase, checks and review |
| Diff between revisions, also after a rebase | Gerrit patch-set diff; `git range-diff` ([docs](https://git-scm.com/docs/git-range-diff)) | "Changed since you last looked": the interdiff from the last revision the owner saw to now, by `git range-diff`, so a rebase does not show as a full rewrite |
| Per-file "viewed", reset when the file changes | GitHub "Viewed" checkbox (unverified page) | Seen per file per revision, tied to Fyr's seen model (6.4) |
| Checks and review comments on the line | GitHub check annotations | CI annotations and unresolved review threads inline in the diff |
| The author explains the change | Phabricator summaries; Copilot agent PR body | The agent's `change_revise` summary per revision, and hunk notes ("why") shown beside the hunk |
| Local state before a PR | `git status`, jj working copy | Dirty files, unpushed commits, ahead/behind the base, conflicts with the base, all without a PR |
| After merge, where it runs | GitHub Deployments | The phase rail continues past merge to the environments (`landed` → `live`) |

The Changes tab for a code change, top to bottom: the phase rail (working · review · landed · live), the facts line (branch, `3 ahead · 1 unpushed · 2 dirty`, checks, review, conflict), the revision list with "new since you looked", the file tree with viewed marks, the diff (split on desktop, unified on the phone) with inline annotations. Cross-repo groups show each repo as one block under the group title.

### 3.6 How it shows

- **Inbox row.** The PR chip becomes a change chip: the most urgent change, short ("#88 failing", "api · applied", "doc · review"), and "+2" when there are more. Same words as today for PRs.
- **Thread, Changes tab.** A list of the thread's changes, newest first: kind icon, title, targets, a phase line (working · review · landed · live, the reached part filled), failing conditions first, evidence links, revisions folded. A `code` change opens the existing branch diff below it; the PR detail of V0.43 opens from its evidence. The PR tab stays as the detail of a change's PR while the thread has one, so V0.43 needs no rework.
- **Conversation.** `change_report` calls render as a one-line card (V0.21): "Reported: api replicas 4 · landed". Phase moves by observers appear as lines like `effect.result`.
- **Cross-thread view.** A "Changes" screen with all changes not `live` or ended, grouped by phase, filtered by project and by target ("what is landed but not live in production"). Groups and stacks show as one block. This is the screen for "what is in flight across my agents", which the inbox, sorted by thread, does not answer.

### 3.7 Attention

A change adds three reasons to `blocked` (6.2), all with a cause in words:

1. a `fail` condition on any change not ended ("CI failed", "apply failed", "rollout degraded");
2. `needs: owner` set by the agent, or `review` on a kind whose reviewer is the owner (a doc, a plan); this is "Review" in the chip;
3. `landed` for more than a set time without `live`, only for changes that have a production target (off by default, see decisions).

`PR.Blocking()` becomes one case of rule 1. Push (V0.20) uses the same reasons: "Blocked · apply failed on gke-prod/api".

### 3.8 Trade-offs and rejected options

| Option | Why not |
|---|---|
| Keep the PR special and add a second "other changes" list | Two models for one idea; stacks and cross-repo still break; the owner asked for the PR to be one case. |
| A free state per kind (Terraform's states, Argo's states, GitHub's states) | Attention, push and the cross-thread view would need a table per kind. Free `label` plus fixed `phase` gives the same words with one rule set. |
| The change as its own entity with its own table, movable between threads | More power than v0 needs, and V0.55 shows the owner removes structure that is not used (refs, workspaces). Changes live in the thread's record, like `PR` today. The cross-thread view folds all threads. A table can come when a change must outlive its thread. |
| CloudEvents or CDEvents as the wire format of `change_report` | The agent writes the call; a flat schema is cheaper in tokens and harder to get wrong. Take the names (`change.merged`, `service.deployed`) for events instead. |
| One `change_report` tool for everything | The first draft. Asks to the owner disappear inside status updates, and the agent has no read side. The owner asked for a set of tools. |
| Parse Bash tool calls for `kubectl apply` and make changes from them | Fragile and wrong half the time (dry runs, reads, other contexts). Use them only as a hint for the Stop-hook reminder. |
| jj change IDs as identity | Most of the owner's repos use git branches; the branch is the identity Graphite and Copilot use. A jj change ID can be a revision field later. |
| Agent may mark its own change approved | Breaks D5: the owner approves, or GitHub's review does. |

**Costs.** Six more MCP tools in every session (about 900 tokens of schema), offset by removing `pr_link` and `effect_request`. Agent reports can be wrong; rule 1 and the "reported" mark limit the damage. Thread records grow (20 revisions, a few changes); cap changes per thread at 50 and drop ended ones after 30 days.

## 4. Phased plan

Each slice ships alone. Git comes first because it is most of the work. SPEC rows are new V0 rows unless marked amend.

| Slice | Ships | Code | SPEC |
|---|---|---|---|
| **1. The record, git observed** | `core.Change` on `Thread.Changes`; `Thread.PR` migrated into a `code` change at start; the poller writes phase and conditions; a git observer after each run (branch, ahead/behind, unpushed, dirty); several PRs per thread (stacks, other repos); the chip from the change | `internal/core/model.go`, `observe.go`, `attention.go`; `internal/github`; `internal/gitdiff`; `web/src/pr.ts`, `changes.ts`, `inbox.tsx` | New row "Changes as records"; amend V0.8, V0.25, V0.36, V0.43, V0.4, 12.5 |
| **2a. Actions** | One model on changes, the thread and the live view: `offer_action`; the `action.run` command with digest; script runner; built-in PR actions; `fyr.run`/`data-action` in views; buttons and run output | `internal/core/offer.go`, `internal/engine/change_run.go`, `web/src/offers.ts`, `web/src/offers-ui.tsx` | V0.60 |
| **2. The change tools** | `change_open`, `change_revise`, `change_status`, `change_attach`, `change_list`; `pr_link` as an alias; conversation cards; the system-prompt block; attention for reported fails and `needs: owner` | `internal/engine/tools.go`, `internal/mcp`; `web/src/conversation.ts` | Amend V0.12 (tools), 3.7, 13.5 |
| **3. The git view** | Stack rendering, range-diff "new since you looked", per-file viewed, inline CI annotations and review threads, revision summaries | `internal/gitdiff` (range-diff), `web/src/changes-ui.tsx` | Amend V0.25, 6.4 |
| **4. Asks and landing** | `change_request` replaces `effect_request`; `merge` lands, `deploy` goes live; GitHub Deployments poll; `settle_when {change, phase}` | `internal/engine/engine.go` `runEffect`, `internal/deploy`, `internal/github` | Amend V0.12, V0.13, V0.39, 5.10 |
| **5. The cross-thread Changes screen** | Grouped by phase, filters by project and target; groups and stacks as blocks | new `web/src/changes-board.tsx`; engine read query | New row; amend 13.3 |
| **6. Later: applied by Fyr** | Plan-then-apply effects (`apply` binds to a plan digest, like a Terraform saved plan); probes for `live` on non-git targets; the Stop-hook reminder | `internal/engine`, new job kind | Amend 5.10, 12.3; decision record |

Slices 1 and 2 need a decision record (0018 "Changes are records") before code, per `docs/PROCESS.md`.

## 5. Decisions for the owner

0. **The tool set.** (a) Six tools as in 3.3; (b) one `change_report` plus `change_request`; (c) six tools, and `effect_request`/`pr_link` stay as they are. Recommendation: (a).
1. **Where a change lives.** (a) In the thread's record, like `PR` today; (b) its own table and entity. Recommendation: (a). It matches V0.55's direction; move to (b) when a change must outlive or move between threads.
2. **The phase set.** (a) Five fixed phases plus a free label; (b) free states per kind; (c) A2A's task states. Recommendation: (a).
3. **Agent claims about things Fyr can observe.** (a) Allowed, marked "reported", overwritten by the next observation; (b) refused. Recommendation: (a), so a report before the first poll still shows.
4. **When `review` needs the owner.** (a) Only with `needs: owner` or an owner-reviewed kind (doc, plan); (b) every `review`; (c) never. Recommendation: (a). An open PR waiting for CI should not put the thread in Needs you, as today.
5. **Landed but not live.** (a) Off by default, a project setting turns on "remind me after N hours"; (b) always on; (c) never. Recommendation: (a). Threads would otherwise settle on merge and leave a deploy unwatched.
6. **Settle on merge.** (a) `settle_when merged` keeps its meaning (every code change landed); (b) it waits for `live` when a production target exists. Recommendation: (a) for now; add `{phase: live}` as an explicit choice.
7. **Fyr runs infrastructure changes (slice 6).** (a) Later, after slices 1 to 5; (b) now, with `kubectl` and `terraform` first. Recommendation: (a). The record must prove useful before Fyr takes on the risk of applying changes itself.
