# V0.layout-e2e-load: the layout e2e no longer fails a healthy run under load

→ V0.ci-self-hosted; `web/test/layout.e2e.mjs`. Three runs of `scripts/ci-local.sh` on 25 Nov, beside other sessions' builds, failed only here; `make web-layout` alone passed every time (7877 checks).

(1) **A wall-time budget on the whole run.** The watchdog fired 420 s after the start. A run beside a build and the Go tests took 401 s for its jobs alone, so the next one passed the limit with nothing wrong. The watchdog is now a hang guard in its own sense: it fires when no job has ended for 240 s, and each job that ends re-arms it.

(2) **A single key press that stands in for a state.** "round 2 draft desktop" closed the New thread dialog, sent the key `c` once, and waited for the dialog. Under load the app's key handler may not be bound again right after the close, so the press was lost and the line never came. The wait now sends the key again each poll until the dialog is there.

Before calling this a flake the test was run alone (twice, passing) and under load (the gate's runs, failing); the two causes above are the root, not a retry.
