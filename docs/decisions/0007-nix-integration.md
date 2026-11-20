# 0007 NixOS owns the frame, the launcher owns the builds

Date: 2026-11-14 · Status: accepted · Spec: 12.6, 0.5 V0.13 · Issue: #10

**Context.** The owner's machines are NixOS (`~/nixos-config`, flake with home-manager). T3 runs there as `modules/nixos/services/t3code.nix`: a system unit as the owner's user, `restartIfChanged = false`, the agent CLIs on the unit PATH, a delegated cgroup. Fyr must use that config system, but a self-deploy (V0.13) must not need `nixos-rebuild` each time.

**Decision.** Split by how often a part changes.

| Part | Changes | Owned by |
|---|---|---|
| Unit, user, PATH (claude wrappers, git, gh, openssh, nix), env (HOME, TZ, NIX_PATH), cgroup and memory limits, port | rarely | NixOS module `nixosModules.fyr`, imported by `~/nixos-config` through a flake input |
| Profiles (`claude`, `claude-acme`, `claude-deepseek` as store paths), permission mode | rarely | the module writes `/etc/fyr/config.json` from options |
| The launcher `fyr` | rarely | the module (`pkgs.fyr`), in the store |
| A known-good `fyrd` (the **seed**) | when the owner blesses a version | the module (`pkgs.fyr`), in the store |
| Dev builds of `fyrd` and the web client | every self-deploy | the launcher, in `~/.local/share/fyr/builds` |

The launcher runs `current` (a dev build) when it exists, else the seed. Rollback goes current → previous → seed, and the seed is immutable, so there is always a build that starts. A file `~/.local/share/fyr/pin` makes the launcher run the seed only (safe mode, no rebuild).

A self-deploy builds with the repo's own toolchain: `nix develop <dir> --command make build`. The toolchain is pinned by the repo's `flake.lock` and cached in the store, so the system profile is not touched.

`nixos-rebuild` is needed only to change the unit or its options, to update the launcher, or to bless a version (bump the `fyr` input in `~/nixos-config/flake.lock`; the new seed is the new floor). As with T3, a switch does not restart the unit.

**Consequences.** Dev builds are not reproducible store paths; that is the price of a fast loop, and blessing is how a build becomes one. The flake needs `vendorHash` and `npmDepsHash`; CI updates them. v0 runs on `host-a` only (loopback, decision 0002); `host-b` needs the tailnet transport (M0).
