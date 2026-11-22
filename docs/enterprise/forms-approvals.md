# Forms and approvals — design

Status: design. Both build on custom fields, workflow and automation.

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

## Goals

- "This cannot go to print until the editor in chief approves" — an explicit,
  recorded decision by named people, not just a status anyone can set.
- Approvals plug into the workflow: a transition can **require approval**.

## Model

### `approval_requests`
id, task_id (index), project_id, transition_id (null for ad-hoc requests),
from_status_id, to_status_id, rejected_status_id (null), title, description,
mode (`any` — first decision wins, `all` — every approver must approve,
one rejection rejects), status (`pending` | `approved` | `rejected` |
`cancelled`), requested_by_id, due (null), created, resolved.

### `approval_approvers`
id, request_id (index), user_id (index), decision (null | `approved` |
`rejected`), comment, decided.

Approvers on a transition are configured as users, a team (expanded to its
members when the request is created), the task's assignees, or a `user`
custom field of the task ("the editor named on the task").

## Behaviour

- A transition with `requires_approval`: performing it does **not** change
  the status. It checks the transition's other requirements first, then
  creates a pending request and returns `202` with it. The task shows
  "Waiting for approval → Print ready".
- When the request is approved the transition is performed by the system on
  behalf of the requester (history shows requester and approvers). If the
  task no longer satisfies the transition's requirements, or left the
  `from` status meanwhile, the request is cancelled with the reason instead.
- When rejected the task stays, or moves to `rejected_status_id` if the
  transition names one; the rejection comment is added to the task.
- Only one pending request per task and target status. The requester (or a
  project admin) can cancel. An approver cannot approve their own request
  unless the transition allows self-approval.
- Ad-hoc approvals: "Request approval" on any task without a status change
  (to_status null) — just the recorded decision.
- Events: `approval.requested`, `approval.decided` (per approver),
  `approval.resolved` — automation triggers; an automation action
  `approval.request` creates one.
- Approvers get an in-app notification (and e-mail when the mailer is on),
  with a reminder when `due` passes.

## API (`/api/v2`)

- `GET /approvals?status=pending&role=approver|requester` — "my approvals".
- `GET /tasks/{task}/approvals`, `POST /tasks/{task}/approvals` (ad-hoc).
- `POST /approvals/{request}/decision` `{decision, comment}`,
  `POST /approvals/{request}/cancel`.
- Workflow transitions gain `requires_approval`, `approval` (`mode`,
  approvers, `rejected_status_id`, `allow_self`, `due_in`).

## UI

- Task detail: an approval banner (who is asked, who decided what, due),
  approve / reject buttons with a comment for approvers.
- "Approvals" page in the sidebar with a count badge: waiting for me, sent by
  me, history.
- Workflow editor: approval settings on a transition; kanban status mode
  shows a "waiting for approval" marker on the card.

# Steps

- 5a: approvals backend (requests, approvers, workflow integration, events,
  automation trigger/action, notifications).
- 5b: approvals UI.
- 5c: forms backend (definition, public submission path, limits, event).
- 5d: forms UI (builder, public form page).
