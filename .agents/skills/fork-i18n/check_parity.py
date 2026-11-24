#!/usr/bin/env python3
"""Check that en.json and tr-TR.json have the same keys and placeholders,
then run the Turkish style checks of check_turkish.py.

Run from the repository root. Exits 1 when a pair differs or a style check
fails.
"""
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_turkish  # noqa: E402

DIRS = ("frontend/src/i18n/lang", "pkg/i18n/lang")
PLACEHOLDER = re.compile(r"\{[A-Za-z0-9_.]+\}|%(?:\[\d+\])?[sdvqf]")


def flatten(node, prefix=""):
    for key, value in node.items():
        if isinstance(value, dict):
            yield from flatten(value, prefix + key + ".")
        else:
            yield prefix + key, value


def placeholders(text):
    return sorted(PLACEHOLDER.findall(text)) if isinstance(text, str) else []


def main():
    failed = False
    for directory in DIRS:
        with open(f"{directory}/en.json", encoding="utf-8") as f:
            en = dict(flatten(json.load(f)))
        with open(f"{directory}/tr-TR.json", encoding="utf-8") as f:
            tr = dict(flatten(json.load(f)))

        missing = sorted(set(en) - set(tr))
        extra = sorted(set(tr) - set(en))
        differing = sorted(
            key for key in set(en) & set(tr)
            if placeholders(en[key]) != placeholders(tr[key])
        )
        empty = sorted(key for key, value in tr.items() if value == "" and en.get(key) != "")

        print(f"{directory}: en {len(en)} keys, tr-TR {len(tr)} keys")
        for title, keys in (
            ("missing in tr-TR", missing),
            ("only in tr-TR", extra),
            ("placeholders differ", differing),
            ("empty in tr-TR", empty),
        ):
            if keys:
                failed = True
                print(f"  {title} ({len(keys)}):")
                for key in keys:
                    print(f"    {key}")
    if check_turkish.main([]) != 0:
        failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
