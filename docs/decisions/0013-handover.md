# 0013 Handover: the owner moves a thread to another profile, by the best rung that works

Date: 2026-11-14 · Status: accepted (owner) · Spec: 0.5 V0.45 · Replaces for v0 the job design in SPEC 10.3

**Context.** A thread runs on one profile (a claude-compatible binary: plain `claude`, `claude-norban`, `claude-deepseek`). When one runs out of quota, or another model suits the work better, the owner wants the same thread to go on with another profile without losing the work. SPEC 10.3 describes a job with Briefs, held inputs and a child thread; v0 needs a smaller version.

**Decision.** The owner decided:

| Question | Decision |
|---|---|
| Who starts it | "Nothing automatic! It's an action you take!" Only the owner, from the thread ⋯ menu, the right-click menu or the phone ⋯ sheet. Never quota, errors or agents. |
| Where it goes | The same thread. It keeps its history, project and folder; a divider in the conversation says e.g. "Handed over from Claude to Claude (norban) · 17:20", and later runs use the new profile and its model. |
| To what | Only another profile for now; no archive handover. |
| How the context goes over | "Best of a ladder": try in order, use the first that works, and say in the divider which one was used. |
| While a run is live | Unavailable ("Wait for the run to end"). |

**The ladder.**

1. **Resume** (full history). The target runs the same CLI family (claude). Fyr finds the transcript `<source config dir>/projects/<dir>/<session>.jsonl` and copies it, with the session's folder `<session>/` (subagent transcripts, saved tool results) and `file-history/<session>/` when present, into the target's config dir under the same `<dir>`, then starts the target with `--resume <session>`. When both profiles use the same config dir nothing is copied. The rung counts as working when the target process starts: the CLI exits at once with "No conversation found" when it cannot load the session.
2. **Adopt** (the new agent reads the old conversation). Fyr writes the conversation from its own log as Markdown to `<data>/handover/<thread>-<time>.md`, starts the target on a new session with that folder added (`--add-dir`), and the first message asks it to read the file and take over.
3. **Brief** (the current agent writes a handover first). One turn on the source profile writes a handover (goal, done, state, open items, how to continue, branch and PR) as its reply; Fyr saves the reply to a file and the target starts from it as in rung 2. Only possible while the source can still run.

A rung that fails falls to the next; the divider's note says why ("No session transcript for this thread", "Resume failed: …"). If the target cannot start at all, the thread stays on its profile and the error shows in the conversation.

**How a profile's config dir is found.** In order: the profile's `configDir` (Fyr then sets `CLAUDE_CONFIG_DIR` for it), `CLAUDE_CONFIG_DIR` in the profile's `env`, `CLAUDE_CONFIG_DIR` of the daemon, else `~/.claude`. A wrapper script that sets its own `CLAUDE_CONFIG_DIR` inside must declare `configDir` too, since Fyr cannot see into it. On the owner's laptop (14 Nov 2026) the three profiles (`claude`, `claude-norban`, `claude-deepseek`) set no config dir: they share `~/.claude` and differ only in credentials, endpoint and model, so rung 1 resumes directly with no copy. Checked with the real CLI (2.1.322, haiku, a throwaway session): a session started with `claude` resumed under `claude-norban` in the same config dir and recalled the earlier message; after copying the transcript and its folder into a separate empty config dir, `claude-norban` with `CLAUDE_CONFIG_DIR` set there resumed it as well; without the copy it exited with "No conversation found with session ID" on stderr and exit code 1, which is how rung 1 detects a failed resume.

**How the words read in the UI.** "Hand over…" in the menus. The dialog: "Full history: this conversation continues as it is" (resume), "The new agent reads the old conversation" (adopt), "The current agent writes a handover first" (brief).

**Consequences.** A thread has a `handover` field while one runs; sends wait. `thread.handover {profile, model?}` and the `handover` event are in `API-v0.md`. Not built: a handover while a run is live ("after this run"), handover to a non-claude CLI (rung 1 then never applies), and a check that the new endpoint accepts the old transcript (an endpoint such as DeepSeek may reject the old transcript's thinking blocks on the first turn after a resume; the run then fails visibly, and choosing a lower rung by hand is not offered yet).
