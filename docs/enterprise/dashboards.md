# Dashboards and reports — design

Status: design.

## Goals

- A manager opens one page and sees where work stands: how much is open, what
  is late, who is loaded, how fast things get done.
- Everything on a dashboard is a **widget over a task filter**, so it uses the
  vocabulary people already know from views (the same filter syntax, incl.
  `status`, `status_category` and `cf_<id>`).
- A dashboard never shows more than its viewer may read: every number is
  computed over the tasks the **viewer** can read, at request time.
- No new infrastructure: aggregates are plain SQL over existing tables. No
  snapshot tables in the first version.

## Model

### `dashboards`
id, title, description, owner_id, project_id (null = personal, spans all
projects the viewer can read; set = project dashboard, visible to everyone
who can read the project), position, created / updated.

### `dashboard_widgets`
id, dashboard_id (index), type, title, config (json), x, y, w, h (grid
cells, 12 columns), created / updated.

Personal dashboards can be read and changed only by their owner. Project
dashboards can be read by anyone who can read the project and changed by
project admins.

## Aggregation API

One endpoint does all the counting, so widgets, future exports and scripts
share it.

`POST /api/v2/reports/tasks`

```json
{
  "project_id": 2,              // optional scope; else all readable projects
  "filter": "done = false && due_date < now",
  "group_by": "status",         // optional
  "then_by": "assignee",        // optional second dimension
  "measure": "count",           // count | sum | avg | min | max
  "field": "cf_12",             // numeric field for sum/avg/min/max
  "time": {                     // optional: makes it a time series
    "field": "created",         // created | done_at | due_date | status_entered
    "bucket": "week",           // day | week | month
    "from": "now-90d", "to": "now"
  },
  "limit": 20                   // top N groups, rest folded into "other"
}
```

`group_by` / `then_by`: `status`, `status_category`, `assignee`, `label`,
`project`, `priority`, `done`, `created_by`, `cf_<id>` (select, multi select,
user, checkbox, rating). Measures on `percent_done`, numeric custom fields and
**tracked time** (`time_spent`, summed from time entries).

Response: `{groups: [{key, label, color, value, series?: [{bucket, value}],
children?: [...]}], total}`. Labels and colours are resolved server side
(status title/colour, user name, option title/colour) so widgets stay dumb.

Tasks that appear in several groups (several assignees, labels, multi select
options) are counted in each; the response says so (`multi_valued: true`) and
`total` is the number of distinct tasks.

Limits: at most 2 dimensions plus time, 366 time buckets, 200 groups; a
statement timeout where the database supports one; results cached per
(viewer, request hash) for 30 s in memory.

## Widgets

| Type | Shows | Config |
|---|---|---|
| `number` | one figure with optional comparison to the previous period | filter, measure, compare period |
| `bar` / `donut` | groups | filter, group_by, measure, stacked then_by (bar) |
| `line` | time series, one line per group | filter, time, group_by |
| `created_vs_done` | two series: created and completed per bucket, with the running open count | filter, bucket, range |
| `task_list` | the tasks themselves (top N, sortable) | filter, sort, columns |
| `workload` | per assignee: open tasks, overdue, and sum of an estimate field | filter, estimate field |
| `status_time` | average time spent in each status (from status history) | project, range |
| `time_tracking` | tracked time per user / project / day | range, group_by |
| `text` | a note (markdown) | text |

Clicking a group or a bar opens the task list filtered to exactly those
tasks.

## Pages

- **Home dashboard**: each user has a personal default dashboard reachable
  from the sidebar; the existing overview page stays as is.
- **Project → Dashboard**: a project view kind next to list / gantt / table /
  kanban is the natural place, but adding a view kind touches a lot of
  upstream code; first version is a separate page linked from the project
  menu, promoted to a view kind later if it proves itself.
- Edit mode: add widget (gallery with a live preview), drag to move, resize
  from the corner, configure in a side panel with the same filter input used
  in views. 12-column grid, single column on phones.
- Charts are drawn with plain SVG components (bar, stacked bar, donut, line):
  no charting dependency, themeable with the existing CSS variables,
  accessible (each chart has a data table alternative).

## Reports

- "Export" on a widget or a task list widget: CSV of the underlying numbers
  or tasks.
- Scheduled e-mail of a dashboard (weekly summary) once the mailer is in use;
  implemented as an automation action later, not as a separate scheduler.

## Steps

- 4a: aggregation endpoint (all dimensions/measures/time, permission scoping,
  limits, cache) + dashboards/widgets CRUD + tests.
- 4b: dashboard pages, SVG chart components, widget gallery and editor,
  drill-down.
- 4c: `status_time`, `workload`, `time_tracking` widgets, CSV export, default
  dashboards created for new projects with a workflow.
