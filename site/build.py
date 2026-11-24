#!/usr/bin/env python3
"""Build the project site into docs/ (Python 3 standard library only).

    python3 site/build.py            build docs/, then verify it
    python3 site/build.py --check    verify that docs/ is up to date, write nothing

The Markdown documents of the repository are the source of truth. This script
renders them into HTML with the shared layout and fails on any broken internal
link or anchor. See site/README.md.
"""

import argparse
import html
import os
import posixpath
import re
import sys
from html.parser import HTMLParser

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import mdsite  # noqa: E402

ROOT = os.path.dirname(HERE)
DOCS = os.path.join(ROOT, "docs")
PAGES_DIR = os.path.join(HERE, "pages")

SITE = "https://vikunja.bahadirdogru.com"
REPO = "https://github.com/bahadirdogru/vikunja-enterprise"
UPSTREAM = "https://github.com/go-vikunja/vikunja"
AUTHOR = "Bahadır Doğru"
AUTHOR_SITE = "https://bahadirdogru.com"
CSP = (
    "default-src 'self'; script-src 'none'; style-src 'self'; img-src 'self'; "
    "object-src 'none'; base-uri 'none'; form-action 'none'"
)
TAGLINE = "Custom fields, workflows and approvals, automation, dashboards and forms on top of Vikunja."


class BuildError(Exception):
    pass


# ---------------------------------------------------------------- site map


class Page:
    def __init__(self, out, title, group, desc, parts, nav=None, kind="doc"):
        self.out = out  # path below docs/
        self.title = title
        self.nav = nav or title
        self.group = group
        self.desc = desc
        self.parts = parts  # [("file", path, skip)] | [("include", path, heading, mode)]
        self.kind = kind  # doc | landing
        self.headings = []  # filled while rendering
        self.h1 = title
        self.edit_path = None


def F(path, skip=()):
    return ("file", path, tuple(skip))


def INC(path, heading, mode="heading"):
    return ("include", path, heading, mode)


def P(name):
    return "site/pages/" + name


GROUPS = ["Start", "Modules", "Reference", "Project", "Design history"]

PAGES = [
    Page("getting-started.html", "Getting started", "Start",
         "Install vikunja-enterprise with Docker: configuration, database choice, reverse proxy, upgrading and backups.",
         [F(P("getting-started.md"))]),
    Page("configuration.html", "Configuration", "Start",
         "The configuration keys the fork adds, with their defaults, and how settings are read.",
         [F(P("configuration.md"))]),
    Page("modules/index.html", "Modules", "Modules",
         "The six modules the fork adds to Vikunja and how they fit together.",
         [F("docs/modules/README.md")], nav="Overview"),
    Page("modules/custom-fields.html", "Custom fields", "Modules",
         "Per-project typed fields on tasks: types, permissions, API, filters and limits.",
         [F("docs/modules/custom-fields.md")]),
    Page("modules/workflows.html", "Workflows", "Modules",
         "Task statuses, categories, restricted transitions and status history.",
         [F("docs/modules/workflows.md")]),
    Page("modules/approvals.html", "Approvals", "Modules",
         "Approvals on workflow transitions and ad-hoc approvals by people, teams or fields.",
         [F("docs/modules/approvals.md")]),
    Page("modules/automation.html", "Automation", "Modules",
         "When / If / Then rules, the outbox, triggers, actions, scripts, limits and run history.",
         [F("docs/modules/automation.md")]),
    Page("modules/dashboards-reports.html", "Dashboards and reports", "Modules",
         "The task aggregation API, dashboards of widgets, drill-down and CSV export.",
         [F("docs/modules/dashboards-reports.md")]),
    Page("modules/forms.html", "Forms", "Modules",
         "Form builder and public forms whose submissions become tasks.",
         [F("docs/modules/forms.md")]),
    Page("api.html", "API reference", "Reference",
         "The vikunja-enterprise API: versions, authentication, filters, errors, and every endpoint the fork adds.",
         [F("API.md", ["Contents"])], nav="API"),
    Page("architecture.html", "Architecture", "Reference",
         "How vikunja-enterprise is built: deployment, packages, request lifecycle, data model, permissions and decisions.",
         [F("ARCHITECTURE.md", ["Contents"])]),
    Page("design.html", "Design", "Reference",
         "User interface and visual design decisions of the fork's features.",
         [F("DESIGN.md", ["Contents"])]),
    Page("roadmap.html", "Roadmap and status", "Reference",
         "What is done, what has been tested on which database, and what is still open.",
         [F("docs/enterprise/ROADMAP.md")], nav="Roadmap"),
    Page("upstream.html", "Relationship to upstream", "Project",
         "How vikunja-enterprise relates to Vikunja: what the fork is and is not, credit, and when to use upstream instead.",
         [F(P("upstream.md"))], nav="Upstream"),
    Page("security.html", "Security", "Project",
         "How to report a security problem in vikunja-enterprise, and what the public surface is.",
         [F(P("security.md"))]),
    Page("contributing.html", "Contributing", "Project",
         "How to contribute to vikunja-enterprise.",
         [F(P("contributing.md"))]),
    Page("license.html", "License", "Project",
         "vikunja-enterprise is licensed under AGPL-3.0-or-later. The full license text.",
         [F(P("license.md"))]),
    Page("enterprise/index.html", "Design documents", "Design history",
         "The original design documents of the fork's modules, kept for their history and rationale.",
         [F(P("history.md"))], nav="Overview"),
    Page("enterprise/custom-fields.html", "Custom fields design", "Design history",
         "Design document of the custom fields module.", [F("docs/enterprise/custom-fields.md")]),
    Page("enterprise/workflow.html", "Workflow design", "Design history",
         "Design document of the workflow module.", [F("docs/enterprise/workflow.md")]),
    Page("enterprise/forms-approvals.html", "Forms and approvals design", "Design history",
         "Design document of the forms and approvals modules.", [F("docs/enterprise/forms-approvals.md")]),
    Page("enterprise/automation.html", "Automation design", "Design history",
         "Design document of the automation module.", [F("docs/enterprise/automation.md")]),
    Page("enterprise/dashboards.html", "Dashboards design", "Design history",
         "Design document of the dashboards and reports module.", [F("docs/enterprise/dashboards.md")]),
]

LANDING = Page(
    "index.html", "vikunja-enterprise: an independent community fork of Vikunja", None,
    "vikunja-enterprise is an independent community fork of Vikunja, the self-hosted to-do and project app, adding "
    "custom fields, workflows and approvals, automation, dashboards and forms. Not an official Vikunja product.",
    [], kind="landing")

# Repository paths that stand for a page. README.md is the landing page; the
# README sections that are shown elsewhere register their anchors on that page.
HOME_ALIASES = {
    "README.md": "index.html",
    "docs/modules": "modules/index.html",
    "docs/enterprise": "enterprise/index.html",
}

# Top navigation (desktop) and the footer
TOP_NAV = [
    ("modules/index.html", "Modules"),
    ("getting-started.html", "Get started"),
    ("api.html", "API"),
    ("architecture.html", "Architecture"),
    ("roadmap.html", "Roadmap"),
    ("upstream.html", "Upstream"),
]

# ---------------------------------------------------------------- helpers


def read(path):
    with open(os.path.join(ROOT, path), encoding="utf-8") as f:
        return f.read()


def exists(path):
    return os.path.exists(os.path.join(ROOT, path))


def rel(from_out, to_out):
    """Relative URL from one output file to another."""
    d = posixpath.dirname(from_out)
    r = posixpath.relpath(to_out, d or ".")
    return r


def page_url(out):
    return SITE + "/" + ("" if out == "index.html" else out)


class Builder:
    def __init__(self):
        self.pages = PAGES
        self.by_out = {p.out: p for p in PAGES}
        self.by_out[LANDING.out] = LANDING
        self.home = {}  # repo path -> output path
        for p in PAGES:
            if p.parts and p.parts[0][0] == "file":
                self.home[p.parts[0][1]] = p.out
        self.home.update(HOME_ALIASES)
        self.registry = {}  # (source, anchor id) -> output path
        self.pass_no = 1
        self.out_files = {}  # docs-relative path -> bytes
        self.stats = {"links": 0, "anchors": 0, "external": 0, "repo": 0}
        self.errors = []

    # -------------------------------------------------- links
    def make_resolver(self, source, page):
        def resolve(href, text, image=False):
            resolve.last_label = None
            href = href.strip()
            if re.match(r"^(https?:|mailto:)", href):
                if image:
                    raise BuildError("%s: external image %s (CSP allows only local images)" % (source, href))
                self.stats["external"] += 1
                return href
            if image:
                raise BuildError("%s: image %s (only external or missing images exist today)" % (source, href))
            path, _, frag = href.partition("#")
            if not path:
                target_src, target_out = source, None
            else:
                target_src = posixpath.normpath(posixpath.join(posixpath.dirname(source), path))
                target_out = None
            home = self.home.get(target_src) or self.home.get(target_src.rstrip("/"))
            if frag and self.pass_no == 2:
                key = (target_src, frag)
                if key not in self.registry:
                    self.errors.append("%s: [%s](%s): anchor #%s not found in a rendered page" % (source, text, href, frag))
                    return "#"
                target_out = self.registry[key]
                self.stats["anchors"] += 1
                self.stats["links"] += 1
                if target_out == page.out:
                    return "#" + frag
                return rel(page.out, target_out) + "#" + frag
            if home:
                self.stats["links"] += 1
                if not frag:
                    resolve.last_label = "Home" if home == "index.html" else self.by_out[home].title
                if home == page.out and not frag:
                    return "#" if False else rel(page.out, home)
                return rel(page.out, home) + ("#" + frag if frag else "")
            if not path:
                return "#" + frag if self.pass_no == 1 else "#"
            if exists(target_src):
                kind = "tree" if os.path.isdir(os.path.join(ROOT, target_src)) else "blob"
                self.stats["repo"] += 1
                return "%s/%s/main/%s%s" % (REPO, kind, target_src.rstrip("/"), "#" + frag if frag else "")
            if self.pass_no == 2:
                self.errors.append("%s: [%s](%s): no such file %s" % (source, text, href, target_src))
            return "#"

        resolve.last_label = None
        return resolve

    # -------------------------------------------------- source handling
    @staticmethod
    def section_range(lines, heading):
        """(start, end) line indexes of the section whose heading text is `heading`."""
        in_fence = False
        start = level = None
        for idx, ln in enumerate(lines):
            if re.match(r"^\s*(```|~~~)", ln):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            m = re.match(r"^(#{1,6})\s+(.*?)\s*$", ln)
            if not m:
                continue
            if start is None:
                if m.group(2) == heading:
                    start, level = idx, len(m.group(1))
            elif len(m.group(1)) <= level:
                return start, idx
        if start is None:
            raise BuildError("section %r not found" % heading)
        return start, len(lines)

    def render_source(self, page, source, lines, line0, ctx_cache):
        ctx = ctx_cache.get(source)
        if ctx is None:
            ctx = mdsite.Context(
                source,
                self.make_resolver(source, page),
                mermaid_url=lambda n, s=source: "%s/blob/main/%s#L%d" % (REPO, s, n),
            )
            ctx_cache[source] = ctx
        return ctx

    def segments(self, lines, skip):
        """Split lines around skipped sections: list of (line0, lines)."""
        spans = []
        for heading in skip:
            try:
                spans.append(self.section_range(lines, heading))
            except BuildError:
                raise BuildError("skip section %r not found" % heading)
        spans.sort()
        out, pos = [], 0
        for s, e in spans:
            out.append((pos, lines[pos:s]))
            pos = e
        out.append((pos, lines[pos:]))
        return out

    def render_page_body(self, page):
        ctxs = {}
        chunks = []
        page.headings = []
        h1 = None
        for k, part in enumerate(page.parts):
            if part[0] == "file":
                _, src, skip = part
                lines = read(src).split("\n")
                base = 0
                if lines and lines[0].startswith("# "):
                    if k == 0:
                        h1 = lines[0][2:].strip()
                    lines = lines[1:]
                    base = 1
                segs = [(base + s, ls) for s, ls in self.segments(lines, skip)]
            elif part[0] == "text":
                _, src, start, tl = part
                lines = list(tl)
                if k == 0 and lines and lines[0].startswith("# "):
                    h1 = lines[0][2:].strip()
                    lines = lines[1:]
                    start += 1
                segs = [(start, lines)]
            elif part[0] == "raw":
                chunks.append('<pre class="code"><code>%s</code></pre>' % html.escape(read(part[1])))
                continue
            else:
                _, src, heading, mode = part
                full = read(src).split("\n")
                s, e = self.section_range(full, heading)
                if mode == "body":
                    s += 1
                segs = [(s, full[s:e])]
            if k == 0:
                page.edit_path = src
            ctx = self.render_source(page, src, None, 0, ctxs)
            if part[0] == "include" and part[3] == "body":
                ctx.level_shift = -1
            for line0, ls in segs:
                n0 = len(ctx.headings)
                chunks.append(mdsite.render_blocks(ls, ctx, line0=line0))
                if part[0] == "include":
                    ctx.level_shift = 0
                for lvl, text, hid in ctx.headings[n0:]:
                    page.headings.append((lvl, text, hid))
                    key = (src, hid)
                    if self.pass_no == 1:
                        self.registry[key] = page.out
        body = "\n".join(chunks)
        return body, h1

    # -------------------------------------------------- layout
    def head(self, page, prefix, root_abs=False):
        title = page.title if page.kind == "landing" else "%s - vikunja-enterprise" % page.title
        pre = "/" if root_abs else prefix
        css = ["assets/site.css", "assets/landing.css" if page.kind == "landing" else "assets/docs.css"]
        if page.out == "404.html":
            css = ["assets/site.css"]
        canonical = page_url(page.out)
        lines = [
            "<!DOCTYPE html>",
            '<html lang="en">',
            "<head>",
            '<meta charset="utf-8">',
            '<meta name="viewport" content="width=device-width, initial-scale=1">',
            '<meta http-equiv="Content-Security-Policy" content="%s">' % CSP,
            "<title>%s</title>" % html.escape(title),
            '<meta name="description" content="%s">' % html.escape(page.desc, quote=True),
            '<meta name="author" content="%s">' % html.escape(AUTHOR, quote=True),
        ]
        if page.out == "404.html":
            lines.append('<meta name="robots" content="noindex">')
        else:
            lines.append('<link rel="canonical" href="%s">' % canonical)
        lines += [
            '<meta name="color-scheme" content="light dark">',
            '<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)">',
            '<meta name="theme-color" content="#0d1020" media="(prefers-color-scheme: dark)">',
            '<link rel="icon" href="%sassets/favicon.svg" type="image/svg+xml">' % pre,
            '<link rel="icon" href="%sassets/favicon-32.png" type="image/png" sizes="32x32">' % pre,
            '<link rel="apple-touch-icon" href="%sassets/apple-touch-icon.png">' % pre,
            '<link rel="manifest" href="%ssite.webmanifest">' % pre,
        ]
        lines += ['<link rel="stylesheet" href="%s%s">' % (pre, c) for c in css]
        if page.out != "404.html":
            og_title = html.escape(title, quote=True)
            lines += [
                '<meta property="og:type" content="website">',
                '<meta property="og:site_name" content="vikunja-enterprise">',
                '<meta property="og:title" content="%s">' % og_title,
                '<meta property="og:description" content="%s">' % html.escape(page.desc, quote=True),
                '<meta property="og:url" content="%s">' % canonical,
                '<meta property="og:image" content="%s/assets/og.png">' % SITE,
                '<meta property="og:image:width" content="1200">',
                '<meta property="og:image:height" content="630">',
                '<meta property="og:image:alt" content="vikunja-enterprise, an independent community fork of Vikunja">',
                '<meta name="twitter:card" content="summary_large_image">',
                '<meta name="twitter:title" content="%s">' % og_title,
                '<meta name="twitter:description" content="%s">' % html.escape(page.desc, quote=True),
                '<meta name="twitter:image" content="%s/assets/og.png">' % SITE,
            ]
        lines.append("</head>")
        return "\n".join(lines)

    def header(self, page, prefix, root_abs=False):
        pre = "/" if root_abs else prefix

        def href(out):
            return (pre + out) if root_abs else rel(page.out, out) if not out.endswith("/") else out

        def link(out, label, extra=""):
            cur = ' aria-current="page"' if out == page.out else ""
            return '<li><a href="%s"%s%s>%s</a></li>' % (href(out), cur, extra, html.escape(label))

        top = "\n".join(link(o, l) for o, l in TOP_NAV)
        groups = []
        for g in GROUPS:
            items = [p for p in self.pages if p.group == g]
            lis = "\n".join(link(p.out, p.nav if g in ("Modules", "Design history") else p.title) for p in items)
            groups.append('<div class="menu-group"><p class="menu-title">%s</p><ul>\n%s\n</ul></div>' % (g, lis))
        logo_href = href("index.html") if not root_abs else "/"
        return """<a class="skip" href="#main">Skip to content</a>
<header class="site-header">
  <div class="wrap">
    <a class="logo" href="%(logo_href)s" aria-label="vikunja-enterprise home"><img src="%(pre)sassets/favicon.svg" alt="" width="30" height="30">vikunja-enterprise</a>
    <nav class="top-nav" aria-label="Main">
      <ul>
%(top)s
        <li><a class="gh" href="%(repo)s">GitHub</a></li>
      </ul>
    </nav>
    <details class="menu">
      <summary><span class="menu-icon" aria-hidden="true"></span>Menu</summary>
      <nav class="menu-panel" aria-label="Site">
        <div class="menu-group"><p class="menu-title">Site</p><ul>
%(home)s
        <li><a href="%(repo)s">GitHub repository</a></li>
        </ul></div>
%(groups)s
      </nav>
    </details>
  </div>
</header>""" % {
            "logo_href": logo_href, "pre": pre, "top": top, "repo": REPO, "groups": "\n".join(groups),
            "home": link("index.html", "Home"),
        }

    def footer(self, page, prefix, root_abs=False):
        def href(out):
            return ("/" + out) if root_abs else rel(page.out, out)

        def li(out, label):
            return '<li><a href="%s">%s</a></li>' % (href(out), label)

        return """<footer class="site-footer">
  <div class="wrap">
    <div class="footer-about">
      <p><strong>vikunja-enterprise</strong> is an independent community fork of Vikunja. It is not affiliated with, endorsed by or supported by the Vikunja project. Almost all of the software is their work.</p>
      <p>Licensed under AGPL-3.0-or-later, as upstream. This site has no scripts, cookies or trackers.</p>
    </div>
    <nav class="footer-nav" aria-label="Footer">
      <div><p class="footer-title">Project</p><ul>
        %(a)s
        %(b)s
        %(c)s
        %(d)s
      </ul></div>
      <div><p class="footer-title">Reference</p><ul>
        %(e)s
        %(f)s
        %(g)s
        %(h)s
      </ul></div>
      <div><p class="footer-title">Community</p><ul>
        <li><a href="%(repo)s">Repository</a></li>
        <li><a href="%(repo)s/issues">Issues</a></li>
        %(i)s
        %(j)s
      </ul></div>
      <div><p class="footer-title">Upstream Vikunja</p><ul>
        <li><a href="%(up)s">Source code</a></li>
        <li><a href="https://vikunja.io/docs/">Documentation</a></li>
        <li><a href="https://vikunja.io/support/">Support upstream</a></li>
      </ul></div>
    </nav>
    <p class="footer-by">A project by <a class="by-link" href="%(author_site)s" rel="author">%(author)s<span class="by-domain">bahadirdogru.com</span></a></p>
  </div>
</footer>""" % {
            "a": li("index.html", "Home"), "b": li("getting-started.html", "Get started"),
            "c": li("roadmap.html", "Roadmap and status"), "d": li("upstream.html", "Relationship to upstream"),
            "e": li("modules/index.html", "Modules"), "f": li("api.html", "API"),
            "g": li("architecture.html", "Architecture"), "h": li("configuration.html", "Configuration"),
            "i": li("security.html", "Security"), "j": li("contributing.html", "Contributing")
            + "\n        " + li("license.html", "License"),
            "repo": REPO, "up": UPSTREAM, "author": html.escape(AUTHOR), "author_site": AUTHOR_SITE,
        }

    def sidebar(self, page):
        parts = []
        for g in GROUPS:
            items = [p for p in self.pages if p.group == g]
            lis = []
            for p in items:
                cur = ' aria-current="page"' if p.out == page.out else ""
                label = p.nav if g in ("Modules", "Design history") else p.title
                lis.append('<li><a href="%s"%s>%s</a></li>' % (rel(page.out, p.out), cur, html.escape(label)))
            parts.append('<p class="side-title">%s</p><ul>\n%s\n</ul>' % (g, "\n".join(lis)))
        return '<nav class="sidebar" aria-label="Documentation">\n%s\n</nav>' % "\n".join(parts)

    @staticmethod
    def toc(page, cls):
        items = [h for h in page.headings if h[0] in (2, 3)]
        if sum(1 for h in items if h[0] == 2) < 2:
            return ""
        lis = []
        for lvl, text, hid in items:
            lis.append('<li class="toc-%d"><a href="#%s">%s</a></li>' % (lvl, hid, html.escape(text)))
        ul = "<ul>\n%s\n</ul>" % "\n".join(lis)
        if cls == "inline":
            return '<details class="toc-inline"><summary>On this page</summary>\n<nav aria-label="On this page">%s</nav></details>' % ul
        return '<nav class="toc-aside" aria-label="On this page"><p class="side-title">On this page</p>%s</nav>' % ul

    def doc_page(self, page):
        body, h1 = self.render_page_body(page)
        page.h1 = h1 or page.title
        order = self.pages
        idx = order.index(page)
        prev_p = order[idx - 1] if idx > 0 else None
        next_p = order[idx + 1] if idx + 1 < len(order) else None
        crumbs = ['<li><a href="%s">Home</a></li>' % rel(page.out, "index.html")]
        crumbs.append("<li>%s</li>" % html.escape(page.group))
        crumbs.append('<li aria-current="page">%s</li>' % html.escape(page.title))
        pager = []
        if prev_p:
            pager.append('<a class="prev" href="%s" rel="prev"><span>Previous</span>%s</a>' % (rel(page.out, prev_p.out), html.escape(prev_p.title)))
        if next_p:
            pager.append('<a class="next" href="%s" rel="next"><span>Next</span>%s</a>' % (rel(page.out, next_p.out), html.escape(next_p.title)))
        edit = ""
        if page.edit_path:
            edit = '<p class="edit"><a href="%s/blob/main/%s">Source of this page on GitHub</a></p>' % (REPO, page.edit_path)
        prefix = "../" * page.out.count("/")
        return "\n".join([
            self.head(page, prefix),
            '<body class="docs">',
            self.header(page, prefix),
            '<div class="docs-layout">',
            self.sidebar(page),
            '<main id="main" class="doc">',
            '<nav class="crumbs" aria-label="Breadcrumb"><ol>%s</ol></nav>' % "".join(crumbs),
            "<h1>%s</h1>" % mdsite.inline(page.h1, mdsite.Context("", lambda *a, **k: "#")),
            self.toc(page, "inline"),
            '<div class="prose">',
            body,
            "</div>",
            edit,
            '<nav class="pager" aria-label="Pages">%s</nav>' % "".join(pager),
            "</main>",
            self.toc(page, "aside"),
            "</div>",
            self.footer(page, prefix),
            "</body>",
            "</html>",
            "",
        ])

    def landing_page(self):
        page = LANDING
        frag = read("site/pages/landing.html")
        frag = self.fix_fragment_links(page, frag, "site/pages/landing.html")
        return "\n".join([
            self.head(page, ""), "<body>", self.header(page, ""), '<main id="main">', frag, "</main>",
            self.footer(page, ""), "</body>", "</html>", "",
        ])

    def fix_fragment_links(self, page, frag, src):
        """Hand-written HTML fragments use `@out/path.html` for page links; a
        fragment is resolved to a path relative to the page."""
        def sub(m):
            target = m.group(1)
            frag_id = m.group(2) or ""
            out = target
            if out not in self.by_out:
                raise BuildError("%s: link to unknown page %s" % (src, out))
            return 'href="%s%s"' % (rel(page.out, out), frag_id)
        return re.sub(r'href="@([^"#]+)(#[^"]*)?"', sub, frag)

    def not_found_page(self):
        page = Page("404.html", "Page not found", None,
                    "This page does not exist. vikunja-enterprise is an independent community fork of Vikunja.", [])
        body = """<main class="wrap lost" id="main">
  <p class="eyebrow">404</p>
  <h1>Nothing here.</h1>
  <p>This page does not exist. It may have moved when the site grew from one page to a documentation site.</p>
  <div class="actions"><a class="btn btn-primary" href="/">Back to the home page</a><a class="btn btn-ghost" href="/modules/index.html">Browse the modules</a><a class="btn btn-ghost" href="/getting-started.html">Get started</a></div>
  <p class="note">vikunja-enterprise is an independent community fork, not affiliated with the Vikunja project.</p>
</main>"""
        return "\n".join([
            self.head(page, "", root_abs=True), "<body>", self.header(page, "", root_abs=True), body,
            self.footer(page, "", root_abs=True), "</body>", "</html>", "",
        ])

    # -------------------------------------------------- output
    def sitemap(self):
        urls = [LANDING] + [p for p in self.pages]
        rows = "\n".join("  <url><loc>%s</loc></url>" % page_url(p.out) for p in urls)
        return '<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n%s\n</urlset>\n' % rows

    def build(self):
        for self.pass_no in (1, 2):
            self.stats = {"links": 0, "anchors": 0, "external": 0, "repo": 0}
            self.errors = []
            self.out_files = {}
            self.parts_expand()
            for p in self.pages:
                self.out_files[p.out] = self.doc_page(p).encode("utf-8")
            self.out_files[LANDING.out] = self.landing_page().encode("utf-8")
            self.out_files["404.html"] = self.not_found_page().encode("utf-8")
            self.out_files["sitemap.xml"] = self.sitemap().encode("utf-8")
            self.out_files["robots.txt"] = ("User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n" % SITE).encode("utf-8")
            self.out_files["site.webmanifest"] = MANIFEST.encode("utf-8")
        if self.errors:
            raise BuildError("\n".join(self.errors))

    def parts_expand(self):
        """Expand `{{include PATH | Heading | mode}}` lines of hand-written pages."""
        for p in self.pages:
            if getattr(p, "_expanded", False):
                continue
            new_parts = []
            for part in p.parts:
                if part[0] == "file" and part[1].startswith("site/pages/"):
                    new_parts.extend(self.split_directives(part))
                else:
                    new_parts.append(part)
            p.parts = new_parts
            p._expanded = True

    def split_directives(self, part):
        _, src, skip = part
        lines = read(src).split("\n")
        parts, buf, start = [], [], 0
        for idx, ln in enumerate(lines):
            mr = re.match(r"^\{\{raw\s+(.+?)\s*\}\}\s*$", ln)
            if mr:
                if buf:
                    parts.append(("text", src, start, tuple(buf)))
                    buf = []
                parts.append(("raw", mr.group(1)))
                start = idx + 1
                continue
            m = re.match(r"^\{\{include\s+(.+?)\s*\|\s*(.+?)\s*(?:\|\s*(heading|body)\s*)?\}\}\s*$", ln)
            if m:
                if buf:
                    parts.append(("text", src, start, tuple(buf)))
                    buf = []
                parts.append(("include", m.group(1), m.group(2), m.group(3) or "heading"))
                start = idx + 1
            else:
                if not buf:
                    start = idx
                buf.append(ln)
        if buf:
            parts.append(("text", src, start, tuple(buf)))
        return parts


MANIFEST = """{
  "name": "vikunja-enterprise",
  "short_name": "vikunja-enterprise",
  "description": "An independent community fork of Vikunja with custom fields, workflows, approvals, automation, dashboards and forms.",
  "lang": "en",
  "start_url": "./",
  "scope": "./",
  "display": "browser",
  "background_color": "#ffffff",
  "theme_color": "#4252d9",
  "icons": [
    {"src": "assets/favicon-192.png", "sizes": "192x192", "type": "image/png"},
    {"src": "assets/favicon-512.png", "sizes": "512x512", "type": "image/png"},
    {"src": "assets/favicon.svg", "sizes": "any", "type": "image/svg+xml"}
  ]
}
"""


# ---------------------------------------------------------------- verification


class PageScan(HTMLParser):
    """Collect what the checks need from one generated HTML file."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.ids, self.links, self.res, self.problems = set(), [], [], []
        self.headings, self.csp, self.title, self._in_title = [], False, "", False
        self.desc = self.canonical = self.og_image = False

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if "id" in a:
            if a["id"] in self.ids:
                self.problems.append("duplicate id %s" % a["id"])
            self.ids.add(a["id"])
        if "style" in a:
            self.problems.append("style attribute on <%s>" % tag)
        for k in a:
            if k.startswith("on"):
                self.problems.append("event handler attribute %s on <%s>" % (k, tag))
        if tag in ("script", "style", "iframe", "object", "embed", "form"):
            self.problems.append("<%s> element" % tag)
        if tag == "a" and "href" in a:
            self.links.append(a["href"])
        if tag == "img":
            if "alt" not in a:
                self.problems.append("img without alt: %s" % a.get("src"))
            self.res.append(a.get("src", ""))
        if tag == "link" and "href" in a:
            self.res.append(a["href"])
            if a.get("rel") == "canonical":
                self.canonical = True
                self.res.pop()
        if tag == "meta":
            if a.get("http-equiv") == "Content-Security-Policy":
                self.csp = a.get("content") == CSP
            if a.get("name") == "description" and a.get("content"):
                self.desc = True
            if a.get("property") == "og:image":
                self.og_image = True
        if re.fullmatch(r"h[1-6]", tag):
            self.headings.append(int(tag[1]))
        if tag == "title":
            self._in_title = True

    def handle_endtag(self, tag):
        if tag == "title":
            self._in_title = False

    def handle_data(self, data):
        if self._in_title:
            self.title += data


def verify(files, root_for_assets=DOCS):
    """Check the generated site. Returns (problems, stats)."""
    problems = []
    stats = {"pages": 0, "links": 0, "anchors": 0, "resources": 0, "external": 0}
    scans = {}

    def asset_exists(path):
        return path in files or os.path.isfile(os.path.join(root_for_assets, path))

    html_files = [p for p in files if p.endswith(".html")]
    for out in html_files:
        scan = PageScan()
        scan.feed(files[out].decode("utf-8"))
        scans[out] = scan
        stats["pages"] += 1
        for pr in scan.problems:
            problems.append("%s: %s" % (out, pr))
        if not scan.csp:
            problems.append("%s: missing or changed Content-Security-Policy" % out)
        if not scan.title.strip():
            problems.append("%s: empty title" % out)
        if not scan.desc:
            problems.append("%s: no meta description" % out)
        if out != "404.html" and not (scan.canonical and scan.og_image):
            problems.append("%s: missing canonical or og:image" % out)
        if scan.headings.count(1) != 1:
            problems.append("%s: %d h1 elements" % (out, scan.headings.count(1)))
        prev = 0
        for h in scan.headings:
            if prev and h > prev + 1:
                problems.append("%s: heading level jumps from h%d to h%d" % (out, prev, h))
            prev = h
    for out, scan in scans.items():
        base = posixpath.dirname(out)
        for res in scan.res:
            stats["resources"] += 1
            if re.match(r"^(https?:)?//|^data:", res):
                problems.append("%s: external or data resource %s" % (out, res))
                continue
            target = posixpath.normpath(posixpath.join("/" if res.startswith("/") else base, res.lstrip("/"))) if not res.startswith("/") else res.lstrip("/")
            if not asset_exists(target):
                problems.append("%s: missing resource %s" % (out, res))
        for href in scan.links:
            if re.match(r"^(https?:|mailto:)", href):
                stats["external"] += 1
                continue
            stats["links"] += 1
            path, _, frag = href.partition("#")
            if not path:
                target = out
            elif path.startswith("/"):
                target = path.lstrip("/") or "index.html"
            else:
                target = posixpath.normpath(posixpath.join(base, path))
            if target.endswith("/"):
                target += "index.html"
            if target not in files and not os.path.isfile(os.path.join(root_for_assets, target)):
                problems.append("%s: broken link %s" % (out, href))
                continue
            if frag:
                stats["anchors"] += 1
                if target in scans and frag not in scans[target].ids:
                    problems.append("%s: anchor #%s not found in %s (link %s)" % (out, frag, target, href))
    return problems, stats


def collect_disk(root):
    files = {}
    for dp, _, fns in os.walk(root):
        for fn in fns:
            full = os.path.join(dp, fn)
            files[os.path.relpath(full, root).replace(os.sep, "/")] = full
    return files


GENERATED_ROOTS = ("index.html", "404.html", "getting-started.html", "configuration.html", "api.html",
                   "architecture.html", "design.html", "roadmap.html", "upstream.html", "security.html",
                   "contributing.html", "license.html", "sitemap.xml", "robots.txt", "site.webmanifest")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check", action="store_true", help="verify that docs/ is up to date; write nothing")
    args = ap.parse_args()
    b = Builder()
    try:
        b.build()
    except BuildError as e:
        print("BUILD FAILED:\n%s" % e, file=sys.stderr)
        return 1
    problems, stats = verify(b.out_files)
    if problems:
        print("SITE CHECK FAILED (%d problems):" % len(problems), file=sys.stderr)
        for p in problems:
            print("  " + p, file=sys.stderr)
        return 1
    if args.check:
        stale = []
        for out, data in b.out_files.items():
            path = os.path.join(DOCS, out)
            if not os.path.isfile(path) or open(path, "rb").read() != data:
                stale.append(out)
        if stale:
            print("docs/ is out of date, run: python3 site/build.py\n  " + "\n  ".join(stale), file=sys.stderr)
            return 1
    else:
        for out, data in b.out_files.items():
            path = os.path.join(DOCS, out)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "wb") as f:
                f.write(data)
    print("%s: %d pages; %d internal links, %d anchors, %d resources checked; %d external links, %d links to repository files"
          % ("up to date" if args.check else "built", stats["pages"], stats["links"], stats["anchors"],
             stats["resources"], stats["external"], b.stats["repo"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
