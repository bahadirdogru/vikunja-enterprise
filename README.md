# vikunja-enterprise

An independent community fork of [Vikunja](https://github.com/go-vikunja/vikunja), the self-hosted to-do and project app. The fork keeps upstream's Go API and Vue 3 frontend and adds project workflows, custom fields, approvals, forms, automation and dashboards on top.

[![License: AGPL-3.0-or-later](https://img.shields.io/badge/License-AGPL--3.0--or--later-blue.svg)](LICENSE)

> [!IMPORTANT]
> **This is not an official Vikunja product.** It is not affiliated with, endorsed by or supported by the Vikunja project or its maintainers. Do not ask them for help with this fork.
>
> Almost everything here is their work. If Vikunja is useful to you, please [support the upstream project](https://vikunja.io/support/).

- Fork: <https://github.com/bahadirdogru/vikunja-enterprise>
- Upstream: <https://github.com/go-vikunja/vikunja>
- Image: `ghcr.io/bahadirdogru/vikunja-enterprise` (linux/amd64 and linux/arm64)

## Table of contents

- [What this fork adds](#what-this-fork-adds)
- [Feature status at a glance](#feature-status-at-a-glance)
- [Quick start with Docker](#quick-start-with-docker)
- [Configuration](#configuration)
- [Supported databases](#supported-databases)
- [Development](#development)
- [Status and known limitations](#status-and-known-limitations)
- [Relationship to upstream](#relationship-to-upstream)
- [Security reports](#security-reports)
- [Contributing](#contributing)
- [License](#license)

## What this fork adds

The design documents in [docs/enterprise/](docs/enterprise/) are the detailed reference; the [roadmap](docs/enterprise/ROADMAP.md) tracks what is done and what is open. New features live in their own packages and tables, and all new API routes are on `/api/v2`.

### Custom fields

Per-project fields on tasks. Design: [custom-fields.md](docs/enterprise/custom-fields.md).

- Thirteen types: text, long text, number, currency, date, checkbox, select, multi select, user, url, email, rating and progress. Values are validated per type (min/max, option membership, read access for user fields).
- Filter and sort tasks by field in the API (`cf_<id>`). In the filter input you type the field name (`Budget > 100`) and the UI translates it, so renaming a field does not break a saved filter.
- Values can be embedded in task lists, shown as table columns and as fields on kanban cards.
- Fields can be required on workflow transitions, used in automation conditions and actions, and asked for in forms.

### Workflows and approvals

Real statuses on a task, independent of kanban buckets. Design: [workflow.md](docs/enterprise/workflow.md) and, for approvals, [forms-approvals.md](docs/enterprise/forms-approvals.md#approvals).

- A project can have a workflow with statuses (each with a category, including done and cancelled), optional restriction to listed transitions, and per-transition rules: minimum permission, allowed teams, required fields and a required comment.
- Status history per task; the done flag stays in sync with the status category.
- Editor in project settings (list and matrix), status menu and history on the task, kanban status mode (columns are statuses, a drag is a transition), table column, badges, and filter and sort by `status` and `status_category`.
- A transition can require approval: one or several approvers (people, teams, assignees, a user field), mode "any" or "all", a deadline and a status after rejection. Ad-hoc approval requests work on any task. There is an approvals page with a badge, and notifications.
- A plain "done" update that needs an approval is refused with `409` / `24007`; the response names the target status and any pending request, and the UI offers to request the approval.

### Automation

Rules of the form When / If / Then, run by a database outbox and workers inside the same process (no separate worker container). Design: [automation.md](docs/enterprise/automation.md).

- **Triggers:** task created, updated, completed, reopened, deleted; status changed; custom field changed; assignee added or removed; comment or attachment added; approval requested or resolved; form submitted; schedule (cron expression, in the server's time zone); task overdue; due soon; manual run from a task; incoming webhook with a per-rule secret URL and an optional HMAC signature.
- **Conditions:** nested all / any / not groups over task fields, status, custom fields and the event payload.
- **Actions:** set a field or custom field, change status (through the normal transition checks), assign, comment, labels, subtasks and related tasks, create a task, notify users or a team, request an approval, wait (a stored delay, not a sleeping goroutine), stop, `http.request` (signed, SSRF-protected, with retry) and JavaScript scripts.
- **Scripts and secrets:** sandboxed JavaScript for actions and conditions, with an API reference in the editor. Project secrets are stored encrypted and masked.
- **Dry run and history:** test a saved rule without writing anything; every run is recorded with its steps and can be retried.
- **Safety limits:** maximum chain depth, actions per rule, step and run timeouts, a per-rule rate limit, automatic disabling after repeated failures, caps on waits and on what one ticker pass starts, and loop guards so a rule does not fire for tasks its own runs created. Each limit is a config key, and there are kill switches for automation and for scripts.

### Dashboards and reports

Dashboards of widgets over task filters. Every number is computed over the tasks the viewer can read. Design: [dashboards.md](docs/enterprise/dashboards.md).

- Aggregation API (`POST /api/v2/reports/tasks`) with grouping, time buckets, measures and limits; dashboards and widgets CRUD.
- Widgets: number (with previous-period comparison), bar, donut, line, created vs done, task list, text, time in status, workload and time tracking.
- Hand-written SVG charts, a drag-and-resize grid, a widget editor with live preview, drill-down to the tasks behind a value, and CSV export.

### Forms

Forms that turn submissions into tasks. Design: [forms-approvals.md](docs/enterprise/forms-approvals.md).

- Builder in project settings with a live preview; fields map to task fields, labels, attachments and custom fields; hidden fields pre-set a value.
- Access modes: project members, any signed-in user, or public (a standalone page, no login).
- Submissions are listed per form and create the task as the form's owner through the normal create path, so workflows, custom field validation and automation apply. `form.submitted` is an automation trigger.
- Branding for the form page: brand name, accent colour, footer and a logo.
- Protections for public forms: per-IP and per-form hourly limits, a per-IP request floor, a honeypot, size limits, an optional maximum number of submissions and a close date. Attachments on public forms are off by default.

## Feature status at a glance

"UI" means the web client. Details and open points are in the [roadmap](docs/enterprise/ROADMAP.md).

| Area | Backend | UI | Notes |
|---|---|---|---|
| Custom fields | Done | Done | Field names in the filter input included. Not exposed over MCP. |
| Workflows | Done | Done | Kanban status mode, table column, badges. |
| Approvals | Done | Done | A refused approval-gated "done" update explains itself and offers a request. |
| Automation core and scripting | Done | Done | JavaScript, `http.request`, secrets, dry run, run history. |
| Automation time, manual and webhook triggers | Done | Done | Plus the wait, create task and notify actions. UI not checked in a browser. |
| Dashboards and reports | Done | Done | Time in status, workload, time tracking, CSV export. New widgets not checked in a browser. Default dashboards for new workflow projects are open. |
| Forms | Done | Done | Branding UI not checked in a browser. |
| Organisation / tenant layer, AI features | - | - | Deferred. |

## Quick start with Docker

The image is built from the repository's `Dockerfile` as one binary on a `scratch` base. The API and the web frontend are served by the same process on port `3456`, and the container runs as user `1000`.

CI publishes `ghcr.io/bahadirdogru/vikunja-enterprise` with `.github/workflows/enterprise-docker.yml`: the tags `main` and `sha-<short>` on pushes to `main`, and `<tag>` plus `latest` for tags that start with `e`.

```bash
mkdir -p vikunja/db vikunja/files
sudo chown -R 1000:1000 vikunja

docker run -d --name vikunja \
  -p 3456:3456 \
  -e VIKUNJA_SERVICE_PUBLICURL=https://tasks.example.com/ \
  -e VIKUNJA_SERVICE_SECRET=change-me-to-a-long-random-string \
  -v "$PWD/vikunja/db:/db" \
  -v "$PWD/vikunja/files:/app/vikunja/files" \
  ghcr.io/bahadirdogru/vikunja-enterprise:main
```

- **Database.** Without further settings the image uses SQLite at `/db/vikunja.db` (`VIKUNJA_DATABASE_PATH`), so mount `/db` and make it writable for user `1000`. For MySQL or PostgreSQL set `VIKUNJA_DATABASE_TYPE` and the connection settings; the keys are in [`config-raw.json`](config-raw.json).
- **Files.** Attachments go to `files` (`files.basepath`) relative to the root path, which the image sets to `/app/vikunja/`.
- **Secret.** `service.secret` signs tokens. If it is unset a random one is generated at every start, which invalidates all issued tokens on each restart. Project secrets of automation rules are encrypted with it as well, so changing it makes them unreadable until they are set again.
- **Public URL.** Set `service.publicurl` to the address people really use. Incoming webhook addresses of automation rules are built from it, and so are links in e-mails. The automation engine also uses its host to recognise this server's own webhook endpoints and refuse calls to them. Forms are served from `/forms/<token>` on the same origin.
- **Reverse proxy.** Put TLS in front, and check `service.ipextractionmethod` and `service.trustedproxies`: the per-IP limits of public forms and webhooks use the address the router determines.
- **arm64.** The arm64 image is built in CI but has not been started on real ARM hardware yet.

For everything generic (mail, authentication, reverse proxy examples, upgrades, backups) the [upstream installation docs](https://vikunja.io/docs/installing/) apply.

## Configuration

Settings come from a `config.yml` or from environment variables; environment variables override the file. The variable name is `VIKUNJA_` plus the key in upper case with dots as underscores, so `automation.ticker.interval` is `VIKUNJA_AUTOMATION_TICKER_INTERVAL`. The full list with comments is [`config-raw.json`](config-raw.json); `config.yml.sample` is not checked in; it is generated from the JSON with `mage generate:config-yaml`, so edit the JSON, not the sample.

Fork-specific keys you are most likely to touch, with their defaults (plus `service.publicurl` and `service.timezone`, which come from upstream):

| Key | Default | What it does |
|---|---|---|
| `service.publicurl` | empty | Public address of the instance; used in e-mails and for incoming webhook addresses. Set it. |
| `service.timezone` | `GMT` | Time zone of the server. Cron schedules of automation rules are evaluated in it. |
| `automation.enabled` | `true` | Kill switch for all automation. When off, nothing is recorded and no rule runs. |
| `automation.scripts.enabled` | `true` | Kill switch for scripts. Turn it off if you do not trust your project admins. |
| `automation.workers` | `4` | Concurrent automation workers in this process. |
| `automation.maxdepth` | `3` | How long a chain of rules triggering rules may be. `0` forbids chains. |
| `automation.ratelimit` | `60` | Runs per rule per minute. |
| `automation.retention` | `30` | Days to keep finished events, runs and steps. `0` keeps them forever. |
| `automation.ticker.interval` | `30s` | How often schedules, overdue and due-soon rules are checked. |
| `automation.waitmax` | `720h` | Longest pause the `wait` action may request. |
| `automation.webhook.ratelimit` | `60` | Incoming webhook calls per rule and minute. |
| `reports.cachettl` | `30s` | In-memory cache of report results. `0` disables it. |
| `reports.maxrows` | `50000` | Rows an in-memory report or a CSV export may read before it is rejected. |
| `forms.enabled` | `true` | Kill switch for all forms. |
| `forms.public.enabled` | `true` | Whether forms with public access can exist. |
| `forms.public.attachments` | `false` | Whether public forms may ask for file attachments. |
| `forms.public.ratelimit` | `10` | Public submissions per client IP and hour. |
| `forms.public.formratelimit` | `300` | Public submissions per form and hour. |

`config-raw.json` has more `automation.*` limits (step and run timeouts, script limits, scan caps) and `reports.*` limits.

## Supported databases

From [.agents/docs/databases.md](.agents/docs/databases.md). The minimums come from the hand-written SQL in the code, not from a version check at startup, and CI does not test them.

| Database | Documented minimum |
|---|---|
| MySQL | 8.0 |
| MariaDB | 10.2 |
| PostgreSQL | 9.5 |
| SQLite | 3.25 |

The fork's own test suites have been run on SQLite and PostgreSQL 17, and parts of them on MySQL 8. See [Status and known limitations](#status-and-known-limitations).

## Development

Prerequisites: Go (the version in `go.mod`), Node.js 24 or newer, pnpm, [mage](https://magefile.org/) and golangci-lint. [CONTRIBUTING.md](CONTRIBUTING.md) also describes a devenv shell that sets these up.

- **Backend:** `mage build`. `mage -l` lists all targets.
- **Tests:** `mage test:feature`, `mage test:web`, `mage test:filter <go-test-filter>`. **Plain `go test` does not work**; the tests need the setup mage provides. Save the output to a file and read it instead of re-running.
- **End-to-end tests:** `mage test:e2e ""` (not `pnpm test:e2e` directly).
- **Lint:** `mage lint:fix` for backend changes; `cd frontend && pnpm lint:fix` for frontend changes, plus `pnpm lint:styles:fix` when styles changed.
- **Frontend:** in `frontend/`, `pnpm install`, `pnpm dev` (port 4173), `pnpm test:unit`, `pnpm typecheck`. `pnpm run` lists everything.
- **API:** `/api/v1` is frozen (bug fixes and ports only). Every new route goes on `/api/v2`, in `pkg/routes/api/v2/`. The frontend reaches the API only through the generated client in `frontend/src/client/generated` and the query modules in `frontend/src/client/queries`. After changing a v2 route or schema run `mage generate:frontend-client` and commit the result. Never edit generated files by hand (this includes `pkg/swagger/`, which CI regenerates, and `config.yml.sample`).
- **Raw SQL and migrations** must work on the minimum database versions above.

More in [.agents/docs/](.agents/docs/): [API design](.agents/docs/api.md), [testing](.agents/docs/testing.md), [code style](.agents/docs/code-style.md), [translations](.agents/docs/translations.md), [dev commands](.agents/docs/dev-commands.md) and [supported databases](.agents/docs/databases.md).

Two practical notes:

- Run code generators with `LC_ALL=C` on machines with a Turkish locale; the dotless `ı` breaks generated identifiers.
- Vitest's on-disk module cache is disabled in `frontend/vite.config.ts`, because cached transforms broke the i18n virtual modules on repeat runs.

## Status and known limitations

This is a development fork. Read this section before relying on it.

**What has been tested**

- The backend suites (`mage test:feature` and `mage test:web`) passed on SQLite and, as of the review pass recorded in the roadmap, on PostgreSQL 17.
- MySQL has only been exercised for some areas (reports, approvals and forms). Automation, workflows and custom fields have not been run on MySQL.
- The arm64 image has not been started on real ARM hardware.
- The newest UI parts (form branding, the new dashboard widgets and CSV export, the automation 3d editor and manual run) have been type-checked and unit-tested but **not** checked manually in a browser.

**Known gaps**

- The incoming webhook signature has no replay protection: it covers the body only, with no timestamp or nonce. Rotate the address or the secret if a signed request may have been captured.
- The fork's endpoints are not exposed over MCP; the new automation routes are not in the MCP allow-list.
- Default dashboards for new projects with a workflow are not created yet.
- Scripts have no hard memory cap, and a project admin who writes scripts can read that project's secrets. Scripts are on by default; `automation.scripts.enabled` turns them off.
- More open points need a design decision (automation, approvals, workflow, reports, custom fields, forms, SQLite locking). They are listed under "Remaining" in the [roadmap](docs/enterprise/ROADMAP.md).

**Behaviour that differs from upstream**

- Creating a task that is already done in a workflow project whose done transition is restricted or needs approval is refused, also through CalDAV.
- Organisation / multi-tenant hierarchy and AI features are deferred.

### What "all features enabled" means

Upstream gates a few optional features (`admin_panel`, `time_tracking`, `audit_logs`, `user_invites`) behind a license key. In this fork `license.Init` in [`pkg/license`](pkg/license/) neither reads a key nor contacts a license server: `IsFeatureEnabled` is true for every known feature and `MaxUsersReached` is always false. The upstream validation code is still in the package but nothing calls it, and the `license.key` setting is still listed in `config-raw.json` without effect. That is all it means.

## Relationship to upstream

- The fork was created from upstream `main` (v2.8.0-187). The repository uses an `upstream` remote pointing at <https://github.com/go-vikunja/vikunja.git> next to `origin`, and the roadmap states the aim of keeping the diff to upstream small so upstream can be merged. There is no promised sync schedule.
- API compatibility: `/api/v1` is frozen and only gets bug fixes and ports to v2. New routes, including everything the fork adds, are on `/api/v2`.
- Upstream's documentation (installation, configuration, user guide, webhooks, CalDAV) applies to everything this README does not list.
- Upstream workflows inherited by the fork are disabled; only `enterprise-docker.yml` builds the image.

## Security reports

[SECURITY.md](SECURITY.md) is upstream's policy and points at upstream's advisories and contact page. That is right for problems that also exist in upstream Vikunja. For issues in the fork's own code (custom fields, workflows, approvals, forms, automation, dashboards and reports), use the [security advisories of this repository](https://github.com/bahadirdogru/vikunja-enterprise/security/advisories) if private reporting is available there; otherwise open an [issue](https://github.com/bahadirdogru/vikunja-enterprise/issues) that says you have a security report, without exploit details. Please do not send fork-specific problems to the upstream maintainers. The trust model and the list of non-issues in SECURITY.md apply here too.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) is inherited from upstream and its development parts apply. In short:

- Use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, ...).
- Run the linters before committing (see [Development](#development)).
- Run tests through `mage test:*` and add a test for what you change; permission paths need a positive and a negative case.
- New user-facing strings go into `frontend/src/i18n/lang/en.json` or `pkg/i18n/lang/en.json` only.
- Open an issue before starting a larger feature.

Changes that belong in Vikunja itself are better sent upstream.

## License

Most of this repository is licensed under [AGPL-3.0-or-later](LICENSE), as upstream. The contents of [`desktop/`](desktop/) are licensed under [GPL-3.0-or-later](desktop/LICENSE).

### Unsplash images

Background images from Unsplash are distributed under the [Unsplash License](https://unsplash.com/license). The license requires giving credit to the photographer and Unsplash. See [Unsplash's terms](https://unsplash.com/terms) for more information.
