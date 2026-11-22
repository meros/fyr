# V0.owner-without-tailnet: owner identity comes from config or a first-run step, never from tailnet.Self, and docs describe the tailnet as one optional route

→ V0.14, V0.158, V0.159, V0.175, decision 0026, decision 0028, decision 0030 (3.4, 3.6), #197. *Amends V0.14 (config `ownerLogin` no longer falls back to `tailnet.Self`) and V0.159 (3) (an untagged install no longer writes `tailnet.Self` into the roster automatically).*

(1) **No fallback to `tailnet.Self`.** `fyrd` never takes the owner's Tailscale login from `tailnet.Self` at startup (`cmd/fyrd/config.go` and `internal/peerwire`). Without Tailscale, no owner login is needed at all (local browser via `owner.sock`/`fyr open`, LAN mDNS + `fyr pair`, LAN HTTPS via `tls` + `fyr device add`). With Tailscale present, `tailnet.Self` is read only for this node's MagicDNS address hint, never for identity.

(2) **Config or first-run step.** When using the optional Tailscale route (Tailscale Serve sign-in, condition 1 automatic pairing, or `wellKnownListen`), the Tailscale owner login comes from:
- config `ownerLogin` (or `-owner-login` / NixOS `services.fyr.ownerLogin`), which is also recorded in the roster so paired installs learn it by sync, and always wins over the roster;
- the first-run / owner-socket CLI step `fyr auth owner <login>` (`GET`/`POST /auth/owner` on `owner.sock`), which stores a signed `owner-login` statement in `<data>/roster.json` so it survives restarts and spreads to paired installs; or
- roster sync from a paired install (as tagged nodes already did).

Existing installs that ran V0.159+ already carry their `owner-login` in `<data>/roster.json`, so upgrading keeps Tailscale Serve working without lockout. `Server.OwnerFn`, `peer.Env.OwnerLogin`, and `peer.OnlyWellKnown` read the owner dynamically so setting it via `fyr auth owner` takes effect without a restart.

(3) **Quiet start without `tailscaled`.** When `tailscaled` is absent, `fyrd` logs one line saying the tailnet is off (`tailnet: off (tailscaled not running); serving without a tailnet`), with no raw socket dial error and no spurious tagged-node message.

(4) **Docs.** `README.md`, `docs/INSTALL.md`, `docs/API-v0.md`, `nix/module.nix`, and the install scripts describe Tailscale as one optional route alongside single-machine and plain-LAN setups, and document `ownerLogin` and `fyr auth owner`.

*Tests:* `cmd/fyrd` (`TestOverrideDoesNotFallBackToTailnetSelf`), `internal/peerwire` (`TestOwnerLoginComesFromConfigOrOwnerRouteNotTailnetSelf`), `internal/peer` (`TestCondition1`, `TestOnlyWellKnownDynamicOwner`), `internal/server` (`TestServerOwnerFnDynamic`), `cmd/fyr` (`TestAuthOwnerCommand`), `internal/authtest` (`TestNoFallbackToTailnetSelfAndFirstRunOwnerStep`, `TestWithoutTailscaledStartsQuietlyWithSingleTailnetLine`, `TestNoAutoPairWithoutConfiguredOwnerLogin`).
