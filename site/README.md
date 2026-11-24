# Website generator

This directory builds the project website, <https://vikunja.bahadirdogru.com>, into `docs/`. GitHub Pages serves `docs/` from `main` with no build step, so the generated files are committed.

```bash
python3 site/build.py          # build docs/ and verify it
python3 site/build.py --check  # verify that docs/ is up to date; write nothing (exit 1 if not)
```

Python 3 standard library only; nothing to install.

## Why it lives here

`site/` is a fork-owned top-level directory outside `docs/`, so that the generator, its page sources and the asset sources are not served as public pages. GitHub Pages publishes only `docs/`.

## Where the text comes from

The Markdown documents of the repository are the single source of truth. They are rendered, not copied:

- `README.md`, `ARCHITECTURE.md`, `DESIGN.md`, `API.md`, `docs/modules/*.md`, `docs/enterprise/*.md` (including the roadmap) are rendered as they are. Edit those files.
- `site/pages/*.md` are hand-written pages (getting started, configuration, upstream relationship, security, contributing, license, design history). They pull sections of other documents in with a directive on its own line: `{{include README.md | Quick start with Docker}}` (heading and body) or `... | body` (without the heading), and `{{raw LICENSE}}` for a file shown as preformatted text.
- `site/pages/landing.html` is the hand-written landing page body. Links to other pages are written `href="@modules/forms.html"`.
- The page list, order, titles, descriptions and navigation are in `PAGES` in `build.py`.

## What the generator does

- Renders Markdown (headings with GitHub-style anchors, tables, code, lists, admonitions such as `> [!IMPORTANT]`) in `mdsite.py`. Raw HTML in Markdown is escaped, never passed through.
- Rewrites links between documents to the generated pages, and links to other repository files to GitHub (`blob` or `tree` on `main`).
- Wraps every page in the shared layout: header and mobile menu, sidebar, breadcrumbs, table of contents, previous and next page, footer.
- Writes `sitemap.xml`, `robots.txt` and `site.webmanifest`.
- Fails loudly (exit 1, message on stderr) on a link to a missing file, a missing anchor, an unknown include, an external image, a missing section, and, after the build, on any generated page that has a `style` attribute, a script or an event handler, a changed Content-Security-Policy, a broken relative link or anchor, an external resource, a missing title, description, canonical URL or social image, more or fewer than one `h1`, or a skipped heading level.

Mermaid diagrams cannot run without JavaScript and no renderer is available offline, so each diagram is shown as a labelled block of its Mermaid source with a link to the file on GitHub, where it renders.

## Assets

`docs/assets/*.css` are hand-written (`site.css` is shared, `landing.css` and `docs.css` are per page type). The icons and the social image are made from the sources in `site/assets-src/`, see the commands in `site/assets-src/README.md`.

## Rules for the site

No JavaScript, no inline styles, no external fonts, images or scripts: every page carries the strict Content-Security-Policy and the check above enforces it. The site is English only. It must not claim more than `docs/enterprise/ROADMAP.md` supports.
