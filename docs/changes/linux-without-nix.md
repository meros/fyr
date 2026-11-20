# V0.linux-without-nix: Fyr installs on a Linux without Nix with scripts/install-linux.sh, and the agents' PATH takes the login shell's

→ #199 (go public), decision 0007, decision 0027, V0.80, V0.agents-unit, V0.psi-trigger-slice. *Amends decision 0007's PATH: the unit's PATH plus the NixOS profile folders.*

(1) **Why.** The only Linux install was the NixOS module: the unit, the agents' slice, the polkit rule and the agents' PATH existed only as Nix. Debian and Ubuntu had nothing but a build from source with no unit, no cgroups and no updates.

(2) **The agents' PATH.** fyrd sets it at start from, in order: the config's new `agentPath` (`~/` is the home folder), the PATH it started with, the PATH of the user's login shell (`$SHELL -l -c`, else the shell in `/etc/passwd`; its own output is skipped by a marker line; 5 s at most, a failure is logged and leaves it out), then the profile folders that exist (`~/.local/bin`, `~/.nix-profile/bin`, `/etc/profiles/per-user/<user>/bin`, `/run/wrappers/bin`, `/run/current-system/sw/bin`). Each folder is kept once, where it first appears, so on NixOS the module's packages still come first and the order there is unchanged; the login shell's folders only add.

(3) **`scripts/install-linux.sh`**, the stand-in for the module as `install-macos.sh` is on a Mac. It runs as the user and refuses root. As the user: it downloads the release `deployable` points at with `gh` (as the updater does until #200), checks `SHA256SUMS`, lays out `builds/<sha12>`, `current`, `seed/fyrd` and `launcher/fyr`, and sets the `cgroups` keys of `~/.config/fyr/config.json` to the module's defaults (other keys kept). As root, through `sudo` with each command printed first: the system unit `fyr.service` with `User=` the user (a system unit, not a user unit, so it starts at boot without a login, the oomd omit holds on a root-owned unit, and polkit can grant the scopes), `fyr-agents.slice`, the polkit rule and the `ExecStartPre` helper of V0.psi-trigger-slice, then daemon-reload, enable and restart. These mirror `nix/module.nix`; a change to one goes to the other.

(4) **The layout follows polkit.** polkit 0.106 or later (JavaScript rules: Debian 12, Ubuntu 24.04) gets V0.agents-unit's scope layout. An older polkit (Ubuntu 22.04's 0.105) or none gets V0.80's delegated unit (`Delegate=cpu memory`, no slice, no rule, no `agentsSlice` in the config).

(5) **Safe to run again; `--print` and `--uninstall`.** A second run installs the current release, rewrites the root files and restarts the unit. `--print` prints every file and command and changes nothing (no download, no sudo). `--uninstall` stops the unit and removes the root files; the data and the config stay.

(6) **Not done here.** The script has not yet run on a fresh Debian or Ubuntu machine; that run is part of v1.0's exit check. Reading releases without a `gh` login is #200.

Tests: `internal/agent/path_test.go` (TestAgentPathNixOS, TestAgentPathLogin, TestAgentPathConfig, TestLoginPathSkipsProfileOutput, TestLoginPathFails, TestPasswdShell); `scripts/install_linux_test.go` (TestInstallLinuxPrintWritesNothing, TestInstallLinuxScopeLayout, TestInstallLinuxUnitLayout); shellcheck of the script.
