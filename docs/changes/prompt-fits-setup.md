# V0.prompt-fits-setup: the agent's instructions fit the host and the folder it starts in, and subagents get the key rules

→ V0.12, V0.149 (4), V0.136, decision 0035. *Amends V0.12 (the Fyr block is no longer one fixed text).*

Every agent got the same block: the merge and gh rate-limit advice in a repository without GitHub, pkexec on a Mac, and nothing when gh was not signed in. A merge ask on a change without a pull request failed with "args.pr must be a GitHub pull request URL", which does not say that there is no GitHub here. The appended system prompt reaches only the main agent, so a subagent saw the tool descriptions and one line of MCP instructions.

(1) **Facts at start.** When an agent's process starts, Fyr reads the setup of its folder: whether it is in a git repository, whether one of its remotes is on github.com, whether gh is signed in on this host (`gh auth token`, no call to GitHub, kept 5 min), the OS and whether pkexec is installed. The Fyr block is built from these facts.

(2) **What is left out.** In a git repository without a GitHub remote the merge, gh rate-limit and "PR opened another way" advice is left out, and one sentence says there are no pull requests or CI for Fyr and that the owner lands the branch. A folder outside git says so and keeps the GitHub advice only while gh is signed in (the agent may work in GitHub repositories below it). Without pkexec the root advice says the card shows the owner the script to run as root themselves. On macOS a line says the shell tools are BSD's; another OS is named. On a Linux host with pkexec in a GitHub repository the block is the same text as before.

(3) **Merge ask without a PR.** A merge ask on a change with no pull request is refused with why and what to do instead: a repository with no GitHub remote cannot be merged by Fyr (the agent says the work is done and the owner lands the branch; never merge it yourself); a GitHub one needs its PR first (push, `gh pr create`, ask again).

(4) **gh not signed in.** An agent that starts in a GitHub repository while gh is not signed in (or not installed) gets one [Fyr] line at its next tool call: Fyr cannot read its pull requests or CI or merge them, its own gh calls fail, and it should ask the owner to run `gh auth login`. The line is said once while it stays the same (input `agent.setup`, notice rule `setup`).

(5) **Subagents.** The MCP server's `instructions` (the initialize answer) carry the key rules: asks return at once and answer later in a [Fyr] message; never merge a pull request yourself or run sudo; a question or a command for the owner goes on a card; Fyr watches PRs and CI, so do not poll gh.

(6) **The sudo gate** no longer promises pkexec: it says the owner presses Run as root, and a host without pkexec has the owner run it themselves.

Not in this change: the `gh pr merge` gate's wording and the tool vocabulary (#451).
