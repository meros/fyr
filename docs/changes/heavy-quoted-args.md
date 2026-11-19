# V0.heavy-quoted-args: the heavy-job gate reads no command inside a single-quoted argument, and tells a subagent not to retry a queued job

→ V0.209 (the gate), decision 0029 S1 (the patterns). (owner, 19 Nov: the session retro of NorbanListingCollector #946 to #953, "Fyr: heavy-job matching" and "Fyr: subagent queue text".)

(1) **Single-quoted text is an argument.** The shell runs nothing inside single quotes, so a `|`, a backtick or a `;` there starts no command. `segments` blanks the text of each single-quoted argument before it splits, after the heredoc bodies are dropped. A script after `-c` (`sh -c '…'`, `bash -lc '…'`, `nix develop -c '…'`) stays a command line. A `'` inside double quotes is no quote. On 19 Nov two edits of a PR body file, `sed -i 's/- \`make check SHELL_RUN=\`: …/' body.md`, were queued as `make check`, and the agents worked around them with 5 calls.

(2) **A subagent is told not to retry.** The hook input names an `agent_id` when a subagent makes the call. The admission notice goes to the thread's own agent, and a subagent ends by handing back, so "Fyr tells you … run the command again then" left it nothing to wait for: on 19 Nov one sent the same queued check four times. For a subagent the refusal now says not to run the command again, to report it as queued and not run, and to hand back when the rest is done.
