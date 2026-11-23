# Forms and approvals — design

Status: approvals (5a, backend; 5b, UI) and forms (5c, backend; 5d, UI) are implemented. Both build on custom fields, workflow and automation.

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

## Implementation (5c, backend)

### Differences to the sketch above

- `forms` has two more columns: `disabled_reason` (set when the system disables a form) and `max_submissions`
  (a copy of `settings.max_submissions`, so the maximum can be enforced with one conditional `UPDATE`).
  `form_submissions.created` is set by the code (not by xorm) so the rate limits can be tested with a clock.
- `fields[].options` (only for a shown `labels` field) lists the label ids the submitter may pick. Not allowed on
  public forms.
- Attachments travel in **one multipart request** (`POST /forms/{token}/submissions/multipart`), not in a second
  request with an upload token. One transaction, so a rejected file creates nothing and nothing has to be stored
  between two requests. The JSON endpoint cannot carry files; a form that asks for attachments requires the
  multipart one. The response has no `upload` member.
- The owner of a form is the user who last saved it (like the owner of an automation rule).

### API (`/api/v2`)

Admin (project admin only, never link shares): `GET /projects/{project}/forms/options` (what a field can target,
generated from the project; `public_visible` / `public_hidden` say what a public form may do with it),
`GET|POST /projects/{project}/forms`, `GET|PUT|PATCH|DELETE /projects/{project}/forms/{form}`,
`POST /projects/{project}/forms/{form}/token` (new token, the old link stops working),
`GET /projects/{project}/forms/{form}/submissions` (task id, submitter, created; never the IP hash).

Public (no `Security`, listed in `unauthenticatedAPIPaths` and `optionalAuthAPIPaths`): `GET /forms/{token}`,
`POST /forms/{token}/submissions`, `POST /forms/{token}/submissions/multipart`.

Public definition (`GET /forms/{token}`; Huma adds `$schema`):

```json
{
  "title": "Cover request",
  "description": "<p>Tell us what you need.</p>",
  "fields": [
    {"target": "title", "label": "What do you need?", "help": "", "required": true, "kind": "text", "max_length": 250},
    {"target": "cf_12", "label": "Stage", "help": "", "required": false, "kind": "select",
     "options": [{"id": 1, "title": "Open", "color": "#00aa00"}]},
    {"target": "cf_13", "label": "Budget", "help": "", "required": false, "kind": "number",
     "config": {"min": 0, "max": 1000, "precision": 2}}
  ],
  "success": {"message": "Thanks", "redirect_url": "", "submit_another": true},
  "submitter": {"collect_name": true, "name_required": true, "collect_email": true, "email_required": false}
}
```

A field may also carry `default` (the prefill; omitted when it contains a placeholder) and `max_length`; when a
form asks for attachments there is a top-level `attachments: {max_files, max_file_size}`. Nothing else is exposed:
no project, owner, count, hidden field or default, user or label list (labels only on non-public forms).

Submission (`POST /forms/{token}/submissions`) and its answer:

```json
{"values": {"title": "Laptop", "cf_12": 1, "cf_13": 12.5}, "submitter": {"name": "Ada", "email": "ada@example.com"}, "honeypot": ""}
```
```json
{"message": "Thanks", "redirect_url": "", "submit_another": true}
```

`task_id` is only added for signed-in submitters who can read the project, never for public submissions.
Validation errors are `422`, code `25006`, one entry per field:

```json
{"status": 422, "code": 25006, "detail": "Some answers are not valid.",
 "details": {"errors": [{"field": "cf_12", "code": "invalid", "message": "option does not belong to this field"},
                        {"field": "title", "code": "required", "message": "This field is required."}]}}
```

Codes: `required`, `invalid`, `too_long`, `too_many`, `unknown_field` (also for hidden targets), `file_too_large`,
`file_type_not_allowed`, `attachments_disabled`; submitter details are `submitter.name` / `submitter.email`.
Definition errors on save are `400`, code `25002`, with `details.issues[] = {path, message}` (e.g. `fields[2].target`).

Other typed errors: `25001` unknown token or form (404), `25003` form takes no submissions (410, `details`
`{reason: disabled|closed|full, title}`), `25004` login required (401), `25005` not allowed (403: link share, or a
user who cannot read the project of a members form), `25007` rate limited (429, `Retry-After` header),
`25008` forms disabled on the instance (403, saving only).

### Access modes on the public routes

`public`: no authentication at all. `users`: any signed-in user; `members`: signed-in users who can read the
project. Link shares are not users (403 for `users`/`members`, treated as anonymous for `public`). The routes are
optional-auth: no `Authorization` header means anonymous; a header is validated like everywhere (an invalid or
expired token is a `401` with code `11`, so the client can refresh). An anonymous request to a non-public form
is `401` code `25004`, which the UI turns into a login redirect.

### Protections and their config keys

| Protection | Key (default) |
|---|---|
| Kill switch for all forms (public endpoints answer 410 `disabled`) | `forms.enabled` (true) |
| Kill switch for public forms (cannot be saved, existing ones answer 410 `disabled`) | `forms.public.enabled` (true) |
| Submissions per client IP and hour on public forms, counted in the database from the salted hash | `forms.public.ratelimit` (10) |
| Submissions per public form and hour | `forms.public.formratelimit` (300) |
| Request floor per IP and minute on the public routes (router middleware, `ratelimit.store`) | `forms.public.requestratelimit` (60) |
| JSON body size, 413 above (also the `data` part of the multipart route) | `forms.maxbodysize` (262144 bytes) |
| Attachments on public forms | `forms.public.attachments` (false) |
| Files per submission | `forms.maxattachments` (5) |
| Size per file (the smaller of this and `files.maxsize` applies) | `forms.maxattachmentsize` (10MB) |
| Value caps | title 250, text/url/email 10000, long text 50000 characters (fixed) |
| Honeypot | a non-empty `honeypot` is answered like a success and creates nothing |
| Maximum submissions | `settings.max_submissions`, enforced by `UPDATE forms SET submission_count = submission_count + 1 WHERE id = ? AND (max_submissions = 0 OR submission_count < max_submissions)` in the submission transaction, after a lock write on the form row |
| Close date, disabled flag | `settings.close_date`, `enabled` |

Rate limits are 429 with `Retry-After`; the window is one hour and runs on the clock of the server. The client IP is
the one the router determines (`service.ipextractionmethod`, `service.trustedproxies`); it is stored only as
`HMAC-SHA256(key derived from service.secret, ip)`. Files are sniffed by content (not only by extension) and
executable, script, HTML and SVG types are rejected.

### Behaviour of a submission

One transaction: lock the form row, check availability and access, honeypot, rate limits, load the owner and
check that they can still write to the project (otherwise the form is disabled with `owner_not_writer` or
`owner_unavailable`, that change is committed, nothing else is created, answer 410 `disabled`), validate every
visible field, count the submission, create the task with `Task.Create` as the owner (workflow initial status,
`task.created` and the automation outbox apply), write custom field values, labels, default assignees, the initial
status override (a system transition without checks) and the attachments, record the submission, and dispatch
`form.submitted` on commit. Hidden targets cannot be supplied by the client. A shown field that is missing from
`values` takes its default. Title and description defaults (and text custom fields) accept the placeholders
`{{submitter.name}}`, `{{submitter.email}}`, `{{form.title}}` and `{{date}}`; it is a plain substitution, answers
are never substituted. Rich text is HTML sanitized by an allow-list (basic formatting, links with http, https
and mailto, no images, styles or handlers); text that is not HTML is escaped into paragraphs. Titles and custom
field text are stored as plain text without control characters. A "Submitted by name <email>" paragraph is added to
the description when a name or an e-mail address is known. `submitter.name` / `submitter.email` of a request are only
taken when the form collects them (`collect_name` / `collect_email`); otherwise they are ignored and a signed-in
submitter is recorded with the name of the account.

### Events, automation, notifications

`form.submitted` (form id/title, submission id, task, doer = form owner, submitter id/name/email, no token, no IP)
is an automation trigger with the option `form_id`, the condition fields `event.new.form_id`, `form_title`,
`submitter_name`, `submitter_email`, and the placeholders `{{event.form.title}}`, `{{event.form.submitter_name}}`,
`{{event.form.submitter_email}}`. Its task is the created task; `task.created` of the same submission is delivered
too. Users in `settings.notify_user_ids` get the in-app (and mail) notification `form.submitted`
(`form_id`, `form_title`, `submitter_name`, `submitter_email`, `task`, `project`), if they can read the project.

### Lifecycle

Purging a project deletes its forms and submissions; deleting a form keeps its tasks; duplicating a project copies
forms disabled (`duplicated`, or `duplicated_unmapped_reference`) with a new token and remapped custom field, option
and status ids; deleting a custom field, a workflow status or a label that a form uses disables the form
(`custom_field_deleted`, `status_deleted`, `label_deleted`); deleting the owner disables their forms
(`owner_unavailable`).

### Security model

An anonymous submitter can read the definition of one form (what is listed above) and create one task per
accepted submission, as the form owner, in the one project of the form, with the fields the form asks for and the
presets the admin chose. They cannot choose where the task goes, set hidden fields, see the task or its id, any
other task, project, user or label, or learn how many submissions exist. They cannot upload unless the instance and
the form allow it, and then only limited, non-executable files. Every text they send is escaped, sanitized or
stored as plain text. They are limited per address and per form, cannot exceed the maximum number of submissions
even in parallel, and a form silently stops when its owner loses write access. The routes use no cookies and
no CSRF tokens (nothing depends on ambient credentials); the CORS policy is the global one (only the configured
origins, nothing wider for these routes), and JSON `Content-Type` is required, so a cross-site HTML form cannot
post to the JSON endpoint.

## UI

- Project settings → Forms: list with link copy, enable switch, submission
  count. Builder: field list on the left (add from the project's fields,
  reorder, label/help/required/default/hidden per field), live preview on the
  right, settings tab, share tab with the link and an embed snippet.
- The form page itself is a clean, standalone page (no app chrome for public
  forms), mobile first, with the instance's logo, per-field errors and a
  confirmation screen.

## UI status (5d)

Done (web client, `frontend/`):

- **Forms page** (project settings -> Forms, project admins only, route `project.settings.forms`). The list shows
  title, access badge (members / signed-in users / public), the open/closed switch (saved at once), the submission
  count, a warning badge with the translated reason when the system switched the form off, and copy link, open,
  edit, submissions, duplicate (the copy starts closed) and delete (confirm; the tasks stay). Without forms the
  page says what forms do and offers three starters ("Simple request", "Request with a due date", "Bug report")
  built from what the project offers.
- **Builder** (tabs Fields, Settings, Share; the live preview sits on the right, below on narrow screens).
  Everything comes from `GET .../forms/options`; no target list is hard-coded. Fields: reorder with buttons, label,
  help, required, "do not show" (a hidden field takes its default), a typed default (custom fields reuse the custom
  field editors, labels use a picker, dates a date input), the label subset of a shown labels field, and an add menu
  grouped into task and custom fields where every target can be added once. On a public form a target that may not
  be asked but may be preset (labels, user fields) is added as a hidden field; one that can be neither (attachments
  when the instance forbids them) is disabled with the reason. Switching to public lists the fields that block it.
  The title rule (asked and required, or hidden with a value) is explained inline, with the placeholders. Settings:
  access with a warning box for public, initial status (only with a workflow), default assignees, success message,
  redirect, "submit another", close date, maximum, who is notified, name/e-mail (off / optional / required, public
  only). Share: link and copy, "create a new link" (confirm), iframe snippet. Client checks mirror the server;
  server issues (`400` / `25002`, `details.issues[].path`) are mapped to the control and tab (the tab is
  switched, the field opened, the control focused, tabs show a badge with the count). Unsaved changes are guarded
  (route leave, tab close, back to the list). Saving keeps the builder open.
- **Submissions** (paged table: date, submitter name / e-mail or "signed in", link to the task, empty state).
- **Public page** `/forms/:token`. A route with `meta.standalone` is rendered by `App.vue` without header, sidebar
  or the login layout, and the router guard lets it through for visitors without a session. The page loads the
  definition without credentials (the `publicClient`, no `Authorization`); only when a form for signed-in users
  answers `25004` and the browser has a token, it asks again with the session. A logged-out visitor therefore
  sends only the instance `/info` the app always loads, the definition and, later, the submission.
  Fields are native inputs by `kind` (radios or checkboxes up to six options, selects above, stars as radios,
  range, date or date-time input, textarea with a counter near `max_length`, file picker honouring the limits);
  labels above, help below, required marked with an asterisk and "(required)" for screen readers, errors tied with
  `aria-describedby`, an `aria-live` summary, focus on the first invalid field, 44px targets. A honeypot input is
  `tabindex=-1`, `autocomplete=off` and inside an `aria-hidden` box. 422 answers (`25006`) are mapped to the
  inputs; 404, 410 (closed / full / disabled, with the form title), 401 (a button that returns to the form after
  login through the saved last-visited route), 403, 429 (retry time from `details.retry_after`) and network errors
  have their own states. A submission with files uses the multipart endpoint, one without uses JSON. Success shows
  the message (or a default), "submit another" resets the form, a redirect (http/https only) shows the target host
  and goes after four seconds. The language follows `navigator.language` when nobody is logged in (the app's own
  pre-login rule) and dark mode follows `prefers-color-scheme`.
- **Notifications and automation.** `form.submitted` has text and a link to the task. The catalog fixture and the
  English and Turkish strings cover the trigger, its `form_id` option and the new condition fields; the generic
  editor renders `form_id` as a select over the project's forms (a number input when the list is not known).

Behaviour worth knowing:

- Date defaults and date answers: a date without a time is sent as noon in the viewer's time zone (RFC 3339), a date
  with a time as that time. The server accepts no relative dates (`+3d`), so the builder offers none.
- Submitter name / e-mail are only part of a public form; the builder sends them as off for other access modes and a
  signed-in submitter is not asked.
- No QR code: it needs an encoder and no dependency may be added.

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
| Done checkbox: `PUT /tasks/{task}` with `done`, kanban moves into a done bucket of a manual view, bulk updates | **Refused**: `409` / `24007` with `details` `task_id`, `from_status_id`, `from_status_title`, `to_status_id`, `to_status_title`, `to_status_category`, `pending_approval_id` (0 when none) and `requestable` (`true`: the task is where it was and the client may ask with `POST /tasks/{task}/status`), `approval_pending` (an open request for the same task and target status already exists, so the client says so instead of offering a second request; looking it up creates nothing); the whole update is rolled back and **no request is created**. Silently ignoring `done: true` would look like a bug to clients; creating a request as a side effect of a failed update would be a surprise. The client asks explicitly with `POST /tasks/{task}/status`. The same holds for un-doing. |
| A task created done (`done: true` or the done bucket of a manual view), a done task moved in from another project | **Refused** like the checkbox: `409` / `24007` with the same `details`, but `requestable` is `false` (the creation or the move is rolled back as a whole, so there is no task in the project to request the approval for; the client only explains the refusal), nothing is created or moved. The task starts in the first done status only when its creator or mover could perform the done transition without an approval. |
| Kanban status-mode move (`PUT .../buckets/{bucket}/tasks`) | A drag is an explicit status change: the request is created, the card does not move, `202` with `approval` and the column the card is still in as `bucket`. |
| Task created in a status column | The task is created in the initial status, the request for the column is created, `bucket_id` of the response is the initial column. |
| Automation `task.set_status`, script `vikunja.status.set` | Creates the request as the owner of the rule and finishes the step successfully with the output `approval requested (#id)`. The dry run says `would request approval for the status change to <id>`. Nobody to ask fails the step (`24006`). |
| Automation `task.set_done`, `vikunja.tasks.update({done})` | Like the checkbox: the step fails with `24007`. |
| Re-opening a repeating task, the move after a rejection, assigning the first status, moving the tasks of a removed status | System transitions: **never** create approvals. |

## API (`/api/v2`)

- `GET /approvals?status=&role=approver|requester&project_id=&page=&per_page=`
  — "my approvals": the requests where the caller is approver or requester and can
  still read the task (in the project the task is in now, not the one the
  request was made in), newest first, each with the approvers, the statuses and a
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

## UI status (5b)

Done (web client, `frontend/`):

- **Workflow editor.** A transition has an "Approval" section: the switch, the
  mode (any / all, each with one line of explanation), the approver sources
  (a person, a team the project is shared with, the assignees of the task, a
  person in a `user` field of the project), the status after a rejection, the
  duration to decide within (`12h`, `2d`, `1w`) and the self-approval switch.
  Transitions with an approval carry a tag in the list and an icon in the
  matrix. The client checks what the server checks (at least one complete
  source, the duration format, a rejected status that differs from the
  target). The server's 21002 messages name no transition, so one that is
  about an approval control is shown at that control of the transition that
  is open.
- **Task detail.** The status menu labels transitions that need an approval
  ("Request approval -> Print ready") and previews who will be asked; the
  requirements dialog runs first, the `202` shows "Approval requested", never
  an error. The done button goes through the status endpoint, so it asks for
  the approval too and no failing `PUT` is needed. An approval banner sits
  above the property list: title, requester, mode, due (overdue marked),
  state, comment and time per approver, approve / reject (comment required for
  a rejection, 24005 shown at the field) for approvers who have not decided,
  cancel with a confirmation for the requester and project admins. Resolved
  requests are in a collapsible section (outcome, decisions, the cancel
  reason in words). The status history links an entry made by an approval to
  that section. "Request approval" in the actions menu opens the ad-hoc
  dialog.
- **Boards, lists, table.** A status mode drop that answers `202` leaves the
  card in place and shows a toast; cards, list rows and the table status cell
  show a "Waiting for approval" marker (from `expand=approvals`, requested only
  for projects with a workflow).
- **Approvals page** (`/approvals`, sidebar entry with a count badge): waiting
  for me, requested by me, history, with a project filter, paging, and
  approve / reject in the first tab. Notifications `approval.requested`,
  `approval.resolved` and `approval.reminder` have text and a link to the task.
- **Automation.** The catalog fixture and the English and Turkish strings
  cover the new triggers, options, fields and the `approval.request` action;
  the generic editor renders its parameters with the existing user list
  (people plus `assignees`, `task_creator`, `event_doer`) and enum controls.

Behaviour worth knowing:

- `GET /approvals?role=approver&status=pending` also lists the requests the
  caller has already decided (they stay open for the others). "Waiting for me"
  and the badge therefore read all pages and keep only the requests the
  caller can still decide. The badge refreshes on window focus, when an
  `approval.*` notification arrives over the websocket, and by polling every
  minute without a socket.
- The history tab asks for one outcome at a time (approved, rejected,
  cancelled), because the endpoint takes a single status; there is no
  combined "all resolved" list.
- Plain updates with `done: true` (checkbox of a list row, a drop into a done
  bucket of a manual board) still answer `409` / `24007`; the backend stays
  strict. The client handles that refusal in one place (the task update and
  the kanban move mutations): the optimistic change is rolled back, so the
  checkbox goes back, and a message names the status ("The task was not marked
  as done: "Done" needs an approval first.") with the action **Request
  approval**. The action starts the same transition as the status menu of the
  task (`POST /tasks/{task}/status`), including the dialog for missing fields
  and a required comment. If the refusal says a request is already pending,
  the message says so and offers to open the task instead. The same applies to
  re-opening a task whose transition back needs an approval. The task detail
  done button asks for the approval directly, as before.
- Paging of "Waiting for me" is done in the client.

# Steps

- 5a (done): approvals backend (requests, approvers, workflow integration,
  events, automation triggers and action, notifications, reminders).
- 5b (done): approvals UI.
- 5c (done): forms backend (definition, public submission path, limits, event, notification, lifecycle).
- 5d (done): forms UI (builder, public form page).

