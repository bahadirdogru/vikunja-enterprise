#!/bin/sh
# Regenerates the PNG icons and the social image in docs/assets/ from the sources in this directory.
# Needs Chromium (headless). Run from anywhere: sh site/assets-src/make-assets.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
out="$here/../../docs/assets"
shot() { # file size output [extra flags]
  src=$1; w=$2; h=$3; dst=$4; shift 4
  chromium --headless --no-sandbox --disable-gpu --hide-scrollbars "$@" \
    --window-size="$w,$h" --screenshot="$dst" "file://$here/$src" >/dev/null 2>&1
}
shot icon.html 32 32 "$out/favicon-32.png" --default-background-color=00000000
shot icon.html 192 192 "$out/favicon-192.png" --default-background-color=00000000
shot icon.html 512 512 "$out/favicon-512.png" --default-background-color=00000000
shot icon-solid.html 180 180 "$out/apple-touch-icon.png"
shot og.html 1200 630 "$out/og.png"
