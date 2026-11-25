# Contributing

Contributions are welcome. This is a small development fork, so please open an issue before you start on anything larger than a fix.

{{include README.md | Contributing | body}}

## Rules specific to the fork

The guide for contributors and agents is [`.agents/docs/fork.md`](../../.agents/docs/fork.md) in the repository. The short version:

- **Stay mergeable.** The fork must always be able to merge upstream's `main`. Put fork code in fork-owned packages and files, touch upstream-owned files as little as possible, and list every upstream-owned file a change touched.
- **Translations.** `en.json` and `tr-TR.json` are kept complete and identical in keys, by hand, in the same commit as the change.
- **Documentation.** A change that alters behaviour updates the module page in `docs/modules/`, `API.md` when the API changed, `config-raw.json` for configuration keys, and the log in the roadmap.
- **Databases.** Raw SQL and migrations must run on the minimum database versions that upstream documents. Test on SQLite and PostgreSQL before merging anything that touches models, migrations or SQL.

## This website

The site is generated from the Markdown documents of the repository by a small script, so a documentation fix is a Markdown edit. See [`site/README.md`](../README.md) for the one command that regenerates `docs/`.
