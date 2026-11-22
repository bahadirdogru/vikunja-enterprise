# vikunja-enterprise roadmap

vikunja-enterprise is an independent, community fork of [Vikunja](https://github.com/go-vikunja/vikunja)
(AGPL-3.0-or-later). It is not an official Vikunja product.

## Principles

- **One image, one process.** A single Docker image (amd64 + arm64). No Redis
  requirement, no separate worker containers. Durable background work uses a
  database outbox.
- **Small upstream diff.** New features live in their own files/packages and
  tables; upstream files are touched as little as possible so upstream can be
  merged regularly.
- **Everything unlocked.** No license key, no feature gating.
- New API routes go on `/api/v2`.
- Organisation / multi-tenant hierarchy is deferred until there is a real need.

## Status

| # | Milestone | Status |
|---|---|---|
| 0a | All features enabled without a license key | Done |
| 0b | Multi-arch image (amd64 + arm64) published to GHCR | Done |
| 1a | Custom fields: backend (models, migration, `/api/v2` routes, events) | Done |
| 1b | Custom fields: frontend (project settings page, task detail editors, en + tr) | Done |
| 1c | Custom fields: filter (`cf_<id>`) and sort in the API, values removed when a task changes project | Done |
| 1d | Custom fields: values in task lists (`expand=custom_fields`), table columns, kanban card fields | Done |
| 1e | Custom fields: filter UI with field names instead of `cf_<id>` | Planned |
| 2a | Workflow backend: statuses, transitions, required fields, teams, history, done sync, `/api/v2` ([workflow.md](workflow.md)) | Done |
| 2b | Workflow UI: editor (list + matrix) in project settings, status menu, requirements dialog and history on the task | Done |
| 2c | Workflow in views: kanban status mode (columns = statuses, drag = transition), table column, list/card badges, filter and sort by `status` / `status_category` | Done |
| 3a | Automation core: transactional outbox, workers, rules, conditions, non-script actions, run history, limits, dry run, catalog ([automation.md](automation.md)) | Done |
| 3b | Automation: script condition/action, http, secrets | Planned |
| 3c | Automation UI: rule list, catalog-driven When / If / Then editor, dry run, runs with steps and retry | Done |
| 3d | Automation: schedule / manual / incoming webhook triggers, wait action, templates | Planned |
| 4a | Dashboards: task aggregation API (`POST /api/v2/reports/tasks`), options endpoint, dashboards/widgets CRUD ([dashboards.md](dashboards.md)) | Done |
| 4b | Dashboards: pages, SVG charts, widget editor, drill-down | Next |
| 4c | Dashboards: status time, workload, time tracking widgets, CSV export | Planned |
| 5a | Approvals backend ([forms-approvals.md](forms-approvals.md)) | Designed |
| 5b | Approvals UI | Planned |
| 5c | Forms backend | Designed |
| 5d | Forms UI (builder, public form page) | Planned |
| – | Organisation / tenant layer, AI features | Deferred |

## Log

- Fork created from upstream `main` (v2.8.0-187). License gate removed
  (`pkg/license`), gating tests still pass.
- Multi-arch image published as `ghcr.io/bahadirdogru/vikunja-enterprise:main`
  (linux/amd64, linux/arm64). Inherited upstream workflows are disabled on the
  fork; only `enterprise-docker.yml` runs.
- Custom fields backend added, see [custom-fields.md](custom-fields.md).
  Known gaps: not exposed through MCP yet; golangci-lint has
  not been run on the new code.
- Custom fields UI added: project settings page and per-type editors in the
  task detail view. Checked in a real browser against the API for number,
  select, multi select, rating, checkbox, progress, currency, url, email
  (invalid input rejected), text and long text. Not yet exercised in a
  browser: picking a date, picking a user, hover/focus styling, read-only
  users. Long text shows a single clipped line until focused.
- Custom field filtering and sorting added to the task list API. Full models
  and webtests suites pass on SQLite; the custom field tests also pass on
  PostgreSQL 17. MySQL not exercised. Date `=` compares exact instants, use a
  range. Picking a date and a user in the task detail view checked in a
  browser.
- Custom field values can be embedded in task lists; table view columns and
  kanban card fields added (selection stored in the browser, per project /
  per view). Checked visually.
- Workflow backend added. Full models suite passes on SQLite and PostgreSQL
  17, full webtests on SQLite; MySQL not exercised, golangci-lint not run,
  not exposed through MCP. Notes: a transition that requires a comment cannot
  be done through the done checkbox; enabling a workflow on an existing
  project assigns statuses in bulk without history rows.
- Workflow UI added (en + tr). Exercised in a browser: enable default
  workflow, add a transition with required fields and comment, perform it
  through the dialog, history, done button, removing a status with tasks,
  dark mode. Not exercised in a browser: forbidden (403) and team-restricted
  transitions, inline server validation errors. In workflow projects the done
  button posts a status change instead of a task update.
- Automation core added (backend only, no UI yet). Race-detector runs of the
  engine tests pass on SQLite; engine, models and selected webtests pass on
  PostgreSQL 17; full webtests on SQLite. Not done: MySQL, golangci-lint,
  in-app notification action, overdue and label triggers. Known gaps: code
  paths that commit their own sessions (cron, CalDAV, importers) only log a
  failed outbox insert; a rule enabled on one replica can miss another
  replica's events for up to `automation.cachettl` (30 s).
- Workflow views added. Full models and webtests suites pass on SQLite; the
  new model tests pass on PostgreSQL 17. Exercised in a browser: status-mode
  board, allowed drag, drag into a gated column completed through the dialog,
  rejected drop snapping back, create in column, table sort, badges, filter
  suggestions. Not exercised in a browser: forbidden moves, create-in-column
  that needs input, a status board after its workflow is deleted. In status
  mode a column's bucket id is the negated status id.
- Automation UI added (en + tr); full frontend unit suite passes on main.
  Exercised in a browser: rule from a template, hand-built rule with nested
  conditions, server validation error mapped to its input, dry run, real runs
  with steps, retry, enable switch, 390px width, dark mode. The dry run only
  tests saved rules.
- Reports API and dashboards backend added. The report reuses the task
  list's own scope and filter builder; a permission matrix test (7 viewers x
  11 filters x 4 scopes) asserts report totals equal task list totals. New
  tests pass on SQLite, PostgreSQL 17 and MySQL 8; full suites on SQLite
  only. Deferred: status time / workload / time tracking data, CSV export,
  number comparison, running open count.
