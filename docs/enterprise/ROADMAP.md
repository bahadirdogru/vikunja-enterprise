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
| 1e | Custom fields: filter UI with field names instead of `cf_<id>` (stored filters keep `cf_<id>`) | Done |
| 2a | Workflow backend: statuses, transitions, required fields, teams, history, done sync, `/api/v2` ([workflow.md](workflow.md)) | Done |
| 2b | Workflow UI: editor (list + matrix) in project settings, status menu, requirements dialog and history on the task | Done |
| 2c | Workflow in views: kanban status mode (columns = statuses, drag = transition), table column, list/card badges, filter and sort by `status` / `status_category` | Done |
| 3a | Automation core: transactional outbox, workers, rules, conditions, non-script actions, run history, limits, dry run, catalog ([automation.md](automation.md)) | Done |
| 3b | Automation scripting: JavaScript action and condition, `http.request` action, encrypted project secrets, script editor with API reference | Done |
| 3c | Automation UI: rule list, catalog-driven When / If / Then editor, dry run, runs with steps and retry | Done |
| 3d | Automation: schedule / overdue / due soon / manual / incoming webhook triggers, wait / `task.create` / notify actions, ticker, loop guards, editor support, manual run from a task | Done (editor, webhook address, dry run, manual run and notify checked in a browser; time triggers and `wait` not run end to end; no templates gallery for the new types) |
| 4a | Dashboards: task aggregation API (`POST /api/v2/reports/tasks`), options endpoint, dashboards/widgets CRUD ([dashboards.md](dashboards.md)) | Done |
| 4b | Dashboards UI: SVG charts, dashboard pages with drag/resize grid, widget editor with live preview, drill-down | Done |
| 4c | Dashboards: time in status, workload, time tracking widgets, CSV export, previous-period comparison on number widgets | Done (checked in a browser; default dashboards for new workflow projects open) |
| 5a | Approvals backend: requests and decisions, approval on workflow transitions, ad-hoc approvals, automation triggers/action, notifications ([forms-approvals.md](forms-approvals.md)) | Done |
| 5b | Approvals UI: transition approval settings, task banner with approve / reject, approvals page with badge, kanban and list markers, notifications | Done |
| 5c | Forms backend: definitions, public and signed-in submission path, limits, attachments, `form.submitted` trigger | Done |
| 5d | Forms UI: forms list, builder with live preview, submissions, standalone public form page | Done |
| 5e | Forms: brand name, accent colour, footer and logo for public forms (backend + builder settings + public page) | Done (checked in a browser) |
| 5f | Approvals: the `409` / `24007` refusal names the target status and a pending request; the UI offers "Request approval" | Done |
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

- Review pass over the fork's code (branch `chore/enterprise-overhaul`).
  golangci-lint v2.14.0 run for the first time: 240 findings, 12 left, all
  `unused` in `pkg/license`. Build the linter with `GOTOOLCHAIN=go1.27.0`;
  do not run `lint:fix` blind, its err113 autofix corrupted a test file.
  The intermittent `./pkg/models/` failure was `TestApprovals_Notifications`
  asserting the order of two mails (7 failures in 100 runs). Full
  `test:feature` and `test:web` passed on SQLite and PostgreSQL 17 at that
  time; an earlier run on PostgreSQL 9.5 passed apart from an unordered
  member list, fixed since. Fixed in the review, each with a test: scripts
  reading any task through an empty update, rules seeing tasks their owner
  lost, duplicate steps and an expiring event lock, idle workers locking
  SQLite on every poll, three issues on the public form path and a 500 on
  malformed form tokens, workflow rules skipped by entering a workflow done,
  done out of sync after a category change, report buckets in time zones
  where daylight saving starts at midnight, duplicate custom field values
  under concurrent writes. Behaviour change: creating a done task in a
  workflow project whose done transition is restricted or needs approval is
  now refused, also through CalDAV. Frontend: eslint, stylelint and vue-tsc
  are clean on the fork's files; the unit suite was not run in this pass.

- Custom field names in the filter input (1e). The input shows and accepts
  field names and option titles and sends `cf_<id>` and option ids; saved
  filters keep the id form, so renaming a field breaks nothing.
- Approvals: the `24007` refusal of a plain `done: true` update now carries
  the target status and a pending request in `details`; the client rolls the
  change back and offers "Request approval" (or opens the task when a request
  is pending).
- Form branding added (backend + UI): brand name, accent colour, footer and
  a logo (PNG, JPEG, GIF or WebP, no SVG; scaled down, size and pixel
  limits). Checked in a browser, see the browser pass below.
- Automation 3d added (backend + UI, en + tr). Triggers: schedule (cron, time
  zone of the server), task overdue, due soon, manual, incoming webhook
  (secret URL, optional HMAC signature). Actions: `wait`, `task.create`,
  `notify`. A ticker claims firings in the database so replicas never fire
  twice; arming markers keep tasks that were already overdue from firing;
  loop guards (task origins, depth, a header the API refuses with 508 on
  requests the engine sends). Editor forms, webhook address shown once,
  "Run automation..." on a task, `automation.notify` notification. Checked
  in a browser, see the browser pass below. No replay protection on the
  webhook signature.
- PostgreSQL 18 and 19: full `test:feature` and `test:web` pass uncached on
  PostgreSQL 18.7 and 19.0 (commit 995b6414f). The fork tests on the newest
  stable PostgreSQL and recommends 18 or newer. The minimum versions in
  `.agents/docs/databases.md` are upstream's and are left as they are, so
  the fork's SQL stays portable and upstream stays mergeable; they are not
  tested here.
- Database pass after automation 3d: `test:feature` passes on SQLite,
  PostgreSQL 17 and MySQL 8, `test:web` on all three, run uncached after the fixes (the 3d failures on
  PostgreSQL and MySQL were test problems, see below). Go caches test results
  without regard to `VIKUNJA_DATABASE_*`: run other databases with
  `GOFLAGS=-count=1` or the result is the previous database's. Found and
  fixed: `automation_task_origins` was not reset between tests, so rows of
  earlier tests with the same task id made the unique index fail runs and
  suppressed the due and schedule events (the loop guard read the stale
  origin); product bug: `recordTaskOrigin` now replaces an existing row for
  the task instead of failing on the unique index (ids can be handed out
  again, and cleanup of origins is only daily). The overdue, due soon,
  schedule and webhook triggers themselves behaved correctly on both
  databases. `TestClaimDue_ARealInsertErrorIsReturnedNotSwallowed` needs a
  SQLite trigger and is skipped elsewhere. The triggers migration test
  inserted an empty json value, rejected by PostgreSQL.
- Dashboards 4c added (backend + UI): time in status, workload and time
  tracking widgets, CSV export from the widget menu and the drill-down,
  previous-period comparison on number widgets (a partial period is compared
  with the same elapsed share of the previous one). Checked in a browser,
  see the browser pass below. Default dashboards for new projects with a workflow are not created.
- Browser pass over the three newest UI parts (Playwright through
  `mage test:e2e`, system Chromium, SQLite; 19 tests in
  `frontend/tests/e2e/forms/form-branding.spec.ts`,
  `dashboards/dashboard-widgets.spec.ts` and
  `automation/automation-triggers-actions.spec.ts`; screenshots are written
  only when `E2E_SCREENSHOT_DIR` is set; uncaught errors and unexpected 4xx /
  5xx responses fail the specs). Exercised: brand name, accent colour, logo
  upload / replace / refusal / removal and the footer, in the builder and on
  the public page (light, dark, 390 px wide); the three new widget types
  added in the editor and rendered with data and with none, a number widget
  with the previous-period comparison, CSV from the widget menu and from the
  drill-down (header row asserted), dark and 390 px dashboards; the
  schedule editor (bad expressions, presets), overdue, due soon, manual and
  webhook triggers with a save and reload, the webhook address shown once
  with its copy button, the leave confirmation and "create a new address"
  (the old address stops, the new one starts the rule), a dry run with a JSON
  payload, `wait` / `task.create` / notify actions, "Run automation..." on a
  task with its toast, and the `automation.notify` notification in the bell.
  Found and fixed: a pale accent (#ffe066) made links on the public form
  unreadable (1.3:1) and mid blues and greens got white button text below
  4.5:1, so links now get a per-theme shade and the button text is whichever
  of black and white contrasts more; time in status and tracked time showed
  bare seconds on the chart axis and bars (now hours and minutes); picking
  one of the new widget types sent the number widget's request to its
  endpoint, three 422 responses per pick; the accent swatch and its input
  wrapped onto two rows in the builder. Open: the builder's live preview does
  not use the accent colour; the dry-run block of a webhook rule is titled
  "Test with a task"; the accent is rounded to whole hue / saturation /
  lightness steps (a few RGB units off). Not run end to end: the 30 s ticker
  firing schedule, overdue and due soon rules, a `wait` continuation, the HMAC
  signature header, notify to the doer or assignees, the workload estimate
  field (needs a numeric custom field), a server-side validation error shown
  in the builder, and the Turkish translations of these screens.
- Review-pass operational notes: run code generators with `LC_ALL=C` on
  machines with a Turkish locale (the dotless `i` breaks generated
  identifiers). Vitest's on-disk module cache is disabled in
  `frontend/vite.config.ts` (`fsModuleCache: false`) because cached
  transforms broke the i18n virtual modules on repeat runs.

## Remaining

- Default dashboards for new projects with a workflow (4c).
- MCP exposure of the fork's endpoints; the new automation routes are not in
  the MCP allow-list.
- The arm64 image has not been started on real ARM hardware. MariaDB and the
  documented minimum database versions are not tested.
- The parts of the newest UI listed as not run end to end in the browser
  pass above (ticker-driven rules, `wait`, HMAC, workload estimate field,
  Turkish strings).
- Automation: the catalog does not expose `automation.waitmax` or
  `service.timezone` to the editor; the stuck-run reaper does not disable
  rules or update metrics; the `stop` step is still recognised by an output
  prefix; no templates gallery for the 3d types; no e-mail action.
- Reports: the time tracking report builds `IN` lists from task ids
  (parameter-limit risk); bar and donut widgets with a time range ignore it
  in drill-down and export (only number widgets are fixed).

Open after the review pass, not fixed because they need a decision:

- Automation: a script can outlive its timeout inside array natives
  (`Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`) and holds a
  worker for good. Scripts are enabled by default.
- Automation: retrying one failed run re-queues the whole event, so rules
  created after the event run for it too. No cap on rules per project.
- Automation: the incoming webhook signature covers the body only, so a
  captured request can be replayed until the secret or address changes.
- Approvals: a decision and a direct status change of the same task
  deadlock on PostgreSQL (the status change fails with 40P01). Locking the
  task first in the decision path fixed it on PostgreSQL in a trial; MySQL
  not checked.
- Approvals: an approved transition is performed for a requester whose
  account was disabled; a request whose only approver was disabled stays
  pending.
- Workflow: removing a status with `move_tasks_to` writes no history and
  fires no event.
- Reports: the response size is unbounded in groups x children x buckets
  (13 MB measured with two 20-option fields and 366 buckets).
- Custom fields: no cap on fields per project or options per field;
  `value_text` is 64 KB on MySQL and values are not length-checked.
- Forms: the e-mail of a signed-in submitter is never recorded although the
  field documentation says so; the title of a closed members form is shown
  to an anonymous holder of the link; no hourly limit on members forms;
  per-IP limits count full IPv6 addresses; uploads are buffered before the
  token is looked up.
- SQLite: a transaction that reads and then writes fails at once when
  another connection holds the write lock (`_txlock=immediate` would change
  that for every transaction).
