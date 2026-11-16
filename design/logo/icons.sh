#!/usr/bin/env bash
# The app icons (favicon, PWA, Apple touch icon, Android push badge), cut from the master render.
#   design/logo/icons.sh            (from the repo root; needs nix for ImageMagick)
# Writes web/assets/icons/*.png. The build copies them as they are, so it needs no image library.
set -euo pipefail
cd "$(dirname "$0")/../.."
SRC=design/logo/lighthouse-512.png
OUT=web/assets/icons
NIGHT='#14212d' # the --night token: the white tower and the amber beams need a dark tile
mkdir -p "$OUT"
im() { nix run nixpkgs#imagemagick -- "$@"; }
strip=(-strip -define png:exclude-chunks=date,time)

# "any" icons and the favicon: the render as it is, on transparent.
im "$SRC" "${strip[@]}" "PNG32:$OUT/icon-512.png"
im "$SRC" -filter Lanczos -resize 192x192 "${strip[@]}" "PNG32:$OUT/icon-192.png"
im "$SRC" -filter Lanczos -resize 32x32 "${strip[@]}" "PNG32:$OUT/favicon-32.png"

# Maskable and Apple touch: a full-bleed night square, the lighthouse at 80 % (the maskable safe zone).
tile() { # SIZE OUT
  local inner=$(( $1 * 8 / 10 ))
  im -size "$1x$1" "xc:$NIGHT" \( "$SRC" -filter Lanczos -resize "${inner}x${inner}" \) \
    -gravity center -composite -alpha off -depth 8 "${strip[@]}" "PNG24:$2"
}
tile 512 "$OUT/maskable-512.png"
tile 180 "$OUT/apple-touch-icon.png" # iOS rounds the corners itself

# Android status-bar badge: the render's alpha as a white silhouette on transparent.
im "$SRC" -filter Lanczos -resize 96x96 -channel RGB -evaluate set 100% +channel "${strip[@]}" "PNG32:$OUT/badge-96.png"
