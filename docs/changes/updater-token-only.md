# V0.updater-token-only: github.env's token is the updater's alone, so gh's own login works, and a failed gh login says why

→ V0.168, V0.release-without-gh. `~/.config/fyr/github.env` holds a token that may only read the release repo (V0.168 (2)). The unit loads it into fyrd's environment, and every gh fyrd and its agents ran inherited it: gh prefers `GH_TOKEN` over its stored login, so Setup called the host "signed in (missing repo scope)" and offered "Sign in with repo scope", and that button ran `gh auth login`, which refuses to run while `GH_TOKEN` is set. The card showed only "exit status 1", and the login could never work on such a host.

1. **Taken out at start.** fyrd reads `GH_TOKEN` (else `GITHUB_TOKEN`) once at start, before it starts anything, and unsets both. The updater sends that token to GitHub's API as before (V0.release-without-gh (2)); without one it uses `gh auth token`, as before. gh in Setup, the pollers and the agents use gh's own login: a host whose github.env has a token and whose gh has no login now shows GitHub as not signed in, and "Sign in" works.
2. **A failed login says why.** The Setup card's error is gh's exit status and the last line gh printed.
3. **Not changed:** the unit and the Mac's launchd job still load github.env; an agent that needs a token signs gh in with it (`gh auth login --with-token`).

Tests: `internal/deploy` TestTokenFromEnv (TakeEnvToken returns GH_TOKEN over GITHUB_TOKEN and leaves neither in the environment), `internal/setup` TestGHLoginFailureSaysWhy.
