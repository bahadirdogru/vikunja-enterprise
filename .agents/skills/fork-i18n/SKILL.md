---
name: fork-i18n
description: Keep `en.json` and `tr-TR.json` complete and identical in keys in this fork. Use whenever a change adds, changes, renames or removes a user-facing string in `frontend/src/i18n/lang/` or `pkg/i18n/lang/`, after merging upstream, and before merging any branch.
---

# English and Turkish, always together

In this fork every user-facing string exists in English and in Turkish at all times. This replaces upstream's "edit only `en.json`" rule; Crowdin is not used here.

## Rule

A commit that adds, changes, renames or removes a key in `en.json` makes the same change in `tr-TR.json`, in the same commit:

- frontend: `frontend/src/i18n/lang/en.json` and `tr-TR.json`
- backend (notifications, some errors): `pkg/i18n/lang/en.json` and `tr-TR.json`

Same keys, same nesting, same placeholders (`{name}`, `%s`, `%[1]s`, pluralisation with `|`).

## Check

```bash
python3 .agents/skills/fork-i18n/check_parity.py
```

It exits non-zero and lists the keys when the two files of a directory differ in keys or in placeholders, or when a Turkish string fails a style check of `check_turkish.py` (section 16 of the standard: markup, suffix on a placeholder, plural after a number, `%` after a number, `...`, *ve/veya*, stray spaces, ASCII substitutes, *I* for *İ*, rejected terms and TDK misspellings, *siz* in the app or *sen* on the public form, Title Case, *başarıyla* and *tıkla* in fork keys). Run it before every commit that touches strings and before merging. The fork-only checks need `upstream/main` fetched.

A justified exception goes into `tr_allow.txt` with a reason; do not weaken a check to silence one string.

`python3 .agents/skills/fork-i18n/check_turkish.py --upstream` lists the upstream keys whose Turkish the fork changed, and the upstream keys the fork translated because upstream had no Turkish. Those are the lines a Crowdin sync can conflict with (see the `fork-merge-upstream` skill).

When a key is renamed or removed, search the code for the old key (`grep -rn "old.key" frontend/src pkg`); a stale reference shows the raw key in the interface and no test catches it.

## Writing the Turkish

The rules are in [the Turkish localisation standard](../../docs/turkish-style.md). Read it before writing Turkish. The essentials:

- Register: *sen* in the application, *siz* for outsiders (`forms.public.*`, e-mails to form submitters). Buttons and menu commands are bare imperatives everywhere (*Kaydet*, *Gönder*). Status messages are impersonal (*Kural kaydedildi.*, *Kaydediliyor…*).
- Sentence case in every string, also where the English uses Title Case. No period after labels and buttons; full sentences end with one. `…` for an ellipsis, straight `'` and `"`.
- No suffix on a placeholder: *{name} öğesini sil*, *"{title}" kuralı*, *{date} tarihine kadar*, *Paylaşan: {user}*.
- No plural after a number: *{count} görev*. Keep the number of `|` slots of the English.
- Abbreviations take the suffix of their Turkish reading: *API'si*, *URL'yi*, *CSV'den*, *JSON'u*; product names an apostrophe: *Vikunja'da*.
- Use the glossary (section 11 of the standard). Before inventing a term, search both `tr-TR.json` files for the English word.
- Translate the meaning, not the words. Error messages say what failed and what to do.
- Never alter placeholders, markup, key names, header names, config keys, code or URLs.
- In upstream keys change the Turkish only to fix an error (section 15 of the standard); style-only rewording there costs merge conflicts.

## After an upstream merge

Upstream adds keys to `en.json` only. The parity check then fails; translate the new keys and commit them as `i18n(tr): ...`.
