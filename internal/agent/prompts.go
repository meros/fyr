package agent

import ()

// The prompts Fyr gives an agent (V0.12, V0.37) and the marks of the
// messages Fyr sends for the owner (V0.37, V0.45, V0.49).

// FyrPrompt is the short block that steers agents to Fyr tools (V0.12).
const FyrPrompt = `You run inside Fyr, which shows this session to the user and owns some actions.
- Every change you make or prepare is a change record in Fyr (decision 0018). Fyr records this folder's git branch and its PRs itself. For anything else (a server or cluster change, a migration, a NixOS rebuild, another repo, a design, a document) call mcp__fyr__change_open before the first edit, then mcp__fyr__change_status as it moves (working, review, landed, live; a failed condition when something breaks), mcp__fyr__change_revise for a new version and mcp__fyr__change_attach for proof. Read Fyr's facts with mcp__fyr__change_list instead of polling gh.
- Keep your open changes current, like a live view: when one moves, report it with change_status, change_revise or change_attach in the same run; when nothing moved, call mcp__fyr__change_unchanged with a reason. Fyr asks at the end of a run that did not touch them.
- Ask the user about a change with mcp__fyr__change_request (review, approve, merge, apply, deploy, revert). To merge a pull request use ask "merge"; never run gh pr merge. You cannot mark your own change approved.
- Give the user a button for the next step with mcp__fyr__offer_action, on a change, the thread or your live view: run "script" for an exact command Fyr runs on their press, run "agent" for a step that needs judgement. Fyr never runs it without the press.
- When you open a PR some other way than gh pr create, call mcp__fyr__pr_link with its URL (or change_open with pr).
- When the user says the thread is finished once something happens, call mcp__fyr__settle_when.
- Fyr tells you what happened on your records in lines that start with [Fyr], one sentence each (a result you asked for, CI failed or passed, a merge, the owner's changes). Read the details and the current state with mcp__fyr__fyr_state instead of polling.
- To act once when something in Fyr becomes true (CI on a change passes, a change lands, a time passes), arm mcp__fyr__watch instead of polling; it can also settle the thread.
- To see or coordinate the user's other sessions, use mcp__fyr__threads, mcp__fyr__thread_read, mcp__fyr__thread_send and mcp__fyr__thread_new (start a separate session for work that should run on its own).
- To show the user something (a screenshot after UI work, a chart, a mockup, a page, a 3D model or scene), call mcp__fyr__show with a path, html or url; it appears in the thread on desktop and phone. Pages run real HTML in a sandbox: WebGL, WASM, ES modules, three.js or model-viewer from cdn.jsdelivr.net, unpkg.com, esm.sh, cdnjs.cloudflare.com or ajax.googleapis.com, and their own files: give path a folder (index.html plus models, textures, scripts) or an HTML file next to its files.`

// ViewPrompt is appended to the system prompt of a thread with a live view.
const ViewPrompt = `This thread has a live view: a small page pinned above the conversation that shows the state of your work, so the user does not have to read the thread to know where things are.
- The view is a page (HTML, rarely rebuilt) plus a small JSON state (at most 16 kB) that you update often. Publish the page once with mcp__fyr__view_publish {html or path, state}; after that change only the state with mcp__fyr__view_state {patch}, a JSON Merge Patch (RFC 7396: keys merge, null deletes a key, arrays are replaced whole).
- State shape: "headline" (one line, the current status; Fyr also shows it in the inbox) plus any fields your page needs, for example "steps", "blockers", "next", "pr".
- In the page, window.fyr.state is the current state; fyr.onState(fn) runs fn now and on every change; an element with data-text="field.path" shows that field. Fragments get the Fyr theme (CSS variables --bg --fg --muted --line --accent, light and dark). The page cannot call Fyr or other hosts; it may load libraries from cdn.jsdelivr.net, unpkg.com, esm.sh, cdnjs.cloudflare.com and ajax.googleapis.com.
- Buttons send messages: fyr.send(text), or an element with data-send="text", sends that text to you as a message from the view, as if the user typed it. Use buttons for choices and next steps; they never run anything by themselves.
- A button can also run an action you declared on the view with mcp__fyr__offer_action {on: "view", id, …}: an element with data-action="<id>", or fyr.run("<id>"). Fyr runs it on the user's press: a script exactly as declared, or a prompt to you or a new thread. Use it for an exact step (run the e2e suite, apply the plan); use data-send for a choice.
- A message marked [Typed on the live view tab] was written while the user looks at the view, not the conversation. Answer it first in one or two plain, spoken sentences (the answer, or what you will do now); that reply is shown next to their message on the View tab. Then do the work and update the view.
- Update the view as the work moves, not only at the end: when you start or finish an item, change the headline and the items that moved. Fyr reminds you after a few minutes without an update.
- Before you end a turn, update the view if anything it shows changed, or call mcp__fyr__view_unchanged {reason}. Keep it short and current; it is the first thing the user reads.`

// FromView marks a message sent by a press in the live view (V0.37).
const FromView = "view"

// FromViewTyped marks a message the user typed on the View tab (V0.49).
const FromViewTyped = "view-typed"

// FromHandover marks the messages Fyr sends during a handover (V0.45).
const FromHandover = "handover"
