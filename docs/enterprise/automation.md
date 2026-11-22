# Automation and scripting — design

Status: **3a implemented** (outbox, workers, rules, conditions, the non-script
actions, run history, limits, API v2, catalog, dry run). 3b-3d are still
design. Builds on custom fields and workflow (their events are the most useful
triggers). Where this document describes something that 3a does not do yet it
says so; the section "What 3a does and does not do" lists the differences in one
place.

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
disabled_reason, consecutive_failures, last_run_at, last_run_status,
created / updated.

A rule runs **as its owner**. Creating or changing a rule needs project
admin, and the user who saves a rule becomes its owner (so nobody can borrow
the permissions of another admin by editing their rule). If the owner loses
admin on the project, or cannot log in any more, the rule is disabled and
`disabled_reason` says why (`owner_not_admin`, `owner_unavailable`). The system
also disables a rule that failed `automation.maxconsecutivefailures` times in a
row (`too_many_failures`), one whose custom field or status was deleted
(`custom_field_deleted`, `status_deleted`) and the copy of a duplicated project
(`duplicated`, or `duplicated_unmapped_reference`). Saving the rule by hand
clears the reason. Actions can never do more than the owner could do by hand.

Reading and writing rules and runs needs **project admin**: conditions,
templates and run errors can reveal data of users who are not allowed to see the
whole project.

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
id, rule_id (index), rule_revision, event_id, project_id (index), task_id (null),
status (`running` | `success` | `failed` | `skipped` | `aborted`), skip_reason,
error, depth, started, finished. Unique on (event_id, rule_id): the row is
inserted first, which makes a delivery idempotent. `running` also covers a run
that waits for a retry. `skipped` is a limit (`max_depth`, `rate_limit`,
`self_trigger`) or a condition (`conditions_not_met`, `task_not_found`);
`aborted` means the owner may no longer run the rule.

### `automation_run_steps`
id, run_id (index), position, action_type, status (`success` | `failed`), error,
output (text, truncated to 16 KiB), duration_ms, unique (run_id, position). The
step row of a successful step is written in the same transaction as the work of
the step, so a step recorded as successful has been applied exactly once; a run
that is resumed starts at the first step that is not recorded as successful.

### `automation_secrets` (3b, not implemented)
id, project_id, name, value (encrypted with a key derived from
`service.secret`), created / updated. Values are write-only in the API and
are masked in run output.

Old events and runs are deleted by a daily job (`automation.retention`,
default 30 days; 0 keeps everything). Runs that are still running are never
deleted.

### Code layout

- `pkg/automation/spec` - pure data: the rule document, the catalog (triggers,
  fields, operators, actions with their parameter schemas), structural validation
  with paths, and the walk over the ids of a document (used for validation of
  references, remapping when a project is duplicated and disabling rules whose
  custom field or status is deleted). No database access.
- `pkg/models/automation_*.go` - tables (`automation_rules`, `automation_events`,
  `automation_runs`, `automation_run_steps`), the CRUD of rules, runs and retry,
  permissions (project admin), the subscription cache and the lifecycle hooks.
- `pkg/automation` - the engine: the outbox hook, workers and claiming, the
  runner, conditions, templates, actions (plan / apply) and the dry run.
- `pkg/routes/api/v2/automation.go` - the routes.

### How the outbox works

- `pkg/events` has a generic transactional hook
  (`RegisterTransactionalHook`). `DispatchOnCommit(s, event)` calls the hooks
  synchronously with the session `s`, before the transaction commits. The
  automation hook inserts the `automation_events` row there, so it commits or
  rolls back with the change. The generic CRUD handlers (`handler.Do*`) check
  `events.PendingHookError(s)` before committing: a hook that could not write
  fails the request instead of silently losing the event. Code that commits
  sessions itself (cron jobs, CalDAV, importers) does not run that check; there
  a failed outbox insert is logged and the event is lost for automation. This is
  the one known gap in "at least once".
- The hook writes a row only when `automation.enabled` is true and at least one
  enabled rule of the project listens to the event. The set of event names per
  project is cached in memory (`automation.cachettl`, 30 s). A rule change made
  through this process invalidates the cache at once (and again after commit);
  another replica notices the change after at most the TTL, so a rule enabled on
  replica A may miss events written by replica B in the last 30 seconds.
- Workers poll `status='pending' AND run_after<=now` in id order. A row is
  claimed with `UPDATE ... SET status='processing', locked_until=?,
  attempts=attempts+1 WHERE id=? AND status='pending'` and only used when exactly
  one row was changed - no `SKIP LOCKED`, so the same SQL runs on SQLite, MySQL
  and PostgreSQL. Rows left in `processing` past `locked_until` go back to
  `pending` (or `dead` when the attempts are used up). Events written by this
  process wake the workers at once; `automation.pollinterval` (1 s) finds the
  ones of other replicas.
- Events written by an action of a run carry `origin_run_id` and `depth + 1`.
  The action runs in a session that is registered under the run; the hook looks
  the session up. Only the session of the step is marked: work an action does in
  a second session of its own would lose the mark (no action does that today).
- A step failure that will happen again (rejected by a permission or workflow
  rule, invalid value, vanished task) fails the run at once. Anything else - a
  database error, a timeout - leaves the run `running` and the event goes back to
  `pending` with exponential backoff (10 s, 20 s, 40 s ...) until
  `automation.maxattempts`, then the event is `dead` and the run `failed`.

## Triggers

3a implements the triggers marked with *(3a)*. The other ones need events that
do not exist yet (labels, relations, moving between projects) or belong to 3d.

| Kind | Triggers |
|---|---|
| Task | created *(3a)*, updated (optionally: only when a given field changed) *(3a)*, deleted *(3a)*, completed *(3a)*, reopened *(3a)*, moved into / out of the project |
| Workflow | status changed (optionally from / to a status or to a category) *(3a)* |
| Custom field | value changed (a given field) *(3a)* |
| People | assignee added / removed *(3a)* |
| Content | comment added *(3a)*, attachment added *(3a)*, label added / removed, relation added |
| Time | task became overdue, due date approaching (N hours/days before), reminder fired, schedule (cron expression + time zone) |
| Manual | a button on the task ("Run automation") |
| External | incoming webhook with a per-rule secret URL |

Trigger types in the API: `task.created`, `task.updated` (option `fields`),
`task.completed`, `task.reopened`, `task.deleted`, `task.status_changed` (options
`from_status_id`, `to_status_id`, `to_category`), `task.custom_field_changed`
(option `field_id`), `task.assignee_added`, `task.assignee_removed`,
`task.comment_added`, `task.attachment_added`.

- *completed* and *reopened* are `task.updated` events in which the done flag
  changed; they need the old state, which `TaskUpdatedEvent` now carries in
  `OldTask` (in-process only, not part of the serialized event or webhook
  payload). An update that is not made through the task model and has no old
  state (assignee changes, kanban moves to a done bucket) matches *updated* but
  never *completed* / *reopened* or a `fields` filter.
- *Task deleted* runs after the task is gone; conditions and templates see the
  task as it was when it was deleted, and actions that need the task fail.
- *Task overdue* is not offered: the existing overdue events are per user, fire
  at the user's reminder time and are only dispatched when webhooks are
  configured. A real overdue / due-approaching trigger belongs to 3d.
- Label changes only produce a generic `task.updated` without old state, so
  label triggers are not offered either.
- A rule that creates tasks on `task.created` reacts to the tasks it created (the
  self-trigger guard only covers the same task); `automation.maxdepth` bounds the
  chain, but it is a way to create many tasks.

## Conditions

A tree of `all` / `any` / `not` groups over comparisons. `not` is true when none
of its children holds. A comparison is
`{field, op, value}` where `field` is a built-in task field, `status`,
`status_category`, `cf_<id>`, or a value from the trigger payload
(`event.old.status`, `event.doer.username`). The operators are `=`, `!=`, `>`, `>=`, `<`, `<=`,
`like`, `in`, `not in`, `is_empty`, `is_not_empty`. Conditions are evaluated in
memory over a normalized snapshot of the task as it is when the rule runs, not in
SQL: dates accept RFC 3339, plain dates and `now`, `now+3d`, `now-12h`; `like`
is case-insensitive, `%` is a wildcard and without one it is a substring match;
`=` and `in` are case-sensitive; for lists (assignees, labels, multi select and
user custom fields) `=` / `in` mean "contains", `!=` / `not in` "contains none";
`is_empty` is null, empty text or empty list. The user keywords `event_doer` and
`task_creator` can be used as values of user fields. Fields: the built-in task
fields, `assignees`, `labels`, `status`, `status_category`, `cf_<id>`,
`event.doer.id|username|name` and `event.old.<key>` / `event.new.<key>` (the old
and new value the event knows: the task fields for updates, `status` and
`status_category` for status changes, `value` for custom field changes,
`assignee`, `comment`, `attachment_name`). `GET /automations/catalog` lists them
with the operators of each. A condition is evaluated once, before the first
action; a resumed run does not evaluate it again. A condition can also be a
script that returns a boolean (3b).

## Actions

| Kind | Actions |
|---|---|
| Task | set a field (title, description, priority, due/start/end date — absolute or relative such as `+3d` or `none`, percent done, color) *(3a)*, set a custom field *(3a)*, mark done / reopen *(3a)*, move to project *(3a)*, duplicate, delete (to trash) |
| Workflow | change status (goes through the normal transition checks, as the rule owner) *(3a)* |
| People | assign / unassign (a user id, the event's doer, the task creator) *(3a)*, round robin over a team |
| Content | add comment *(3a)*, add / remove label *(3a)*, create subtask or related task from a template *(3a)*, add checklist to description |
| Notify | in-app notification, e-mail to users / assignees / a team (not in 3a: a persisted notification type needs a frontend renderer and a mail template) |
| Integrate | call a webhook (signed, SSRF-protected, with retry) |
| Flow | stop if a condition holds *(3a)*, wait (delay — stored as a scheduled event, never a sleeping goroutine) |
| Script | run JavaScript |

Text parameters are templates: `{{task.title}}`, `{{task.identifier}}`,
`{{task.cf.12}}`, `{{event.doer.name}}`, `{{project.title}}` (plus `task.id`,
`task.description` and `event.doer.username`). It is a plain substitution, no
logic and no code. Unknown placeholders render empty and are reported as warnings
in the dry run and in the output of the step. Select custom fields render the
option title.

Details of the 3a actions: dates in `set_field` are relative to *now*, not to the
current value; `create_subtask` / `create_related` take `due_offset` relative to
now; comment text is escaped and wrapped in paragraphs; an action that would not
change anything ("assign a user who is assigned already") succeeds with the
output "nothing to do" and writes nothing, so it causes no event. Every action
exists as a *plan* (reads, checks, resolves templates; used by the dry run) and an
*apply* (writes); a real run is plan followed by apply in one transaction.

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
| Consecutive failed runs before a rule is disabled | 20 | `automation.maxconsecutivefailures` |
| Poll interval / cache TTL / retention (days) | 1 s / 30 s / 30 | `automation.pollinterval`, `automation.cachettl`, `automation.retention` |

`automation.scripts.*` does not exist yet (3b). Chain depth: events caused by
the actions of a run have depth + 1; an event deeper than `maxdepth` is recorded
as a skipped run (`max_depth`), so with the default a person's change can start a
chain of four rules (depths 0 to 3). The step timeout cancels the database work
of the step (the session context); the run timeout cancels the whole run. Both
count as temporary failures. A rule with more actions than `maxactions` (the
limit was lowered after it was saved) fails its run without executing anything.

A rule that fails 20 times in a row is disabled automatically with the reason
`too_many_failures` (visible on the rule). The owner is not notified in 3a: that
needs a new notification type with a frontend renderer. The interpreter has no hard memory cap; the mitigations are the
timeouts (the interpreter is interrupted), the call limits, and that only
project admins can write scripts. Instances that do not trust their project
admins should turn scripts off.

A rule never reacts to an event whose chain of origin runs (`origin_run_id`,
followed up to `maxdepth`) contains a run of the same rule for the same task; the
event is recorded as a skipped run (`self_trigger`). A different task still
triggers the rule.

## API (`/api/v2`)

- `GET|POST /projects/{project}/automations`, `GET|PUT|DELETE
  /projects/{project}/automations/{rule}`
- `POST /projects/{project}/automations/{rule}/test` with `{task_id}` — dry run
  against a task of the project: evaluates the conditions, plans every action as
  the owner of the rule (templates, permissions, workflow transitions) and
  returns the plan, warnings and what would fail. Nothing is written; the event
  is simulated (you are the doer, `event.old.*` / `event.new.*` are empty).
  Scripts will run with a read-only API (3b).
- `POST /tasks/{task}/automations/{rule}/run` — manual trigger.
- `GET /projects/{project}/automations/{rule}/runs`, `GET .../runs/{run}`
  (with steps), `POST .../runs/{run}/retry`.
- `GET /automations/catalog[?project_id=]` — machine readable list of triggers,
  condition fields/operators and actions with their parameter schemas (name,
  kind, required, enum, `title_key`). With `project_id` the custom fields of the
  project are added as `cf_<id>` fields. The visual
  editor is generated from this, so a new action needs no frontend change.
- `GET|PUT|DELETE /projects/{project}/automation-secrets/{name}` (3b).

Errors: an invalid rule is `400` with code 22002 and `details.issues`, a list of
`{path, message}` for every problem (`actions[1].params.status_id`, ...). 22001
rule not found, 22003 run not found, 22004 run cannot be retried (409; only runs
with the status `failed`). Retrying a run puts its event back in the queue; steps
that succeeded are not executed again. Manual trigger (`/tasks/{task}/automations/
{rule}/run`) is 3d.

## UI

- Project settings → Automations: list with enable switch, last run status
  and time, run count; "New rule" starts from a small gallery of templates.
- Editor: three stacked blocks **When / If / Then**; conditions as nested
  groups; actions as an ordered list with drag to reorder; a script editor
  with syntax highlighting and the API reference beside it; "Test with a
  task" shows the plan before saving.
- Runs: table of runs, a run opens to its steps with output and error.

## What 3a does and does not do

Implemented: everything marked *(3a)* above, rules CRUD with validation against
the catalog (`pkg/automation/spec`) and the project (ids must exist, labels and
move targets must be usable by the saving user), the dry run, run history with
steps, retry of failed runs, the outbox with its hook, the workers, all limits of
the table above except the script ones, the retention job, project purge (deletes
rules, events, runs), project duplicate (rules are copied **disabled**; ids of
custom fields, options and statuses are remapped, labels and projects are kept),
deleting a custom field or removing / deleting a workflow status (the rules that
reference it are disabled with a reason, the delete is not blocked) and the
Prometheus metrics `vikunja_automation_runs_total{status}` and
`vikunja_automation_events{status}` (only with `metrics.enabled`).

Not implemented: `notify.in_app` and e-mail actions, the owner notification on
auto-disable, task overdue / label / relation / move triggers, scripts, `http`,
secrets, schedule, manual, incoming webhook and due-approaching triggers, the
wait action, the frontend (3c), the generated frontend client for the new routes.

Test coverage: the automation, model and web tests run on SQLite (a file in WAL
mode, like a real installation; the shared-cache in-memory database of the other
tests cannot run concurrent workers) and the automation, model and automation web
tests were also run against PostgreSQL 17. MySQL was **not** run; the SQL is plain
xorm builder code (conditional update, no `SKIP LOCKED`), times in conditions are
formatted as UTC text like the existing reminder code does.

SQLite note: a transaction that reads and then writes fails immediately when
another writer committed in between, which `busy_timeout` cannot fix. Every step
therefore starts its transaction with a write (a no-op update of its run row), so
concurrent steps wait for each other instead of failing. A genuinely busy
database is treated as a temporary failure and retried.

A run that is resumed (retry, or the next attempt) first checks that the steps it
recorded still are the actions at their positions in the rule; if the rule was
edited so that they are not, the run is `aborted` with `rule_changed` instead of
guessing. Editing the action that failed and retrying works.

## Steps

- 3a: outbox + transactional hook, workers, rules, conditions, the non-script
  actions, run history, limits, API, catalog. **Done.**
- 3b: script condition/action, `http.fetch`, secrets.
- 3c: UI.
- 3d: schedule, due-date-approaching, manual and incoming webhook triggers;
  wait action; templates gallery.
