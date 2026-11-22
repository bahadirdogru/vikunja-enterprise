# Automation and scripting — design

Status: design. Builds on custom fields and workflow (their events are the
most useful triggers).

## Goals

- Project admins describe rules as **when** (trigger) → **if** (conditions) →
  **then** (actions), without code, in a visual editor.
- When the building blocks are not enough, an action or condition can be a
  **script** (JavaScript) with a small, safe API.
- Everything a rule does is recorded: every run, every step, its result,
  error and duration.
- Still one process, no Redis: durable work is a database outbox processed by
  workers inside the Vikunja process. Several replicas may run; a row is
  claimed by exactly one worker.
- Delivery is **at least once**. Every action step is idempotent per
  (event, rule revision, step) so a retry never performs a step twice.

## Model

### `automation_rules`
id, project_id (index), title, description, enabled, revision (int, bumped on
every change), owner_id, trigger (json), conditions (json), actions (json),
last_run_at, last_run_status, created / updated.

A rule runs **as its owner**. Creating or changing a rule needs project
admin. If the owner loses admin on the project the rule is disabled and
marked with the reason. Actions therefore can never do more than the owner
could do by hand.

### `automation_events` (the outbox)
id, project_id (index), task_id (null), name (e.g. `task.status.changed`),
payload (json), depth (int), origin_run_id (null), status (`pending` |
`processing` | `done` | `dead`), attempts, run_after, locked_until,
created.

Rows are inserted **in the same database transaction** as the change that
caused them (through a transactional hook in the event dispatcher), so an
event is never lost and never exists for a rolled back change. Only events
of projects that have at least one enabled rule are written.

### `automation_runs`
id, rule_id (index), rule_revision, event_id, task_id (null), status
(`success` | `failed` | `skipped` | `aborted`), skip_reason, error, depth,
started, finished.

### `automation_run_steps`
id, run_id (index), position, action_type, status, error, output (text,
truncated to 16 KiB), duration_ms, unique (run_id, position).

### `automation_secrets`
id, project_id, name, value (encrypted with a key derived from
`service.secret`), created / updated. Values are write-only in the API and
are masked in run output.

Old events and runs are deleted by a daily job (`automation.retention`,
default 30 days).

## Triggers

| Kind | Triggers |
|---|---|
| Task | created, updated (optionally: only when a given field changed), deleted, completed, reopened, moved into / out of the project |
| Workflow | status changed (optionally from / to a status or category) |
| Custom field | value changed (a given field) |
| People | assignee added / removed |
| Content | comment added, attachment added, label added / removed, relation added |
| Time | task became overdue, due date approaching (N hours/days before), reminder fired, schedule (cron expression + time zone) |
| Manual | a button on the task ("Run automation") |
| External | incoming webhook with a per-rule secret URL |

## Conditions

A tree of `all` / `any` / `not` groups over comparisons. A comparison is
`{field, op, value}` where `field` is a built-in task field, `status`,
`status_category`, `cf_<id>`, or a value from the trigger payload
(`event.old.status`, `event.doer.username`). Operators match the task filter
syntax so the UI and the API share one vocabulary. A condition can also be a
script that returns a boolean.

## Actions

| Kind | Actions |
|---|---|
| Task | set a field (title, description, priority, due/start/end date — absolute or relative such as `+3d`, percent done, color), set a custom field, mark done / reopen, move to project, duplicate, delete (to trash) |
| Workflow | change status (goes through the normal transition checks, as the rule owner) |
| People | assign / unassign (user, the event's doer, the task creator, round robin over a team) |
| Content | add comment, add / remove label, create subtask or related task from a template, add checklist to description |
| Notify | in-app notification, e-mail to users / assignees / a team |
| Integrate | call a webhook (signed, SSRF-protected, with retry) |
| Flow | stop if a condition holds, wait (delay — stored as a scheduled event, never a sleeping goroutine) |
| Script | run JavaScript |

Text parameters are templates: `{{task.title}}`, `{{event.doer.name}}`,
`{{task.cf.12}}`.

## Scripting

Scripts run in an embedded pure-Go JavaScript interpreter inside the Vikunja
process. There is no `require`, no file system, no process, no timers and no
network except the API below.

```js
// available globals
event            // trigger payload (read-only)
task             // the task the rule runs for, including custom field values
vikunja.tasks.get(id) / .update(id, patch) / .create(projectId, data) / .comment(id, text)
vikunja.tasks.find(filter)             // same filter syntax as the task list
vikunja.fields.get(taskId, fieldId) / .set(taskId, fieldId, value)
vikunja.status.set(taskId, statusId, comment)
vikunja.notify(userIds, text)
http.fetch(url, {method, headers, body})   // SSRF-protected, counted
secrets.get(name)
log(...)                               // goes to the run step output
```

Every `vikunja.*` call is executed with the rule owner's permissions through
the same model functions the API uses.

## Safety limits

| Limit | Default | Config |
|---|---|---|
| Kill switch (whole instance) | on | `automation.enabled` |
| Scripts allowed | on | `automation.scripts.enabled` |
| Chain depth (a rule's action triggering further rules) | 3 | `automation.maxdepth` |
| Actions per run | 25 | `automation.maxactions` |
| Step timeout | 10 s | `automation.steptimeout` |
| Run timeout | 60 s | `automation.runtimeout` |
| Script: API calls per run / HTTP requests per run | 100 / 5 | `automation.scripts.*` |
| Runs per rule per minute (excess is skipped and recorded) | 60 | `automation.ratelimit` |
| Concurrent workers | 4 | `automation.workers` |
| Retries with exponential backoff, then `dead` | 5 | `automation.maxattempts` |

A rule that fails 20 times in a row is disabled automatically and its owner
is notified. The interpreter has no hard memory cap; the mitigations are the
timeouts (the interpreter is interrupted), the call limits, and that only
project admins can write scripts. Instances that do not trust their project
admins should turn scripts off.

A rule never reacts to events caused by its own run (same `origin_run_id`).

## API (`/api/v2`)

- `GET|POST /projects/{project}/automations`, `GET|PUT|DELETE
  /projects/{project}/automations/{rule}`
- `POST /projects/{project}/automations/{rule}/test` — dry run against a
  given task: evaluates conditions, returns the planned steps; scripts run
  with a read-only API.
- `POST /tasks/{task}/automations/{rule}/run` — manual trigger.
- `GET /projects/{project}/automations/{rule}/runs`, `GET .../runs/{run}`
  (with steps), `POST .../runs/{run}/retry`.
- `GET /automations/catalog` — machine readable list of triggers, condition
  fields/operators and actions with their parameter schemas. The visual
  editor is generated from this, so a new action needs no frontend change.
- `GET|PUT|DELETE /projects/{project}/automation-secrets/{name}`.

## UI

- Project settings → Automations: list with enable switch, last run status
  and time, run count; "New rule" starts from a small gallery of templates.
- Editor: three stacked blocks **When / If / Then**; conditions as nested
  groups; actions as an ordered list with drag to reorder; a script editor
  with syntax highlighting and the API reference beside it; "Test with a
  task" shows the plan before saving.
- Runs: table of runs, a run opens to its steps with output and error.

## Steps

- 3a: outbox + transactional hook, workers, rules, conditions, the non-script
  actions, run history, limits, API, catalog.
- 3b: script condition/action, `http.fetch`, secrets.
- 3c: UI.
- 3d: schedule, due-date-approaching, manual and incoming webhook triggers;
  wait action; templates gallery.
