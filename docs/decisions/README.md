# Decision records

One file per decision: context, options, decision, consequences. A decision that changes the spec names the spec section; the spec links back. Discussion happens in a `type:decision` issue; the file is the result.

The owner decisions D1–D5 from the design phase stay in `../SPEC.md` 0.3. Records here start at 0001.

| # | Decision | Status |
|---|---|---|
| [0001](0001-spec-plan-status.md) | Spec in the repo, plan and status in GitHub issues, red/green per change | accepted |
| [0002](0002-v0-transport.md) | v0 uses HTTP + JSON + SSE on localhost; ConnectRPC and tsnet come in M0 | accepted |
| [0003](0003-fake-agent.md) | Tests drive a scripted fake `claude` over the real protocol | accepted |
| [0004](0004-t3-adoption.md) | Adopt open T3 threads by Claude session id, history from the session transcript | accepted |
| [0005](0005-android-pwa-first.md) | Phone client: PWA first, no APK; adb + Obtainium if an APK is needed | accepted |
| [0006](0006-rail-direction.md) | The v0 client uses the Rail direction | accepted |
| [0007](0007-nix-integration.md) | NixOS owns the unit, profiles, launcher and a seed build; dev builds live outside the store | proposed |
| [0009](0009-message-delivery.md) | Message delivery follows the CLI's own queue: `command_lifecycle` receipts, `cancel_async_message` | accepted |
