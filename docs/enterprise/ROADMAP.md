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
- **Databases.** The fork is tested and supported on SQLite and PostgreSQL.
  MySQL and MariaDB are compatible by design (portable SQL, upstream's rules)
  but are not tested and not guaranteed. Decided on 2026-11-25; entries in the
  log before that date also report MySQL 8 runs, the last on that day.
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
- Approvals: a decision and a direct status change of the same task
  deadlocked (40P01 on PostgreSQL) because they locked the task row and the
  request row in opposite order. Decisions, cancellations and approver removal
  now lock the task first (`lockApprovalRequest`). The regression test
  `TestApprovals_DecisionAndStatusChangeDoNotDeadlock` failed 150 of 150
  rounds before the fix on PostgreSQL 18 (40P01) and on MySQL 8 (1213); after
  it, it passed 5 runs on each. SQLite skips it (no row locks).
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
  in a browser, see the browser pass below. (The replay protection of the
  signature came later, see the hardening entry at the end of the log.)
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
- Automation scripts run in child processes. goja checks its interrupt only
  between instructions, so one native call ran past the timeout. Of 70
  surveyed calls (array built-ins on `{length: 2**53 - 1}`, also through
  `Reflect.apply`, bound functions, a Proxy and iterators; typed arrays;
  `repeat` / `padStart`; `JSON.stringify`; `eval` and `new Function` of huge
  sources; BigInt) at a 200 ms timeout, 27 still ran after 10 s, 23 hit a
  fatal out-of-memory error (under a 3 GiB cap; in the server that error ends
  the process) and 8 overran by 0.5 to 7 s. Now every script, and the compile
  check on save, runs in the same binary re-executed with an empty
  environment; host calls come back over a pipe and are answered in the
  step's transaction. The process is killed 0.5 s after the timeout, has an
  address-space limit (`automation.scripts.maxmemory`, 256 MiB) and a CPU
  limit on Linux, and dies with the server; one that finished in time is
  pooled (workers + 1). All 70 now end within timeout + 0.5 s: as a timeout,
  a memory error or the built-in's own exception (4 `find*` calls on an
  array-like hit a nil-pointer panic in goja, reported as an internal error,
  before and after). Overhead 0.8 ms per script pooled, 25 ms per process
  start (0.2 ms in-process before). Wrapping the built-ins was rejected: the
  class is open-ended. Not closed: the memory limit is only soft on macOS and
  Windows; a script still costs up to one core for its timeout + 0.5 s; idle
  pooled processes take about 13 MB each.
- Script processes, adversarial review. A pooled process kept the previous
  script's heap: two `new Float64Array(20*2**20)` in one process, the second
  failed with the memory error and counted against its (maybe another
  project's) rule. Now the heap is collected and returned between scripts
  when more than 8 MiB of garbage is left, `MALLOC_ARENA_MAX=1` stops glibc
  from reserving 64 MiB of address space per thread, and the child reports
  its spare address space: a process is pooled only after a success or an
  ordinary exception and within 32 MiB of a new one, else retired. The
  CPU rlimit did nothing (Go ignores `SIGXCPU`); a watchdog goroutine in the
  child exits 1.5 s after the deadline or when stdin closes or the parent
  changes; preemption lets it run inside a native call with `GOMAXPROCS=1`
  (a child whose parent was SIGKILLed while it spun in `indexOf`: gone after
  0.04 s, before still running at 30 s). Children are `/proc/self/exe` on
  Linux and must send a hello with protocol and build within 5 s; started
  from one locked thread (Pdeathsig). Live processes capped at workers + 2
  (dry runs and checks share 2, wait within their timeout, else "busy").
  Lines over 4 MiB are refused before buffering (was 32 MiB with two
  copies); results, errors and logs are cut in the child; the parent counts
  calls and requests and checks sizes again; sequence numbers; unread output
  kills; the child gets `oom_score_adj` 1000. Machinery failures (spawn,
  hello, protocol, busy, unexplained death) are temporary and, when the
  retries run out, do not count towards disabling the rule; a SIGKILL not
  sent by the server counts as the script's memory. `NaN`, `Infinity` and
  `-0` from the host reach the script unchanged. The compile check takes
  the request context. Headroom at 256 MiB: 190 MiB in one typed array,
  390000 small objects, 2.1 million short strings (default kept). Overhead
  0.7 ms per pooled script (0.3 ms before with this development build),
  28 ms per process start; idle pooled process 65 MB private with a
  development build. Not closed: host calls do not take the deadline
  (a worker is held for timeout + 0.5 s + the host call in flight); the
  memory limit is soft on macOS, the BSDs and Windows (no Job Objects);
  the child is no sandbox (same uid, reused across projects).
- Script processes, second review. The server built the whole JSON of the
  start and of every host-call answer (HTML escaped, 6 bytes per `<`)
  before the child refused it at 64 MiB: `tasks.find` of 100 tasks with
  1 MiB descriptions took 2.3 GiB in one call, 4 runs on a 20 MiB task
  1.4 GiB, and the child's death was "unavailable", retried five times and
  never counted. Now both directions are estimated before encoding (8 MiB
  to the child, no HTML escapes): an answer above it is an exception in the
  script, a task above it fails the step for good without blame (anyone
  with write access can make one; fields are not cut, a script writing one
  back would destroy it). Measured after: 0 MiB extra for both repros.
  Blame is default-deny once the start was sent (an exit code, closed
  output or garbage from the child is the script's), except a SIGKILL the
  server did not send: not counted, retried only before an `http.fetch`
  (else failed, since a retry repeats the request). The hello has its own
  5 s; the compile check never falls back to compiling in the server (it
  refused the save instead; 7.9 s in the server reproduced before). One
  interactive process per project. Lost wake-up of waiters fixed, memory
  message says "hit the memory limit of N MiB", one error at worker start
  when scripts cannot run, every run of a given-up event fails with its
  own rule's error, the child is started with an argument so an upstream
  binary stops with "unknown command". Not closed: two projects can still
  hold both extra processes; memory pressure on the server can fail any
  rule's run (not counted); the child is no sandbox.
- Script processes, third pass: an answer too large for the script that it
  does not catch is not counted either (someone with write access to a task
  the owner can read could otherwise get the rule disabled); payloads nested
  deeper than 1000 levels are refused like oversized ones and keep the
  process; specials count in the size estimate; a save checks only new or
  changed script sources, so a rule can be disabled while checking is
  unavailable.
- Review-pass operational notes: run code generators with `LC_ALL=C` on
  machines with a Turkish locale (the dotless `i` breaks generated
  identifiers). Vitest's on-disk module cache is disabled in
  `frontend/vite.config.ts` (`fsModuleCache: false`) because cached
  transforms broke the i18n virtual modules on repeat runs.

- Automation: cap on rules per project. `automation.maxrulesperproject`
  (default 100; 0 or less is replaced with the default like
  `automation.maxpendingwaits`). Creating a rule and duplicating a project
  (copies are refused whole, never half) over the cap answer 409 with code
  22014 (en and tr message, shown at the top of the rule editor). A project
  already above the cap keeps its rules: they run and can be edited,
  disabled and deleted. The count and the insert are not locked together
  (no engine-specific SQL), so concurrent creates can overshoot by about the
  number of requests in flight. Checked with model and `/api/v2` tests; not
  checked in a browser.

- Automation: replay protection for the incoming webhook. A call to a hook
  that names a secret now carries `X-Vikunja-Timestamp` and
  `X-Vikunja-Signature: sha256=` + HMAC-SHA256 over `timestamp.token.body`
  (the token of the hook URL, so a call signed for one hook is refused by a
  sibling hook with the same secret)
  (unreleased trigger, so the body-only signature is refused, not kept).
  Timestamp window `automation.webhook.tolerance` (default 5m, 0 or less
  becomes the default), up to 1 minute ahead accepted. Accepted signatures
  are kept in the new table `automation_webhook_seen` (unique on rule and
  signature, migration 20261128100000) because several instances can share
  one database and the keyvalue store is per process unless Redis is
  configured; the row is inserted in the transaction of the queued event, so
  a call refused for its payload or the rate limit does not use its
  signature up, and two identical calls at once race on the unique index
  (6 parallel identical calls: exactly 1 accepted, run on SQLite). The row keeps the
  signed timestamp (`signed_at`); the 30 s ticker deletes rows older than
  this instance's tolerance plus 2 minutes, which assumes instance clocks
  within that margin and one tolerance for all instances. A replay is
  looked up before the rate limit, so it is a 401 and costs no budget. Bad signature, malformed or missing header,
  stale or future timestamp and replay are one 401 / 22011 answer (reason
  only in the debug log). Hooks WITHOUT a secret stay callable unsigned
  and are not replay protected; this is documented. Verified: unit tests
  (valid, replay, stale, future, body-only signature, tampered body,
  malformed headers, unsigned hook unchanged, expiry cleanup, ticker
  cleanup, concurrent replay), `/api/v2` tests, the migration test (unique
  index, expiry index), the editor strings (en and tr) in vitest. Not
  checked: two real server processes against one PostgreSQL or MySQL
  (the cross-instance refusal is shown by the memory being a table, in one
  process), the editor text in a browser, a sender in another language.
  A retry of an accepted call was refused like a replay at that time (changed
  later: it now gets the `202` of the first call, see the entry on the retry
  of an accepted call). `automation.webhook.tolerance` needs a unit and at least 1s (a
  bare `300` is nanoseconds and would refuse every call: it falls back to the
  default with a warning). Changing `automation.webhook.tolerance` upward does not bring back rows
  already deleted: calls captured earlier can be replayed until their new
  window ends.
- Hardening verification (cap on rules per project and replay protection):
  `mage test:feature` and `mage test:web` pass uncached (`GOFLAGS=-count=1`)
  on SQLite, PostgreSQL 18 and MySQL 8, one database at a time. MySQL found
  the first draft of the new table: xorm builds the unique index name from
  the table name and the index name, and 64 characters is the limit there;
  the index is now called `rule_sig`. `mage lint` reports only the 12 known
  `unused` findings in `pkg/license/`. A review of these changes then found
  and fixed: replay across sibling hooks with one secret, a stale curl
  example in API.md (both examples' signature now checked against the Go
  helper with openssl), clock-skew dependent row expiry, files of a refused
  project duplicate (now refused before anything is written), and a replay
  that was answered 429 instead of 401. Re-verified on SQLite, PostgreSQL 18
  and MySQL 8, see the report of that pass.
- Approvals and disabled accounts (fixed). Disabling an account
  (`ChangeUserStatus`, the one function behind the admin routes and the
  CLI) now drops it from the undecided slots of every pending request in the
  same transaction, with the code path of a deleted user: a request that
  waited only for it ends as `no_approvers`, one where everybody who is left
  approved is approved. An approved transition for a requester whose
  account is disabled is not performed: the request ends `cancelled` with
  the new reason `requester_disabled` (decisions stay, no history row, the
  usual resolved event and notification). A disabled approver could
  already not decide and was already not asked. Verified: the new tests in
  `approvals_disabled_test.go` failed before the change and pass after it;
  `TestApprovals*`, `TestAdmin*` and `TestUser*` pass on SQLite; the frontend
  cancel-reason test passes.
- Workflow, removing a status with `move_tasks_to` (fixed). Each moved task
  now gets a status history row (comment `Moved because the status "<title>"
  was removed from the workflow.`, doer is the person who saved the
  workflow) and a `task.status.changed` event, plus `task.updated` when the
  category change flips the done flag. Chunks of 200 tasks in the
  transaction of the removal: 4 statements per chunk, 4 * ceil(N/200) for N
  tasks, N to 2N events held until the commit. Verified: the new tests in
  `workflow_status_move_test.go` failed before the change and pass after
  it; `TestWorkflow*` and `TestApprovals*` pass on SQLite. Not measured with
  a large N.
- Custom field limits (fixed). New keys `customfields.maxfieldsperproject`
  (100) and `customfields.maxoptionsperfield` (200), error codes 20006 (409),
  20007 (400) and 20008 (400, also an invalid value to code that checks for
  one, so forms need no change). Only adding is refused: a project or field
  that is already above the limit keeps working, can be edited, and an
  option list can shrink. Text, long text, url and email values are checked
  in bytes against 65535, the size of MySQL's `text` column, so a value that
  one database stores all three store; multi-byte characters count with
  their bytes. The messages of the three codes are in the error table of
  both frontend languages (with the limit as parameter) and the value editor
  checks the byte length before sending. Verified: the new Go tests failed
  before the change and pass after it (`custom_field_limits_test.go`, the
  wire test `huma_custom_field_limits_test.go`), the vitest tests of the
  helper and the message table pass; en and tr keys and placeholders are
  identical. Not verified: the editor in a browser; concurrent creates can
  overshoot the field limit by the requests in flight; field descriptions
  are still not length-checked.
- Incoming webhook, retry of an accepted call (fixed). An exact replay of an
  accepted signed call (same hook, timestamp, body and signature) now gets
  the answer of the first call, `202` with `{"queued": true}`, and queues
  nothing; two identical calls at once queue one run and both get the `202`.
  The replay is still looked up before the rate limit, so it does not use
  the budget, also when the budget is used up. A wrong or malformed
  signature, a missing, stale or future timestamp and a signature made for
  another hook are unchanged: the uniform `401` / 22011. What this tells an
  attacker who holds a captured call: previously the `401` of a replay told
  them the original had been accepted; now both answer `202`, so the answer
  tells nothing, and a call that was blocked on the way is accepted once
  when they send it first, as before. The timing is not the same (a replay
  skips the insert), the answer is. Updated: the tests that asserted `401`
  on a replay (`TestWebhook_ReplayProtection`, the `/api/v2` trigger test),
  the editor text in both languages, the route description, API.md and the
  automation module page. Verified: the changed tests failed before the
  change and pass after it (`TestWebhook*`, `TestHumaAutomation*` on SQLite).
- Verification of the four fixes above (approvals and disabled accounts,
  status removal, custom field limits, webhook retry): at commit 7d12ae6ed
  `mage test:feature` and `mage test:web` pass uncached
  (`GOFLAGS=-count=1`) on SQLite, PostgreSQL 18 and MySQL 8, one database at
  a time; the first SQLite feature run failed once in
  `TestIdempotency_ConditionsAreNotEvaluatedAgainWhenResuming` (a test that
  relies on a 1 ms backoff and real time, run while other test suites were
  running on the machine) and passed on the rerun of the whole suite. An
  earlier run of the same tree caught the missing `TestI18nParamsContract`
  cases of the new error codes, fixed in that commit. `mage lint` reports
  only the 12 known `unused` findings in `pkg/license/`; `mage
  check:yaegiSymbols` is clean; `vue-tsc --noEmit` and vitest of the touched
  frontend areas (1460 tests) pass; `pnpm lint` reports one error in
  `TaskStatus.vue` (unused `WorkflowStatusResponse`) that these changes do not
  touch. Not run: the UI in a browser, the e2e tests.
- Known defects pass (reports, forms, automation), SQLite only so far. Reports:
  `reports.maxcells` (default 50000, 0 turns it off) refuses a task report with
  more cells (groups, child groups, time points of leaf groups) with `400` /
  `23011`; the count is exact for the kept groups and checked before the
  series statement runs, and widgets show the translated message (en and tr).
  The time tracking report already read its task ids through a subquery; what
  could exceed the parameter limit were the id lists that name users,
  projects, labels, statuses and options: they are chunked (500) now, shown by
  a test with 33000 ids (fails with "too many SQL variables" on SQLite before).
  Number, bar, donut and line widgets limit drill-down and task export to
  their time range, created vs completed exports the union of both ranges.
  Forms: the address of a signed-in submitter was empty because the claims of
  a token carry none and `GetUserByID` leaves it out (the old test compared
  two empty strings); it is stored on the submission of members forms only,
  not on users or public forms (a review found that a users form would let
  anyone collect the account addresses of its submitters), the form page
  tells the submitter so, the address is cleared when the account is deleted,
  and it is kept out of the task, the placeholders and the `form.submitted`
  event. A closed members or users form answers an
  anonymous caller like an unknown token (404 / 25001) and a signed-in caller
  who may not submit with 403; an open one still answers 401. New
  `forms.users.ratelimit` (60 per user, form and hour). IPv6 clients are
  counted by /64 in the hourly per-IP limit and in the per-minute floor, which
  is the same limiter as the one of the incoming webhooks (`hooks`), so that
  one is keyed by /64 now too. The submission routes look the form up before
  Huma reads the body (a test counts the bytes read: none for an unknown
  token, a closed form, a non-member; the form that takes the submission
  reads it). Automation: retrying a run queues the event a `wait` writes
  (names the run and its rule) instead of resetting the original event; a
  test shows that a rule created, and one enabled, after the event do not run
  and the original event stays as it was. Each new regression test was run
  against the unfixed code and failed (the cap, id lookups, drill-down and
  export, closed forms, body reads, retry; the per-user limit and the key
  helpers are new code, their tests did not compile before; the IPv6 test of
  the hourly limit was not run against the unfixed code). Verified on
  SQLite (`GOFLAGS=-count=1`): `mage test:feature` and `mage test:web` pass,
  `mage lint` reports only the 12 known `unused` findings of
  `pkg/license/`, `pnpm lint` only the known error in `TaskStatus.vue`,
  `vue-tsc --noEmit` is clean, vitest of helpers, dashboards and client
  passes (127 files, 1880 tests). Not run: PostgreSQL and MySQL, a browser
  (the translated 23011 text in a widget was not looked at), IPv6 through a
  real proxy. Still open from these areas: a reachable form that does not ask
  for files still reads an upload before the 422; the per-user limit is per
  form, not across forms; the account address is stored in plain text like
  the one a form collects. After the review: the logo of a closed or full
  non-public form answers an anonymous caller like an unknown token; two
  retries of one run at the same time queue one event (the second gets
  22004); a retry whose original event was deleted is refused (22015). These
  follow-up tests were not run against the unfixed code.
- Review round on the four defect fixes (fixed unless listed below):
  - Disabling an account no longer calls into the approvals from upstream
    code. The cleanup of the approver slots hangs on the event hook
    (`events.OnCommit`, registered in `approvals_accounts.go`), runs in its own
    transaction after the status change has committed, and a failure is logged,
    never returned, so it cannot veto the disabling. It covers the admin routes
    and the CLI (both `ChangeUserStatus`; the CLI does dispatch the event after
    its commit, an earlier note here said otherwise), a disabled bot, and the
    bots of a disabled owner. `admin_user_actions.go` is back to upstream; no
    upstream line remains for this. The requests are walked in task order, which
    removes a deadlock between two accounts disabled or deleted at once. An
    approver who is only locked out by failed logins keeps the slot.
  - The status removal marks its history rows and events with `cause:
    status_removed` (a reserved marker in the stored comment, no column), the
    editor shows the sentence in the language of the viewer, the tasks are
    locked before they are read, repeating tasks and tasks that need a done
    bucket go through the normal status change, the update time is touched, a
    category change announces `task.updated`, and a status with more than 20000
    tasks is refused (`21011`). Measured on SQLite with 5001 tasks: 136
    statements, 0.42 s, about 12 MB of extra heap without a rule; with one rule
    on `task.status_changed` 5137 statements, 0.69 s, about 16 MB. Not measured
    on PostgreSQL or MySQL, or above 5001 tasks.
  - Text values with a NUL character are refused (PostgreSQL cannot store them).
  - The flaky `TestIdempotency_ConditionsAreNotEvaluatedAgainWhenResuming` now
    uses the fake clock.
  - Not done or not verified: the path of the forced move that handles tasks
    with manual kanban buckets has no test of its own; the done buckets of manual
    kanban views are still not updated when a status changes category in the
    editor (only the done flag and the event are); the history entry in the
    editor has no component test; `TestI18nParamsContract` still has the cases of
    the new error codes in the upstream-owned test file because its coverage
    check compares one table.
  - Verified at commit b37f0e3ec: `mage test:feature` and `mage test:web` exit 0
    on SQLite, PostgreSQL 18 and MySQL 8, one at a time, uncached; `mage lint`
    reports 0 issues; `vue-tsc --noEmit` and vitest of the touched areas pass.
    The new deadlock test fails with 40P01 on PostgreSQL when the requests are
    walked in request id order again, and passes in task order.
- Second review round on the defect fixes:
  - A repeating task in a removed initial status was reopened into the status
    being removed (the flag is cleared only on archiving) and ended on an
    archived status. The reopen now ignores the statuses that go away, and the
    tests assert that no task of the project is left on a removed or archived
    status.
  - Lock order of the removal: the save locks, in task order and in chunks, every
    task in a removed status and every task with a pending request that names one
    (a locking read, not an `UPDATE ... IN (subquery)`), then cancels the
    requests, which now takes task, then request, per request in task order.
    `lockApprovalRequest` uses two lookups by primary key on PostgreSQL and MySQL
    (no dependent subquery on MySQL before 8.0.21 or MariaDB 10.2); on SQLite
    it keeps the single `UPDATE` with a subquery as its first statement (one
    writer, no row locks, the write-first property is kept). Tests for a
    decision and for the disable cleanup against a status removal run on
    PostgreSQL and MySQL; both also pass on the code before this change (the
    window is narrow), so they only guard against a regression.
  - The size limit is per save: the tasks of all removed statuses and of all
    statuses whose category changes between done and not done, trashed tasks
    included, are counted before anything is written; 21011 is refused with no
    rule disabled and no request cancelled (tested for rules and approvals, not
    for forms).
  - Accounts: any status other than active in the admin event drops the slot
    (disabled, waiting for confirmation, locked by an admin); a lockout by failed
    logins never does. Whether a stored "locked" came from an admin or from
    failed logins is not recorded: if the hook was missed for an admin lock, the
    repair keeps the slot (a gap that needs a stored reason). The approval cron
    repairs the requests a failed or missed cleanup left behind. The rule table
    is in the approvals module page and tested row by row.
  - Status changes carry `event.new.cause` in the automation payload and the
    catalog.
  - Verified at commit 868e90bed: `mage test:feature` and `mage test:web` exit 0
    on SQLite, PostgreSQL 18 and MySQL 8, one at a time, uncached; `mage lint`
    reports 0 issues; `vue-tsc --noEmit`, vitest of the touched areas and
    `check_parity.py` pass. Not run: the UI in a browser.
- Test audit of the six modules (branch `wt/automation-3d`). Scope: Go tests of
  the fork's own code (models, v2 routes, `pkg/automation`, migrations); the
  frontend tests were not part of it. Method: statement coverage as a map (one
  run over `pkg/models`, `pkg/automation/...` and `pkg/webtests` with
  `-coverpkg`), then the risks in the order permissions, documented limits and
  error codes, behaviour sentences of the module pages, weak assertions,
  cross-database behaviour, migrations, events and loop guards. Every new test
  was run against code broken on purpose (about 25 mutations: a permission
  check removed, a limit moved by one, a key renamed, a rate limit keyed by the
  full IPv6 address) and failed there.
  - **Found by the new tests and fixed** (each with its own commit):
    link shares with admin permission could create, change and delete custom
    field definitions and replace and delete the workflow, although API.md
    says "never" for both (the old tests asserted the opposite and were
    changed); the list of manual rules answered link shares and told a missing
    project from one the caller cannot write to (404 against 403, which the
    module page says it does not); the lists of rules and secrets did the
    same for a missing project; the notification of an approval outcome built
    its translation keys by concatenation, so `check:translations` reported
    them as dead and as missing; `too_many_actions`,
    `duplicated_secrets_missing` and the outcome label "any" had no text.
  - **New tests**: a permission matrix over every route of the six modules
    (68 route cases) against 15 kinds of callers (anonymous, stranger, reader,
    writer, admin, owner, link shares of three permissions, API tokens with the
    scope, with another scope and with only the read permissions of the group,
    a bot's token, a disabled account), with the exact refusal status and a
    separate test of the "403 whether or not it exists" promise of automation;
    every error code is held to its row in API.md (status) and the source must
    not define a code the table lacks; the codes no web test asserted by number
    (20001 to 20005, 22006, 23006, 23010, 23011, 24004, 24010) are reached over
    HTTP; boundaries of the title length (in characters, two- and four-byte
    letters, on all three databases), of 50 approver sources and of the save
    size; the forced move of a removed status follows manual kanban buckets
    (both directions); a refused over-limit save leaves forms alone, and a
    save that goes through disables exactly the forms that name the status;
    deleting an account through `DeleteUser` anonymises its form submissions;
    duplicating a project remaps the initial status, fields and option
    defaults of its forms; the notification texts of forms and automation are
    rendered (escaping included) and the form event carries what the page says
    and nothing else; dry runs of scripts plan their writes, conditions cannot
    set a status, assignees or labels; the table migrations of five modules run
    on an empty schema; the title keys the server sends (catalog, enums, skip
    and disabled reasons, report options) exist in both language files; the
    IPv6 /64 keying of the public routes is tested through the real router.
  - **Weak assertions strengthened**: the check that a field value is refused
    only looked for a 400 and now looks for the code; the unreadable-field
    probe test compared statuses and now compares codes and messages; the
    "a project that does not exist" test of automation accepted any error and
    now requires the plain refusal; the form notification test never rendered
    the message.
  - **Open items of the earlier rounds**: the manual-kanban path of the forced
    move, the forms case of a refused save and the `DeleteUser` flow for form
    submissions are now tested; the per-user submission limit and the /64
    keying were run against broken code and fail there (the "reports" rate
    limit named in an earlier entry is the per-user limit of forms; reports
    have none).
  - `mage check:translations` still exits 1: the 4 API keys are fixed, and the
    remaining 178 frontend keys are looked up at run time from titles the server
    sends (`title_key`, `automation.enum.*`, `automation.skipReason.*`, report
    options), which the check cannot see; they are not dead and were not
    removed, and a Go test checks that each one exists in both files. Not
    touched, upstream-owned magefile.
  - **Not covered, and why**: `pkg/automation/script/child.go` (the sandbox
    process is a re-executed binary, its coverage is not collected);
    23009 (report timeout), 22008 (undecryptable secret as an answer) and
    24008 (self approval) are reached in model tests only; the 20000-task
    limit of 21011 is tested at its boundary with a lowered limit, not with
    20000 real rows; the error returns of cleanup crons and of `Error()`
    methods; the case sensitivity of `like` on text custom fields (PostgreSQL
    is case-sensitive, SQLite and MySQL are not, like the title filter of
    upstream) and the order of secrets by name across collations are not
    promised by the docs and not tested. A request whose body is invalid is
    answered 422 before the permission check of the route (Huma validates
    first), so a caller without access sees the validation errors of a body.
  - Verified: `mage test:feature` (59 packages ok) and `mage test:web` exit 0 on SQLite,
    PostgreSQL 18 and MySQL 8, one at a time, uncached, on the tree after the
    last code change; `mage lint` reports 0 issues; `check_parity.py`
    passes; `mage check:translations` still exits 1 for the reason above. A
    first full run failed once in the new matrix test, which relied on another
    test leaving time tracking switched on; it now switches the feature on
    itself. Not run: the frontend tests, a browser.

- **End-to-end pass over the six modules (2026-11-25).** 48 new Playwright
  tests (51 with the three existing fork specs) in
  `frontend/tests/e2e/{custom-fields,workflow,approvals,automation,dashboards,forms,i18n}/`,
  run through `mage test:e2e` against the real server on its own SQLite.
  - Found and fixed in the browser: a filter made only of a custom field
    condition (`Budget > 100`) was sent as search text and returned nothing;
    one click on "Create" in the dashboard list sent two requests.
  - Covered: the main journeys of each module, permissions per role in the
    UI, one screen per module in Turkish without raw keys, and the formal
    register of the public form page.
  - Second pass (same day): 18 more tests (69 in all): the status board of
    a workflow (drops, refusals, requirements dialog, approval request, view
    settings), the done checkbox through the workflow and approvals, the
    approval settings of a transition in the editor, the script and HTTP
    request actions with a script error and the retry of a failed run,
    priority, label and file answers, the redirect and the `users` access
    mode of forms, the form builder in Turkish, and the line,
    created-vs-done and time-in-status widgets against seeded history.
  - Not covered: a successful outgoing HTTP request (the e2e server keeps
    the address guard on and has no public network; only the refusal, the
    dry run and the retry are tested), a file answer on a public form
    (`forms.public.attachments` is off; the builder's refusal is tested,
    the upload runs on a `users` form), the schedule trigger firing (the
    ticker needs minutes; building, saving and reopening are tested) and
    its next run, which the editor does not show.
  - Observed, not changed: the error of a run or a step in the history is
    the server's English text, whatever the language of the user; unticking
    the done checkbox cannot be tested from the list because the list hides
    done tasks (the task page is used).
  - Changed: a closed, switched-off, full, expired, missing or private
    public form no longer also shows an error toast; a network failure or a
    server error still does.
  - Verified: the 51 fork tests pass (3 runs by the author, 1 independent
    run on the merged tree), then all 69 twice in a row; vitest 323 files / 4296 tests, `vue-tsc` and
    lint clean. The full upstream e2e suite: 587 pass, 13 fail in upstream
    specs (admin panel, invite links, OpenID, registration mail, e-mail and
    linked-account settings, API token scopes, unlicensed time tracking);
    the same 13 fail without the fork's changes of this pass, cause not
    established beyond missing mail and OpenID services. PostgreSQL is not
    used by the e2e run.

## Remaining

- Workflow: a save that would move or change more than 20000 tasks is
  refused (21011) and there is no tool to get below the limit: no bulk status
  change exists, the limit is not a config key, and changing the category of
  a large status, a cheap bulk update before, is refused as well. The way
  out today is one status change per task through the API.
- Workflow: on PostgreSQL a task can be left on a removed (archived) status
  when another request moves it into that status in the instant the save
  removes it; the save locks the tasks it finds, not the status. Older than
  the review rounds, not fixed.
- Approvals and workflow: three paths take several task locks out of task id
  order (a category flip before the removal lock in one save, the two passes
  of a user deletion, tasks that arrive in a removed status after the first
  lock). A deadlock needs a second multi-task locker on the same tasks at the
  same time; the database then aborts one of them with an error.
- Upstream test `TestConnectionAuth/closes_the_socket_when_the_token_expires`
  (`pkg/websocket`, untouched by the fork) failed once on PostgreSQL in a full
  run and passed six times in a row afterwards; cause not established.
- Workflow: changing the category of a status in the editor flips the done flag of its tasks (and announces `task.updated`), but does not move them to the done bucket of manual kanban views; tasks moved because their status was removed do.
- Default dashboards for new projects with a workflow (4c).
- MCP exposure of the fork's endpoints; the new automation routes are not in
  the MCP allow-list.
- The arm64 image has not been started on real ARM hardware. MySQL, MariaDB and
  the documented minimum database versions are not tested.
- The parts of the newest UI listed as not run end to end in the browser
  pass above (ticker-driven rules, `wait`, HMAC, workload estimate field,
  Turkish strings).
- Automation: the catalog does not expose `automation.waitmax` or
  `service.timezone` to the editor; the stuck-run reaper does not disable
  rules or update metrics; the `stop` step is still recognised by an output
  prefix; no templates gallery for the 3d types; no e-mail action.

Open after the review pass, not fixed because they need a decision:

- Automation: goja's `find`, `findIndex`, `findLast` and `findLastIndex` on
  an array-like hit a nil-pointer panic (reported as an internal error).
- SQLite: a transaction that reads and then writes fails at once when
  another connection holds the write lock (`_txlock=immediate` would change
  that for every transaction).
