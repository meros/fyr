# V0.auto-title: Fyr titles a thread by itself after its first run and once more when its goal is clear

→ V0.108, V0.title-goal, V0.69, V0.115. *Amends V0.108 (4), "Fyr never calls on its own": it now calls at most twice per thread. Also amends V0.108 (3): the call reads attachments, and the goal call also reads the current title and the change titles. Amends V0.69 (4): an agent's rename to the same words makes the title the agent's.*

Why: a thread's title was the first line of its first message, cut at 80 characters with "…". A message that was only a screenshot got the title "Image". A short question ("Stämmer det?", "Not nice", "fix 4711", a pasted link) said nothing about the work. A long first sentence was cut in the middle. The title stayed the same when a question turned into a fix. The model title (V0.108) ran only when the owner pressed Regenerate or the agent asked for it, and agents seldom did.

(1) **Start title.** A thread whose title came from its first message (`TitleFrom`) is marked `titleAuto: "first"`. When its first run ends, Fyr asks for a title by itself (the V0.108 call, on the System agents setting of V0.115) and moves the mark to `"start"`. A title from `thread_new`, a rename by the owner, and a rename by the agent each stop the automatic titles. A thread made before this change has no mark and is never retitled by itself. The agent's `thread_rename` to the title Fyr just made counts as the agent's rename (`titleBy: agent`).

(2) **Goal title.** Fyr asks once more when the goal becomes clear: when the thread's first change appears (`change_open`, or a PR Fyr links), or when it settles, whichever comes first. The mark then becomes `"done"`. If the goal comes before the first run ends, there is no separate start title. This call first reads `The thread's current title: …` and `Its changes: …` (the titles of the change cards). The prompt says to keep the current title word for word unless the goal moved, for example from a question to the fix it led to. Without this, the goal call in the evaluation often named the last step it read ("Closed #12 and #13, handover updated") instead of the goal.

(3) **Landing.** The result is the observer Input `title.made {by: "fyr", auto}`. It lands only while nobody has named the thread (`titleBy` is empty), so a name the owner or the agent gave while the call ran is kept. The title stays Fyr's (`titleBy` stays empty), so the owner and the agent can still replace it. The log gets `thread.renamed {by: "fyr", auto: "start"|"goal"}`.

(4) **Failure is quiet.** Nobody asked for this title, so a failed call only logs a line on the host and the title stays. A call that finds another title call running for the same thread waits for it, up to the 90 s limit of one call, so the start and goal calls cannot drop each other (before, the second call failed with "a title is being made already").

(5) **What the call reads.** An attachment is named once per message (`[attached image]`, `[attached file]`), so a message that is only a screenshot reads as one. Fyr's own `[Fyr]` messages (a handover note) are left out.

(6) **Placeholder.** Until the start title arrives, a long first line gives its first sentence if that sentence has 3 or more words and at most 60 characters. Otherwise the line is cut at a word boundary within 60 characters, with "…". A line of 60 characters or fewer is kept whole.

(7) **Prompt** (`internal/agent/title.go`, also used by Regenerate and `thread_rename {regenerate}`): one line of 3 to 8 words and at most 50 characters. It is written in the language of the owner's own words (the first Owner: line when they differ), not the agent's or a pasted text's. It names the specific feature, bug, component, issue or question. If the first message is vague or only an image, the subject comes from what the agent found and did, and the title is never generic ("Image", "Question", "Bug", "Check this"). It names the newest goal, not the agent's current step. The `thread_rename` description now says that Fyr titles threads by itself and that the agent's title stops Fyr's.

(8) **Cost.** At most two Haiku calls per new thread, about $0.001 each (V0.108 (4)).

**Measured.** The evaluation set holds 47 real threads that the owner started over three days, across several projects. Threads started by agents (`thread_new` gives them a title) are not in it. It is kept outside the repository because it holds private conversations. For each thread a title was written by hand. Candidates were made with the call as Fyr makes it (`claude -p`, claude-haiku-5-5, the same flags) on the conversation as it stood at each moment. A blind judge (claude-sonnet-5-5, with the candidates shuffled and scored twice) gave each candidate 2 (specific, as useful as the hand-written title), 1 (right topic, but vague, long, cut off or in the wrong language) or 0 (wrong or generic):

| title | judge (0–2) | scored 2 | scored 0 | chars | cut with "…" | generic | over 50 chars |
|---|---|---|---|---|---|---|---|
| before: first line of the first message | 0.87 | 7 | 11 | 45 | 15 | 8 | 17 |
| before: Regenerate pressed at the goal (old prompt) | 1.35 | 14 | 3 | 54 | 0 | 0 | 29 |
| new: start title, after the first run | **1.51** | **22** | **0** | 42 | 0 | 0 | 6 |
| new: goal title (keeps or moves the start title) | 1.44 | 16 | 0 | 41 | 0 | 0 | 1 |

The goal title scores a little below the start title because the hand-written references name the thread's starting question. Where a question turned into a fix, the goal title names the fix. An example from an unchanged thread: "Undersök varför den inte uppdaterades inatt" became "Varför <address> inte uppdaterades" at the start and "Ta bort bekräftat sålda objekt samma natt" at the goal. The first goal prompt, which got neither the current title nor the change titles, scored 1.45. One in five of its titles moved for no reason or named a step, and one came out in German. The final version fixes that. A hand check of the language found one wrong-language title among the new titles (a Swedish title for an English request whose agent wrote Swedish), against none for the first line, which is the owner's own words. The other differences that the language check flagged were threads where the owner wrote in both languages.

Examples from Fyr's own threads (before → start title):

- "Image" → "Blank Undo button on pressed Settled toast"
- "Image" → "Tailscale 401 fix for Fyr host setup"
- "Not nice" → "Phone "Answered" bar text runs under Back button"
- "Make s thorough analysis of todays threads, I feel it's not always clear what i…" → "Sprawling Fyr Threads: Bugs vs Overload"
- "Om man tar bort view så försvinner inte tabben" → "Stäng av live view tar inte bort View-fliken"

Tests: `TestAutoTitleAtFirstRunEndAndGoal`, `TestAutoTitleOnSettleAndGoalFirst`, `TestNoAutoTitleForAGivenName`, `TestAutoTitleLands`, `TestTitleFrom` (core); `TestAutoTitleAtTheFirstRunsEnd`, `TestAutoTitleAtTheGoalKeepsOrMoves`, `TestTitleTextNamesImages`, `TestAgentRegeneratesTheTitle` (engine); `TestTitlePromptAsksForTheGoal` (agent).

Not done: a thread handed to another host (`ThreadExport`) does not carry its mark, so it gets no automatic titles there. Both fronts show the title as before and need no change.
