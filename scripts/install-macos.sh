#!/usr/bin/env bash
# Installs Fyr on a Mac as a launchd agent of the logged-in user (the Mac's
# stand-in for the NixOS module, decision 0007): builds this checkout, makes
# it the current build, keeps a copy as the seed, starts the launcher at
# login, and runs scripts/update.sh once a day at 04:00 (it builds origin's
# default branch and switches to it). Self-deploys and updates build with
# the toolchain on the agent's PATH (no Nix needed). Run it again to rebuild
# and restart; `--uninstall` stops both agents and removes their plists (the
# data folder stays).
#
#	scripts/install-macos.sh [--uninstall]
set -euo pipefail
cd "$(dirname "$0")/.."

label=com.github.meros.fyr
ulabel=$label.update
data="$HOME/.local/share/fyr"
plist="$HOME/Library/LaunchAgents/$label.plist"
uplist="$HOME/Library/LaunchAgents/$ulabel.plist"
log="$HOME/Library/Logs/fyr.log"
domain="gui/$(id -u)"

if [ "${1:-}" = --uninstall ]; then
  launchctl bootout "$domain/$ulabel" 2>/dev/null || true
  launchctl bootout "$domain/$label" 2>/dev/null || true
  rm -rf "$plist" "$uplist" "$HOME/Applications/Fyr.app"
  echo "stopped and removed $label and $ulabel; data stays in $data"
  exit 0
fi

# The web client needs a Node LTS (Makefile node-lts). fnm's lts-latest alias
# is a stable path that follows the newest LTS it installed.
if command -v fnm >/dev/null; then
  fnm install --lts >/dev/null 2>&1 || true # its corepack hook fails on Node 26 (no corepack); the install stands
  node_bin="$(fnm exec --using=lts-latest sh -c 'dirname "$(command -v node)"')"
  lts_alias="${FNM_DIR:-$HOME/.local/share/fnm}/aliases/lts-latest/bin"
  [ -d "$lts_alias" ] && node_bin="$lts_alias"
else
  node_bin="$(dirname "$(command -v node)")"
fi
export PATH="$node_bin:$PATH"

for t in go git gh claude; do
  command -v "$t" >/dev/null || { echo "missing $t on PATH (brew install $t)" >&2; exit 1; }
done

make build
mkdir -p "$data/seed" "$data/launcher" "$(dirname "$plist")" "$(dirname "$log")"
install -m 0755 bin/fyrd "$data/seed/fyrd"
install -m 0755 bin/fyr "$data/launcher/fyr"
install -m 0755 scripts/update.sh "$data/update.sh"
bin/fyr install -data "$data" bin

# What the agents and a self-deploy need: node (LTS), go, git, gh, claude, make.
path="$node_bin"
for t in go git gh claude; do
  d="$(dirname "$(command -v "$t")")"
  case ":$path:" in *":$d:"*) ;; *) path="$path:$d" ;; esac
done
path="$path:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$data/launcher/fyr</string>
    <string>-data</string><string>$data</string>
    <string>-seed</string><string>$data/seed/fyrd</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$path</string>
    <key>HOME</key><string>$HOME</string>
  </dict>
  <key>WorkingDirectory</key><string>$HOME</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>$log</string>
  <key>StandardErrorPath</key><string>$log</string>
</dict>
</plist>
EOF

cat >"$uplist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$ulabel</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string><string>$data/update.sh</string>
    <string>-data</string><string>$data</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$path</string>
    <key>HOME</key><string>$HOME</string>
  </dict>
  <key>StartCalendarInterval</key>
  <dict><key>Hour</key><integer>4</integer><key>Minute</key><integer>0</integer></dict>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>$HOME/Library/Logs/fyr-update.log</string>
  <key>StandardErrorPath</key><string>$HOME/Library/Logs/fyr-update.log</string>
</dict>
</plist>
EOF

# ~/Applications/Fyr.app, for Spotlight and the Dock: the page in a Chrome
# app window (or the default browser), with the lighthouse icon.
app="$HOME/Applications/Fyr.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cat >"$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Fyr</string>
  <key>CFBundleIdentifier</key><string>$label.app</string>
  <key>CFBundleExecutable</key><string>Fyr</string>
  <key>CFBundleIconFile</key><string>fyr</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
EOF
cat >"$app/Contents/MacOS/Fyr" <<'EOF'
#!/bin/sh
url=http://127.0.0.1:7777
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
if [ -x "$chrome" ]; then exec "$chrome" --app="$url"; fi
exec open "$url"
EOF
chmod +x "$app/Contents/MacOS/Fyr"
icons="$(mktemp -d)/fyr.iconset"
mkdir -p "$icons"
for s in 16 32 128 256 512; do
  sips -z $s $s web/dist/icons/lighthouse/icon-512.png --out "$icons/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) web/dist/icons/lighthouse/icon-512.png --out "$icons/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$icons" -o "$app/Contents/Resources/fyr.icns"
touch "$app"
# Spotlight lists it only once LaunchServices knows it.
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app"
mdimport -i "$app" >/dev/null 2>&1 || true

# bootout returns before the agent is gone; bootstrap fails until it is.
for l in "$ulabel" "$label"; do
  launchctl bootout "$domain/$l" 2>/dev/null || true
  for _ in $(seq 1 50); do launchctl print "$domain/$l" >/dev/null 2>&1 || break; sleep 0.2; done
done
launchctl bootstrap "$domain" "$plist"
launchctl bootstrap "$domain" "$uplist"
echo "started $label; open http://127.0.0.1:7777 · log $log"
echo "daily update $ulabel at 04:00 · log ~/Library/Logs/fyr-update.log · run now: launchctl kickstart $domain/$ulabel"
