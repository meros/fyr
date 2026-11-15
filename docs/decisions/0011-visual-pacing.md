# 0011 Visual pacing: the outcome at full weight, the work quiet and folded

Date: 2026-11-14 · Status: accepted (owner, 2026-11-15, shipped in V0.41) · Spec: 0.5 V0.41

**Context.** The owner's request: "make sure I naturally read/focus on what is important for me to see and naturally glance over what is mostly noise, intermediate stuff". In the conversation every agent paragraph had the same 15 px body text, so narration ("Test first, then the code and the spec row:") weighed as much as a result ("160 web tests pass…") or a question. Each tool group was a bordered card with the weight of an answer. Run ends, effect lines, "Agent is here", delivery words and View chips sat at even 12 px gaps, and the owner's own messages were dark, heavy bubbles: the heaviest thing on the screen was the owner's own words.

**What others do.** A short survey, from memory of the products plus two web searches (sources below); none of it was measured.

| Product | Pattern |
|---|---|
| Claude.ai, ChatGPT (thinking, agent and deep-research modes) | The answer is plain body text. The work (thinking, searches, tool calls) is one muted line above it ("Thought for 12 s", a summary of what was done) that opens into the steps. While it runs, the line shows the current step. |
| GitHub Copilot agent sessions, Cursor agent mode | Tool steps are small, borderless rows with an icon; consecutive steps group and collapse; the agent's prose stays readable. Edits and failures keep a visible mark. |
| Slack, Linear | System events ("X joined", "status changed") are one small muted line with no avatar; people's words keep full weight. |
| Claude Code issue tracker | Both failure modes are reported: steps expanded by default bury the answer; steps collapsed immediately hide what is happening live. |

Typographic principles that apply: hierarchy comes from size, weight, colour and spacing together (NN/g); the squint test (blur the screen: the most important thing must still stand out); progressive disclosure (essentials first, detail one action away, never hide critical information such as failures); proximity (space before an important block separates it, tight spacing groups the minor ones).

**Decision.** Every conversation item gets a role, decided in the client (`pace` in `web/src/conversation.ts`, pure and tested):

| Role | Items | How it reads |
|---|---|---|
| Outcome | The owner's messages; the agent's last message of a run (the newest one while the run goes on); previews the agent chose to show; failures (a failed run, effect or agent) | Full weight: 15 px body ink (16 px on the phone), about 22 px of room before it |
| Process | The agent's earlier messages in the run, tool groups, successful effects, answers, Fyr notes, started or finished agents | Muted ink, 13.5 px messages, tool rows without a card border, 4 px apart |
| End | "Finished in 14 min" | Faint, close under the outcome, 30 px before the next turn |

Rules:

1. **Consecutive process items fold into one line** ("Read 3 files · ran the map tests and 1 more · 9 steps · 1m 15s ▸"); one tap shows every step in order, and each tool group still opens into its calls. A failed call keeps its red mark on the folded line.
2. **Not folded:** one step alone, a stretch of system lines only (each is one line already), the newest stretch of a run that is still going (the owner sees what is happening now), and everything when "Show tool details" is on.
3. **The owner's own messages** stay right-aligned bubbles but in a pale tint (`--select`) with body ink: clearly theirs, no longer the darkest block. Delivery words are 11 px.
4. **Rhythm:** more room before an outcome, little between steps, most room after a run's end. Spacing is set on the item wrappers (`.cv.out`, `.cv.proc`, `.cv.end`), not by a uniform gap.
5. Words over symbols (V0.21) stays: the fold line is a sentence, not an icon.

**Rejected.** Hiding process entirely (the owner wants the work one tap away and live progress visible). Folding by user message instead of by run (a steering message mid-run would then end the turn). Classifying narration by its wording (fragile; the position in the run is enough).

**Consequences.** An informative intermediate message ("That was not mine: the deploy at 16:07 …") is folded once a later message arrives; it is one tap away. The run grouping uses `run.started`/`run.ended`; events from before those markers fall back to "the last message before the owner's next one". A run that never got its `run.ended` (a crash) keeps its last stretch open.

Sources: [NN/g squint test](https://www.nngroup.com/videos/squint-test/), [Progress steps pattern](https://aiuxplayground.com/pattern/status-steps), [Designing tool-enabled chat interfaces](https://glama.ai/blog/2025-08-27-designing-tool-enabled-chat-interfaces), [Making long agent conversations scannable](https://timetobuildbob.github.io/blog/making-long-agent-conversations-scannable), Claude Code issues [46110](https://claudeissues.com/issue/46110-bug-multiple-tool-calls-in-single-response-shown-as-one-collapsed-summary-line-i) and [59283](https://claudeissues.com/issue/59283-feature-request-option-to-hide-collapse-tool-call-panel-headers-in-agent-sdk-env).
