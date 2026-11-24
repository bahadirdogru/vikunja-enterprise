# Automation and scripting — design

Status: **3a, 3b and 3d implemented (backend and UI).** 3a: outbox, workers, rules, conditions, the
non-script actions, run history, limits, API v2, catalog, dry run. 3b: JavaScript
script actions and script conditions, the `http.request` action, `http.fetch` in
scripts and project secrets. 3d adds the schedule, overdue, due soon, manual and
incoming webhook triggers and the wait, create task and notify actions; their UI is
described under "UI status (3d)". 3c (UI) is implemented (see [ROADMAP.md](ROADMAP.md)). Builds on custom fields and
workflow (their events are the most useful triggers). Where this document
describes something that is not implemented yet it says so; the section "What 3a
and 3b do and do not do" lists the differences in one place.

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

### `automation_secrets` (3b)
id, project_id, name (unique per project, `^[A-Za-z_][A-Za-z0-9_]{0,63}$`),
value, created_by_id, created / updated. A secret is a named value of a project
(a signing key, a token, a list of user ids) that rules may use without it being
shown anywhere. Values are write-only in the API and are masked in everything a
run stores (see "Secrets" below).

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
  runner, conditions, templates, actions (plan / apply) and the dry run; for 3b
  `script_*.go` (the script action and condition, the three hosts, snapshots),
  `secrets.go` (secret store and masker).
- `pkg/automation/script` - the JavaScript runtime (goja): sandbox, limits,
  the `Host` interface. It knows nothing of the database.
- `pkg/automation/outbound` - the one HTTP client path for `http.request` and
  `http.fetch` (SSRF protection, redirects, size caps, signing).
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
  `pending` (or `dead` when the attempts are used up). The lock is renewed
  before every rule of the event, because each run has the run timeout to
  itself. Should two workers hold the same run anyway (an expired lock, a manual
  retry while the event is still being processed), a step checks again, after
  taking the write lock of its run, whether it was recorded as successful in
  the meantime, and does nothing then. Events written by this
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

3a implements the triggers marked with *(3a)*, 3d the time, manual and external ones
(*(3d)*). The others need events that do not exist yet (labels, relations, moving
between projects).

| Kind | Triggers |
|---|---|
| Task | created *(3a)*, updated (optionally: only when a given field changed) *(3a)*, deleted *(3a)*, completed *(3a)*, reopened *(3a)*, moved into / out of the project |
| Workflow | status changed (optionally from / to a status or to a category) *(3a)* |
| Custom field | value changed (a given field) *(3a)* |
| People | assignee added / removed *(3a)* |
| Content | comment added *(3a)*, attachment added *(3a)*, label added / removed, relation added |
| Time | task became overdue *(3d)*, due date approaching (N hours/days before) *(3d)*, reminder fired, schedule (cron expression, in the time zone of the server) *(3d)* |
| Manual | a button on the task ("Run automation") *(3d)* |
| External | incoming webhook with a per-rule secret URL *(3d)* |

Trigger types in the API: `task.created`, `task.updated` (option `fields`),
`task.completed`, `task.reopened`, `task.deleted`, `task.status_changed` (options
`from_status_id`, `to_status_id`, `to_category`), `task.custom_field_changed`
(option `field_id`), `task.assignee_added`, `task.assignee_removed`,
`task.comment_added`, `task.attachment_added`, `approval.requested`,
`approval.resolved` (option `outcome`: `any`, `approved`, `rejected`,
`cancelled`). The approval triggers see the fields of the request as
`event.new.*` and the placeholders `{{event.approval.id|title|outcome}}`; see
[forms-approvals.md](forms-approvals.md#approvals).

- *completed* and *reopened* are `task.updated` events in which the done flag
  changed; they need the old state, which `TaskUpdatedEvent` now carries in
  `OldTask` (in-process only, not part of the serialized event or webhook
  payload). An update that is not made through the task model and has no old
  state (assignee changes, kanban moves to a done bucket) matches *updated* but
  never *completed* / *reopened* or a `fields` filter.
- The task of an event is only handed to a rule (conditions, templates, the
  `task` global of scripts) while the owner of the rule can read it. A task
  that was moved to a project the owner cannot see since the event was written
  is treated like a task that is gone: the run is skipped (`task_not_found`), a
  run that is already under way fails.
- *Task deleted* runs after the task is gone; conditions and templates see the
  task as it was when it was deleted, and actions that need the task fail.
- *Task overdue* does not use the existing overdue events (per user, at the user's
  reminder time, only dispatched when webhooks are configured); 3d has its own
  ticker, see "Time, manual and webhook triggers (3d)".
- Label changes only produce a generic `task.updated` without old state, so
  label triggers are not offered either.

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
| Workflow | change status (goes through the normal transition checks, as the rule owner; a transition that requires approval creates the request and the step finishes with `approval requested (#id)`) *(3a)* |
| Approval | `approval.request`: ask users (ids, `assignees`, `task_creator`, `event_doer`) to approve the task, ad hoc, `mode`, templated title and description, `due_offset` *(5a)* |
| People | assign / unassign (a user id, the event's doer, the task creator) *(3a)*, round robin over a team |
| Content | add comment *(3a)*, add / remove label *(3a)*, create subtask or related task from a template *(3a)*, add checklist to description |
| Notify | `notify`: in-app notification (plus the e-mail the person's settings allow) to users, `assignees`, `task_creator`, `event_doer` or a team *(3d)* |
| Content | `task.create`: create a task from templated parameters; with no task in the run (webhook, project-scope schedule) it becomes the task of the actions after it *(3d)* |
| Integrate | call a webhook: `http.request` (signed, SSRF-protected, with retry) *(3b)* |
| Flow | stop if a condition holds *(3a)*, `wait` (delay, 1 minute to `automation.waitmax`; stored as a scheduled event, never a sleeping goroutine) *(3d)* |
| Script | run JavaScript: `script.run` *(3b)* |

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

## Scripting (3b)

Scripts run in an embedded, pure-Go JavaScript interpreter
([goja](https://github.com/dop251/goja): ECMAScript 5.1 and most of ES6 - `let`,
`const`, arrow functions, template strings, `for...of`, destructuring, `Map`, `Set`,
`Promise` objects exist, but there is no event loop). Every run gets a **fresh
interpreter** that is thrown away afterwards; nothing is shared between runs. There
is no `require`, no file system, no process, no environment, no timers (`setTimeout`
and friends do not exist) and no network except `http.fetch`.

A script is used in two places:

- **Script action** `{"type": "script.run", "params": {"source": "..."}}`. The
  source is the **body of a function** in strict mode. Use `return` to give a
  result; it is stored as JSON in the output of the step (cut off after 16 KiB). A
  thrown error fails the step; the error text carries the JavaScript stack with
  line and column of the source (`at explode (line 3, column 9)`).
- **Script condition** `{"script": "..."}` as a node of the condition tree (also
  inside `flow.stop_if`). Same convention - the body of a function - and the result
  of `return` is converted to true or false. The other convention (the value of the
  last expression, as in `task.priority > 3` without `return`) is **refused**: a
  condition script that returns nothing fails the run with "the condition script
  returned nothing: end it with `return true` or `return false`". Condition scripts
  always get a **read-only** API, even in a real run, and a shorter timeout (1 s).
  `vikunja.*` calls that change something, `http.fetch` and `secrets.get` throw
  "... is not available in a condition script".

### Globals

```js
event      // the trigger event: {name, task_id, project_id, doer: {id, username, name}, old, new, changed, field_id}. Frozen.
task       // the task the rule runs for, as it is when the script starts. Frozen. null for task.deleted.
           //   {id, title, description, done, priority, percent_done, due_date, start_date, end_date, done_at,
           //    created, updated, hex_color, identifier, project_id, created_by, assignees: [userId],
           //    labels: [labelId], status: {id, title, category} | null, custom_fields: {"<fieldId>": value},
           //    relations: {"subtask": [taskId], "parenttask": [taskId], "related": [...], ...}}
           //   Dates are RFC 3339 strings or null.
project    // {id, title}. Frozen.

vikunja.tasks.get(id)                       // -> task (same shape as the global)
vikunja.tasks.find(filter, {project_id}?)   // -> task[]; filter syntax of the task list, e.g. "done = false && cf_12 > 5"
vikunja.tasks.update(id, patch)             // -> task. patch: title, description, priority (0-5), percent_done (0-1),
                                            //    hex_color, done, due_date / start_date / end_date ("2026-12-24T09:00:00Z",
                                            //    "2026-12-24", "+3d", "now-12h", "none" or null to clear, or a Date)
vikunja.tasks.create(projectId, data)       // -> task. data: title (required), description, priority, percent_done,
                                            //    hex_color, due_date, start_date, end_date, assignees: [userId],
                                            //    parent_id (makes it a subtask of that task)
vikunja.tasks.comment(id, text)             // plain text, escaped like the add_comment action
vikunja.fields.get(taskId, fieldId)         // -> value
vikunja.fields.set(taskId, fieldId, value)  // like task.set_custom_field; null clears
vikunja.status.set(taskId, statusId, comment?)   // through the workflow transition checks
vikunja.assignees.add(taskId, userId) / .remove(taskId, userId)
vikunja.labels.add(taskId, labelId) / .remove(taskId, labelId)

http.fetch(url, {method, headers, body, timeout})
           // -> {status, ok, headers (lower-case names), body, truncated, json()}
secrets.get(name)                           // -> string
log(...args)                                // goes to the step output; console.log is the same function
```

Nothing else is a global beyond the standard built-ins (`Object`, `Array`, `JSON`,
`Math`, `Date`, `RegExp`, ...). `console` has only `log`.

- `event`, `task`, `project` and the namespaces are **frozen** plain objects:
  assigning to them throws a `TypeError` in the script and cannot change anything in
  Vikunja. Values returned by `vikunja.*` are fresh plain objects that a script may
  change freely - they are copies.
- Every `vikunja.*` call runs through **the same model functions and permission
  checks the built-in actions use** (it literally builds the plans of
  `task.set_field`, `task.set_custom_field`, `task.set_status`, `task.assign`,
  `task.add_label`, `task.add_comment`), **as the rule owner**, in the transaction of
  the step. So events caused by a script carry the origin run and depth like those of
  any action: the depth limit and the self-trigger guard apply. Text passed to
  `vikunja.*` is data: `{{placeholders}}` are **not** replaced.
- Errors surface as thrown `Error`s with the typed message ("Forbidden", "Workflow
  requirements not met ..."), so they can be caught with `try/catch`. An uncaught
  error fails the step. A failing script is a failure of the rule and is not retried;
  a temporary failure (a cancelled step timeout, a database error) is retried like
  for any action.
- `vikunja.tasks.find` returns only tasks the rule owner can read, at most
  `automation.scripts.maxresults`. Each result has the shape of `task`.
- A database error (as opposed to a permission or validation error) inside a script
  should not be caught and ignored: on PostgreSQL the transaction of the step is
  unusable after it.

### Examples

When the status of a subtask becomes a done status, sum a number custom field (here
field 12, "Hours") over all subtasks into the parent. Trigger `task.status_changed`
with `to_category: done`:

```js
const FIELD = 12;
const parentId = (task.relations.parenttask || [])[0];
if (!parentId) return "no parent";
let sum = 0;
for (const id of vikunja.tasks.get(parentId).relations.subtask || []) {
  const value = vikunja.fields.get(id, FIELD);
  if (typeof value === "number") sum += value;
}
vikunja.fields.set(parentId, FIELD, sum);
return {parent: parentId, sum: sum};
```

Post to a chat webhook with a signed body - no script needed, the `http.request`
action does it (`CHAT_TOKEN` and `CHAT_SIGNING_KEY` are project secrets):

```json
{"type": "http.request", "params": {
  "url": "https://chat.example.com/hooks/vikunja",
  "method": "POST",
  "headers": {"Authorization": "Bearer {{secrets.CHAT_TOKEN}}"},
  "body": "{\"text\": \"{{event.doer.name}} created {{task.identifier}}: {{task.title}}\"}",
  "secret": "CHAT_SIGNING_KEY"
}}
```

Assign from a list of user ids stored in the secret `ASSIGNEES` (a JSON array such as
`[4, 7, 9]`), spread over the tasks by their id (there is no counter that survives
between runs, so this is an even spread, not a strict rotation):

```js
const team = JSON.parse(secrets.get("ASSIGNEES"));
const pick = team[task.id % team.length];
vikunja.assignees.add(task.id, pick);
log("assigned", pick);
return pick;
```

### Test run (dry run)

`POST .../automations/{rule}/test` runs scripts with a **read-only** host: `get`,
`find`, `fields.get` and `log` work against the real data; every mutating call
(`update`, `create`, `comment`, `fields.set`, `status.set`, `assignees.*`,
`labels.*`) is checked exactly like a real call - permissions, workflow transitions,
validation - and then only **recorded** as `would call vikunja.tasks.update(12,
{"priority":5}): set priority to 5` in the step summary, returning a stub (`update`
returns the task with the patch applied, `create` a task with the id -1);
`http.fetch` is recorded and returns `{status: 200, body: "{}"}` without sending
anything; `secrets.get` checks that the secret exists and can be read and returns the
placeholder `[secret NAME]`, never the value. This is structural: the runner builds
one of three implementations of the same `Host` interface (`liveHost`, `dryHost`,
`readOnlyHost`) and the script cannot choose; all three share the code that builds
the changes (`hostBase`), and only `liveHost` ever applies them. A script condition
gets the read-only host in every mode.

### HTTP (`http.request` and `http.fetch`)

Both use one code path (`pkg/automation/outbound`) and the SSRF-protected client the
webhooks use (`utils.NewSSRFSafeHTTPClient`, built on `code.dny.dev/ssrf`): requests
to loopback, private, link-local and cloud-metadata addresses, and to anything not
globally routable, are refused unless the administrator set
`outgoingrequests.allownonroutableips` (the same switch as for webhooks;
`outgoingrequests.proxyurl` is honoured too, and connections through a proxy are
the proxy's business as it is for webhooks). The check is made when the connection is
**dialled** (a `net.Dialer` control function on the address that is actually
connected to), not on a pre-resolved name, so DNS rebinding does not help, and it runs
again for every redirect because each redirect dials. Only `http` and `https`; at
most 3 redirects, a redirect to another host drops the headers the rule set; no
credentials in the URL; the default `User-Agent` is `Vikunja-Automation`; no
Vikunja credential is ever sent. Timeout: the request's own (`timeout` in
`http.fetch`) but at most `automation.scripts.httptimeout` (10 s), and the context of
the step. Response bodies are read up to `automation.scripts.maxresponsesize` (1 MiB;
`truncated` is true when more was cut off); request bodies larger than that are
refused.

`http.request` action: `url` (template; the values of placeholders are
URL-escaped), `method` (default `POST`), `headers` (name to template; `Host`,
`Content-Length`, `Transfer-Encoding`, `Connection`, `Upgrade` cannot be set),
`body` (template) and optionally `secret`, the name of a project secret that signs
the body: the request gets `X-Vikunja-Signature: sha256=<hex>` with the hex
HMAC-SHA256 of the exact body, keyed with the secret. This mirrors the signature of
the user webhooks (`pkg/models/webhooks.go`: the same header name, the same
HMAC-SHA256 of the exact payload, hex encoded); the difference is the `sha256=`
prefix, which names the algorithm. Without a `Content-Type` header the body is sent
as `application/json` when it is valid JSON, otherwise as `text/plain`. The step
output is `METHOD url: status text` plus the first 2 KiB of the answer, with secrets
masked.

Retry semantics are those of the engine: a network error, a timeout or a **5xx**
answer is a temporary failure (the step is retried with backoff up to
`automation.maxattempts`); a **4xx** answer, a refused address, an invalid URL or
header fail the run at once. The call happens inside the transaction of the step, so
it is delivered **at least once**: when the commit fails after the request went out,
the retry sends it again.

### Secrets

Project secrets are managed with API v2, **project admin only**:
`GET /projects/{project}/automation-secrets` (names, timestamps, `unreadable`; never
values), `PUT /projects/{project}/automation-secrets/{name}` with `{"value": "..."}`
(creates or replaces; at most 8 KiB) and `DELETE .../{name}`. There is no endpoint
that returns a value.

- **At rest**: AES-256-GCM, key derived with HKDF-SHA256 from `service.secret` (info
  string `vikunja automation secrets v1`), a random 96-bit nonce per value, the
  project id and name authenticated as additional data (a stored value cannot be moved
  to another secret), and a version prefix (`v1:`) so the scheme can change later. If
  `service.secret` changes - note that Vikunja generates a random one at every start
  when none is configured, so **set `service.secret` explicitly** - the values cannot
  be decrypted any more: the API lists the secret as `unreadable: true`,
  `secrets.get` throws "the secret "X" cannot be decrypted (service.secret changed
  since it was stored): set it again", and setting it again fixes it.
- **Where they can be used**: `secrets.get(name)` in scripts (not in condition scripts),
  and `{{secrets.NAME}}` **only** in `http.request` header values and body, plus the
  `secret` parameter. A rule that uses `{{secrets.NAME}}` anywhere else (a comment, a
  title, a field value, the URL, a script source) is rejected when it is saved, and a
  rule that names a secret that does not exist in the project is rejected too (`actions[1].params.secret`).
- **Masking**: before anything is stored - step output, step error, run error, the
  last error of the event, the dry-run result - the engine replaces every secret value
  the step read **and every other readable secret value of the project** (values
  shorter than 4 characters only when the step read them) by `***`, in plain,
  URL-encoded, base64 and JSON-escaped form. A script that does `log(secrets.get("X"))`
  or throws it, or an answer that echoes it, shows `***`. The error type is kept, so
  masking does not change the retry classification.
- **Lifecycle**: purging a project deletes its secrets; **duplicating a project copies
  none**: the rules of the copy are disabled as before, with the reason
  `duplicated_secrets_missing` when they use secrets, and enabling one fails until the
  secrets exist in the new project.

### Security model

*Who can write scripts*: only project admins (creating or changing a rule needs admin
on the project), and `automation.scripts.enabled = false` turns scripts off for the
whole instance: rules with scripts cannot be saved (typed 400, code 22005) and
existing ones fail their step with "scripts are disabled on this instance" without
executing anything (`http.request` is not a script and is not affected). Instances
that do not trust their project admins should turn scripts off.

*What a script can reach*: only what the rule owner could do by hand - every
`vikunja.*` call is permission-checked as the owner with the code the API uses - plus
public HTTP(S) destinations through the SSRF guard, plus the secrets of **its own
project**. It cannot reach the file system, the process, the environment, the
database directly, other users' data beyond the owner's permissions, other projects'
secrets, or any Go object: everything handed to the interpreter is built as plain
JavaScript objects, never as wrapped Go values with methods or pointers; what comes
back is converted to plain data with a size and depth cap. A `constructor.constructor`
escape yields the interpreter's own global object and nothing else. Secrets are
protected from *display*, not from the script author: a project admin who can write
scripts can make a script send a secret to a server of their choice. That is why
writing scripts needs project admin and why the kill switch exists.

*What is not protected*: (0) **The script process is not a sandbox.** It runs as the
server's user, reused across projects; an escape from the interpreter would be a
compromise of the server (it could read the server's configuration and the
environment of the server process). What the server does against the child (see
"Script processes") makes the protocol robust and the resources bounded, nothing
more. (1) **Memory and CPU up to the limits.** Every script runs in a child process
that is killed half a second after its timeout and, on Linux, cannot map more than
`automation.scripts.maxmemory` (256 MiB) beyond what it uses before its first script.
At most `automation.workers` (4) + 2 script processes live at a time: every worker can
always get one; dry runs and the compile check on save share the two others, one per
project, so parallel test requests of one project admin wait or are refused as busy
and can neither take a worker's process nor block the test runs of every other project
(two projects together can). So a malicious script costs at most one core for its
timeout plus half a second and that much memory, times `automation.workers` + 2; the
server reads at most 4 MiB of one message of a script process and sends at most 8 MiB
(the start with the task, the answer of a host call), estimated before it is encoded.
On macOS, the BSDs and Windows the memory limit is only the soft limit of the Go
runtime, so there a script can still allocate until the timeout kills it. (2) **Idle
script processes**: up to `automation.workers` + 1 wait for the next script (about
65 MB of private memory each with a development build).
(3) **Database locks**: a script runs inside the
transaction of its step, so a script (or `http.request`) that waits for the network
holds that transaction open, and on SQLite the write lock, for up to the step
timeout (10 s). (4) **External effects are not rolled back** when the step fails
afterwards, and are delivered at least once. (5) The regexp match timeout is a
process-wide setting of the regexp engine the interpreter uses; in the script process
it is the timeout of the script.

## Safety limits

| Limit | Default | Config |
|---|---|---|
| Kill switch (whole instance) | on | `automation.enabled` |
| Scripts allowed (kill switch for scripts) | on | `automation.scripts.enabled` |
| Chain depth (a rule's action triggering further rules) | 3 | `automation.maxdepth` |
| Actions per run | 25 | `automation.maxactions` |
| Step timeout | 10 s | `automation.steptimeout` |
| Run timeout | 60 s | `automation.runtimeout` |
| Script wall-clock timeout (action / condition) | 5 s / 1 s | `automation.scripts.timeout`, `automation.scripts.conditiontimeout` |
| Script source size (checked when saving) | 64 KiB | `automation.scripts.maxsource` |
| Script `vikunja.*` calls per run | 100 | `automation.scripts.maxapicalls` |
| Script `http.fetch` requests per run | 5 | `automation.scripts.maxhttprequests` |
| HTTP request body and response body read (`http.request`, `http.fetch`) | 1 MiB | `automation.scripts.maxresponsesize` |
| Duration of one HTTP request, whatever a script asks for | 10 s | `automation.scripts.httptimeout` |
| Script log output per step (then a truncation marker) | 16 KiB | `automation.scripts.maxlogsize` |
| Script call stack depth | 512 | `automation.scripts.maxcallstack` |
| Tasks returned by `vikunja.tasks.find` | 200 | `automation.scripts.maxresults` |
| Memory of a script (hard limit on Linux) | 256 MiB | `automation.scripts.maxmemory` |
| Runs per rule per minute (excess is skipped and recorded) | 60 | `automation.ratelimit` |
| Concurrent workers | 4 | `automation.workers` |
| Live script processes (dry runs and checks share 2 of them, one per project) | workers + 2 | derived from `automation.workers` |
| Retries with exponential backoff, then `dead` | 5 | `automation.maxattempts` |
| Consecutive failed runs before a rule is disabled | 20 | `automation.maxconsecutivefailures` |
| Poll interval / cache TTL / retention (days) | 1 s / 30 s / 30 | `automation.pollinterval`, `automation.cachettl`, `automation.retention` |

Chain depth: events caused by the actions of a run (and by the `vikunja.*` calls of
scripts) have depth + 1; an event deeper than `maxdepth` is recorded as a skipped run
(`max_depth`), so with the default a person's change can start a chain of four rules
(depths 0 to 3). The step timeout cancels the database work of the step (the session
context); the run timeout cancels the whole run. Both count as temporary failures. A
rule with more actions than `maxactions` (the limit was lowered after it was saved)
fails its run without executing anything.

The script timeout is enforced four times: a timer interrupts the interpreter
(`vm.Interrupt`; an interrupt cannot be caught by the script, not even in a `finally`);
the same deadline is the context of every blocking host call (HTTP), which is
cancelled, and no new host call is answered after it; half a second after the deadline
the server kills the script process; and the process ends itself 1.5 s after the
deadline, or as soon as the server is gone (its input closes or its parent changes),
also inside a native call. A script that runs into its own timeout fails its step for
good ("the script ran longer than 5s and was stopped"); when the step or run timeout
ends it first, it is the temporary failure those are. The interrupt alone is only seen
between JavaScript instructions; the kill is what stops a single native call that never
returns to the interpreter (an array built-in over a huge array-like such as
`Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`, a huge `sort`, `join`,
`JSON.stringify` or `eval`, big integer arithmetic). A worker is held at most for the
timeout plus half a second plus the host call in flight at the deadline (a database
call is bounded by the step timeout, an HTTP request by its own timeout). A script that
allocates more than `automation.scripts.maxmemory` fails with "the script hit the
memory limit of 256 MiB and was stopped". The Go runtime needs part of the limit: at the
default a script can allocate about 190 MiB in one typed array, about 390000 small
objects or about 2 million short strings (at 512 MiB: 440 MiB, 780000, 4 million).

**Script processes.** A script, and the compile check when a rule is saved, runs in a
child process: on Linux the running binary itself (`/proc/self/exe`, so an upgrade
that replaced the file does not matter), elsewhere the file at its path, started again
with the argument `__vikunja-script-process`, `VIKUNJA_AUTOMATION_SCRIPT_PROCESS=1`,
`GOMAXPROCS=1`, `MALLOC_ARENA_MAX=1` and nothing else of the server's environment
(only `TZ` so dates are the same; this is about the environment handed over, not a
sandbox: see (0) above). A new process has to identify itself within 5 s, a window of
its own that the caller's remaining time does not cut, with the protocol version and
the build of the server; anything else is killed and the log asks to restart Vikunja.
The argument makes a binary without the script process (an older or upstream build at
the same path, which outside Linux is what gets started after an upgrade) stop with
"unknown command" instead of starting a server for those 5 s. When the workers start,
one process is started as a check; if that fails, one error in the log says why
scripts cannot run. The compile check is never done in the server: when no script
process is available, the save is refused with "the script could not be checked".
Only new or changed script sources are checked, so a rule can still be disabled, or
edited outside its scripts, while checking is unavailable. The
server sends the source, the event, the task and the limits as JSON over a pipe with a
sequence number (at most 8 MiB, without HTML escapes; a larger task or event fails the
step for good with "the task or the event is too large to be passed to a script",
which does not count against the rule, because anyone who can write the task can make
it that large; fields are not cut, since a script that writes a cut field back would
lose the rest) and answers every host call (`vikunja.*`, `http.fetch`, `secrets.get`)
on the goroutine of the step, so database work stays in the step's transaction and
under the owner's permissions; an answer larger than 8 MiB is an exception in the
script instead, and if the script does not catch it, the step fails for good without
counting against the rule, like an oversized start. A task or event nested deeper than
1000 levels (an incoming webhook can send that) is refused the same way. It checks the sequence number of every message, refuses a line longer
than 4 MiB before holding it, and enforces the limits of the run again (calls,
requests, sizes of strings, size and depth of values); a child that breaks any of it
is killed. A process serves one script at a time, each with a fresh interpreter. It
waits in a pool (`automation.workers` + 1, ended after a minute idle or 1000 scripts)
only after a script that succeeded or threw an ordinary exception, with nothing left
unread on its output, and only when the next script gets about the room of a new
process: a script that left more than 8 MiB of garbage has the heap collected and
returned before the next one, and a process whose address space grew by more than
32 MiB is retired, because the Go runtime never returns address space. After a
timeout, a stack or memory error, a crash or a broken protocol the process is killed.
On Linux it is the first choice of the kernel's OOM killer and is killed when the
server exits. The overhead is about 0.7 ms per script with a pooled process and about
25 ms when a process has to be started (measured with a development build; a
UPX-compressed binary starts more slowly). `NaN`, `Infinity` and `-0` from the server
reach the script unchanged, but a number that is not finite cannot be passed to a
`vikunja.*` call ("the value cannot be passed to the host"); the return value, an
error message and a stack are cut off at 16 KiB, the log at 256 KiB, and a host call
larger than 4 MiB throws.

**Who is blamed.** Blame is default-deny: once a process got the script, whatever ends
the run is the script's fault unless it is positively not. The script fails its step
for good, which counts towards disabling the rule, when it threw, hit its timeout,
overflowed the stack, hit the memory limit, passed more than the limits allow, or its
process ended or broke the protocol in any other way. Not counted: (a) failures before
the script got to a process (none could be started, it did not identify itself, none
was free in time, a pooled process that had died while it waited, a message left over
by the previous script of a pooled process) are temporary and retried, and when the
retries run out the run fails without counting towards
`automation.maxconsecutivefailures`; (b) a SIGKILL the server did not send: with the
OOM killer preferring script processes, memory pressure caused by anyone can kill a
running script, while the script's own excess is the memory limit above. It is
retried only when the script had not called `http.fetch`; after a request the run
fails for good without counting, since a retry would send the request again (writes
to the database are rolled back with the step and are safe to retry); (c) a task too
large to be passed to a script; (d) a memory error in a reused process, which runs
again in a new one with a whole new timeout (so a worker is held up to twice as long) (after an `http.fetch` it fails without counting instead), because
a reused process gives about, not exactly, the room of a new one. When an event gives up, each of its runs fails with
the error of its own rule. So the shared processes do not let one project's scripts get
another project's rules disabled; memory pressure on the server can still fail any
rule's run, without counting.

A rule that fails 20 times in a row is disabled automatically with the reason
`too_many_failures` (visible on the rule). The owner is not notified in 3a/3b: that
needs a new notification type with a frontend renderer.

A rule never reacts to an event whose chain of origin runs (`origin_run_id`,
followed up to `maxdepth`) contains a run of the same rule, whichever task the
event is about; the event is recorded as a skipped run (`self_trigger`). So a
rule that creates tasks does not react to the tasks it created.

## API (`/api/v2`)

- `GET|POST /projects/{project}/automations`, `GET|PUT|DELETE
  /projects/{project}/automations/{rule}`
- `POST /projects/{project}/automations/{rule}/test` with `{task_id}` — dry run
  against a task of the project: evaluates the conditions, plans every action as
  the owner of the rule (templates, permissions, workflow transitions) and
  returns the plan, warnings and what would fail. Nothing is written; the event
  is simulated (you are the doer, `event.old.*` / `event.new.*` are empty).
  Scripts run with a recording, read-only host (see "Test run" under Scripting):
  nothing is written, no HTTP request is sent, secrets are placeholders.
- `POST /projects/{project}/automations/{rule}/run` with `{task_id}` — manual trigger
  (3d), 202 `{event_id, status: "queued"}`; see below. `GET /projects/{project}/
  manual-automations` lists the enabled manual rules (id, title, button label) for
  people with write access to the project. `POST /projects/{project}/automations/
  {rule}/webhook-token` regenerates the secret of an incoming webhook (project admin).
- `POST /automation/hooks/{token}` — the public incoming webhook (3d), no auth.
- `GET /projects/{project}/automations/{rule}/runs`, `GET .../runs/{run}`
  (with steps), `POST .../runs/{run}/retry`.
- `GET /automations/catalog[?project_id=]` — machine readable list of triggers,
  condition fields/operators and actions with their parameter schemas (name,
  kind, required, enum, `title_key`). With `project_id` the custom fields of the
  project are added as `cf_<id>` fields. The visual
  editor is generated from this, so a new action needs no frontend change. Params have a
  `kind` (3b adds `code` with `language: "javascript"`, `secret` - the name of a project
  secret - and `map` - an object of templates, for headers), an optional `description`
  (plain English help for the editor) and a `default`. The response has a `script`
  object: the language, the convention of script actions and conditions, the limits
  with their configured values and config keys, and `api`, the list of every global and
  function with its signature, description, whether it is `mutating` and whether it is
  available in a condition (`in_condition`), from which the editor's help panel is
  generated.
- `GET /projects/{project}/automation-secrets` (names, timestamps, `unreadable`; never
  values), `PUT /projects/{project}/automation-secrets/{name}` with `{"value"}`
  (create or replace), `DELETE /projects/{project}/automation-secrets/{name}` (3b;
  project admin only).

Errors: an invalid rule is `400` with code 22002 and `details.issues`, a list of
`{path, message}` for every problem (`actions[1].params.status_id`, ...); a script
that does not compile is reported with its path and the `line` and `column` of the
syntax error in the source (`actions[2].params.source`, `conditions.all[1].script`).
22005 scripts are disabled on this instance (400, on saving a rule with a script),
22006 invalid secret name or value (400), 22007 secret not found (404), 22008 secret
cannot be decrypted (409). 22001
rule not found, 22003 run not found, 22004 run cannot be retried (409; only runs
with the status `failed`). Retrying a run puts its event back in the queue; steps
that succeeded are not executed again. 22009 the rule cannot be run (409: disabled, or no manual trigger), 22010 webhook not
found (404), 22011 invalid signature (401), 22012 webhook rate limited (429 with
`Retry-After`), 22013 the webhook body is not a JSON object (400).

### Time, manual and webhook triggers (3d)

All of these write an outbox event for **one rule** (`rule_id` on the event; the model
events of the project do not reach it and it does not see theirs), so they appear in
the run history, obey the conditions and the self-trigger guard and run as the
rule owner like any other run. The owner is checked again when the run starts and, for
schedule and due rules, when the ticker is about to fire: an owner who is gone or no
longer administers the project disables the rule (`owner_not_admin` /
`owner_unavailable`) instead of firing it.

- **Manual** (`manual`, option `label`): `POST .../{rule}/run` needs write access to
  the task and the task to belong to the project of the rule. The actions still run
  with the permissions of the owner of the rule, never more than the owner has.
  Access to the project is checked first: whoever cannot write in it gets the same 403
  whether the project, the rule or the task exist, are enabled or have a manual
  trigger (an archived project included: it does not reveal itself to an outsider). For people with access, disabled rules and rules with another trigger are 409.
- **Schedule** (`schedule`, options `cron`, `scope`): a five field cron expression
  or `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`, evaluated in the **time zone of the server**
  (`service.timezone`), on wall clock time. `@every ...` and `TZ=` / `CRON_TZ=` prefixes are
  rejected: an interval would fire on every tick, and a zone of its own would break the daylight saving handling. `scope: tasks` (default) starts one run per
  task of the project that matches the conditions; `scope: project` starts one run
  with no task (use `task.create` first). Nothing fires for the time before the rule
  was last saved (enabling does not replay the past), and a server that was down fires
  once for all the occurrences it missed, however long it was down. Daylight saving: a wall clock time that does
  not exist (02:30 on the night the clocks jump forward) is skipped that day, an hourly
  job loses only the hour that does not exist; a wall clock time that happens twice
  (the clocks go back) fires once, for the first of them. When the day of the month and
  the day of the week are both restricted, a day matches when **either** does (classic cron
  semantics: `0 0 1 * 1` is the 1st and every Monday). A schedule that never happens
  (30 February) is rejected on save.
- **Overdue** (`task.overdue`) and **due soon** (`task.due_soon`, option `offset`,
  at least 1m): fire for not done tasks of the project the first time they cross the
  line. A marker per (rule, task) stores the due date it fired for, so a task fires
  once per due date and a changed due date arms it again. Tasks that were already
  overdue when the rule was armed do not fire, and neither do tasks that were in the
  due soon window then. A rule is armed when it is created, enabled again or gets another
  trigger (type or options); editing its title, conditions or actions does not move the
  line, so a task that crossed meanwhile still fires. Arming writes a marker, with its
  current due date, for every task of the project that already is overdue (overdue) or
  inside the window (due soon), in the transaction of the save and at most 20000 of them
  (tasks beyond that have no marker and fire once, at the pace of the ticker caps). The
  ticker then fires for every task without a marker for its due date: a task that
  entered the window or became overdue since, one created there, and one whose due date
  was edited into the window or into the past after arming (a new due date). A task
  created with a due date in the past fires overdue, too. A task a rule created that sits at
  max depth is skipped by all time based rules until the chain is shorter (see Loops).
  (The schedule keeps counting from the last save.) Conditions are checked before a run is
  queued (script conditions only in the run); a non matching task is marked too.
- **The ticker**: every process runs it every `automation.ticker.interval`
  (30s). It only reads while nothing is due. A firing is claimed in the database in the
  same transaction that writes the events: a schedule by a conditional update of
  `last_fired_at` (only one replica changes the row), a due marker by its unique
  (rule, task) index. Several instances therefore never fire twice. Per tick and
  rule at most `automation.ticker.maxtasks` runs start (the rest is skipped for a
  schedule and the note is on the rule as `last_fire_note`; overdue and due soon
  continue on the next tick) and a schedule looks at no more than
  `automation.ticker.maxscan` tasks. The per-minute run limit does not apply to the
  time based triggers; this cap does. A schedule scans the tasks in a read only session
  and then claims its occurrence and writes the events in one short transaction; a
  replica that loses the claim discards its scan.
  **Loops**: a task created by a run (`task.create`, the subtask and related task
  actions, scripts) is remembered with that run (`automation_task_origins`). The time
  based events for such a task carry the run as their origin and one more level of
  depth, so a rule never fires for what its own run created and a chain of rules that
  create tasks for each other ends at `automation.maxdepth`. `http.request` and
  `http.fetch` refuse URLs that are incoming webhooks of this server (the host of
  `service.publicurl` and the path `/api/v2/automation/hooks/`, whatever the scheme, port,
  trailing dot of the host or path prefix; without a public URL the path alone decides),
  also after redirects. That is only the cheap first line. Every request the engine sends
  carries the header `X-Vikunja-Automation-Depth: 1` (a rule cannot remove or change it,
  and it survives redirects), and this server answers every `/api/v1` and `/api/v2`
  request that carries it with 508, so a rule cannot call the hook endpoint or the task
  API of its own instance, whatever address or token it uses. A reverse proxy that strips
  the header, or a second hop through an external service that calls back without it, is
  outside what the guard can see.
  The loop guards are bounded by retention: origin rows are deleted with their run, so
  after `automation.retention` days the own-run guard no longer knows a task was created by
  a rule, and a task a person took over from a rule stays marked only that long.
  Limits that need a value above 0 (`automation.ticker.maxscan`,
  `automation.webhook.maxbody`, `automation.maxpendingwaits`) are replaced with their
  defaults at startup when set to 0 or less; a negative `automation.maxdepth` gets its
  default (0 forbids chains), a negative `automation.ratelimit` or `automation.ticker.maxtasks`
  means 0, which turns that limit off.
- **Incoming webhook** (`webhook.incoming`, option `secret`):
  `POST /api/v2/automation/hooks/{token}` with a JSON object (`{}` for none; at most
  `automation.webhook.maxbody` bytes, 413 above) starts the rule asynchronously (202).
  The body is `event.payload` for templates (`{{event.payload.customer.name}}`) and
  conditions (`event.payload.<path>`). The token is 32 random bytes (base64url),
  returned once, in the answer that created or regenerated it (`webhook_token`,
  `webhook_url`); the database stores its SHA-256 only, and it is looked up by that
  hash, after a shape check that never lets a malformed token reach the database.
  It is not logged. Unknown tokens, disabled rules and rules with another trigger are
  all the same 404. With a `secret` option the call has to carry
  `X-Vikunja-Signature: sha256=<hex HMAC-SHA256 of the raw body>` keyed with that
  project secret (401 otherwise, compared in constant time; checked before the rate
  limit counts). **There is no replay protection**: the signature covers the body only,
  without a timestamp or nonce, so whoever captured a signed request can send it again
  until the secret or the address is changed. Rules keep no state between runs, so they
  cannot filter out requests they have seen. The address (or the secret) should be
  rotated when a capture is suspected, and a proxy that verifies requests should sit in
  front of Vikunja where more is needed. The wire format is kept as it is. Calls are limited per
  rule (`automation.webhook.ratelimit` per minute, 429 + `Retry-After`; counted from
  the outbox, so with simultaneous calls it can overshoot by a few) and per IP by the
  router (the floor of the public form endpoints). The route sets no cookies and
  needs no CSRF token. The test run (`/test`) takes an optional `payload`.
- **Notify** only reaches users who can read the project of the rule (and so its
  tasks), at most 100 per step; others are skipped with a warning and the step fails
  when nobody is left. The notification is `automation.notify` and is shown by the
  notification list, which shows the message and links to the task (or the project when the
  run had none).
- **Wait**: the run stays `running`, a `rule.resume` event due after the delay
  continues it with the actions after the wait; conditions are not evaluated again.
  A rule that was disabled or deleted meanwhile aborts the run. Only the resume event
  continues past a wait: another delivery of the original event never does. A project
  can have `automation.maxpendingwaits` (1000) runs waiting; a wait over that fails. The
  cap is soft: it is checked without a lock, so workers times replicas can overshoot it by
  a few. A retry of a run that stopped after its wait queues a fresh resume event for it
  instead of the original event, and a dead event of another rule does not fail the run
  that waits.
  The retention cleanup keeps the event of a run that is still running, and the daily
  cleanup fails runs that are running for longer than `automation.waitmax` per wait
  action of their rule plus a day (the resume event was lost); a reaped run counts as a
  failed run in the statistics of its rule. Lowering `automation.waitmax` can therefore
  reap runs that legitimately wait for a longer wait than the new maximum, and
  `automation.waitmax: 0` turns the reaper (and the maximum wait) off.

## UI

- Project settings → Automations: list with enable switch, last run status
  and time, run count; "New rule" starts from a small gallery of templates.
- Editor: three stacked blocks **When / If / Then**; conditions as nested
  groups; actions as an ordered list with drag to reorder; a script editor
  with syntax highlighting and the API reference beside it; "Test with a
  task" shows the plan before saving.
- Runs: table of runs, a run opens to its steps with output and error.

### UI status (3c, 3b scripting UI)

Implemented in `frontend/`: project settings -> Automations (project admins),
rule list with enable switch, last run, a warning badge for rules the system
disabled, duplicate and delete; starter templates (only offered when the
catalog and the project can serve them); the rule editor with When / If / Then
generated from the catalog (one generic parameter renderer per catalog `kind`,
nested all / any / not condition builder, ordered actions with move, duplicate
and remove); client-side required checks and mapping of the server's issue
paths (`actions[1].params.status_id`) onto the exact inputs; "Test with a task"
(dry run); per-rule runs table with steps, output, error, retry and automatic
refresh while a run is `running`. Translation keys of the catalog are checked
against `en.json` and `tr-TR.json` by a unit test on a catalog snapshot (taken
from a real 3b backend, with the custom fields of a test project).

Scripting UI (3b), checked in a browser against a real backend (script action
with log, `task`, `vikunja.fields.set` and a comment; syntax error marked on its
line and fixed; dry run with the "would call" lines and `log()` output; a real
run with its step output; a script condition that returns false (run skipped,
"conditions not met"); `http.request` to a loopback address (step fails with the
SSRF message); `while (true) {}` (fails with the timeout after 5 s); secrets
created, replaced, deleted and shown as unreadable after `service.secret`
changed): the script editor (line numbers, the conventions of the catalog as a
hint, a collapsible filterable API reference that inserts at the cursor, example
snippets from the examples above, a collapsible limits table), compile errors
under the editor and on the offending line, the multi-line dry run output in a
monospace box, the `secret` parameter as a select over the project secrets with
an inline "New secret..." form, the `map` parameter (headers) as key/value rows
with a secret placeholder menu, and a Secrets section on the Automations page
(names, timestamps, `unreadable` badge, add, replace the value, delete with
confirmation; values are write-only: a password input, cleared after the
request, never fetched or logged).

Known limits / deferred:

- The dry run endpoint only tests a *saved* rule, so the editor asks to save
  first and disables the test for a changed draft.
- The runs list has no server-side status filter; the filter narrows the loaded
  page.
- Script editing has no syntax highlighting (monospace textarea with line
  numbers, Tab support and an error marker on the line the server reports).
  A script is only checked by the server when the rule is saved (syntax errors
  with line and column); the editor does not compile it while typing.
- `{{secrets.NAME}}` is offered in the placeholder menu only for the `http.request`
  header values and body. The catalog does not mark those places, so the list
  is a constant in `frontend/src/helpers/automationScript.ts`; the server still
  rejects the placeholder elsewhere when the rule is saved.
- Scripts switched off on the instance (`automation.scripts.enabled = false`):
  the catalog still carries the `script` object, but its limits row
  `automation.scripts.enabled` has the value `false`. The UI treats that (or a
  catalog without `script`) as "scripts unavailable": the script action and the
  script condition are not offered, existing ones are shown read-only with a note.
- No drag-and-drop reordering (buttons only).

### UI status (3d)

Built in `frontend/` (generated client regenerated for the 3d routes; the catalog snapshot
and the English and Turkish strings cover the new triggers, options, parameters, enum
values and the error codes 22009 to 22013):

- **Rule editor.** The five triggers and the three actions are selectable and are
  rendered from the catalog like any other. `schedule`: a cron input with the presets
  hourly / daily / weekly / monthly, a check of the expression in the browser
  (`helpers/automationCron.ts`, mirrors `spec.ParseCron`: five fields with ranges,
  steps, lists and month / weekday names, the descriptors, `@every` and `TZ=` refused,
  a day that no month has refused) and a note that times are in the time zone of the
  server. `task.overdue` / `task.due_soon`: a hint that tasks that are already overdue or
  inside the window when the rule is created or enabled do not fire; `offset` is a
  duration input (at least one minute). `manual`: the optional button text. `webhook.incoming`: see below.
  `wait`: duration of at least one minute; `task.create`: title, description, priority,
  assignee and due offset; `notify`: recipients (people and the keywords), a team
  (`team` parameter kind, a select over the teams of the person) and the message with the
  placeholder menu. Client side checks (cron, durations, a recipient or a team) are a
  copy of the server's rules in `validateSpecial` in `helpers/automation.ts`; the
  server stays the authority and its issues are mapped onto the same inputs. Hints are
  looked up as `automation.hint.<type with _>.<name>` and `automation.triggerHint.<type>`.
- **Incoming webhook address.** The save that creates the address returns it once
  (`webhook_url`); the editor shows it with a copy button and the warning that it cannot be shown
  again, together with the notes on the signature header and the missing replay
  protection. Later the editor says that the address exists and offers "Create a new
  address" (a confirmation, the old address stops working, disabled while the rule has
  unsaved changes). The address lives only in the component that shows it: the save and
  the token request run through `useSecretMutation` (no mutation cache entry), the rule
  list cache gets the rule without it, nothing is logged or stored. Closing the editor
  forgets it. The signing secret option is the usual select over the project secrets.
- **Manual run.** The actions menu of a task has "Run automation..." when the project has
  enabled manual rules (`GET .../manual-automations`, only asked for people who can write);
  a dialog lists the rules by their button text and queues the run
  (`POST .../automations/{rule}/run`). Success is a toast, failures are the usual error
  toast with the localized code (22009 and the others); the dialog stays open on a failure.
- **Notification.** `automation.notify` shows the message of the rule and links to the
  task, or to the project when the run had no task; it has no doer.

Not built / deferred: the maximum of a wait and the time zone of the server are not in the
catalog or `/info`, so the editor can only say "at most what the administrator allows
(30 days by default)" and "the time zone of the server" and leaves the check to the
server; no per-rule display of the last scan note (`last_fire_note`) or of the number of
waiting runs; the editor does not warn that an action needs a task when a project scope
schedule or a webhook starts the rule (the server rejects it on save and the message is
shown under the action); the list of
rules shows no marker for the new trigger types beyond their title. Checked in a browser
against a running server since (see the roadmap).

## What 3a and 3b do and do not do

Implemented: everything marked *(3a)* or *(3b)* above, rules CRUD with validation against
the catalog (`pkg/automation/spec`) and the project (ids must exist, labels and
move targets must be usable by the saving user, scripts must compile, named secrets
must exist), the dry run, run history with
steps, retry of failed runs, the outbox with its hook, the workers, all limits of
the table above, the retention job, project purge (deletes
rules, events, runs and secrets), project duplicate (rules are copied **disabled**; ids of
custom fields, options and statuses are remapped, labels and projects are kept;
secrets are not copied), deleting a custom field or removing / deleting a workflow status (the rules that
reference it are disabled with a reason, the delete is not blocked) and the
Prometheus metrics `vikunja_automation_runs_total{status}` and
`vikunja_automation_events{status}` (only with `metrics.enabled`).

3b: JavaScript script action and script condition, `http.request`, `http.fetch`,
project secrets with masking, the script reference in the catalog.

3d (backend): the triggers `schedule`, `task.overdue`, `task.due_soon`, `manual` and
`webhook.incoming`, the actions `notify`, `wait` and `task.create`, the ticker, the
due markers table, the incoming webhook endpoint with token regeneration and
optional HMAC signature, `event.payload.*` in templates and conditions.

Not implemented: the e-mail action, the owner notification on auto-disable, label /
relation / move / reminder triggers, the rest of the frontend (3c), a templates gallery for the 3d types, round robin and
duplicate / delete task actions.
`vikunja.notify(userIds, text)` of the first design is not part of the script API: it
needs the notification type of 3a. Scripts cannot create or delete relations, labels,
or projects, move tasks between projects or delete tasks. Secrets are not rotated or
versioned; changing `service.secret` makes all of them unreadable until they are set
again.

Test coverage: the sandbox (no `require`, `process`, timers, Go objects; escapes through
`constructor.constructor`; frozen globals), every limit, every `vikunja.*` function with
its permission failure as the rule owner, origin and depth of script-caused events,
the dry run (nothing written, no request sent), conditions, the HTTP paths against
`httptest` servers (the SSRF guard is replaced in tests only through
`outbound.SetClientFactory`, with a guard that allows the single test listener
`127.0.0.1/32` and keeps everything else blocked at dial time, so redirects to
private addresses are still stopped by the real check), secrets (round trip, wrong
key, masking) and the API are covered by the tests of `pkg/automation/...`,
`pkg/models` and `pkg/webtests`. The automation, model and web tests run on SQLite (a file in WAL
mode, like a real installation; the shared-cache in-memory database of the other
tests cannot run concurrent workers) and the automation, model and automation web
tests (including all of 3b: scripts, HTTP, secrets) were also run against PostgreSQL 17. MySQL was **not** run; the SQL is plain
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
- 3b: script condition/action, `http.request`, `http.fetch`, secrets. **Done.**
- 3c: UI. **Done.**
- 3d: schedule, due-date-approaching, manual and incoming webhook triggers;
  wait action; notify action. **Done**, backend and UI (editor forms, webhook address,
  manual run, notification); templates gallery for them not started.
