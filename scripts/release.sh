#!/usr/bin/env bash
# CI's release step (#158), run in the flake's dev shell after `make test`
# passed on a push to main: cross-compile fyr and fyrd for every target,
# sign the checksums, publish the GitHub Release build-<sha12> and move the
# tag `deployable` to it. Hosts auto-update only to what this publishes.
#
# With FYR_VERSION=vX.Y.Z (V0.versioned-releases: a pushed version tag, or a
# workflow_dispatch with a version) it builds the same commit again with the
# version in the binaries and the signed manifest and publishes the release
# vX.Y.Z instead, with the commits since the previous version as its notes.
# A version with a -suffix (v1.0.0-rc.1) is a prerelease: the stable channel
# passes it over. `deployable` and the build releases are left alone.
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
version="${FYR_VERSION:-}"
if [ -n "$version" ]; then
  if ! [[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]]; then
    say "$version is not a version (vX.Y.Z or vX.Y.Z-rc.N)"
    exit 1
  fi
  # A tag that exists names its commit; a dispatch must not move it.
  if at="$(git rev-parse -q --verify "refs/tags/$version^{commit}")" && [ "$at" != "$sha" ]; then
    say "the tag $version is at ${at:0:12}, not $short; publishing nothing"
    exit 1
  fi
fi

# Publish only a commit that main still contains. A re-run of an old run checks out its old SHA, and after a
# history rewrite (#196) that SHA is no longer on main: a release of it would pin the old history again and take
# `deployable` off main's line.
git fetch -q --force origin "refs/heads/main:refs/remotes/origin/main"
if ! git merge-base --is-ancestor "$sha" origin/main; then
  say "$short is not on main (a re-run of an old run, or rewritten history); publishing nothing"
  [ -z "$version" ] || exit 1 # a version of a commit off main is a mistake to see
  exit 0
fi

# The web client is the same for every target; build it once.
make web
export CGO_ENABLED=0
for t in linux-amd64 linux-arm64 darwin-arm64 darwin-amd64; do
  os="${t%-*}" arch="${t#*-}"
  say "building $t"
  # V0.versioned-releases: `fyr --version` and `fyrd --version` print the version, or the build on main.
  ld="-s -w -X main.buildID=$short -X main.version=$version"
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ld" -o "$out/fyrd-$t" ./cmd/fyrd
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ld" -o "$out/fyr-$t" ./cmd/fyr
done
run=""
if [ -n "${GITHUB_RUN_ID:-}" ]; then run="${GITHUB_SERVER_URL:-https://github.com}/$repo/actions/runs/$GITHUB_RUN_ID"; fi
# Built into a folder of its own and run from there, not with `go run`:
# Go 1.24+ runs a `go run` binary from its build cache, and on the
# self-hosted runner that cache lives in a systemd CacheDirectory where an
# entry could not be executed ("fork/exec …-d/fyrrelease: permission
# denied"), which failed the release of main several times on 20 Nov
# (V0.release-tool-built).
tools="$(mktemp -d)"
GOOS= GOARCH= go build -o "$tools/fyrrelease" ./cmd/fyrrelease
"$tools/fyrrelease" pack -commit "$sha" -dir "$out" -src . \
  -go "$(go env GOVERSION)" -node "$(node --version)" -run "$run" -version "$version"

if [ -n "$version" ]; then
  # The notes: the commits since the previous version on this line (for a
  # stable version the previous stable one; prereleases sort before their
  # release).
  pre=""
  [[ "$version" == *-* ]] && pre=1
  prev=""
  while read -r t; do
    [ "$t" = "$version" ] && continue
    [[ "$t" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || continue
    [ -z "$pre" ] && [[ "$t" == *-* ]] && continue
    prev="$t"
    break
  done < <(git -c versionsort.suffix=- tag --merged "$sha" --list 'v*' --sort=-v:refname)
  notes="$(mktemp)"
  range="$sha"
  [ -n "$prev" ] && range="$prev..$sha"
  n="$(git rev-list --no-merges --count "$range")"
  {
    if [ -n "$prev" ]; then echo "Changes since $prev ($n commits):"; else echo "The first version ($n commits; the newest 200 below):"; fi
    echo
    git log --no-merges -n 200 --format='- %s (%h)' "$range"
    [ "$n" -le 200 ] || echo "- … and $((n - 200)) more"
    echo
    echo "Built from $sha, signed like every release. Install it with \`scripts/install-linux.sh --version $version\` or \`scripts/install-macos.sh --version $version\` (docs/INSTALL.md)."
  } >"$notes"
  if gh release view "$version" -R "$repo" >/dev/null 2>&1; then
    say "$version exists already (a rerun); leaving it"
    exit 0
  fi
  flags=(--latest)
  [ -n "$pre" ] && flags=(--prerelease --latest=false)
  say "publishing $version (${prev:+since $prev, }$n commits)${pre:+ as a prerelease}"
  gh release create "$version" -R "$repo" --target "$sha" --title "Fyr $version" --notes-file "$notes" "${flags[@]}" "$out"/*
  exit 0
fi

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
