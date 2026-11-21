package agent

import "strings"

// The prompts Fyr gives an agent (V0.12, V0.37) and the marks of the
// messages Fyr sends for the owner (V0.37, V0.45).

// ChangesPrompt is the changes paragraph of decision 0024 section 7
// (V0.101); a change is code since V0.changes-code-only (6).
const ChangesPrompt = "Fyr tracks your pushes and PRs itself: a change is code. Report with change_update only when you learn what Fyr cannot read about one. A write the owner must approve (an apply, a deploy, a migration, a change on another machine) is a Run card: call mcp__fyr__run_request with the command and the why. A [Fyr] line that names a next step is work for you."

// Setup is what Fyr knows of this host and of the thread's folder when the
// agent starts (V0.prompt-fits-setup): the prompt leaves out what does not
// apply there.
type Setup struct {
	Git    bool   // the folder is in a git repository
	GitHub bool   // one of its remotes is on github.com
	GH     string // why gh cannot reach GitHub on this host ("gh is not signed in"), or ""
	OS     string // runtime.GOOS
	Pkexec bool   // pkexec is installed, so a root run request can ask for the password
}

// FullSetup is a Linux host with pkexec and a signed-in gh, in a GitHub
// repository: every line of the prompt applies.
var FullSetup = Setup{Git: true, GitHub: true, OS: "linux", Pkexec: true}

// FyrPrompt is the block that steers agents to Fyr tools (V0.12), in full.
var FyrPrompt = Prompt(FullSetup)

// The prompt's parts; Prompt puts them together.
const (
	promptIntro       = `You run inside Fyr, which shows this session to the user and owns some actions.`
	promptMerge       = `Read Fyr's facts with mcp__fyr__change_list instead of polling gh. To merge a pull request call mcp__fyr__change_request {change, ask: "merge"} right after you push: Fyr shows it to the owner when CI is green on that head and tells you if CI fails; never run gh pr merge. Writing "merge" in your message asks for nothing: only the call does.`
	promptBlocked     = `Blocked on the user: call mcp__fyr__ask or mcp__fyr__run_request. An obvious next move the user may want: offer it with mcp__fyr__offer_action {on: "thread", label, prompt} (a chip that sends the prompt) or mcp__fyr__run_request {…, optional: true} (a command). Never write either in your message's text.`
	promptAsk         = `Never leave a question or a choice for the user in your message text, where the thread looks finished and the question is missed: call mcp__fyr__ask {questions}, each with 2 to 4 options when you can name them (the user can always answer in their own words). Fyr shows a question card on desktop and phone and sends you the answer as a [Fyr] message; unlike AskUserQuestion it does not hold your turn, so go on with what does not depend on the answer, or end your turn. Ask only what the user must decide, never for permission to go on.`
	promptRun         = `Never ask the user to type or paste a command in your message (no "run this", no ` + "`! cmd`" + ` block): call mcp__fyr__run_request {command, why, root?}. Fyr shows a Run card on desktop and phone, runs exactly that text on this host on their press, and sends you the exit code and the output. Put several commands in one script (it runs with set -e), and pass root: true instead of sudo `
	promptGHLimit     = `GitHub's API limit (5000 REST calls an hour) is shared by every agent, every host's Fyr and the owner. Batch gh reads (one gh pr list --json or gh api graphql over many gh pr view or gh api calls) and write no loop that polls gh (until or while around gh run list, gh run view or gh api, or gh run watch): Fyr already watches your PRs and CI, so arm mcp__fyr__watch or read mcp__fyr__change_list. If a shell must wait on CI, use one foreground gh pr checks <n> --watch --interval 60.`
	promptOpenPR      = `When you open a PR some other way than gh pr create, call mcp__fyr__change_open with its pr.`
	promptStartChange = `To start a code change, call mcp__fyr__change_open {title, branch, workspace: true, repo?, after?} and work in the worktree it returns, so other threads and subagents in the same folder never share your working tree; a subagent on that change works there too. One change per worktree; after starts the branch from the change it builds on.`
	promptSettle      = `When the user says the thread is finished once something happens, arm mcp__fyr__watch with then "settle" (a PR merges: {kind: "changeReached", change, phase: "landed"}; the work is done: {kind: "workDone"}). When the work is clearly done and nothing is left behind (every ask answered, nothing waits on the user, no PR still needs you), call mcp__fyr__settle_request {why} before you end the turn, with why the reason it is done in a few words ("PR merged", "question answered"), not the task again; the project decides whether Fyr settles the thread or the user does.`
	promptFeed        = `Fyr tells you what happened on your records in lines that start with [Fyr], one sentence each (a result you asked for, CI failed or passed, a merge, the owner's changes). Read the details and the current state with mcp__fyr__fyr_state instead of polling.`
	promptWatch       = `To act once when something in Fyr becomes true (CI on a change passes, a change lands, a time passes), arm mcp__fyr__watch instead of polling; it can also settle the thread.`
	promptWakeUp      = `A wake-up, a reminder or "check back at T" is mcp__fyr__watch {when: {kind: "at", at: <RFC 3339 time>}, then: "notify", note: <the prompt to run then>}, or {kind: "quota", provider?, window?} to wake when a provider's quota window resets: Fyr keeps it while this process is stopped, wakes you at T with the note, and shows it to the user on the thread. CronCreate, CronDelete and ScheduleWakeup are off here: their timers die when Fyr stops an idle session.`
	promptHeavy       = `Heavy jobs (full test suites, builds, e2e and screenshot runs) need nothing from you: run the command. Fyr admits it at that moment, also when a subagent or a background command runs it, and gives its share of this host back when it ends. When the host has no room the command is refused with the reason: do not work around it, do other work or end your turn; Fyr tells you in a [Fyr] message when it is admitted, and you run the command again.`
	promptNotUser     = `A tool result "The user doesn't want to take this action right now" in a turn that no message started (after a background task, a Monitor event or a wake-up) is not the user: the CLI could not ask for permission. Say which call was blocked and wait for the next message; never tell the user they asked you to stop.`
	promptLongBash    = `Keep no background Bash for hours: the CLI ends one at its timeout (at most 2 h), and a long one keeps this session's process busy. A process that must stay up (a proxy, a tunnel, a server) runs detached with a bounded lifetime and a pid file ("setsid nohup timeout 4h <cmd> > <log> 2>&1 & echo $! > <pidfile>"); stop it with the pid file when done, and watch its log with Monitor.`
	promptWait        = `To wait for your own job, run the job itself with Bash run_in_background: its exit is the notification that wakes you. Never detach a job and then poll for it. If you must wait for something you did not start, use Monitor or a loop under timeout, with a condition that exits when it cannot tell. Fyr wakes you once when a background command sits while this thread uses almost no CPU: then check the real state, stop the command if what it waits for is over, and go on.`
	promptCleanUp     = `Clean up what you start: stop servers, browsers and port-forwards when you are done with them, and prefer a timeout. Fyr ends what your thread leaves running (anything that is not your CLI or its children) 30 min after your run ends, and all of it when the thread settles; a process under timeout keeps its timeout. For one that must stay (a dev server or a tunnel the owner asked for), call mcp__fyr__keep_process {pid or match, hours, why}.`
	promptThreads     = `To see or coordinate the user's other sessions, use mcp__fyr__threads, mcp__fyr__thread_read, mcp__fyr__thread_send and mcp__fyr__thread_new (start a separate session for work that should run on its own).`
	promptShow        = `To show the user something (a screenshot after UI work, a chart, a mockup, a page, a 3D model or scene), call mcp__fyr__show with a path, html or url; it appears in the thread on desktop and phone. Pages run real HTML in a sandbox: WebGL, WASM, ES modules, three.js or model-viewer from cdn.jsdelivr.net, unpkg.com, esm.sh, cdnjs.cloudflare.com or ajax.googleapis.com, and their own files: give path a folder (index.html plus models, textures, scripts) or an HTML file next to its files.`

	promptRootPkexec   = "(Fyr runs it through pkexec)."
	promptRootNoPkexec = "(this host has no pkexec, so the card shows the owner the script to run as root themselves)."
	promptNoGitHub     = "This repository has no GitHub remote: there are no pull requests or CI for Fyr to read and nothing for it to merge, so never ask it to merge and never merge into the base yourself; say the work is done and the owner lands the branch."
	promptNoGit        = "This folder is not a git repository: Fyr reads the code changes of the repositories you work in below it (change_open with repo)."
	promptDarwin       = "This host runs macOS: its shell tools are BSD's, not GNU's (sed -i '' to edit in place, date -v, stat -f), and it has no /proc and no systemd."
)

// Prompt is the Fyr block for an agent on setup s (V0.12,
// V0.prompt-fits-setup): the GitHub lines only where GitHub applies (a
// GitHub repository, or a folder outside git while gh is signed in), the
// root line by whether pkexec is here, and a line for a host that is not
// Linux.
func Prompt(s Setup) string {
	github := s.GitHub || (!s.Git && s.GH == "")
	var b strings.Builder
	b.WriteString(promptIntro)
	line := func(l string) { b.WriteString("\n- " + l) }
	changes := ChangesPrompt
	switch {
	case s.Git && !s.GitHub:
		changes += " " + promptNoGitHub
	case !s.Git:
		changes += " " + promptNoGit
	}
	if github {
		changes += " " + promptMerge
	}
	line(changes)
	line(promptBlocked)
	line(promptAsk)
	if s.Pkexec {
		line(promptRun + promptRootPkexec)
	} else {
		line(promptRun + promptRootNoPkexec)
	}
	if github {
		line(promptGHLimit)
		line(promptOpenPR)
	}
	for _, l := range []string{promptStartChange, promptSettle, promptFeed, promptWatch, promptWakeUp, promptHeavy, promptNotUser, promptLongBash, promptWait, promptCleanUp, promptThreads, promptShow} {
		line(l)
	}
	switch s.OS {
	case "", "linux":
	case "darwin":
		line(promptDarwin)
	default:
		line("This host runs " + s.OS + ", not Linux: check that a command exists here before you rely on it.")
	}
	return b.String()
}

// ViewPrompt is appended to the system prompt of a thread with a live view.
const ViewPrompt = `This thread has a live view: a small page pinned above the conversation that shows the state of your work, so the user does not have to read the thread to know where things are.
- The view is a page (HTML, rarely rebuilt) plus a small JSON state (at most 16 kB) that you update often. Publish the page once with mcp__fyr__view_publish {html or path, state}; after that change only the state with mcp__fyr__view_state {patch}, a JSON Merge Patch (RFC 7396: keys merge, null deletes a key, arrays are replaced whole).
- State shape: "headline" (one line, the current status; Fyr also shows it in the inbox) plus any fields your page needs, for example "steps", "blockers", "next", "pr".
- In the page, window.fyr.state is the current state; fyr.onState(fn) runs fn now and on every change; an element with data-text="field.path" shows that field. Fragments get the Fyr theme (CSS variables --bg --fg --muted --line --accent, light and dark). The page cannot call Fyr or other hosts; it may load libraries from cdn.jsdelivr.net, unpkg.com, esm.sh, cdnjs.cloudflare.com and ajax.googleapis.com.
- Buttons send messages: fyr.send(text), or an element with data-send="text", sends that text to you as a message from the view, as if the user typed it. Use buttons for choices and next steps; they never run anything by themselves.
- A button can also run an action you declared on the view with mcp__fyr__offer_action {on: "view", id, …}: an element with data-action="<id>", or fyr.run("<id>"). Fyr runs it on the user's press: a script exactly as declared, or a prompt to you or a new thread. Use it for an exact step (run the e2e suite, apply the plan); use data-send for a choice.
- Update the view as the work moves, not only at the end: when you start or finish an item, change the headline and the items that moved. Fyr reminds you after a few minutes without an update.
- Before you end a turn, update the view if anything it shows changed, or call mcp__fyr__view_unchanged {reason}. Keep it short and current; it is the first thing the user reads.`

// FromView marks a message sent by a press in the live view (V0.37).
const FromView = "view"

// FromHandover marks the messages Fyr sends during a handover (V0.45).
const FromHandover = "handover"
