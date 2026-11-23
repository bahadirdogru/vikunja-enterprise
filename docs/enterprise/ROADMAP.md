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
| 3b | Automation scripting: JavaScript action and condition, `http.request` action, encrypted project secrets, script editor with API reference | Done |
| 3c | Automation UI: rule list, catalog-driven When / If / Then editor, dry run, runs with steps and retry | Done |
| 3d | Automation: schedule / manual / incoming webhook triggers, wait action, templates | Planned |
| 4a | Dashboards: task aggregation API (`POST /api/v2/reports/tasks`), options endpoint, dashboards/widgets CRUD ([dashboards.md](dashboards.md)) | Done |
| 4b | Dashboards UI: SVG charts, dashboard pages with drag/resize grid, widget editor with live preview, drill-down | Done |
| 4c | Dashboards: status time, workload, time tracking widgets, CSV export | Planned |
| 5a | Approvals backend: requests and decisions, approval on workflow transitions, ad-hoc approvals, automation triggers/action, notifications ([forms-approvals.md](forms-approvals.md)) | Done |
| 5b | Approvals UI: transition approval settings, task banner with approve / reject, approvals page with badge, kanban and list markers, notifications | Done |
| 5c | Forms backend: definitions, public and signed-in submission path, limits, attachments, `form.submitted` trigger | Done |
| 5d | Forms UI: forms list, builder with live preview, submissions, standalone public form page | Done |
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
- Automation scripting added (backend + UI). Engine tests with the race
  detector, full models and webtests pass on SQLite; engine and automation
  model tests pass on PostgreSQL 17; frontend suite passes (3186 tests).
  Exercised in a browser against the real backend: script action writing a
  custom field and a comment, syntax error marked on its line, dry run,
  script condition, SSRF block on `http.request`, 5 s timeout on an endless
  loop, secrets create/replace/delete and the unreadable state.
  Known limits: no hard memory cap inside the interpreter; a project admin
  who writes scripts can read that project's secrets; the step transaction
  stays open during scripts and HTTP calls; MySQL not exercised;
  `vikunja.tasks.find` with `cf_` / status filters not tested.
- Dashboards UI added (en + tr); full frontend suite passes (3319 tests) with
  caches cleared. Exercised in a browser: default project dashboard, all 7
  offered widget types with live preview, move/resize and persistence,
  drill-down counts matching chart values, read-only second user, rename /
  delete, widget duplicate / remove, Turkish locale, 390px, dark mode.
  Display options (bar direction, stacked, legend) are stored in the browser.
  Note for test runs: delete `frontend/node_modules/.vitest-cache` after
  translation changes, a stale cache makes new keys render as raw key names.
- Approvals backend added. Full models and webtests pass on SQLite; approval
  tests incl. concurrency pass on PostgreSQL 17 and MySQL 8; automation tests
  pass with the race detector. A plain `done: true` update on a task whose
  done transition needs approval is rejected with 409 (24007) and creates no
  request; the status route and kanban moves create it (202).
  Open: one `./pkg/models/` run failed once and did not reproduce in ten
  further runs (failing test not captured); approvers' Turkish backend
  strings not added; new catalog keys have no UI translations until 5b.
- Approvals UI added (en + tr); frontend suite passes (3374 tests).
  Exercised in a browser with three users: approval settings in the workflow
  editor, request from the status menu and the done button, mode all and any,
  reject with comment and rejected status, cancel, ad-hoc approval, kanban
  marker, approvals page and badge, 390px, dark mode. Not exercised: overdue
  request, reminder notification, table marker. Plain `done: true` updates
  from the list checkbox still answer 409 and point to the status menu.
- Forms backend added (no UI yet). Full models, webtests and race-detector
  automation tests pass on SQLite; new tests pass on PostgreSQL 17 and
  MySQL 8. Public routes skip auth only without an Authorization header.
  Known limits: hourly limits are check-then-insert; honeypot hits and
  validation failures only count against the per-minute request limit; not
  exposed through MCP.
- Forms UI added (en + tr); frontend suite passes (3466 tests). Exercised in
  a browser, logged out: public form with 11 fields submitted and the task
  verified through the API (values, status, hidden defaults, attachment,
  submitter block), client validation, honeypot, full / disabled / closed,
  members form with login and return, 429, link regeneration, 320px, dark
  mode, Turkish. A server-side 422 was only shown with a mocked response. No
  QR code (would need a dependency). Relative date defaults are not offered.

## Remaining

- 1e: custom field filter UI with field names instead of `cf_<id>`.
- 3d: schedule / manual / incoming webhook / overdue triggers, wait action,
  in-app notify action.
- 4c: status time, workload and time tracking widgets, CSV export, number
  comparison.
- MCP exposure of the fork's endpoints; golangci-lint has never been run on
  the fork's code; MySQL only exercised for reports, approvals and forms; the
  arm64 image has not been started on real ARM hardware; one unexplained
  intermittent failure in `./pkg/models/`.
