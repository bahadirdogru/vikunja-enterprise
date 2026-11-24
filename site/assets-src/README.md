# Asset sources

Sources of the PNG files in `docs/assets/`: `icon.html` and `icon-solid.html` wrap `docs/assets/favicon.svg`, `og.html` is the 1200x630 social image. `sh site/assets-src/make-assets.sh` renders them with headless Chromium (`favicon-32.png`, `favicon-192.png`, `favicon-512.png`, `apple-touch-icon.png`, `og.png`). The SVG favicon itself is hand-written.
