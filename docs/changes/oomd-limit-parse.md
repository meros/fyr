# V0.oomd-limit-parse: fyrd reads oomd's limit in the form systemd 262 prints it, so the near-limit warning of V0.oomd-guard works

→ V0.oomd-guard (3), decision 0036 (B).

(1) **The cause.** fyrd reads oomd's limit with `systemctl show --value -p ManagedOOMMemoryPressure -p ManagedOOMMemoryPressureLimit <unit>` and expected a percentage on the second line (`70%`). systemd 262 prints the raw fixed-point value instead, where 2^32 is 100 %: on the host, for `fyr-agents.slice` with `ManagedOOMMemoryPressureLimit=85%` in its unit file, it prints `kill` and `3650722201` (3650722201 / 2^32 = 0.85). The parser took that for out of range and returned 0, so `GET /api/load` never had `oomdLimit`, the start log never said `cgroups: oomd kills under …`, and `daemon.log` has never had a `guard: near oomd's limit` line.

(2) **The fix.** A plain number is the fixed-point form: the limit is value / 2^32 × 100, rounded to 0.01 (85 here). A value with `%` is read as a percentage as before.

(3) **A rule without a limit of its own.** `ManagedOOMMemoryPressure=kill` with limit 0 means oomd applies its `DefaultMemoryPressureLimit` from `oomd.conf` (60 % unless set). fyrd leaves that limit unknown: no `oomdLimit`, no warning. Reading it would mean parsing `oomd.conf` and its drop-ins, or `oomctl dump`, for a case Fyr's own setups do not make: the NixOS module and docs/HOST-SETUP.md always set the limit on the agents' slice.

Tests: `internal/cgroup/oomd_test.go` (TestOomdLimitParsesSystemctl, with the host's verbatim output).
