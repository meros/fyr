# V0.server-typed: the server's optional calls return the types they marshal, and the engine and server share one Providers

No behaviour change. `server.Server.Quota`, `UpdateState` and `Setup` return `[]quota.Status`, `deploy.HostUpdate` and `setup.Report` instead of `any`, so the compiler checks what fyrd hands over. `provider.Service` (`Run`, `List() []DTO`, `Found() []Found`) is the one interface for the provider calls: `*provider.Registry` implements it, `server.Providers` is it, and `engine.Providers` is its `provider.Runner` part. fyrd's `providerAPI` adapter and the front test's copy of it are gone. `Updates` stays a map: its keys are a wire shape the fronts read, not a type of its own.

The `PeerAuth`, `PeerGate` and `PeerNames` fields the review named no longer exist in `server.Server`; nothing to merge.

Tests: the existing server and front tests (the fakes return typed values).
