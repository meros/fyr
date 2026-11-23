#!/usr/bin/env bash
# Installs Fyr on a Linux without Nix (Debian, Ubuntu) as the systemd system
# unit fyr.service that runs as you: the stand-in for the NixOS module
# (nix/module.nix, decision 0007), as scripts/install-macos.sh is on a Mac
# (V0.linux-without-nix). It builds nothing: it downloads a CI release
# (decision 0027) over HTTPS, with no gh login (V0.release-without-gh),
# checks its SHA256SUMS, makes it the current build and keeps a copy as the
# seed. From then on the running fyrd updates itself to each new release of
# its channel (signed, verified, smoke-tested).
#
#	scripts/install-linux.sh [--print | --uninstall] [--channel stable|main|vX.Y.Z] [--version vX.Y.Z]
#
# V0.versioned-releases: the channel is the config's updateChannel. A new
# install follows `stable` (the newest version release, vX.Y.Z, not a
# prerelease); an existing config keeps its own (none is `main`: every green
# commit of main, the release `deployable` points at). --channel sets it.
# --version installs that version and, without --channel, pins the host to
# it (it stays there until you change the channel).
#
# Run it as the user Fyr runs as, not as root. Your part (the release in
# ~/.local/share/fyr, the cgroups keys in ~/.config/fyr/config.json) runs as
# you. The system part needs root and goes through sudo, each command shown
# before it runs: the unit, the agents' slice fyr-agents.slice, the polkit
# rule that lets you start the agents' scopes in it (and pause and resume
# fyr.service from the Linux app, decision 0038), the helper that hands you
# the slice's memory.pressure, then daemon-reload, enable and restart.
# --print shows every file and command and changes nothing (no download, no
# sudo). Run it again to reinstall and restart; --uninstall stops and removes
# the unit and the root files (the data and the config stay).
set -euo pipefail

mode=install
channel=""      # --channel
want_version="" # --version
usage() {
  echo "usage: $0 [--print | --uninstall] [--channel stable|main|vX.Y.Z] [--version vX.Y.Z]" >&2
  exit 64
}
while [ $# -gt 0 ]; do
  case "$1" in
    --print) mode=print ;;
    --uninstall) mode=uninstall ;;
    --channel | --version)
      [ $# -ge 2 ] || usage
      if [ "$1" = --channel ]; then channel="$2"; else want_version="$2"; fi
      shift
      ;;
    --channel=*) channel="${1#--channel=}" ;;
    --version=*) want_version="${1#--version=}" ;;
    *) usage ;;
  esac
  shift
done
# A version tag: vX.Y.Z, or vX.Y.Z-rc.N for a prerelease.
is_version() { [[ "$1" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]]; }
if [ -n "$want_version" ] && ! is_version "$want_version"; then
  echo "--version $want_version is not a version (vX.Y.Z or vX.Y.Z-rc.N)" >&2
  exit 64
fi
case "$channel" in
  "" | main | stable) ;;
  *) is_version "$channel" || { echo "--channel $channel is not stable, main or a version (vX.Y.Z)" >&2; exit 64; } ;;
esac

if [ "$(id -u)" = 0 ]; then
  echo "run this as the user Fyr runs as, not as root; it asks sudo for the system part" >&2
  exit 1
fi

user="$(id -un)"
group="$(id -gn)"
data="$HOME/.local/share/fyr"
cfg="$HOME/.config/fyr/config.json"
repo="${FYR_REPO:-meros/fyr}"
# The root files. The NixOS module (nix/module.nix) makes the same ones;
# keep the two in step.
unit=/etc/systemd/system/fyr.service
slice=/etc/systemd/system/fyr-agents.slice
rule=/etc/polkit-1/rules.d/50-fyr-agents.rules
helper=/usr/local/lib/fyr/agents-pressure-chown

# root runs one command of the system part with sudo; --print only shows it.
root() {
  echo "  root: $*"
  [ "$mode" = print ] || sudo "$@"
}

if [ "$mode" = uninstall ]; then
  echo "removing the unit and the root files with sudo:"
  root systemctl disable --now fyr.service || true
  root rm -f "$unit" "$slice" "$rule" "$helper"
  root systemctl daemon-reload
  echo "stopped and removed fyr.service; data stays in $data, the config in $cfg"
  exit 0
fi

# warn fails the install and only reports in --print.
warn() {
  echo "$*" >&2
  [ "$mode" = print ] || exit 1
}

[ -d /run/systemd/system ] || warn "systemd is not running here; this install needs it"
[ -f /sys/fs/cgroup/cgroup.controllers ] || warn "no cgroup v2 at /sys/fs/cgroup; Fyr's agents need it (docs/HOST-SETUP.md)"
for t in git curl claude sha256sum; do
  command -v "$t" >/dev/null || warn "missing $t on PATH (git and curl: apt install git curl; claude: https://docs.anthropic.com/claude-code)"
done
# The install and the updates read the public releases without gh
# (V0.release-without-gh); the agents use gh for pull requests.
command -v gh >/dev/null || echo "note: no gh on PATH; the install needs none, but agents need it (logged in) for pull requests" >&2

case "$(uname -m)" in
  x86_64) target=linux-amd64 ;;
  aarch64 | arm64) target=linux-arm64 ;;
  *) echo "unsupported machine: $(uname -m)" >&2; exit 1 ;;
esac

# The layout (V0.agents-unit). A polkit that reads JavaScript rules (0.106
# and later) and a systemd that names the unit to polkit when it starts a
# transient scope (257 and later: Debian 13, Ubuntu 25.04) let fyrd start
# each thread in a scope of fyr-agents.slice, out of fyrd's unit. An older
# polkit (Ubuntu 22.04's 0.105), none, or an older systemd (Debian 12's 252,
# Ubuntu 24.04's 255: the rule sees no unit and cannot grant the scopes,
# V0.fresh-ubuntu-run): the V0.80 layout, a delegated unit that fyrd splits.
layout=unit
pk="$(pkaction --version 2>/dev/null | awk '{print $NF}')" || pk=""
sd="$(systemctl --version 2>/dev/null | awk 'NR == 1 {print $2}')" || sd=""
sd="${sd%%[^0-9]*}"
case "$pk" in
  "" | 0.10[0-5]*) ;;
  *) [ "${sd:-0}" -ge 257 ] && layout=scope ;;
esac
if [ "$layout" = unit ]; then
  case "$pk" in
    "" | 0.10[0-5]*) echo "layout: unit (polkit ${pk:-not installed} reads no JavaScript rules; install polkitd 0.106 or later and run this again for the agents' slice)" ;;
    *) echo "layout: unit (systemd ${sd:-unknown} names no unit to polkit when it starts a scope, so no rule can grant the agents' slice; systemd 257 or later gets it)" ;;
  esac
else
  echo "layout: scope (polkit $pk): fyrd in fyr.service, each thread in fyr-agents.slice"
fi

# The files, made in a folder first so --print can show them.
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

{
  cat <<EOF
# Written by scripts/install-linux.sh (V0.linux-without-nix); the NixOS
# module's fyr.service (nix/module.nix) is the same unit. Run the script
# again rather than editing this.
[Unit]
Description=Fyr host daemon
EOF
  if [ "$layout" = scope ]; then
    echo "Wants=network-online.target fyr-agents.slice"
    echo "After=network-online.target fyr-agents.slice"
  else
    echo "Wants=network-online.target"
    echo "After=network-online.target"
  fi
  cat <<EOF

[Service]
User=$user
Group=$group
WorkingDirectory=$HOME
# GH_TOKEN=... (mode 0600) for a private fork; a public one needs none.
EnvironmentFile=-$HOME/.config/fyr/github.env
# No PATH here: fyrd adds your login shell's PATH and the config's agentPath
# to systemd's default for the agents (V0.linux-without-nix).
ExecStart=$data/launcher/fyr -data $data -seed $data/seed/fyrd
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5
# Agent turns are child processes; SIGINT to the group ends them cleanly.
KillSignal=SIGINT
TimeoutStopSec=30
Nice=10
IOWeight=50
MemoryLow=1G
EOF
  if [ "$layout" = scope ]; then
    cat <<EOF
# V0.agents-unit: the unit holds fyrd and the launcher only, root-owned and
# not delegated, so systemd-oomd and the kernel's OOM killer pass it over.
ManagedOOMPreference=omit
OOMScoreAdjust=-500
# V0.psi-trigger-slice: as root, hand the slice's memory.pressure to $user.
ExecStartPre=+$helper
EOF
  else
    cat <<EOF
# V0.80: systemd hands the subtree to $user; fyrd splits it into server/
# and agents/<thread>/ and sets the budget on agents/.
Delegate=cpu memory
EOF
  fi
  cat <<EOF

[Install]
WantedBy=multi-user.target
EOF
} >"$stage/fyr.service"

cat >"$stage/fyr-agents.slice" <<EOF
# Written by scripts/install-linux.sh: the agents' budget and systemd-oomd's
# rule (V0.agents-unit, V0.agents-budget-share; docs/HOST-SETUP.md). A
# percentage is a share of the RAM, or of the swap for MemorySwapMax.
[Unit]
Description=Fyr's agents (V0.agents-unit)

[Slice]
MemoryHigh=40%
MemoryMax=46%
MemorySwapMax=30%
ManagedOOMMemoryPressure=kill
ManagedOOMMemoryPressureLimit=80%
EOF

cat >"$stage/50-fyr-agents.rules" <<EOF
// Written by scripts/install-linux.sh (V0.agents-unit): $user may start
// Fyr's agents' scopes and set their properties, nothing else: no other
// unit, no stop, no kill.
polkit.addRule(function (action, subject) {
  if (action.id == "org.freedesktop.systemd1.manage-units" && subject.user == "$user") {
    var unit = action.lookup("unit"), verb = action.lookup("verb");
    if (unit && /^fyr-(agent|deploy)-[A-Za-z0-9_.-]+\.scope\$/.test(unit) && (verb == "start" || verb == "set-property")) {
      return polkit.Result.YES;
    }
  }
});
// Decision 0038: the Linux app's Pause and Resume (the module's
// services.fyr.desktop): $user may start, stop and restart fyr.service.
polkit.addRule(function (action, subject) {
  if (action.id == "org.freedesktop.systemd1.manage-units" && subject.user == "$user") {
    var unit = action.lookup("unit"), verb = action.lookup("verb");
    if (unit == "fyr.service" && (verb == "start" || verb == "stop" || verb == "restart")) {
      return polkit.Result.YES;
    }
  }
});
EOF

cat >"$stage/agents-pressure-chown" <<EOF
#!/bin/sh
# Written by scripts/install-linux.sh (V0.psi-trigger-slice): fyr.service runs
# this as root before each start, so fyrd may set its PSI trigger on the
# agents' slice. A failure is logged; fyrd then reads every 10 s.
cg=\$(systemctl show -P ControlGroup fyr-agents.slice)
f=/sys/fs/cgroup\$cg/memory.pressure
if [ -z "\$cg" ] || ! chown $user "\$f"; then
  echo "fyr: could not give $user the PSI trigger on fyr-agents.slice (\$f); the guard reads every 10 s" >&2
fi
exit 0
EOF

# The cgroups keys of the config, as the module writes them (nix/module.nix,
# services.fyr.cgroups defaults). Other keys in the file are kept.
cgroups='{"enable": true, "serverMemoryLow": "1G", "agentsMemoryHigh": "40%", "agentsMemoryMax": "46%", "agentsMemorySwapMax": "30%"'
[ "$layout" = scope ] && cgroups="$cgroups, \"agentsSlice\": \"fyr-agents.slice\""
cgroups="$cgroups}"

# V0.release-without-gh: the release repo over HTTPS, no gh.
base="https://github.com/$repo"
# tag_sha prints the commit of tag $1, an annotated tag's commit first.
tag_sha() {
  local out sha
  out="$(git -c credential.helper= ls-remote --tags "$base.git" "refs/tags/$1")"
  sha="$(awk -v t="refs/tags/$1^{}" '$2 == t {print $1}' <<<"$out")"
  [ -n "$sha" ] || sha="$(awk -v t="refs/tags/$1" '$2 == t {print $1}' <<<"$out")"
  [ "${#sha}" = 40 ] || { echo "no tag $1 in $base" >&2; return 1; }
  echo "$sha"
}
# newest_stable prints the newest version tag vX.Y.Z (no -prerelease) whose
# release is published: a tag whose CI run has not finished has no files yet.
newest_stable() {
  local t
  for t in $(git -c credential.helper= ls-remote --tags "$base.git" 'refs/tags/v*' | awk '{print $2}' |
    sed -n 's|^refs/tags/v\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)$|v\1|p' | sort -t. -k1.2,1nr -k2,2nr -k3,3nr); do
    if curl -fsSL -o /dev/null "$base/releases/download/$t/manifest.json" 2>/dev/null; then
      echo "$t"
      return 0
    fi
  done
  echo "no stable release (vX.Y.Z) in $base yet; --channel main installs main's newest build" >&2
  return 1
}

# V0.versioned-releases: the channel the config gets. --channel; else a
# --version pins; else a new install follows stable; else the config keeps
# its own (none is main).
had_channel=""
[ -s "$cfg" ] && had_channel="$(sed -n 's/.*"updateChannel"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$cfg" | sed -n 1p)"
set_channel="$channel"
[ -n "$set_channel" ] || set_channel="$want_version"
new_stable="" # a new install that follows stable because nothing said otherwise
[ -n "$set_channel" ] || [ -s "$cfg" ] || { set_channel=stable; new_stable=1; }
follow="${set_channel:-${had_channel:-main}}"
# The release to install: --version's, else the channel's.
pick="${want_version:-$follow}"
case "$pick" in
  main) what="the release build-<sha12> of tag deployable (main's newest green commit)" ;;
  stable) what="the newest stable version (vX.Y.Z, not a prerelease)" ;;
  *) what="version $pick" ;;
esac
[ -z "$new_stable" ] || what="$what, or while none is published main's newest build (and follow main)"
if [ -n "$set_channel" ]; then
  echo "channel: $follow (updateChannel $set_channel in $cfg)"
else
  echo "channel: $follow (kept from $cfg)"
fi

if [ "$mode" = print ]; then
  echo
  echo "as $user: download $what in $repo for $target into $data/builds/<sha12>,"
  echo "  by git ls-remote $base.git and curl $base/releases/download/<tag>/ (no gh login),"
  echo "  check SHA256SUMS, link $data/current to it, copy fyrd to $data/seed and fyr to $data/launcher"
  echo "as $user: set \"cgroups\" in $cfg to $cgroups"
  [ -z "$set_channel" ] || echo "as $user: set \"updateChannel\" in $cfg to \"$set_channel\""
  for f in fyr.service fyr-agents.slice 50-fyr-agents.rules agents-pressure-chown; do
    [ "$layout" = scope ] || [ "$f" = fyr.service ] || continue
    echo
    echo "---- $f"
    cat "$stage/$f"
  done
  echo
fi

if [ "$mode" = install ]; then
  # The tag by git (no API call), an annotated tag's commit first; the
  # assets from their public download URLs (V0.release-without-gh).
  case "$pick" in
    main)
      sha="$(tag_sha deployable)"
      tag="build-${sha:0:12}"
      ;;
    stable)
      if tag="$(newest_stable)"; then
        sha="$(tag_sha "$tag")"
      elif [ -n "$new_stable" ]; then
        # V0.fresh-ubuntu-run: before the first version is published, a new
        # install that named no channel takes main rather than failing.
        echo "so this new install follows main: run this again with --channel stable once a version is published" >&2
        follow=main
        set_channel=main
        sha="$(tag_sha deployable)"
        tag="build-${sha:0:12}"
      else
        exit 1
      fi
      ;;
    *)
      tag="$pick"
      sha="$(tag_sha "$tag")"
      ;;
  esac
  short="${sha:0:12}"
  dl="$stage/release"
  mkdir -p "$dl"
  echo "downloading release $tag ($short) for $target from $repo"
  for f in "fyrd-$target" "fyr-$target" manifest.json SHA256SUMS SHA256SUMS.sig; do
    curl -fsSL --retry 3 -o "$dl/$f" "$base/releases/download/$tag/$f"
  done
  # The running fyrd verifies the signature of every later update; this
  # first install trusts HTTPS to GitHub and checks the sums.
  (cd "$dl" && grep -E "  (fyrd-$target|fyr-$target|manifest.json)\$" SHA256SUMS | sha256sum -c -)
  grep -q "\"commit\": *\"$sha\"" "$dl/manifest.json" || { echo "release $tag is not for commit $sha" >&2; exit 1; }

  build="$data/builds/$short"
  mkdir -p "$build" "$data/seed" "$data/launcher"
  install -m 0755 "$dl/fyrd-$target" "$build/fyrd"
  install -m 0755 "$dl/fyr-$target" "$build/fyr"
  cp "$dl/manifest.json" "$dl/SHA256SUMS" "$dl/SHA256SUMS.sig" "$build/"
  date -u +%FT%TZ >"$build/.verified"
  install -m 0755 "$build/fyrd" "$data/seed/fyrd"
  install -m 0755 "$build/fyr" "$data/launcher/fyr"
  ln -sfn "builds/$short" "$data/current.tmp" && mv -fT "$data/current.tmp" "$data/current"

  mkdir -p "$(dirname "$cfg")"
  if [ ! -s "$cfg" ]; then
    printf '{\n  "updateChannel": "%s",\n  "cgroups": %s\n}\n' "$set_channel" "$cgroups" >"$cfg"
  elif command -v python3 >/dev/null; then
    python3 - "$cfg" "$cgroups" "$layout" "$set_channel" <<'PY'
import json, sys
p, want, layout, channel = sys.argv[1], json.loads(sys.argv[2]), sys.argv[3], sys.argv[4]
c = json.load(open(p))
if channel:
    c["updateChannel"] = channel  # V0.versioned-releases
g = c.get("cgroups") or {}
for k, v in want.items():
    g.setdefault(k, v)
g["enable"] = True
if layout == "unit":
    g.pop("agentsSlice", None)  # no polkit rule here: the scopes would not start
else:
    g["agentsSlice"] = want["agentsSlice"]
c["cgroups"] = g
json.dump(c, open(p, "w"), indent=2)
open(p, "a").write("\n")
PY
  else
    echo "$cfg exists and python3 is missing: set \"cgroups\" in it to $cgroups by hand" >&2
    [ -z "$set_channel" ] || echo "  and \"updateChannel\" to \"$set_channel\"" >&2
  fi
  echo "release $tag ($short) is current in $data; config $cfg"
fi

echo "the system part, with sudo:"
root install -D -m 0644 "$stage/fyr.service" "$unit"
if [ "$layout" = scope ]; then
  root install -D -m 0644 "$stage/fyr-agents.slice" "$slice"
  root install -D -m 0644 "$stage/50-fyr-agents.rules" "$rule"
  root install -D -m 0755 "$stage/agents-pressure-chown" "$helper"
else
  root rm -f "$slice" "$rule" "$helper"
fi
root systemctl daemon-reload
root systemctl enable fyr.service
# A restart ends running agent turns; a first install has none.
root systemctl restart fyr.service

[ "$mode" = print ] && exit 0
echo "started fyr.service; open http://127.0.0.1:7777 · log: journalctl -u fyr, $data/daemon.log"
echo "fyrd installs each new release of the $follow channel by itself (Settings → Auto-update)"
if command -v tailscale >/dev/null && tailscale status >/dev/null 2>&1; then
  echo "tailnet (optional): to reach Fyr from your phone over Tailscale, set your Tailscale login once (fyr auth owner <you@example.com>) and publish it: sudo tailscale serve --bg --https=7443 unix:$data/serve.sock"
fi
