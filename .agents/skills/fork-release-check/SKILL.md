---
name: fork-release-check
description: Check that `main` of this fork is fit to be deployed — repository state, suites, published image, a start on a fresh SQLite and a fresh PostgreSQL database, and the points an operator must know. Use before a deployment, before tagging a release (`e*`), or when asked whether the fork is "ready for production".
---

# Is `main` ready to deploy?

Work through the list in order and report each line as done with its result, or as not done. Do not turn "not checked" into "fine".

## 1. Repository

```bash
git status --short                                   # empty
git fetch origin && git fetch upstream
git rev-list --left-right --count origin/main...HEAD # 0 0
git rev-list --count HEAD..upstream/main             # 0, otherwise fork-merge-upstream first
git log --format=%B upstream/main..HEAD --no-merges | grep -ciE 'co-authored-by|generated with'   # 0
python3 .agents/skills/fork-i18n/check_parity.py
python3 site/build.py --check
```

No work may be half-merged: list the worktrees (`git worktree list`) and say which branches hold unmerged commits.

## 2. Suites on the commit to be deployed

- `go build ./...`, `mage lint` (0 issues)
- `test:feature` and `test:web` on SQLite and PostgreSQL (`fork-db-matrix`)
- frontend: `pnpm lint`, `vue-tsc --noEmit`, `vitest`
- the fork's e2e specs (`fork-e2e`)

A run from an earlier commit counts only when nothing but documentation changed since; say which commit each result belongs to.

## 3. The image

The workflow `Enterprise Docker image` builds `ghcr.io/<owner>/vikunja-enterprise` on every push to `main` that is not documentation only: tags `main` and `sha-<short>`; a git tag `e*` gives `<tag>` and `latest`.

```bash
gh run list -R <owner>/vikunja-enterprise --workflow "Enterprise Docker image" --limit 3
```

The newest successful run must belong to the commit to be deployed. A documentation-only push starts no build, and a newer push cancels a running one, so the `sha-` tag of the last code commit is the one to look for. Deploy a `sha-` tag or a release tag, not `main`.

## 4. Start the image on empty databases

With containers you create yourself, on free ports, removed afterwards. Never touch other containers on the machine.

```bash
IMG=ghcr.io/<owner>/vikunja-enterprise:sha-<short>
docker run -d --name vke-smoke-sqlite -p 127.0.0.1:34561:3456 \
  -v "$D/db:/db" -v "$D/files:/app/vikunja/files" \
  -e VIKUNJA_SERVICE_PUBLICURL=http://127.0.0.1:34561 -e VIKUNJA_SERVICE_SECRET=<32 characters> "$IMG"
# PostgreSQL: a fresh database, VIKUNJA_DATABASE_* set, --network host with VIKUNJA_SERVICE_INTERFACE=127.0.0.1:34562
python3 .agents/skills/fork-release-check/smoke.py http://127.0.0.1:34561
```

`smoke.py` registers a user, logs in, creates a project and a task, reads one route of each module and creates a custom field. Expected: 200 or 201 everywhere, except the workflow of a new project, which answers 404 with code 21001. Then:

- `GET /api/v1/info` shows the version of the commit and all pro features enabled;
- the log has the line `All features enabled (vikunja-enterprise).` and no `level=ERROR` or `level=FATAL`;
- restart the container and check again: the second start must not fail on migrations.

## 5. What this does not show

Say these in the report every time, because an empty database hides them:

- **Upgrading existing data.** Migrations were run on an empty schema and in the Go tests, not on a copy of the instance to be upgraded. Before the real upgrade: back up, restore the backup into a scratch database, start the new image against it, read the log.
- **The reverse proxy.** `service.publicurl`, `service.ipextractionmethod` and `service.trustedproxies` decide the per-IP limits of public forms and incoming webhooks and the links in e-mails.
- **Mail, OpenID, real outgoing HTTP** of automation rules: not exercised by any suite here.
- **The secret.** `service.secret` must stay the same across upgrades and restores; the project secrets of automation are encrypted with a key derived from it.
- **Load.** No suite measures many users or large projects.
- **arm64** is built but has not been started on ARM hardware.
- **MySQL and MariaDB** are not tested.

The operator's side of this is on the site: `site/pages/getting-started.md` (upgrading, backups, reverse proxy) and `site/pages/configuration.md`.
