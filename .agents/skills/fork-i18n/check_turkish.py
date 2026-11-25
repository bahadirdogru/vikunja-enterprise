#!/usr/bin/env python3
"""Style checks for tr-TR.json against .agents/docs/turkish-style.md.

Run from the repository root. Exits 1 when a check fails. check_parity.py
runs these checks too.

Exceptions go into tr_allow.txt next to this file, one per line:
    <check> <dir>:<key>   # why
<dir> is "frontend" or "pkg". Lines starting with # are comments.

    --upstream   also list the upstream keys whose Turkish differs from
                 upstream/main's tr-TR.json (the lines a Crowdin sync can
                 conflict with), and exit 0.
"""
import collections
import json
import os
import re
import subprocess
import sys

DIRS = {"frontend": "frontend/src/i18n/lang", "pkg": "pkg/i18n/lang"}
HERE = os.path.dirname(os.path.abspath(__file__))
ALLOW_FILE = os.path.join(HERE, "tr_allow.txt")

PLACEHOLDER = r"(?:\{[A-Za-z0-9_.]+\}|%(?:\[\d+\])?[sdvqf])"
TR_LOWER = "a-zçğıöşüâîû"
WORD = re.compile(r"[A-Za-zÇĞİÖŞÜçğıöşüÂÎÛâîû]+")
TAG = re.compile(r"</?[A-Za-z][^>]*>")

# Section 6: no suffix on a placeholder, also not after a closing quote.
SUFFIX_ON_PLACEHOLDER = re.compile(PLACEHOLDER + r"\"?['’]?[" + TR_LOWER + r"]")
# Section 8: no plural suffix after a number or a numeric placeholder.
NUMERIC = r"(?:\{(?:count|n|num|number|max|min|total|minutes|hours|days|seconds|amount|limit)\}|\b\d+)"
PLURAL_AFTER_NUMBER = re.compile(NUMERIC + r" [" + TR_LOWER + r"]+(?:ler|lar)\b")
# Section 9: percent sign before the number.
PERCENT_AFTER = re.compile(r"(?:\d|\})\s?%(?![\[sdvqf%])")

# Sections 11 and 13: rejected terms and TDK misspellings. Matched on whole
# words, case-insensitively (Turkish lower case).
REJECTED = {
    "önizleme": "ön izleme (TDK)",
    "veritabanı": "veri tabanı (TDK)",
    "işbirliği": "iş birliği (TDK)",
    "çevrimiçi": "çevrim içi (TDK)",
    "çevrimdışı": "çevrim dışı (TDK)",
    "ard arda": "art arda (TDK)",
    "zamanaşımı": "zaman aşımı (TDK)",
    "herşey": "her şey (TDK)",
    "hiç bir": "hiçbir (TDK)",
    "bir kaç": "birkaç (TDK)",
    "bir çok": "birçok (TDK)",
    "yanlız": "yalnız (TDK)",
    "yanlızca": "yalnızca (TDK)",
    "yalnış": "yanlış (TDK)",
    "devredışı": "devre dışı (TDK)",
    "kayıdı": "kaydı (TDK)",
    "eposta": "e-posta (TDK)",
    "e-mail": "e-posta (glossary)",
    "web kancası": "webhook (glossary)",
    "belirteç": "anahtar (glossary: token)",
    "jeton": "anahtar (glossary: token)",
    "süzgeç": "filtre (glossary)",
    "kova": "sütun (glossary: bucket)",
    "aksiyon": "eylem or işlem (glossary)",
    "parola": "şifre (glossary: password)",
    "oturum aç": "giriş yap (glossary: sign in)",
    "meydana geldi": "oluştu (section 10)",
    "ve/veya": "veya, ya da (section 10)",
    "çoğalt": "kopyala (glossary: duplicate)",
    "markalama": "marka görünümü (glossary: branding)",
    "onaylayıcı": "onaylayan (glossary: approver)",
}
# These match as whole words only; the others also with suffixes.
WHOLE_WORD = {"hiç bir", "bir kaç", "bir çok", "kova", "e-mail", "ve/veya", "yanlız"}
REJECTED_RE = {
    term: re.compile(
        r"(?<![" + TR_LOWER + r"])" + re.escape(term)
        + (r"(?![" + TR_LOWER + r"])" if term in WHOLE_WORD else "")
    )
    for term in REJECTED
}

# Section 3: siz forms outside the outsider pages, sen forms on them.
SIZ = re.compile(
    r"(?<![" + TR_LOWER + r"])(?:siz|size|sizi|sizin|sizden)(?![" + TR_LOWER + r"])"
    r"|[" + TR_LOWER + r"](?:ınız|iniz|unuz|ünüz)(?:ı|i|u|ü|a|e|da|de|dan|den|la|le|dır|dir)?(?![" + TR_LOWER + r"])"
    r"|[" + TR_LOWER + r"](?:yınız|yiniz|yunuz|yünüz)(?![" + TR_LOWER + r"])"
)
SEN = re.compile(
    r"(?<![" + TR_LOWER + r"])(?:sen|sana|seni|senin|senden)(?![" + TR_LOWER + r"])"
    r"|[" + TR_LOWER + r"](?:mısın|misin|musun|müsün|abilirsin|ebilirsin|ıyorsun|iyorsun|uyorsun|üyorsun|malısın|melisin)(?![" + TR_LOWER + r"])"
)
# Keys shown to people outside the instance: siz.
OUTSIDER_KEYS = (
    re.compile(r"^frontend:forms\.public\."),
    # Example forms: their labels and texts end up on the public form page.
    re.compile(r"^frontend:forms\.presets\.(?:thanks|[a-zA-Z]+\.(?:formTitle|[a-z_]+_label))$"),
)

# Proper names and terms that keep their capital inside a phrase.
PROPER = {
    "Vikunja", "Kanban", "Gantt", "CalDAV", "Todoist", "Trello", "Microsoft", "To", "Do", "TickTick",
    "Gravatar", "Marble", "OpenID", "Connect", "TOTP", "API", "URL", "JSON", "CSV", "HTTP", "HTTPS",
    "MCP", "Markdown", "Enter", "Esc", "Shift", "Ctrl", "Alt", "Tab", "Cmd", "GitHub", "Google",
    "Mistral", "Claude", "Escape", "ChatGPT", "Cursor", "LDAP", "OIDC", "SMTP", "ID", "UTC", "Webhook", "GET",
    "POST", "PUT", "PATCH", "DELETE", "Unsplash", "Inbox", "Gelen", "Kutusu", "Pazartesi", "Salı",
    "Çarşamba", "Perşembe", "Cuma", "Cumartesi", "Pazar", "Ocak", "Şubat", "Mart", "Nisan", "Mayıs",
    "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık", "Türkçe", "İngilizce",
}

FOLD = str.maketrans("çğıöşüÇĞİÖŞÜâîûÂÎÛ", "cgiosuCGIOSUaiuAIU")
# Short ASCII words that are correct Turkish although a diacritic twin exists.
ASCII_OK = {"mi", "misin", "iste", "bas", "son", "sik", "ac", "gore", "sure", "kim", "ise", "sol", "sag", "on", "ol", "us", "su"}


def lower_tr(text):
    return text.replace("I", "ı").replace("İ", "i").lower()


def flatten(node, prefix=""):
    for key, value in node.items():
        if isinstance(value, dict):
            yield from flatten(value, prefix + key + ".")
        else:
            yield prefix + key, value


def load(path):
    with open(path, encoding="utf-8") as f:
        return dict(flatten(json.load(f)))


def load_ref(ref, path):
    try:
        raw = subprocess.run(
            ["git", "show", f"{ref}:{path}"], capture_output=True, check=True, text=True,
        ).stdout
    except (subprocess.CalledProcessError, FileNotFoundError):
        return None
    return dict(flatten(json.loads(raw)))


def load_allow():
    allow = set()
    if os.path.exists(ALLOW_FILE):
        with open(ALLOW_FILE, encoding="utf-8") as f:
            for line in f:
                line = line.split("#", 1)[0].strip()
                if line:
                    check, key = line.split()[:2]
                    allow.add((check, key))
    return allow


def strings():
    """Yield (id, en, tr, is_fork) for every key; is_fork is None when upstream/main is unknown."""
    for name, directory in DIRS.items():
        en = load(f"{directory}/en.json")
        tr = load(f"{directory}/tr-TR.json")
        upstream_en = load_ref("upstream/main", f"{directory}/en.json")
        for key, value in tr.items():
            if not isinstance(value, str) or key not in en:
                continue
            is_fork = None if upstream_en is None else key not in upstream_en
            yield f"{name}:{key}", en[key], value, is_fork


def check(items):
    vocab = collections.Counter(lower_tr(w) for _, _, tr, _ in items for w in WORD.findall(tr))
    en_vocab = {w.lower() for _, en, _, _ in items for w in WORD.findall(en)}
    folded = collections.defaultdict(set)
    for word in vocab:
        if word.translate(FOLD) != word:
            folded[word.translate(FOLD)].add(word)

    found = collections.defaultdict(list)
    for sid, en, tr, is_fork in items:
        outsider = any(p.search(sid) for p in OUTSIDER_KEYS)
        low = lower_tr(tr)

        if sorted(TAG.findall(en)) != sorted(TAG.findall(tr)):
            found["markup"].append((sid, "tags differ from the English"))
        for m in SUFFIX_ON_PLACEHOLDER.finditer(tr):
            found["suffix-on-placeholder"].append((sid, m.group()))
        for m in PLURAL_AFTER_NUMBER.finditer(tr):
            found["plural-after-number"].append((sid, m.group()))
        for m in PERCENT_AFTER.finditer(tr):
            found["percent-after-number"].append((sid, m.group()))
        if "..." in tr:
            found["three-dots"].append((sid, "use …"))
        if "  " in tr and "  " not in en:
            found["double-space"].append((sid, repr(tr)))
        if (tr != tr.strip()) and (en == en.strip()):
            found["outer-space"].append((sid, repr(tr)))
        # A value that is the English one, word or sentence, is a string nobody translated unless it is a name, a
        # format or a code: those are listed in tr_allow.txt, each with its reason.
        if tr == en and re.search(r"[A-Za-z]{3}", re.sub(r"\{[^}]*\}|%\[?\d*\]?[sdvqf]|<[^>]*>", "", tr)):
            found["untranslated"].append((sid, tr))
        for term, regex in REJECTED_RE.items():
            if regex.search(low):
                found["rejected-term"].append((sid, f"{term} -> {REJECTED[term]}"))
        if outsider:
            for m in SEN.finditer(low):
                found["sen-on-outsider-page"].append((sid, m.group()))
        else:
            for m in SIZ.finditer(low):
                found["siz-in-app"].append((sid, m.group()))
        for w in WORD.findall(tr):
            lw = lower_tr(w)
            if (lw in folded and lw not in ASCII_OK and lw not in en_vocab
                    and not re.search(r"[çğıöşüÇĞİÖŞÜ]", w) and vocab[lw] <= 2):
                found["ascii-substitute"].append((sid, f"{w} (elsewhere: {', '.join(sorted(folded[lw]))})"))
            if len(w) > 1 and w[0] == "I" and w[1:].islower():
                dotted = "i" + lower_tr(w[1:])
                if dotted in vocab and vocab[lower_tr(w)] <= vocab[dotted]:
                    found["capital-dotless-i"].append((sid, f"{w} -> İ{w[1:]}"))
        if is_fork:
            # A capital is fine at the start of each sentence, after a colon,
            # an opening quote or parenthesis, and in "örn." examples.
            caps = []
            # Header names and other hyphenated identifiers are not words.
            plain = re.sub(r"\S*[A-Za-z]-[A-Za-z]\S*", " ", tr)
            for segment in re.split(r"[.?!:…\"“(]\s*|örn\.?\s*", plain):
                words = WORD.findall(segment)
                caps += [w for w in words[1:] if w[0].isupper() and w[1:].islower()
                         and w not in PROPER and lower_tr(w) in vocab]
            if caps:
                found["title-case"].append((sid, ", ".join(caps)))
        if is_fork and re.search(r"başarıyla", low):
            found["basariyla-in-fork-key"].append((sid, "drop başarıyla (section 10)"))
        if is_fork and re.search(r"(?<![" + TR_LOWER + r"])tıkla", low):
            found["tikla-in-fork-key"].append((sid, "name the action instead (section 10)"))
    return found


def upstream_report():
    for name, directory in DIRS.items():
        tr = load(f"{directory}/tr-TR.json")
        upstream_en = load_ref("upstream/main", f"{directory}/en.json")
        upstream_tr = load_ref("upstream/main", f"{directory}/tr-TR.json")
        if upstream_en is None or upstream_tr is None:
            print("upstream/main not available; run git fetch upstream")
            return 1
        changed = [k for k in upstream_en if k in tr and upstream_tr.get(k) and upstream_tr[k] != tr[k]]
        missing = [k for k in upstream_en if k in tr and not upstream_tr.get(k)]
        print(f"{directory}: {len(upstream_en)} upstream keys; fork's Turkish differs from upstream's "
              f"for {len(changed)}; upstream has no Turkish for {len(missing)}")
        for k in changed:
            print(f"  changed  {k}\n           upstream: {upstream_tr[k]}\n           fork:     {tr[k]}")
        for k in missing:
            print(f"  fork-only-translation  {k}")
    return 0


def main(argv):
    if "--upstream" in argv:
        return upstream_report()
    items = list(strings())
    allow = load_allow()
    found = check(items)
    failed = False
    used = set()
    for name in sorted(found):
        hits = [(sid, info) for sid, info in found[name] if (name, sid) not in allow]
        used.update((name, sid) for sid, _ in found[name] if (name, sid) in allow)
        if hits:
            failed = True
            print(f"  {name} ({len(hits)}):")
            for sid, info in hits:
                print(f"    {sid}: {info}")
    for entry in sorted(allow - used):
        print(f"  stale allow-list entry: {entry[0]} {entry[1]}")
    if not any(is_fork is not None for *_, is_fork in items):
        print("  note: upstream/main not available; fork-only checks (title-case, başarıyla, tıkla) skipped")
    print("tr-TR style: " + ("violations found" if failed else "ok"))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
