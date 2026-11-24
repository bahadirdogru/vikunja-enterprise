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

It exits non-zero and lists the keys when the two files of a directory differ in keys or in placeholders. Run it before every commit that touches strings and before merging.

When a key is renamed or removed, search the code for the old key (`grep -rn "old.key" frontend/src pkg`); a stale reference shows the raw key in the interface and no test catches it.

## Writing the Turkish

- Proper orthography: ç, ğ, ı, İ, ö, ş, ü. Never ASCII substitutes.
- Register: informal "sen" in the application ("sil", "kaydet", "seç"); formal "siz" on the public form page, which strangers see ("gönderin", "doldurun").
- Terms used so far, keep them: dashboard → pano, widget → bileşen, submission → yanıt, dry run → deneme, action → eylem, rule → kural, trigger → tetikleyici, approval → onay, custom field → özel alan, workflow → iş akışı, status → durum.
- Translate the meaning, not the words. Error messages say what happened and what to do next, as the English ones do.
- Do not translate identifiers shown to the user as something to type or copy: header names, config keys, code, `{placeholders}`.
- Check an existing Turkish string for the same concept before inventing a wording.

## After an upstream merge

Upstream adds keys to `en.json` only. The parity check then fails; translate the new keys and commit them as `i18n(tr): ...`.
