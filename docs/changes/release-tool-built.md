# V0.release-tool-built: the release step builds its packing tool and runs it from a temporary folder, not with `go run`

→ V0.ci-self-hosted (the runner), V0.versioned-releases (`scripts/release.sh`).

(1) **Why.** Since Go 1.24, `go run` keeps the binary it builds in the build cache and runs it from there (`<GOCACHE>/xx/<hash>-d/<name>`). On the self-hosted runner the build cache sits in the service's systemd `CacheDirectory`, and on 20 Nov the release of main failed several times at `fork/exec …-d/fyrrelease: permission denied`, after every test had passed. The same cache entry failed twice in a row, so a rerun did not help, and no release reached the hosts, which therefore did not update.

(2) **The change.** `release.sh` builds `cmd/fyrrelease` with `go build -o` into a `mktemp -d` folder and runs it from there. Nothing the release runs is executed from the build cache any more. The cross-compiled `fyr` and `fyrd` were already built with `go build -o`.

(3) **Not yet known:** why that cache entry cannot be executed (the file's owner or mode after the runner's dynamic user changed is the first suspect). That is the runner's configuration, outside this repository; the release no longer depends on it.
