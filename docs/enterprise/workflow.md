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
`admin`), require_comment (bool), requires_approval (bool), approval (json,
null unless `requires_approval`: `mode` `any`|`all`, `approvers` as a list of
`{type: user|team|assignees|custom_field, id}`, `rejected_status_id`,
`allow_self`, `due_in`; see [forms-approvals.md](forms-approvals.md#approvals)).

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
doer_id, comment (text null), approval_request_id (null; set for an approved
transition and for the move after a rejection), created.

## Behaviour

- A new task in a project with a workflow gets the initial status. A task that
  is created done (`done: true`, or the done bucket of a manual kanban view)
  starts in the first `done` status, which counts as the transition from the
  initial status: the creator has to pass its checks (allowed pair,
  permission and team, required fields and comment), otherwise nothing is
  created and the request answers with the error of the check; when the
  transition needs an approval it answers `409` / `24007`.
- Changing status goes through **one** function that checks, in order: target
  status exists and is not archived → transition allowed
  (`restrict_transitions`) → permission (`min_permission`, teams) → required
  fields → required comment. A failed check returns a typed 4xx error that
  names the unmet requirement (which fields are missing) so the UI can ask
  for exactly those. Every successful change writes history and dispatches
  `task.status.changed`.
- Transitions with `requires_approval`: after all of the checks above passed,
  the function does not move the task but creates (or returns) the pending
  approval request, and its result says so (`Approval` set, `Changed` false).
  `POST /tasks/{task}/status` answers `202` then. Callers that change the
  status as a side effect (the done checkbox) are refused with `409` / `24007`
  (with the target status and an already pending request in `details`, the UI
  turns it into a "Request approval" action) and create nothing; automation and scripts create the request as the rule
  owner; system transitions (re-opening a repeating task, the move after a
  rejection, removing a status) never ask. When the request is approved the
  same function runs again as the requester with only the approval requirement
  bypassed. Details, cancel reasons and the API: [forms-approvals.md](forms-approvals.md#approvals).
- `done` sync: entering a status of category `done` or `cancelled` sets
  `task.done = true`; leaving it clears it. Marking a task done through the
  existing checkbox performs the transition to the first `done` status and is
  subject to the same checks; un-doing moves it back to the initial status.
- Moving a task to another project: status resets to that project's initial
  status (or none if it has no workflow); history is kept. A task that is done
  when it arrives starts in the first `done` status under the same checks as a
  task that is created done; a move that fails them is refused as a whole, so
  taking a task out of the project and back in does not get around the rules.
- Changing the category of a status in the workflow document sets the done
  flag of the tasks in it when the new category counts differently (no
  history entry, the status did not change).
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
  current user may perform right now, each with its unmet requirements,
  `requires_approval` and the approvers that would be asked, and the pending
  approval requests of the task (`pending_approvals`).
- `POST /tasks/{task}/status` — `{to_status_id, comment}`; `200` with the new
  status, or `202` with `approval` when the transition needs an approval.
- `GET /tasks/{task}/status/history`.
- Task lists accept `status` and `status_category` in the filter syntax,
  `sort_by=status` and `expand=status`; see "Filtering and sorting" and
  "Task lists".

Permissions: reading follows project/task read access; changing the
definition needs project admin; changing a task's status needs task write
access plus the transition's own rules.

## Filtering and sorting

`status` and `status_category` work in the normal task `filter` and `sort_by`
parameters, everywhere the filter syntax does (task lists, views, kanban
buckets, saved filters), and combine freely with built-in fields, `cf_<id>`
fields, `&&`, `||` and groups.

| Field | Comparators | Value |
|---|---|---|
| `status` | `=` `!=` `in` `not in` | status ids, e.g. `status in 1,2` |
| `status_category` | `=` `!=` `in` `not in` | `open`, `in_progress`, `done`, `cancelled`, e.g. `status_category not in done,cancelled` |

- Statuses are matched by id, the ids come from the workflow document. Any
  other comparator (`>`, `like`, ...) returns the usual invalid comparator
  `400` (4018); a value that is not an id, or not a category name, returns the
  invalid value `400` (4019). Both are checked when the filter is parsed, so
  they need no database access and a saved filter with an invalid one is
  rejected on save.
- A task without a status row (its project has no workflow) never matches a
  positive comparison (`=`, `in`) and always matches a negative one (`!=`,
  `not in`). With `filter_include_nulls`, positive comparators match it too.
  A status id of another project's workflow simply matches nothing.
- Archived statuses can still be filtered by; tasks keep the status they
  have.
- No project lookup or permission check is needed: the filter only narrows
  tasks the user can read anyway.

Sorting: `sort_by=status` orders by the position of the status in its
workflow (the order of the pickers and of the kanban columns), `order_by`
reverses it. Tasks without a status sort last in both directions, like the
built-in nullable columns. Tasks of several projects (a saved filter) are
ordered by position within their own workflow, so mixed lists are only
meaningful per project.

## Task lists

`expand=status` (task lists, views, single task read, kanban buckets with
tasks) embeds the current status in every task that has one:

```json
{"id": 7, "title": "...", "status": {"id": 3, "title": "In review", "color": "#d97706", "category": "in_progress"}}
```

Tasks without a status have no `status` property. The statuses are loaded in
two queries per response (status rows, then the definitions), regardless of
the number of tasks. API tokens need the `status` permission of the `tasks`
group (the one of `GET /tasks/{task}/status`) to use the expand, like the
other expands that read other resources.

## Kanban

A kanban view gets a third bucket mode next to manual and filter buckets:
`bucket_configuration_mode: "status"`. In that mode the columns are the
non-archived statuses of the project workflow (in position order) and moving
a card performs the transition with the same checks; a rejected drop snaps
back and shows what is missing. Manual buckets keep working unchanged.

- **No stored buckets.** A status view has no rows in `buckets`; the bucket
  list (`GET .../views/{view}/buckets`, `.../buckets/tasks`) is derived from
  the workflow on every read. Changing the workflow changes the board.
- **Bucket id = negated status id.** The column of status 7 is bucket `-7`.
  Real bucket ids are positive and filter-mode buckets use small indexes
  starting at 0, so a column can never be mistaken for a bucket of another
  view, and a bucket id on its own (task creation has no view in the request)
  says unambiguously what it means. Every bucket of a status view also carries
  `status: {id, title, color, category}`. The existing endpoints just work
  with these ids:
  - `PUT .../views/{view}/buckets/{bucket}/tasks` with `bucket = -<status>` is
    the move. It runs the transition as the current user, without a comment,
    through the same function as `POST /tasks/{task}/status` (done sync,
    history, `task.status.changed`). Rejections return the same typed errors
    with the same details (400 `21005`, 403 `21006`, 422 `21007` with
    `task_id`, `from_status_id`, `to_status_id`, `missing_fields`,
    `comment_required`; 404 `21003` / 400 `21004` for an unknown or archived
    status) and nothing moves. Moving into the column the task is already in
    is a no-op, so dropping within a column only changes the position
    (`POST /tasks/{task}/position`, stored per view as everywhere else).
  - `filter=bucket_id = -7` on the view's task list loads one column (the
    frontend pages through a column this way); it is rewritten to
    `status = 7`.
  - Columns are paginated like buckets of the other modes (`page` and
    `per_page` apply per column, `count` is the total of the column).
- **Creating a task in a column**: `POST /projects/{project}/tasks` with
  `bucket_id: -<status>`. A new task always starts in the initial status. When
  the column is the initial status that is all; otherwise the transition is
  attempted right after the task was stored. If it is rejected the task stays
  created in the initial status and the request answers with the workflow
  error (the `task_id` of the task is in the `21007` details). A column that
  does not exist or is archived creates nothing (404 `21003` / 400 `21004`).
  Only the single create on `/api/v2` reports the rejection; bulk creates
  ignore the column and v1 creates the task in the initial status silently.
- **Setting the mode** needs a workflow: creating a status view or switching
  a view to it on a project without one returns 400 `21009`. Only kanban views
  can have the mode. Switching back to manual keeps the buckets the view had
  before and puts the tasks created meanwhile in its default bucket.
- **Bucket management is rejected** in a status view with 400 `21010`:
  creating, renaming, limiting and deleting buckets, and setting a default or
  done bucket on the view. The columns are edited in the workflow editor. (A
  view update that merely echoes the stored ids is fine.)
- **The workflow is deleted later**: the view keeps its mode and degrades to
  an empty bucket list (no error); moves answer 404 `21001`. It can be renamed
  and switched back to manual; the UI explains that the board has no columns.
- Saved filters and favorites cannot use the mode (they have no workflow).

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

### Board, table and list

- **View settings**: a kanban view offers "Workflow statuses" next to
  manual and filter columns when the project has a workflow, with a one-line
  explanation. The column management of the board (add, rename, delete,
  limit, done and default column, reordering) is not shown in that mode.
- **Status board**: the column header is the `WorkflowStatusBadge` of the
  status (colour, category icon) with the task count. A drop into another
  column calls the move; the card snaps back if it is rejected. A `21007`
  opens the status dialog of the task detail for that task and target status
  (missing fields and comment, then "Change status"); `21005`, `21006` and a
  vanished status show their plain message. Which columns a card may enter
  is not computed in advance, the response decides. "Add a task" in a column
  creates the task there, with the same dialog when the transition needs
  input (the task exists in the initial column meanwhile).
- **Table**: a "Status" column with the badge, sortable (`sort_by=status`).
  It is on by default in projects with a workflow (the Columns menu switches
  it off, the choice is remembered with the other columns) and not offered
  in projects without one. `expand=status` is only requested while it is
  shown.
- **List and manual kanban cards**: a small status badge next to the task
  title (card: below it) in projects with a workflow, via `expand=status`.
  Status columns do not repeat it on the cards.
- **Filter input**: `status` and `statusCategory` are known filter fields
  (the filter popup, the view filter editor and saved filters accept and
  round-trip them; they show as `status_category` in the API and in URLs).
  The editor suggests status titles of the project's workflow (and the four
  category names) like it does for labels, shows status ids as titles and
  sends titles as ids. Suggestions need a project; in a saved filter, where
  there is none, ids and categories are typed by hand.

## Steps

- 2a: tables, the transition function, API, events, done sync, tests.
- 2b (done): workflow editor in project settings; status picker and history
  in the task detail view; missing-requirements dialog. See "UI".
- 2c (done): kanban status mode, table column, status badges in list and
  cards, filter/sort/expand by status. See "Filtering and sorting", "Task
  lists" and "Kanban".
- Approvals as a transition requirement (done in 5a, see
  [forms-approvals.md](forms-approvals.md)).
- Later: WIP limits, SLA timers, workflow templates shared between projects.
