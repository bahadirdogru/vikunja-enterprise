# Working in this fork

This repository is vikunja-enterprise, a fork of [go-vikunja/vikunja](https://github.com/go-vikunja/vikunja). `origin` is the fork, `upstream` is Vikunja. The fork adds custom fields, workflows, approvals, automation, dashboards and reports, and forms. Its site is <https://vikunja.bahadirdogru.com> (GitHub Pages from `main`, folder `docs/`).

Everything in `AGENTS.md` and the other files in `.agents/docs/` is upstream's and still applies, except where this page says otherwise. Where they disagree, this page wins.

## Stay mergeable with upstream

The fork must always be able to merge `upstream/main`. Every change is judged by how much it widens the difference to upstream.

- Put fork code in fork-owned places: `pkg/automation/`, the fork's own files in `pkg/models/` (custom fields, workflow, approvals, forms, dashboards, reports, automation), `pkg/routes/api/v2/` files of those modules, new files in `pkg/migration/`, `frontend/src/` files of those modules, `docs/`, the root `README.md`, `ARCHITECTURE.md`, `DESIGN.md`, `API.md`.
- Touch upstream-owned files as little as possible: a registration line, a hook call, a config key. No renames, no reformatting, no moving of upstream code. When a change needs more than a few lines in an upstream file, look for a seam (an event listener, a new file in the same package) first.
- Do not edit upstream's agent docs, skills, CI workflows or `.agents/docs/databases.md` to describe the fork. Fork rules live in this file and in the `fork-*` skills; the fork's tested database matrix lives in `README.md` and `docs/enterprise/ROADMAP.md`.
- Raw SQL and migrations must still run on upstream's minimum database versions. No engine-specific SQL.
- Merge `upstream/main` often, while conflicts are small. Use the `fork-merge-upstream` skill.
- In a report or pull request, list every upstream-owned file a change touched and why.

## What differs from upstream's rules

- **Translations.** `frontend/src/i18n/lang/en.json` and `tr-TR.json`, and `pkg/i18n/lang/en.json` and `tr-TR.json`, are kept complete and identical in keys by hand. Every change that adds, changes or removes a user-facing string updates both files in the same commit. This replaces "edit only `en.json`" in [translations.md](translations.md); Crowdin is not used by the fork. Turkish uses proper diacritics, the informal "sen" in the application and the formal "siz" on the public form page. Check with the `fork-i18n` skill.
- **License.** `license.Init` enables every feature without a key. The upstream validation code is still in `pkg/license/` and unused; it is excluded from the `unused` linter in `.golangci.yml` instead of being deleted, to keep the difference small. The rule in [license.md](license.md) still holds: confirm before removing or changing anything else there.
- **Lint.** Do not run `mage lint:fix`; it rewrites upstream files. Run `mage lint` and fix what it reports in the files you touched.
- **Commits.** Conventional Commits, one concern per commit, committed as each step is finished. The only author is the repository's configured git identity: no `Co-Authored-By`, no tool or session trailers, no "generated with" lines.
- **Documentation.** A change that alters behaviour updates the module page in `docs/modules/`, `API.md` when the API changed, `config-raw.json` for config keys, and the log in `docs/enterprise/ROADMAP.md`. The roadmap is the single source of truth for status: write what was verified and on which database, and what was not.

## Things that bite on this code base

- Plain `go test` does not work; tests run through `mage` (see [testing.md](testing.md)).
- The Go test cache ignores `VIKUNJA_DATABASE_*`. When running against PostgreSQL or MySQL, set `GOFLAGS=-count=1` or you get cached results of another database.
- Generators and tests need `LC_ALL=C` on machines with a Turkish locale; otherwise generated identifiers contain a dotless `ı`.
- MySQL limits identifiers to 64 characters, including the index names xorm derives from struct tags. Name long indexes explicitly.
- PostgreSQL aborts the transaction after a failed statement. Code that expects an insert to fail (a unique index used as a guard) must do its follow-up read in a new session.
- Two code paths that lock the same rows must lock them in the same order. In approvals the order is task, then request.
- A test that passes on SQLite says little about locking, identifier limits or transaction behaviour. Run the three-database matrix (`fork-db-matrix` skill) before merging anything that touches models, migrations or SQL.
- Tests of the script processes in `pkg/automation/script` are timing-sensitive. Do not run two test suites at the same time on one machine.

## How a change gets into `main`

Use the `fork-change` skill. In short: implement in a worktree with a failing test first, have the change reviewed by someone who has not seen the author's reasoning, fix, run the full suites on SQLite, PostgreSQL and MySQL on the final tree, then fast-forward `main`.
