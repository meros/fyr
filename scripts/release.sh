#!/usr/bin/env bash
# CI's release step (#158), run in the flake's dev shell after `make test`
# passed on a push to main: cross-compile fyr and fyrd for every target,
# sign the checksums, publish the GitHub Release build-<sha12> and move the
# tag `deployable` to it. Hosts auto-update only to what this publishes.
#
# Needs: FYR_RELEASE_KEY (the signing key), GH_TOKEN (contents: write),
# GITHUB_SHA, and a checkout with the tags fetched.
set -euo pipefail

sha="${GITHUB_SHA:-$(git rev-parse HEAD)}"
short="${sha:0:12}"
tag="build-$short"
repo="${GITHUB_REPOSITORY:-meros/fyr}"
out="$(mktemp -d)"
say() { echo "release: $*"; }

# The web client is the same for every target; build it once.
make web
export CGO_ENABLED=0
for t in linux-amd64 linux-arm64 darwin-arm64 darwin-amd64; do
  os="${t%-*}" arch="${t#*-}"
  say "building $t"
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.buildID=$short" -o "$out/fyrd-$t" ./cmd/fyrd
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w" -o "$out/fyr-$t" ./cmd/fyr
done
run=""
if [ -n "${GITHUB_RUN_ID:-}" ]; then run="${GITHUB_SERVER_URL:-https://github.com}/$repo/actions/runs/$GITHUB_RUN_ID"; fi
go run ./cmd/fyrrelease pack -commit "$sha" -dir "$out" -src . \
  -go "$(go env GOVERSION)" -node "$(node --version)" -run "$run"

if gh release view "$tag" -R "$repo" >/dev/null 2>&1; then
  say "$tag exists already (a rerun); leaving it"
else
  say "publishing $tag"
  gh release create "$tag" -R "$repo" --target "$sha" --title "Build $short" \
    --notes "CI build of $sha. Hosts install it when \`deployable\` points here." --latest=false "$out"/*
fi

# deployable only moves forward: a slower run of an older commit must not
# take it back. A move back is a person's act (git push -f origin <sha>:refs/tags/deployable).
git fetch -q --force origin "refs/tags/deployable:refs/tags/deployable" 2>/dev/null || true
if cur="$(git rev-parse -q --verify 'refs/tags/deployable^{commit}')"; then
  if [ "$cur" = "$sha" ]; then
    say "deployable is at $short already"; exit 0
  fi
  if ! git merge-base --is-ancestor "$cur" "$sha"; then
    say "deployable (${cur:0:12}) is not an ancestor of $short; not moving it"; exit 0
  fi
fi
git tag -f deployable "$sha"
git push -f origin refs/tags/deployable
say "deployable → $short"

# Keep the newest 30 build releases.
gh release list -R "$repo" --limit 200 --json tagName,createdAt \
  -q '[.[] | select(.tagName | startswith("build-"))] | sort_by(.createdAt) | reverse | .[30:] | .[].tagName' |
  while read -r old; do
    say "deleting old release $old"
    gh release delete "$old" -R "$repo" --yes --cleanup-tag || true
  done
