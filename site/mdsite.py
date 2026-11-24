"""A small Markdown renderer for the project site (Python 3 standard library only).

It implements the subset the repository's documents use: ATX headings with
GitHub-style anchors, paragraphs, emphasis, code spans and fenced code, links
and images, ordered, unordered and nested lists, block quotes with GitHub
admonitions (`> [!NOTE]`), tables, horizontal rules, and autolinks. Raw HTML in
Markdown is escaped, never passed through.

The renderer does not know about files or pages. Link targets go through the
`resolve_link` callback of the Context, so the caller decides what a link to
another document becomes.
"""

import html
import re

PUNCT = set("\\`*_{}[]()#+-.!|<>~\"'$%&,/:;=?@^")


def esc(text):
    return html.escape(text, quote=True)


class Context:
    """State of one rendered source document."""

    def __init__(self, source, resolve_link, mermaid_url=None):
        self.source = source  # repo-relative path of the document
        self.resolve_link = resolve_link  # (href, text_for_errors) -> href
        self.mermaid_url = mermaid_url  # (line_number) -> url, or None
        self.headings = []  # (level, plain_text, id)
        self.ids = {}  # slug -> count, per source document
        self.level_shift = 0  # added to every heading level; a second h1 starts a +1 shift

    def unique_id(self, text):
        slug = github_slug(text)
        n = self.ids.get(slug, 0)
        self.ids[slug] = n + 1
        return slug if n == 0 else "%s-%d" % (slug, n)


def github_slug(text):
    text = text.strip().lower()
    out = []
    for ch in text:
        if ch.isalnum() or ch in "-_":
            out.append(ch)
        elif ch == " ":
            out.append("-")
    return "".join(out)


# ---------------------------------------------------------------- inline

_AUTOLINK = re.compile(r"<((?:https?|mailto):[^\s<>]+)>")
_BARE_URL = re.compile(r"https?://[^\s<>\"]+")


def plain(markup_html):
    """Plain text of rendered inline HTML (for ids and the table of contents)."""
    return html.unescape(re.sub(r"<[^>]+>", "", markup_html))


def find_close(s, i, ch):
    """Index of the `]` that closes the `[` at s[i], skipping code spans."""
    depth = 0
    j = i
    while j < len(s):
        c = s[j]
        if c == "\\":
            j += 2
            continue
        if c == "`":
            m = re.match(r"`+", s[j:])
            run = m.group(0)
            k = s.find(run, j + len(run))
            j = k + len(run) if k != -1 else j + len(run)
            continue
        if c == "[":
            depth += 1
        elif c == "]":
            depth -= 1
            if depth == 0:
                return j
        j += 1
    return -1


def parse_dest(s, i):
    """Parse `(dest "title")` starting at s[i] == '('. Returns (dest, title, end)."""
    depth = 0
    j = i
    while j < len(s):
        c = s[j]
        if c == "\\":
            j += 2
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                break
        j += 1
    else:
        return None
    inner = s[i + 1 : j].strip()
    title = None
    m = re.match(r'^(\S+)\s+"([^"]*)"$', inner)
    if m:
        inner, title = m.group(1), m.group(2)
    if inner.startswith("<") and inner.endswith(">"):
        inner = inner[1:-1]
    return inner, title, j + 1


def inline(s, ctx):
    out = []
    i = 0
    n = len(s)
    while i < n:
        c = s[i]
        if c == "\\" and i + 1 < n and s[i + 1] in PUNCT:
            out.append(esc(s[i + 1]))
            i += 2
        elif c == "`":
            run = re.match(r"`+", s[i:]).group(0)
            k = s.find(run, i + len(run))
            if k == -1:
                out.append(run)
                i += len(run)
            else:
                code = s[i + len(run) : k]
                if len(code) > 2 and code[0] == " " and code[-1] == " " and code.strip():
                    code = code[1:-1]
                out.append("<code>%s</code>" % esc(code))
                i = k + len(run)
        elif c == "!" and s.startswith("![", i):
            close = find_close(s, i + 1, "[")
            if close != -1 and close + 1 < n and s[close + 1] == "(":
                d = parse_dest(s, close + 1)
                if d:
                    alt = plain(inline(s[i + 2 : close], ctx))
                    src = ctx.resolve_link(d[0], alt, image=True)
                    out.append('<img src="%s" alt="%s" loading="lazy">' % (esc(src), esc(alt)))
                    i = d[2]
                    continue
            out.append("!")
            i += 1
        elif c == "[":
            close = find_close(s, i, "[")
            if close != -1 and close + 1 < n and s[close + 1] == "(":
                d = parse_dest(s, close + 1)
                if d:
                    text_md = s[i + 1 : close]
                    inner = inline(text_md, ctx)
                    href = ctx.resolve_link(d[0], plain(inner), image=False)
                    label = getattr(ctx.resolve_link, "last_label", None)
                    if label and re.fullmatch(r"[\w./-]+\.md|[\w./-]+/|LICENSE", plain(inner)):
                        inner = esc(label)  # a bare file name as link text becomes the page title
                    t = ' title="%s"' % esc(d[1]) if d[1] else ""
                    out.append('<a href="%s"%s>%s</a>' % (esc(href), t, inner))
                    i = d[2]
                    continue
            out.append("[")
            i += 1
        elif c == "<":
            m = _AUTOLINK.match(s, i)
            if m:
                url = m.group(1)
                href = ctx.resolve_link(url, url, image=False)
                out.append('<a href="%s">%s</a>' % (esc(href), esc(url.replace("mailto:", ""))))
                i = m.end()
            else:
                out.append("&lt;")
                i += 1
        elif c in "*_" and i + 1 < n:
            marker = s[i : i + 2] if s[i : i + 2] in ("**", "__") else c
            before = s[i - 1] if i > 0 else " "
            after = s[i + len(marker)] if i + len(marker) < n else " "
            ok_open = not after.isspace() and not (c == "_" and (before.isalnum()))
            if ok_open:
                k = i + len(marker)
                found = -1
                while True:
                    k = s.find(marker, k)
                    if k == -1:
                        break
                    prev = s[k - 1]
                    nxt = s[k + len(marker)] if k + len(marker) < n else " "
                    # skip code spans: a marker inside backticks is not a closer
                    if s[i + len(marker) : k].count("`") % 2 == 1:
                        k += len(marker)
                        continue
                    if not prev.isspace() and not (c == "_" and nxt.isalnum()):
                        if len(marker) == 1 and (nxt == c or prev == c):
                            k += 1
                            continue
                        found = k
                        break
                    k += len(marker)
                if found != -1:
                    body = inline(s[i + len(marker) : found], ctx)
                    tag = "strong" if len(marker) == 2 else "em"
                    out.append("<%s>%s</%s>" % (tag, body, tag))
                    i = found + len(marker)
                    continue
            out.append(esc(c))
            i += 1
        elif c == "h" and (s.startswith("http://", i) or s.startswith("https://", i)) and (i == 0 or not s[i - 1].isalnum()):
            m = _BARE_URL.match(s, i)
            url = m.group(0)
            trail = ""
            while url and url[-1] in ".,;:!?)":
                if url[-1] == ")" and url.count("(") >= url.count(")"):
                    break
                trail = url[-1] + trail
                url = url[:-1]
            out.append('<a href="%s">%s</a>%s' % (esc(url), esc(url), esc(trail)))
            i += len(m.group(0))
        elif c == "&":
            out.append("&amp;")
            i += 1
        elif c == ">":
            out.append("&gt;")
            i += 1
        elif c == '"':
            out.append("&quot;")
            i += 1
        else:
            out.append(c)
            i += 1
    return "".join(out)


# ---------------------------------------------------------------- blocks

_FENCE = re.compile(r"^(\s*)(`{3,}|~{3,})\s*([^`\s]*)[^`]*$")
_HEADING = re.compile(r"^ {0,3}(#{1,6})\s+(.*?)(?:\s+#+)?\s*$")
_HR = re.compile(r"^ {0,3}([-*_])(?:\s*\1){2,}\s*$")
_LIST = re.compile(r"^(\s*)([-*+]|\d{1,9}[.)])(\s+)(.*)$")
_QUOTE = re.compile(r"^ {0,3}>")
_TABLE_SEP = re.compile(r"^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$")
_ADMON = re.compile(r"^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*$", re.I)


def split_row(line):
    line = line.strip()
    if line.startswith("|"):
        line = line[1:]
    if line.endswith("|") and not line.endswith("\\|"):
        line = line[:-1]
    cells, cur, i = [], [], 0
    in_code = 0
    while i < len(line):
        c = line[i]
        if c == "\\" and i + 1 < len(line) and line[i + 1] == "|":
            cur.append("|")
            i += 2
            continue
        if c == "`":
            m = re.match(r"`+", line[i:])
            cur.append(m.group(0))
            i += len(m.group(0))
            in_code = 0 if in_code == len(m.group(0)) else (len(m.group(0)) if not in_code else in_code)
            continue
        if c == "|" and not in_code:
            cells.append("".join(cur).strip())
            cur = []
        else:
            cur.append(c)
        i += 1
    cells.append("".join(cur).strip())
    return cells


def is_block_start(line):
    return bool(
        _FENCE.match(line) or _HEADING.match(line) or _HR.match(line) or _QUOTE.match(line) or _LIST.match(line)
    )


def render_blocks(lines, ctx, tight=False, line0=0):
    """Render a list of source lines. `line0` is the index of lines[0] in the
    source file (for diagram links)."""
    out = []
    i = 0
    n = len(lines)
    while i < n:
        line = lines[i]
        if not line.strip():
            i += 1
            continue
        m = _FENCE.match(line)
        if m:
            indent, fence, lang = len(m.group(1)), m.group(2), m.group(3)
            j = i + 1
            body = []
            while j < n:
                if re.match(r"^\s*%s{%d,}\s*$" % (re.escape(fence[0]), len(fence)), lines[j]):
                    break
                body.append(lines[j][indent:] if lines[j][:indent].strip() == "" else lines[j].lstrip())
                j += 1
            out.append(render_code("\n".join(body), lang, ctx, line0 + i + 1))
            i = j + 1
            continue
        m = _HEADING.match(line)
        if m:
            level = len(m.group(1))
            if level == 1:
                ctx.level_shift = 1  # the page's own h1 is not part of the body
            level = max(2, min(6, level + ctx.level_shift))
            inner = inline(m.group(2), ctx)
            text = plain(inner)
            hid = ctx.unique_id(text)
            ctx.headings.append((level, text, hid))
            out.append(
                '<h%d id="%s">%s <a class="anchor" href="#%s" aria-label="Link to this section: %s">#</a></h%d>'
                % (level, hid, inner, hid, esc(text), level)
            )
            i += 1
            continue
        if _HR.match(line) and not _LIST.match(line):
            out.append("<hr>")
            i += 1
            continue
        if "|" in line and i + 1 < n and _TABLE_SEP.match(lines[i + 1]) and "-" in lines[i + 1]:
            head = split_row(line)
            seps = split_row(lines[i + 1])
            if len(head) == len(seps):
                align = []
                for sp in seps:
                    if sp.startswith(":") and sp.endswith(":"):
                        align.append("c")
                    elif sp.endswith(":"):
                        align.append("r")
                    else:
                        align.append("")
                j = i + 2
                rows = []
                while j < n and lines[j].strip() and "|" in lines[j]:
                    rows.append(split_row(lines[j]))
                    j += 1
                out.append(render_table(head, align, rows, ctx))
                i = j
                continue
        if _QUOTE.match(line):
            j = i
            q = []
            while j < n and (_QUOTE.match(lines[j]) or (lines[j].strip() and q and not is_block_start(lines[j]))):
                q.append(re.sub(r"^ {0,3}> ?", "", lines[j]) if _QUOTE.match(lines[j]) else lines[j])
                j += 1
            out.append(render_quote(q, ctx))
            i = j
            continue
        m = _LIST.match(line)
        if m:
            i, html_list = render_list(lines, i, ctx, line0)
            out.append(html_list)
            continue
        # paragraph
        j = i
        para = []
        while j < n and lines[j].strip() and (j == i or not is_block_start(lines[j])):
            if j > i and "|" in lines[j] and j + 1 < n and _TABLE_SEP.match(lines[j + 1]):
                break
            para.append(lines[j].strip())
            j += 1
        text = "\n".join(para)
        body = inline(text, ctx)
        body = re.sub(r"(?: {2,}|\\)\n", "<br>\n", body)
        out.append(body if tight else "<p>%s</p>" % body)
        i = j
    return "\n".join(out)


def render_code(code, lang, ctx, line_no):
    if lang == "mermaid":
        link = ctx.mermaid_url(line_no) if ctx.mermaid_url else None
        note = ""
        if link:
            note = ' <a href="%s">View the rendered diagram on GitHub</a>.' % esc(link)
        return (
            '<figure class="diagram"><figcaption>Diagram, shown as its Mermaid source. This site has no scripts, '
            "so it cannot draw it.%s</figcaption>"
            '<pre><code class="language-mermaid">%s</code></pre></figure>' % (note, esc(code))
        )
    cls = ' class="language-%s"' % esc(lang) if lang else ""
    return '<pre class="code"><code%s>%s</code></pre>' % (cls, esc(code))


def render_table(head, align, rows, ctx):
    def cell(tag, text, a, scope=""):
        cls = ' class="al-%s"' % a if a else ""
        return "<%s%s%s>%s</%s>" % (tag, scope, cls, inline(text, ctx), tag)

    h = "".join(cell("th", t, align[k], ' scope="col"') for k, t in enumerate(head))
    body = []
    for r in rows:
        r = (r + [""] * len(head))[: len(head)]
        body.append("<tr>%s</tr>" % "".join(cell("td", t, align[k]) for k, t in enumerate(r)))
    return (
        '<div class="table-scroll" role="region" aria-label="Table" tabindex="0"><table><thead><tr>%s</tr></thead>'
        "<tbody>\n%s\n</tbody></table></div>" % (h, "\n".join(body))
    )


def render_quote(q, ctx):
    m = _ADMON.match(q[0].strip()) if q else None
    if m:
        kind = m.group(1).upper()
        inner = render_blocks(q[1:], ctx)
        return (
            '<aside class="admonition admonition-%s" role="note"><p class="admonition-title">%s</p>\n%s\n</aside>'
            % (kind.lower(), kind.capitalize(), inner)
        )
    return "<blockquote>\n%s\n</blockquote>" % render_blocks(q, ctx)


def render_list(lines, i, ctx, line0):
    n = len(lines)
    m = _LIST.match(lines[i])
    base_indent = len(m.group(1).expandtabs(4))
    ordered = m.group(2)[0].isdigit()
    start = int(re.match(r"\d+", m.group(2)).group(0)) if ordered else 1
    items = []  # list of (lines, line0)
    loose = False
    while i < n:
        m = _LIST.match(lines[i])
        if not m or len(m.group(1).expandtabs(4)) != base_indent or m.group(2)[0].isdigit() != ordered:
            break
        width = len(m.group(1).expandtabs(4)) + len(m.group(2)) + len(m.group(3))
        item = [m.group(4)]
        item0 = i
        i += 1
        while i < n:
            ln = lines[i]
            if not ln.strip():
                # blank: continue only if the next non-blank line is indented into the item
                j = i
                while j < n and not lines[j].strip():
                    j += 1
                if j < n and len(lines[j]) - len(lines[j].lstrip()) >= width:
                    item.extend([""] * (j - i))
                    i = j
                    continue
                # a blank line followed by another item of this list makes it loose
                if j < n:
                    m2 = _LIST.match(lines[j])
                    if m2 and len(m2.group(1).expandtabs(4)) == base_indent and m2.group(2)[0].isdigit() == ordered:
                        loose = True
                break
            indent = len(ln) - len(ln.lstrip())
            if indent >= width:
                item.append(ln[width:])
                i += 1
            elif _LIST.match(ln) or is_block_start(ln):
                break
            else:
                item.append(ln.strip())  # lazy continuation
                i += 1
        items.append((item, item0))
    tag = "ol" if ordered else "ul"
    attr = ' start="%d"' % start if ordered and start != 1 else ""
    parts = []
    for item, item0 in items:
        while item and not item[-1].strip():
            item.pop()
        if any(not x.strip() for x in item[1:]):
            # blank lines inside the item: its paragraphs are separate
            inner_tight = False
        else:
            inner_tight = not loose
        body = render_blocks(item, ctx, tight=inner_tight and not loose, line0=line0 + item0)
        parts.append("<li>%s</li>" % body)
    return i, "<%s%s>\n%s\n</%s>" % (tag, attr, "\n".join(parts), tag)


def render(text, ctx, line0=0):
    return render_blocks(text.split("\n"), ctx, line0=line0)
