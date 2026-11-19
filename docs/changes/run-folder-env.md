# V0.run-folder-env: a Run card runs in its folder's environment, through the folder's direnv

→ V0.149, V0.60. (owner, 19 Nov, #355: "Fix it now".)

(1) **What happened.** On 19 Nov a `run_request` script with `gcloud …` exited 127. fyrd's environment (a service's short PATH) has no `gcloud`, and once the agent wrote the full path, gcloud found no account: `CLOUDSDK_CONFIG` was not set. The agent's own Bash in that folder has both from the workspace's `.envrc` and `shell.nix`.

(2) **The change.** A script press that is not root (a Run card, a script action on a change, the thread or its view) runs as `direnv exec <dir> <argv>` when the folder or a folder above it has an `.envrc` and `direnv status --json` in the folder says the owner allowed it. direnv is taken from PATH, else from the usual profiles (`/etc/profiles/per-user/<user>/bin`, `~/.nix-profile/bin`, `/run/current-system/sw/bin`, Nix's default profile, Homebrew). Without direnv, without an `.envrc`, or with one that is not allowed or is denied, the script runs as before. Fyr never allows an `.envrc` itself. A root run (pkexec) keeps its explicit PATH, as before.

Tests: `internal/engine/folderenv_test.go` (TestFolderArgvWrapsOnlyAnAllowedEnvrc; TestFolderArgvRealDirenv with a real direnv and its own allow list).
