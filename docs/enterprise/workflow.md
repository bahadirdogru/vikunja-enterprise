# Workflow — design

Status: design. Depends on custom fields (required fields on transitions).

## Why

Upstream Vikunja only knows `done` / not done, plus kanban buckets that are a
property of one view. A workflow gives a project real statuses and rules for
moving between them. **A status is not a kanban bucket**: the status is a
property of the task and is the same in every view; a bucket is how one
kanban view groups tasks.

## Model

One workflow per project (v1). A project without a workflow behaves exactly
like upstream.

### `workflows`
id, project_id (unique), restrict_transitions (bool, default false),
created / updated.

When `restrict_transitions` is false every status change is allowed and the
transition table only carries extra rules for the listed pairs. When true,
only listed transitions are allowed.

### `workflow_statuses`
id, workflow_id (index), title, color, category, position, is_initial (bool),
archived (bool).

`category` is one of `open`, `in_progress`, `done`, `cancelled`. Exactly one
status is the initial one. A status that still has tasks or history cannot be
deleted: it is archived (hidden from pickers, history stays readable) or the
caller names a status to move its tasks to.

### `workflow_transitions`
id, workflow_id (index), from_status_id, to_status_id (unique pair), title
(optional button label, e.g. "Send to design"), min_permission (`write` |
`admin`), require_comment (bool).

### `workflow_transition_required_fields`
id, transition_id (index), field (varchar). `field` is a built-in name
(`assignees`, `due_date`, `start_date`, `end_date`, `description`,
`attachments`, `labels`) or `cf_<id>` for a custom field. All listed fields
must be set on the task before the transition is allowed.

### `workflow_transition_teams`
transition_id, team_id. Optional: when present, only members of one of these
teams (or project admins) may perform the transition.

### `task_statuses`
task_id (unique), workflow_id, status_id (index), entered (timestamp).
Kept out of the `tasks` table to keep the upstream diff small.

### `task_status_history`
id, task_id (index), from_status_id (null for the first), to_status_id,
doer_id, comment (text null), created.

## Behaviour

- A new task in a project with a workflow gets the initial status.
- Changing status goes through **one** function that checks, in order: target
  status exists and is not archived → transition allowed
  (`restrict_transitions`) → permission (`min_permission`, teams) → required
  fields → required comment. A failed check returns a typed 4xx error that
  names the unmet requirement (which fields are missing) so the UI can ask
  for exactly those. Every successful change writes history and dispatches
  `task.status.changed`.
- `done` sync: entering a status of category `done` or `cancelled` sets
  `task.done = true`; leaving it clears it. Marking a task done through the
  existing checkbox performs the transition to the first `done` status and is
  subject to the same checks; un-doing moves it back to the initial status.
- Moving a task to another project: status resets to that project's initial
  status (or none if it has no workflow); history is kept.
- Enabling a workflow on an existing project assigns the initial status to
  open tasks and the first `done` status to done tasks.
- Deleting the workflow removes statuses/transitions and current task
  statuses; history rows keep the status titles denormalised
  (`from_title`, `to_title`) so they stay readable.

## API (`/api/v2`)

- `GET|PUT|DELETE /projects/{project}/workflow` — the whole definition as one
  document (statuses, transitions with required fields and teams). PUT
  creates or replaces it; ids are matched, missing statuses are archived or
  moved according to `move_tasks_to` in the request.
- `GET /tasks/{task}/status` — current status plus the transitions the
  current user may perform right now, each with its unmet requirements.
- `POST /tasks/{task}/status` — `{to_status_id, comment}`.
- `GET /tasks/{task}/status/history`.
- Task lists accept `status` and `status_category` in the filter syntax and
  `sort_by=status`.

Permissions: reading follows project/task read access; changing the
definition needs project admin; changing a task's status needs task write
access plus the transition's own rules.

## Kanban

A kanban view gets a third bucket mode next to manual and filter buckets:
**status**. In that mode the columns are the workflow statuses (in position
order), and dragging a card performs the transition with the same checks; a
rejected drop snaps back and shows what is missing. Manual buckets keep
working unchanged.

## UI

Workflow editor: project settings, "Workflow" (project admins only, route
`project.settings.workflow`). A project without a workflow shows what a
workflow is and an "Enable workflow" button that pre-fills To do (initial) →
In progress → Done in the user's language. Everything is edited as one draft
and saved with one PUT; leaving with unsaved changes asks first.

- Statuses: colour, title, category (own icon and colour), initial radio,
  up/down, remove. New statuses carry negative temporary ids. Removing a status
  that still has tasks gets a 409 (21008); the editor asks where the tasks go
  and retries with `move_tasks_to`. Statuses with history show up as archived.
- Transitions: "Only allow listed transitions", then a list or a matrix (rows =
  from, columns = to; a ticked cell opens that transition's rules). Each
  transition has an optional button label, minimum permission, comment
  requirement, required fields (built-in names plus the project's custom
  fields) and allowed teams.
- Client-side checks mirror the server (one initial status, a done status,
  unique titles, no self or duplicate transitions); a 400 (21002) is shown
  inline.

Task detail: when the project has a workflow, a Status row sits at the top of
the property list. Its menu lists the transitions from `GET /tasks/{task}/status`
with their button label or target status; ones with unmet requirements say
what is missing. A transition that needs fields or a comment opens a dialog
with editors for the missing fields and a comment box; "Change status" saves
the fields first and then posts the change. A 422 (21007) re-lists what is
still missing, 400 (21005) and 403 (21006) become plain messages. The done
button of a task in a workflow project moves it to the first done status (or
back to the initial one) through the same path, so its rules and dialog apply.
A collapsible status history (who, from → to, when, comment) sits under the
Status row. A project without a workflow answers 404 (21001) once; the UI
treats it as "no workflow" and renders nothing extra.

## Steps

- 2a: tables, the transition function, API, events, done sync, tests.
- 2b (done): workflow editor in project settings; status picker and history
  in the task detail view; missing-requirements dialog. See "UI".
- 2c: kanban status mode, table column, filter/sort by status.
- Later: approvals as a transition requirement, WIP limits, SLA timers,
  workflow templates shared between projects.
