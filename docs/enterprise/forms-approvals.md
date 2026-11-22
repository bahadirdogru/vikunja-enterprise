# Forms and approvals — design

Status: forms are design only; approvals (5a, backend) are implemented. Both build on custom fields, workflow and automation.

# Forms

## Goals

- A team publishes a **request form** ("New cover request", "Purchase
  request"); a submission becomes a task in a project, with the answers in
  the task's fields.
- A form is a view of the project's own fields: the form builder picks which
  task fields and custom fields to ask for. No second field system.
- Forms can be internal (signed-in users) or public (a secret link, no
  account).

## Model

### `forms`
id, project_id (index), title, description (rich text shown above the form),
token (unique, unguessable; the public URL is `/forms/<token>`), access
(`members` — users who can read the project, `users` — any signed-in user,
`public` — anyone with the link), enabled, owner_id, fields (json), settings
(json), submission_count, created / updated.

`fields` is an ordered list of
`{target, label, help, required, default, hidden}` where `target` is `title`,
`description`, `due_date`, `priority`, `labels`, `attachments` or `cf_<id>`.
`title` is mandatory (asked, or built from a template such as
`Request from {{submitter.name}}`). `hidden` fields are not shown and always
take their default — the way to pre-set a status, label or custom field.

`settings`: initial workflow status, default assignees, success message or
redirect URL, "submit another" link, close date, maximum number of
submissions, notify users on submission, collect submitter name / e-mail for
public forms (stored on the submission and added to the task description).

### `form_submissions`
id, form_id (index), task_id, submitter_id (null for anonymous),
submitter_name, submitter_email, ip_hash (salted, for rate limiting only),
created.

## Behaviour

- Submitting creates the task **as the form owner**, through the normal task
  create path, so workflow initial status, custom field validation and
  automations all apply. If the owner loses write access the form is
  disabled with a reason (same rule as automation rule owners).
- Values are validated with the field's own rules (custom field type,
  min/max, options). Errors are returned per field.
- Public forms: no session, no cookies, CSRF-safe by being token-only JSON;
  protected by per-IP and per-form rate limits, a honeypot field, size limits
  and an optional maximum submission count. Attachments on public forms are
  off by default and size/type limited when enabled. A public form never
  reveals project data: option lists of the asked fields are the only thing
  it exposes. User and label pickers are not available on public forms.
- A submission dispatches `form.submitted` (form, task, submitter) — an
  automation trigger.
- Deleting a form keeps the tasks it created.

## API (`/api/v2`)

- `GET|POST /projects/{project}/forms`, `GET|PUT|DELETE
  /projects/{project}/forms/{form}` — project admins.
- `GET /projects/{project}/forms/{form}/submissions` — project admins.
- `GET /forms/{token}` — the public definition (title, description, visible
  fields with labels/options/validation); no auth for public forms.
- `POST /forms/{token}/submissions` — submit.

## UI

- Project settings → Forms: list with link copy, enable switch, submission
  count. Builder: field list on the left (add from the project's fields,
  reorder, label/help/required/default/hidden per field), live preview on the
  right, settings tab, share tab with the link and an embed snippet.
- The form page itself is a clean, standalone page (no app chrome for public
  forms), mobile first, with the instance's logo, per-field errors and a
  confirmation screen.

# Approvals

Status: **5a (backend) is implemented** in the models, migration `20261124100000`
and `/api/v2` routes described below. The UI (5b) is not built yet.

## Goals

- "This cannot go to print until the editor in chief approves" — an explicit,
  recorded decision by named people, not just a status anyone can set.
- Approvals plug into the workflow: a transition can **require approval**.

## Model

### `approval_requests`
id, task_id (index), project_id (index), transition_id (null for ad-hoc
requests), from_status_id, to_status_id, rejected_status_id (all null for
ad-hoc), title, description, comment (the comment sent with the status change;
it ends up in the history), mode (`any` — the first decision wins, `all` —
every approver must approve, one rejection rejects), status (`pending` |
`approved` | `rejected` | `cancelled`, indexed), allow_self, requested_by_id
(index), due (null), reminded_at (null; the reminder runs once), resolved_by_id
(0 when the system resolved), cancel_reason (machine readable, see below),
cancel_message (human text), created, updated, resolved (null).

### `approval_approvers`
id, request_id, user_id (together unique), decision (null | `approved` |
`rejected`), comment, decided (null).

Approvers on a transition are configured as users, a team (expanded to its
members when the request is created, later team changes do not touch the
request), the task's assignees, or a `user` custom field of the task ("the
editor named on the task"). They are resolved to concrete users when the
request is created: in the order of the sources, de-duplicated, only active
users who can read the task, and without the requester unless `allow_self` is
set. Nobody left: the request is not created, the caller gets `422` / `24006`.

### On `workflow_transitions` and `task_status_history`

`workflow_transitions` gets `requires_approval` (bool) and `approval` (json,
null unless `requires_approval`). `task_status_history` gets
`approval_request_id` (null), which is set on the history entry of an approved
transition and of the move after a rejection.

## Transition configuration

The workflow document (`GET|PUT /projects/{project}/workflow`) carries the new
fields on every transition:

```json
{
  "from_status_id": 1, "to_status_id": 5, "title": "Send to print",
  "requires_approval": true,
  "approval": {
    "mode": "any",
    "approvers": [
      {"type": "user", "id": 2},
      {"type": "team", "id": 4},
      {"type": "assignees"},
      {"type": "custom_field", "id": 12}
    ],
    "rejected_status_id": 2,
    "allow_self": false,
    "due_in": "3d"
  }
}
```

Validation (all `400` / `21002`): `approval` is required when
`requires_approval` is set and is dropped (stored as null) when it is not;
`mode` is `any` (default) or `all`; at least one and at most 50 sources; a
`user` source is an active user who can read the project; a `team` source is a
team the project (or a parent) is shared with; a `custom_field` source is a
`user` field of the same project; `rejected_status_id` (by id, or by the
negative id of a new status of the same document) belongs to the document, is
not archived and is not the target status; `due_in` is a positive duration such
as `12h`, `3d` or `2w`. Duplicating a project copies the configuration and
translates the rejected status and the custom field to the copies; requests are
not copied.

## Behaviour

- **Requesting.** Performing a transition with `requires_approval` runs every
  other check first (target exists and is not archived, pair allowed,
  permission and team, required fields, required comment). It then does **not**
  change the status: it creates the pending request and returns it. Asking
  again for the same task and target status returns the existing request (no
  duplicate, no new notification); a pending request for a different target on
  the same task is allowed. Even a project admin asks. Link shares cannot ask
  (`403` / `24010`).
- **Deciding.** `POST /approvals/{request}/decision`. Only an approver who has
  not decided can; a rejection needs a comment. Approvers who lost read access to the
  project are dropped first and no longer count (and cannot decide: they
  cannot see the request any more). With `any` the first decision resolves the
  request. With `all` the last approval resolves it and one rejection resolves
  it at once.
- **Approved.** The system performs the transition on behalf of the requester,
  all checks except the approval requirement run again at that moment. History
  shows the requester as doer, carries the comment of the request and the id of
  the request. If something no longer holds the request becomes `cancelled`
  (the decisions stay as they are, the status does not change) with a
  `cancel_reason`: `task_moved`, `workflow_removed`, `status_removed`,
  `transition_removed`, `target_archived`, `status_changed` (the task is no
  longer in the `from` status), `requirements_not_met` (a required field or the
  comment is gone; the message lists them), `requester_lost_access`,
  `requester_not_in_team` or `requester_deleted`. The response to the decision
  is still `200`; the request in it says what happened.
- **Rejected.** The task stays, or moves to `rejected_status_id` as a system
  transition (rules bypassed; the history names the rejecting approver and the
  request; skipped when the task left the `from` status or the status is gone).
  The comment is added to the task as a comment authored by the approver.
- **Cancelling.** `POST /approvals/{request}/cancel` by the requester or a
  project admin (`cancelled_by_requester`). Automatic cancellations: the task is
  deleted (`task_deleted`) or moved to another project (`task_moved`), the
  workflow is deleted (`workflow_removed`), a status of the request is removed
  or archived (`status_removed`), the transition is removed
  (`transition_removed`) or no longer requires approval (`approval_disabled`),
  the task leaves the `from` status by another route (`status_changed`; ad-hoc
  requests are not tied to a status), or nobody who can read the task is left
  to decide (`no_approvers`, also when an approver is deleted).
- **Ad-hoc approvals.** `POST /tasks/{task}/approvals` — a recorded decision on
  any task without a status change. Needs a title and `approver_ids` (users who
  can read the task; the requester cannot be one), `mode` defaults to `any`,
  `due` is optional. The requester needs write access to the task.
- **Concurrency.** Every decision and cancellation starts by taking the write lock
  of the request (a no-op `UPDATE` of its row) and then reads with locking reads;
  resolving, cancelling and approving are conditional updates on
  `status = pending` inside the same transaction as the transition. Two
  approvers deciding at the same time, or a decision racing a cancel, are
  serialized; the loser gets `409` / `24002` and writes nothing. Creating a
  request takes the lock of the task row for the same reason (status changes
  of one task run one after the other).

### What each caller of the status change does

| Caller | Transition needs approval |
|---|---|
| `POST /tasks/{task}/status` | `202`, the pending request in `approval`, `status` is the status the task is still in. `200` when the status changed, as before. |
| Done checkbox: `PUT /tasks/{task}` with `done`, kanban moves into a done bucket of a manual view, bulk updates | **Refused**: `409` / `24007` with `task_id`, `from_status_id`, `to_status_id`; the whole update is rolled back and **no request is created**. Silently ignoring `done: true` would look like a bug to clients; creating a request as a side effect of a failed update would be a surprise. The client asks explicitly with `POST /tasks/{task}/status`. The same holds for un-doing. |
| Kanban status-mode move (`PUT .../buckets/{bucket}/tasks`) | A drag is an explicit status change: the request is created, the card does not move, `202` with `approval` and the column the card is still in as `bucket`. |
| Task created in a status column | The task is created in the initial status, the request for the column is created, `bucket_id` of the response is the initial column. |
| Automation `task.set_status`, script `vikunja.status.set` | Creates the request as the owner of the rule and finishes the step successfully with the output `approval requested (#id)`. The dry run says `would request approval for the status change to <id>`. Nobody to ask fails the step (`24006`). |
| Automation `task.set_done`, `vikunja.tasks.update({done})` | Like the checkbox: the step fails with `24007`. |
| Re-opening a repeating task, the move after a rejection, assigning the first status, moving the tasks of a removed status | System transitions: **never** create approvals. |

## API (`/api/v2`)

- `GET /approvals?status=&role=approver|requester&project_id=&page=&per_page=`
  — "my approvals": the requests where the caller is approver or requester and can
  still read the task, newest first, each with the approvers, the statuses and a
  compact `task` (`id`, `title`, `identifier`, `project_id`, `project_title`).
- `GET /approvals/{request}` — readable by everyone who can read the task.
- `GET /tasks/{task}/approvals?status=`, `POST /tasks/{task}/approvals` (ad-hoc, `201`).
- `POST /approvals/{request}/decision` `{decision: approved|rejected, comment}`
  (`200`, body has the updated `request`),
  `POST /approvals/{request}/cancel` `{message}` (`200`).
- `GET /tasks/{task}/status` additionally returns `pending_approvals` and, per
  offered transition, `requires_approval`, `approval_mode` and `approvers` (the
  users who would be asked if the caller asked now).
- Task lists accept `expand=approvals`: every task gets
  `approvals: {has_pending, pending_count}` (one query for the page).
- Workflow transitions carry `requires_approval` and `approval`.

Link shares have no access to approvals at all (`403` on every route, no
`approvals` expand, no approval data in the task status).
Requests of tasks the caller cannot read answer `404` on decision and cancel
(`24001`), `403` on read.

Error codes: `24001` request not found, `24002` not pending (`409`), `24003` not
an approver (`403`), `24004` already decided (`409`), `24005` rejection without
comment (`422`), `24006` no approvers (`422`), `24007` approval required for a
done change (`409`), `24008` requester may not decide (`403`), `24009` invalid
ad-hoc request (`400`), `24010` link share cannot request (`403`).

Token scopes: `approvals` (`read_all`, `read_one`, `decision`, `cancel`) and
`tasks_approvals` (`read_all`, `create`); `expand=approvals` needs
`tasks_approvals` `read_all`.

Example: asking for a transition that needs approval (`POST /tasks/20/status`
`{"to_status_id": 5, "comment": "No longer needed"}`) answers `202`:

```json
{
  "task_id": 20,
  "to_status_id": 5,
  "comment": "No longer needed",
  "from_status_id": 1,
  "status": {"id": 1, "title": "Todo", "category": "open", "is_initial": true},
  "done": false,
  "approval": {
    "id": 1, "task_id": 20, "project_id": 11, "transition_id": 3,
    "from_status_id": 1, "to_status_id": 5, "rejected_status_id": 2,
    "title": "Todo → Dropped", "comment": "No longer needed",
    "mode": "any", "status": "pending", "allow_self": false,
    "due": "2026-11-26T00:13:09Z", "resolved": null,
    "approvers": [
      {"id": 1, "request_id": 1, "user_id": 2, "decision": null, "comment": "",
       "decided": null, "user": {"id": 2, "username": "user2"}}
    ],
    "requested_by": {"id": 1, "username": "user1"},
    "task": {"id": 20, "title": "task #20", "identifier": "TEST11-1", "project_id": 11, "project_title": "Test11"},
    "to_status": {"id": 5, "title": "Dropped", "category": "cancelled"}
  }
}
```

Example: a decision (`POST /approvals/1/decision`
`{"decision": "approved", "comment": "Fine"}`) answers `200`; the task is in
Dropped now and its history names user1 (the requester) with
`approval_request_id: 1`:

```json
{
  "request_id": 1,
  "decision": "approved",
  "comment": "Fine",
  "request": {
    "id": 1, "task_id": 20, "status": "approved", "mode": "any",
    "resolved": "2026-11-22T21:13:09Z", "cancel_reason": "",
    "approvers": [
      {"id": 1, "request_id": 1, "user_id": 2, "decision": "approved",
       "comment": "Fine", "decided": "2026-11-22T21:13:09Z",
       "user": {"id": 2, "username": "user2"}}
    ],
    "resolved_by": {"id": 2, "username": "user2"}
  }
}
```

## Events, automation, notifications

- Events, dispatched on commit: `approval.requested` (request, task, doer =
  requester), `approval.decided` (per decision; doer = approver, plus the
  decision and comment) and `approval.resolved` (outcome `approved`, `rejected`
  or `cancelled`; the doer is who ended it, null when the system did). Requests
  that were only returned again announce nothing.
- Automation triggers `approval.requested` and `approval.resolved` (option
  `outcome`: `any` (default), `approved`, `rejected`, `cancelled`). Event fields
  for conditions: `event.new.approval_id`, `.approval_title`, `.approval_mode`,
  `.ad_hoc`, `.requested_by`, `.approvers`, `.outcome`, `.cancel_reason`,
  `event.old.status` (the status the task has to be in) and `event.new.status`
  (the target) for transition requests, `event.doer.*`. Placeholders for
  templates: `{{event.approval.id}}`, `{{event.approval.title}}`,
  `{{event.approval.outcome}}`.
- Automation action `approval.request` (ad-hoc): `approvers` (user ids,
  `assignees`, `task_creator`, `event_doer`), `mode`, `title` and `description`
  (templates), `due_offset`. The owner of the rule asks and is never one of the
  approvers. Events an action causes carry `origin_run_id` and `depth + 1` like
  all others.
- Notifications (in-app, plus e-mail when the mailer is on): `approval.requested`
  to every approver; `approval.resolved` to the requester and to the approvers
  who did not decide (not to whoever resolved or cancelled it);
  `approval.reminder` once to the undecided approvers when `due` has passed (cron
  every five minutes). The English strings are in `pkg/i18n/lang/en.json`; other
  languages come from Crowdin. The web client renders unknown notification
  names with an empty text and without a link until 5b adds them.

## UI

- Task detail: an approval banner (who is asked, who decided what, due),
  approve / reject buttons with a comment for approvers.
- "Approvals" page in the sidebar with a count badge: waiting for me, sent by
  me, history.
- Workflow editor: approval settings on a transition; kanban status mode
  shows a "waiting for approval" marker on the card.

# Steps

- 5a (done): approvals backend (requests, approvers, workflow integration,
  events, automation triggers and action, notifications, reminders).
- 5b: approvals UI.
- 5c: forms backend (definition, public submission path, limits, event).
- 5d: forms UI (builder, public form page).
