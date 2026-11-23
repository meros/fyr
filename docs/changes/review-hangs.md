# V0.review-hangs: an agent never inherits fyrd's credentials for its CLI, and nine calls that could hang or leak are bounded

→ V0.agent-env, V0.38. *Amends V0.agent-env (1) and (2): the list of credential variables is per adapter.*

Why: a review of main found that the agy adapter started its processes with fyrd's whole environment minus the launcher's variables, so a cli-login or no-auth agy provider ran with whatever `GEMINI_API_KEY`, `GOOGLE_GEMINI_BASE_URL` or `AGY_ADC_AUTH` fyrd was started with: the problem V0.agent-env fixed for claude. It also found calls without a bound (a push service, `security`, Chrome's closeTarget), a client built per call to tailscaled on every request, a download that could be cut short silently, and a worker that made up results after its process had exited.

(1) **Credentials per adapter.** `cli.Caps.CredentialVars` names the variables that say who a CLI talks to and as whom: `claude.CredentialVars` (the list V0.agent-env named, moved from `cli`) and `agycli.CredentialVars` (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_GEMINI_BASE_URL`, `AGY_ADC_AUTH`). `launchenv.AgentEnv(credentials, extra...)` is every agent launch's environment: fyrd's own without the launcher's variables and those credentials, then the adapter's. `claude.Env` is it with claude's list and `CLAUDE_CODE_ENTRYPOINT=sdk-go` (it replaces `launchenv.ClaudeEnv`); the agy session, print and model-list runs use it with agy's list, and a CLI login starts from it with the adapter's list. A profile's own env still sets any of them.

(2) **One tailscaled client per socket.** `tailnet` keeps one `http.Client` per socket path (it decides per connection between the socket and the macOS app's port, so it follows tailscaled), and its five calls go through `localGet(ctx, socket, path, &v)`: the `Sec-Tailscale` header, the status check and a bounded decode in one place. `Self` and `IPv4` take a context.

(3) **Push.** Web Push uses one shared client with a timeout (webpush-go's default has none), each send is bounded to 15 s, the answer drain is capped at 64 KiB, and `Notify` sends to up to four push services at once, so one that hangs holds back none of the other devices.

(4) **Keychain.** `/usr/bin/security` runs through `proc.Command` with a 3 s context.

(5) **gh output.** `github.Gh` and `GhIn` share `runGh`, which returns gh's stdout only; stderr (an update notice) goes into the error, so it no longer corrupts the JSON `fromView` reads or the HTTP head `splitHTTP` reads.

(6) **gh download.** The release archive and the binary in it are read to a cap and refused when larger, not cut; the archive is checked against the release's `checksums.txt`, and a release without one, or that does not list the asset, is refused.

(7) **Setup's commands.** `run` uses `proc.Command` (a timeout kills the whole group) and gives git `proc.GitEnv`; `start` reaps what it started; `gh auth login` runs under `proc.Command` (WaitDelay) and its output reader keeps draining after a line too long for the scanner.

(8) **Preview.** Closing a shot's tab is bounded to 5 s (it held the shot lock, so a Chrome that never answered blocked every later shot); a call that got no answer removes its pending entry; a failed start closes its pipes.

(9) **agy process exit.** The worker selected on `done`, which `read()` closes only after the worker has returned, so those cases never fired: on exit the worker emitted an error result for the running prompt and went on to start the queued ones. It now stops once `readDone` is closed, without a result; `read()` closes Out and reports Err.
