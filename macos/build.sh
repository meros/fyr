#!/usr/bin/env bash
# Builds Fyr.app (decision 0037) and, with --dmg, Fyr.dmg. It takes fyr and
# fyrd from the release the tag `deployable` points at (anonymous: the repo
# is public), checks them against SHA256SUMS and joins the two architectures
# into universal binaries; --bin DIR takes fyr-darwin-{arm64,amd64} and
# fyrd-darwin-{arm64,amd64} from DIR instead.
#
#	macos/build.sh [--bin DIR] [--dmg] [--out DIR]
#
# Signing: FYR_SIGN_P12 (a .p12 file, with FYR_SIGN_P12_PASSWORD) signs every
# binary with its certificate, through a keychain made for the build and
# deleted after it; CI and the owner's own builds use "Fyr Local Signing"
# (decision 0037), so macOS ties a permission to the certificate and it
# survives builds. FYR_SIGN_IDENTITY (a name or SHA-1, with FYR_SIGN_KEYCHAIN
# and FYR_SIGN_KEYCHAIN_PASSWORD for a keychain of its own) signs with an
# identity already in a keychain. Without either the app is signed ad hoc,
# for development. FYR_VERSION names the build (default: the release's).
#
# The names, for a second, test install beside the real one:
#	FYR_BUNDLE_ID (fyr.app), FYR_LABEL (fyr.agent), FYR_DATA (~/.local/share/fyr),
#	FYR_LOG (~/Library/Logs/fyr.log), FYR_REPLACES (com.github.meros.fyr: the
#	script install's label, taken over at the first start; empty for none),
#	FYR_FLAGS (more fyrd flags, e.g. "-listen 127.0.0.1:7787").
set -euo pipefail
cd "$(dirname "$0")"
root="$(cd .. && pwd)"

bin="" dmg="" out="$PWD/build"
while [ $# -gt 0 ]; do
  case "$1" in
    --bin) bin="$2"; shift ;;
    --dmg) dmg=1 ;;
    --out) out="$2"; shift ;;
    *) echo "usage: $0 [--bin DIR] [--dmg] [--out DIR]" >&2; exit 64 ;;
  esac
  shift
done
bundle_id="${FYR_BUNDLE_ID:-fyr.app}"
label="${FYR_LABEL:-fyr.agent}"
data="${FYR_DATA:-~/.local/share/fyr}"
log="${FYR_LOG:-~/Library/Logs/fyr.log}"
replaces="${FYR_REPLACES-com.github.meros.fyr}"
flags="${FYR_FLAGS:-}"
repo="${FYR_REPO:-meros/fyr}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# fyr and fyrd for both architectures.
if [ -z "$bin" ]; then
  bin="$tmp/bin"
  mkdir -p "$bin"
  sha="$(git ls-remote "https://github.com/$repo" refs/tags/deployable | cut -f1)"
  [ -n "$sha" ] || { echo "no tag deployable in $repo" >&2; exit 1; }
  short="${sha:0:12}"
  echo "release build-$short"
  for f in SHA256SUMS fyr-darwin-arm64 fyr-darwin-amd64 fyrd-darwin-arm64 fyrd-darwin-amd64; do
    curl -fsSL -o "$bin/$f" "https://github.com/$repo/releases/download/build-$short/$f"
  done
  (cd "$bin" && grep -E '  fyrd?-darwin-(arm64|amd64)$' SHA256SUMS | shasum -a 256 -c - >/dev/null)
  version="${FYR_VERSION:-$short}"
else
  version="${FYR_VERSION:-dev}"
fi

app="$out/Fyr.app"
c="$app/Contents"
rm -rf "$app"
mkdir -p "$c/MacOS" "$c/Resources" "$c/Library/LaunchAgents"
for b in fyr fyrd; do
  lipo -create "$bin/$b-darwin-arm64" "$bin/$b-darwin-amd64" -output "$c/Resources/$b"
  chmod 0755 "$c/Resources/$b"
done

# The shell and fyr-host, universal.
swift build -c release --arch arm64 --arch x86_64 >/dev/null
cp .build/apple/Products/Release/Fyr "$c/MacOS/Fyr"
cc -O2 -Wall -arch arm64 -arch x86_64 -mmacosx-version-min=13.0 -o "$c/MacOS/fyr-host" host/fyr-host.c

# The icon, from the lighthouse.
icons="$tmp/fyr.iconset"
mkdir -p "$icons"
for s in 16 32 128 256 512; do
  sips -z $s $s "$root/design/logo/lighthouse-512.png" --out "$icons/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) "$root/design/logo/lighthouse-512.png" --out "$icons/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$icons" -o "$c/Resources/fyr.icns"

xml() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' <<<"$1"; }
replaces_xml=""
for l in $replaces; do replaces_xml="$replaces_xml<string>$(xml "$l")</string>"; done
cat >"$c/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Fyr</string>
  <key>CFBundleDisplayName</key><string>Fyr</string>
  <key>CFBundleIdentifier</key><string>$(xml "$bundle_id")</string>
  <key>CFBundleExecutable</key><string>Fyr</string>
  <key>CFBundleIconFile</key><string>fyr</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$(xml "$version")</string>
  <key>CFBundleVersion</key><string>$(xml "$version")</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>FyrAgentPlist</key><string>$(xml "$label").plist</string>
  <key>FyrData</key><string>$(xml "$data")</string>
  <key>FyrReplaces</key><array>$replaces_xml</array>
  <!-- The page is plain HTTP on fyr.localhost (decision 0037, spike): ATS
       blocks that even with NSAllowsLocalNetworking. -->
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key><true/>
    <key>NSExceptionDomains</key>
    <dict>
      <key>localhost</key>
      <dict>
        <key>NSIncludesSubdomains</key><true/>
        <key>NSExceptionAllowsInsecureHTTPLoads</key><true/>
      </dict>
    </dict>
  </dict>
</dict>
</plist>
EOF

args="<string>fyr-host</string><string>-data</string><string>$(xml "$data")</string><string>-log</string><string>$(xml "$log")</string>"
for f in $flags; do args="$args<string>$(xml "$f")</string>"; done
cat >"$c/Library/LaunchAgents/$label.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$(xml "$label")</string>
  <key>BundleProgram</key><string>Contents/MacOS/fyr-host</string>
  <key>ProgramArguments</key><array>$args</array>
  <key>AssociatedBundleIdentifiers</key><array><string>$(xml "$bundle_id")</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
EOF
plutil -lint -s "$c/Info.plist" "$c/Library/LaunchAgents/$label.plist"

# A .p12: a keychain of the build's own, which the EXIT trap deletes.
if [ -n "${FYR_SIGN_P12:-}" ]; then
  FYR_SIGN_KEYCHAIN="$tmp/sign.keychain-db"
  FYR_SIGN_KEYCHAIN_PASSWORD="$(openssl rand -hex 16)"
  security create-keychain -p "$FYR_SIGN_KEYCHAIN_PASSWORD" "$FYR_SIGN_KEYCHAIN"
  security unlock-keychain -p "$FYR_SIGN_KEYCHAIN_PASSWORD" "$FYR_SIGN_KEYCHAIN"
  security set-keychain-settings "$FYR_SIGN_KEYCHAIN" # no auto-lock during the build
  security import "$FYR_SIGN_P12" -k "$FYR_SIGN_KEYCHAIN" -P "${FYR_SIGN_P12_PASSWORD:-}" -T /usr/bin/codesign >/dev/null
  security set-key-partition-list -S apple-tool:,apple: -s -k "$FYR_SIGN_KEYCHAIN_PASSWORD" "$FYR_SIGN_KEYCHAIN" >/dev/null
  FYR_SIGN_IDENTITY="$(security find-identity -p codesigning "$FYR_SIGN_KEYCHAIN" | awk '$2 ~ /^[0-9A-F]{40}$/ {print $2; exit}')"
  [ -n "$FYR_SIGN_IDENTITY" ] || { echo "no signing identity in $FYR_SIGN_P12" >&2; exit 1; }
fi

# codesign finds an identity only on the user's keychain search list
# (`--keychain` alone does not do, decision 0037's spike): add the signing
# keychain for the signing and put the list back as it was.
if [ -n "${FYR_SIGN_KEYCHAIN:-}" ]; then
  keychains=()
  while IFS= read -r k; do
    k="${k#"${k%%[![:space:]]*}"}" k="${k#\"}" k="${k%\"}"
    [ -n "$k" ] && keychains+=("$k")
  done < <(security list-keychains -d user)
  [ ${#keychains[@]} -gt 0 ] || { echo "cannot read the keychain search list" >&2; exit 1; }
  trap 'security list-keychains -d user -s "${keychains[@]}"; [ -z "${FYR_SIGN_P12:-}" ] || security delete-keychain "$FYR_SIGN_KEYCHAIN" 2>/dev/null; rm -rf "$tmp"' EXIT
  security list-keychains -d user -s "${keychains[@]}" "$FYR_SIGN_KEYCHAIN"
  if [ -n "${FYR_SIGN_KEYCHAIN_PASSWORD:-}" ]; then # a locked one fails with errSecInternalComponent
    security unlock-keychain -p "$FYR_SIGN_KEYCHAIN_PASSWORD" "$FYR_SIGN_KEYCHAIN"
  fi
fi

# Inner binaries first, then the bundle.
sign=(codesign --force --timestamp=none -s "${FYR_SIGN_IDENTITY:--}")
"${sign[@]}" -i "$bundle_id.host" "$c/MacOS/fyr-host"
"${sign[@]}" -i "$bundle_id.fyr" "$c/Resources/fyr"
"${sign[@]}" -i "$bundle_id.fyrd" "$c/Resources/fyrd"
"${sign[@]}" "$app"
codesign --verify --strict --deep "$app"
if [ -n "${FYR_SIGN_KEYCHAIN:-}" ]; then
  security list-keychains -d user -s "${keychains[@]}"
  [ -z "${FYR_SIGN_P12:-}" ] || security delete-keychain "$FYR_SIGN_KEYCHAIN"
  trap 'rm -rf "$tmp"' EXIT
fi
echo "built $app ($version, $(codesign -dv "$app" 2>&1 | sed -n 's/^Signature=//p;s/^Authority=//p' | head -1 || echo ad hoc))"

if [ -n "$dmg" ]; then
  stage="$tmp/dmg"
  mkdir -p "$stage"
  cp -R "$app" "$stage/"
  ln -s /Applications "$stage/Applications"
  rm -f "$out/Fyr.dmg"
  hdiutil create -quiet -volname Fyr -srcfolder "$stage" -fs HFS+ -format UDZO "$out/Fyr.dmg"
  echo "built $out/Fyr.dmg"
fi
