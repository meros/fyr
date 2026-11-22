# V0.fyrd-wire: host.serve is a list of wire steps, and fyrd's clocks are named

No behaviour change. `host.serve` (cmd/fyrd/main.go, 640 lines) is now the setup of the context and signals, the shutdown, and six steps on a `wiring` value in `cmd/fyrd/wire.go`, run in the old order: `wireQuota`, `wireAgents`, `wireProjects`, `wireHeavy` (slot pool, heavy commands, cgroup jobs, sampler), `wireGuard` (the engine's slot, undeclared-work and rerun hooks), `wireDeploy`, `wireBackground` (GitHub, pollers, watchdog, PSI trigger, tick) and `wireListeners`. Each step reads what the earlier ones stored on `wiring`. The eighteen hard-coded durations are named in `cmd/fyrd/timings.go`; they are consts, not config keys.

Not done: `os.Hostname` is still read in `server.Server.hostName` and `engine.hostname` as well as in fyrd. The server's default is the machine's name, not the configured `name`, so injecting fyrd's value would change what the rail shows when a name is configured.

Tests: the existing `cmd/fyrd` tests and `selftest` build the same wiring.
