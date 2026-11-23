# V0.runner-lookpath: the runner finds the agent's CLI on the PATH it is given

→ V0.linux-without-nix (the agents' PATH), decision 0023 (a runner holds the agent processes), issue #30.

Found on a fresh Ubuntu 24.04 install during the #30 check: the Claude login through Fyr worked, but the first thread failed with `Could not start the agent: exec: "claude": executable file not found in $PATH`.

(1) **Cause.** Claude Code's documented installer puts `claude` in `~/.local/bin`. fyrd puts that folder on the agents' PATH (V0.linux-without-nix) and hands the runner the whole environment. But the runner lives in the launcher, which keeps the systemd unit's minimal PATH. `exec.Command` looks a bare name up on the *calling* process's PATH, not on the PATH in `cmd.Env`, so the runner looked for `claude` on the unit's PATH and did not find it. On NixOS and on the owner's Mac the unit's PATH happens to hold the CLI, which is why it never showed there.

(2) **Fix.** Both places that start an agent process (`runner/server.go` for the launcher's runner, `runner/local.go` for a fyrd without one) resolve a bare binary name on the PATH of the environment the process will run with. An absolute path is kept as it is. A name found nowhere is reported by exec as before.

(3) **Tests.** `lookpath_test.go`: the runner's own PATH lacks the CLI while the given PATH has it, both for `command` and `StartLocal`; an absolute path is unchanged; a missing name is still an error.
