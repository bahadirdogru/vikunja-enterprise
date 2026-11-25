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

- **Translations.** `frontend/src/i18n/lang/en.json` and `tr-TR.json`, and `pkg/i18n/lang/en.json` and `tr-TR.json`, are kept complete and identical in keys by hand. Every change that adds, changes or removes a user-facing string updates both files in the same commit. This replaces "edit only `en.json`" in [translations.md](translations.md); Crowdin is not used by the fork. The Turkish follows the [Turkish localisation standard](turkish-style.md): register, casing, punctuation, suffixes on placeholders, glossary, and which upstream strings may be changed. Check with the `fork-i18n` skill.
- **License.** `license.Init` enables every feature without a key. The upstream validation code is still in `pkg/license/` and unused; it is excluded from the `unused` linter in `.golangci.yml` instead of being deleted, to keep the difference small. The rule in [license.md](license.md) still holds: confirm before removing or changing anything else there.
- **Lint.** Do not run `mage lint:fix`; it rewrites upstream files. Run `mage lint` and fix what it reports in the files you touched.
- **Commits.** Conventional Commits, one concern per commit, committed as each step is finished. The only author is the repository's configured git identity: no `Co-Authored-By`, no tool or session trailers, no "generated with" lines.
- **Documentation.** A change that alters behaviour updates the module page in `docs/modules/`, `API.md` when the API changed, `config-raw.json` for config keys, and the log in `docs/enterprise/ROADMAP.md`. The roadmap is the single source of truth for status: write what was verified and on which database, and what was not.

## Things that bite on this code base

- Plain `go test` does not work; tests run through `mage` (see [testing.md](testing.md)).
- The Go test cache ignores `VIKUNJA_DATABASE_*`. When running against PostgreSQL, set `GOFLAGS=-count=1` or you get cached results of another database.
- Generators and tests need `LC_ALL=C` on machines with a Turkish locale; otherwise generated identifiers contain a dotless `ı`.
- The fork tests SQLite and PostgreSQL. MySQL and MariaDB are not tested and not guaranteed, but the code must stay compatible with them by construction: no engine-specific SQL, and no identifier longer than 64 characters, including the index names xorm derives from struct tags (name long indexes explicitly).
- PostgreSQL aborts the transaction after a failed statement. Code that expects an insert to fail (a unique index used as a guard) must do its follow-up read in a new session.
- Two code paths that lock the same rows must lock them in the same order. In approvals the order is task, then request.
- A test that passes on SQLite says little about locking, identifier limits or transaction behaviour. Run the suites on SQLite and PostgreSQL (`fork-db-matrix` skill) before merging anything that touches models, migrations or SQL.
- Tests of the script processes in `pkg/automation/script` are timing-sensitive. Do not run two test suites at the same time on one machine.

## Testing the fork's modules

The upstream rules in [testing.md](testing.md) hold. What is specific to the fork's tests:

- **A new endpoint goes into the permission matrix.** `pkg/webtests/huma_fork_permissions_test.go` calls every route of the six modules as an anonymous request, a stranger, a reader, a writer, an admin, the owner, link shares of every permission, API tokens (with the scope of the route, with another group's scope, with only the read permissions of the group), the token of a bot and a disabled account. Add the route to `matrixEndpoints()` with the role it needs, the permission a link share needs (`-1` when shares never get through) and the status of the first caller that does. A route that is in no API token scope fails the test.
- **Codes and statuses are held to the documentation.** `pkg/models/error_codes_docs_test.go` fails when an error code is defined in `error_*.go` and not listed in the test, and when the HTTP status of a code differs from its row in `API.md`. `pkg/webtests/huma_fork_error_codes_test.go` asserts, over HTTP, the codes that no other web test checked by number.
- **Compare the body, not only the status**, where the documentation promises that two situations look alike (a missing and an unreadable custom field, a missing project and one the caller cannot administer, a rule that does not exist and one the caller may not see).
- **Strings the server names at run time** (catalog title keys, enum values, skip and disabled reasons, report option keys) are looked up in `frontend/src/i18n/lang/{en,tr-TR}.json` by Go tests (`pkg/automation/i18n_keys_test.go`, `pkg/models/reports_options_i18n_test.go`). `mage check:translations` cannot see these references and reports about 180 of the keys as dead; that is expected, and removing them breaks the editor.
- **Boundaries**: test the limit itself (accepted) and one more (refused), with multi-byte characters where the limit is in characters. Lower the limit through the config key or the package variable (`maxTasksChangedBySave`) instead of creating 20000 rows.
- **Show that a test bites**: break the code under test on purpose (flip a comparison, remove the check), see the test fail, restore. A permission test that passes with the check removed is worth nothing.
- **Table migrations** that only create tables are run on an empty schema by `pkg/migration/20261122000000_fork_tables_test.go`; a migration that changes an existing table needs a test that starts from the old schema, like `20261124100000_test.go`.
- Run the new tests on PostgreSQL as well (`fork-db-matrix`), in particular anything that stores text, compares text or counts under a lock.

## How a change gets into `main`

Use the `fork-change` skill. In short: implement in a worktree with a failing test first, have the change reviewed by someone who has not seen the author's reasoning, fix, run the full suites on SQLite and PostgreSQL on the final tree, then fast-forward `main`.

## Website

The site <https://vikunja.bahadirdogru.com> is generated into `docs/` from the repository's Markdown (README, `ARCHITECTURE.md`, `DESIGN.md`, `API.md`, `docs/modules/`, `docs/enterprise/`) and the hand-written pages in `site/pages/`. The generator, `site/build.py`, is Python 3 standard library only. After changing any of those documents, run:

```bash
python3 site/build.py
```

and commit the result in `docs/`. It fails on a broken internal link or anchor, and on any page with inline styles, scripts or external resources (the site runs under a strict Content-Security-Policy). `python3 site/build.py --check` verifies that `docs/` is up to date without writing. Details are in `site/README.md`. Do not edit the generated `docs/*.html` by hand.
