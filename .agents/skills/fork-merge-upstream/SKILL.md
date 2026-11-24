---
name: fork-merge-upstream
description: Merge upstream Vikunja (`upstream/main`) into this fork and bring the fork back to a verified state. Use when asked to sync, update or merge upstream, or when `git rev-list --count HEAD..upstream/main` is not zero at a natural pause.
---

# Merging upstream into the fork

`origin` is the fork, `upstream` is `https://github.com/go-vikunja/vikunja.git`. Merge, never rebase: the fork's history is published.

## Steps

1. **Look first.**
   ```bash
   git fetch upstream
   git rev-list --left-right --count upstream/main...HEAD   # behind, ahead
   git log --oneline --stat HEAD..upstream/main
   ```
   Read what is coming: new migrations, new i18n strings, changes to files the fork also touches (`pkg/models/models.go`, `pkg/config/config.go`, `config-raw.json`, `pkg/routes/`, `frontend/src/i18n/lang/en.json`, `.golangci.yml`, `AGENTS.md`).

2. **Merge on a clean tree**, with nothing else in flight on the branch:
   ```bash
   git merge --no-edit -m "chore: merge upstream main" upstream/main
   ```
   No attribution trailers in the merge commit.

3. **Resolve conflicts in upstream's favour** for upstream's code, then re-apply the fork's small touch (the registration line, the hook call) on top. If a conflict shows that the fork changed an upstream file more than it needed to, reduce the fork's change instead of defending it. For the root `README.md`, `docs/` and `docs/CNAME` keep the fork's version.

4. **Regenerate, do not hand-merge, generated files**: `frontend/src/client/generated`, `pkg/yaegi_symbols`, `pkg/swagger/`. Run the repo's generators with `LC_ALL=C`.

5. **Translations.** Upstream's `tr-TR.json` files are written by its Crowdin sync (`chore(i18n): update translations via Crowdin`); the fork's files are the same files with the fork's keys added. Before merging, run `python3 .agents/skills/fork-i18n/check_turkish.py --upstream` to see the upstream keys whose Turkish the fork changed. When a merge conflicts in `tr-TR.json`, resolve per key:
   - **Fork keys** (not in upstream's `en.json`): keep ours.
   - **Upstream keys the fork did not change:** take theirs.
   - **Upstream keys the fork changed** (the `--upstream` list): keep ours, unless upstream's new value fixes the same error; then take theirs, so the fork's difference shrinks.
   - **Upstream keys the fork translated because upstream had none:** take upstream's new Turkish when it has no error under section 15 of the [Turkish standard](../../docs/turkish-style.md).
   - Never reorder or reformat the file while resolving.

   Do not restyle Crowdin's Turkish in the merge commit. Afterwards, translate the new upstream keys that Crowdin has not translated yet, run `check_parity.py`, and fix what it reports (for example *siz* forms or rejected terms in new Crowdin strings) in a separate commit (`i18n(tr): ...`). Remove keys upstream removed. Corrections of upstream keys are worth proposing in upstream's Crowdin too; each one accepted there is a line less that can conflict.

6. **Migrations.** Upstream's new migrations run before or between the fork's by timestamp. Check that none of them touches a table or column a fork migration also changes.

7. **Verify.**
   - `go build ./...`
   - for a small merge that does not touch the fork's areas: the tests of the packages upstream changed (`mage test:filter`);
   - otherwise the full suites, and the three-database matrix (`fork-db-matrix` skill) when models, migrations or SQL changed;
   - frontend when it changed: `pnpm lint`, `vue-tsc --noEmit`, `vitest`.

8. **Push** the merge as its own push, separate from feature work, so a problem can be traced to it.

## Afterwards

`git rev-list --count HEAD..upstream/main` must be 0. Note the merge in `docs/enterprise/ROADMAP.md` only if it changed something the fork relies on.

The upstream commits keep their own authors; that is expected and must not be rewritten.
