# Dashboards and reports — design

Status: step 4a (aggregation API and dashboard CRUD) and 4b (the UI) are implemented, 4c (time in status, workload and time tracking widgets, CSV export, previous period comparison) is implemented except the default dashboards. The sections below describe what is built; "Deferred" lists what is not.

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

One endpoint does all the counting, so widgets, future exports and scripts share it.
`GET /api/v2/reports/tasks/options?project_id=` lists what a request may use in a scope (dimensions incl. the
`cf_<id>` fields of that project, measures with their numeric fields, time fields, whether a workflow exists, limits);
the widget editor is generated from it.

`POST /api/v2/reports/tasks`

```json
{
  "project_id": 1,              // optional scope; 0/absent = all readable projects, -1 = favorites
  "filter": "done = false",     // same syntax as views, incl. status, status_category, cf_<id>
  "filter_include_nulls": false,
  "group_by": "done",           // optional dimension
  "then_by": "assignee",        // optional second dimension (needs group_by)
  "measure": "count",           // count (default) | sum | avg | min | max
  "field": "percent_done",      // sum/avg/min/max: percent_done | time_spent | cf_<id> (number, currency, rating, progress)
  "time": {                     // optional: makes it a time series
    "field": "created",         // created | done_at | due_date | status_entered
    "bucket": "month",          // day (default) | week (starts Monday) | month
    "from": "2018-11-01",       // RFC 3339, YYYY-MM-DD, now, now-90d, date math; default now-90d
    "to": "2018-12-31",         // default now
    "timezone": "Europe/Istanbul" // IANA; default: the user's time zone setting, else the instance time zone
  },
  "limit": 20                   // groups kept per level (default 20, max reports.maxgroups); the rest becomes "other"
}
```

Dimensions: `status`, `status_category` (only meaningful where a workflow exists), `assignee`, `label`, `project`,
`priority`, `done`, `created_by`, `cf_<id>` of a select, multi select, user, checkbox or rating field. Any other
field kind is a typed 400 (`23004`). A `cf_<id>` of a project the viewer cannot read (or that does not exist) gives the
same `4016` error the filter gives, so field ids cannot be probed.

Real response of `{"project_id":1,"group_by":"done","time":{"field":"created","bucket":"month","from":"2018-11-01","to":"2018-12-31","timezone":"Europe/Istanbul"}}`
on the test fixtures:

```json
{
  "groups": [
    {"key": false, "label": "Open", "color": "#94a3b8", "value": 19,
     "series": [
       {"bucket": "2018-11-01", "start": "2018-11-01T00:00:00+03:00", "end": "2018-12-01T00:00:00+03:00", "value": 0},
       {"bucket": "2018-12-01", "start": "2018-12-01T00:00:00+03:00", "end": "2019-01-01T00:00:00+03:00", "value": 19}]},
    {"key": true, "label": "Done", "color": "#22c55e", "value": 1, "series": ["..."]}
  ],
  "total": 20, "value": 20, "measure": "count", "unit": "tasks", "multi_valued": false,
  "time": {"field": "created", "bucket": "month", "from": "2018-11-01T00:00:00+03:00",
           "to": "2019-01-01T00:00:00+03:00", "timezone": "Europe/Istanbul", "buckets": 2}
}
```

Response fields: `groups` (largest value first; each has `key`, `label`, `color`, `value`, and `series` on leaf
groups of a time series, `children` for `then_by`), `series` (the series of all tasks when there is no `group_by`),
`total` (distinct tasks counted), `value` (the measure over all of them: this is what a `number` widget shows),
`measure`, `field`, `unit` (`tasks`, `seconds` or empty), `multi_valued`, `excluded` (time series: tasks whose date
field is empty), `time` (the resolved range), `truncated`.

Semantics:

- **Permissions.** The set of tasks is built by the code of the task list (`TaskCollection.readableTasksQuery`, which
  uses `dbTaskSearcher.buildScopeConds` and the filter conversion shared with `Search`), for the requesting auth:
  project access, shares, teams, favorites, link shares pinned to their project. There is no separate permission
  logic. Group labels (users, labels, options, statuses) are looked up only for keys that occur on those tasks.
- **Groups.** A task with several values (assignee, label, multi select, user field) counts in each group;
  `multi_valued` says so and `total` stays the number of distinct tasks. Tasks without a value form the `none`
  group (`key: null`, `none: true`). `priority` 0 counts as no value; `done` and checkbox fields have no none group
  (an unset checkbox is "not checked", as in the filter). The folded rest is one group with `other: true` and
  `key: null`. For a multi valued first level `other` is computed over the distinct tasks that have any folded value
  (one extra statement), so a task with two folded values counts once, for every measure; on the second level it is the sum of the folded children. `other` groups have no children
  and, on a nested time series, no series.
- **Measures.** Tasks without a value are ignored by sum/avg/min/max. `time_spent` is the sum of finished time
  entries (`end_time` set) per task in seconds; running timers are ignored, not counted up to now. It is not
  available to link shares (they have no time tracking access): typed 403 `23005`.
- **Time series.** Empty buckets inside the range come back with `0`. The first bucket is the one containing
  `from`, the last the one containing `to`; the range is half open at its end. Tasks whose date is empty are not in
  the series and counted in `excluded`; `total` counts the tasks inside the range. `status_entered` is the time the
  task entered its current status.
- **Previous period.** `compare: previous_period` runs the measure over as many buckets as the range has, shifted
  back by that many buckets (calendar months, weeks or days; day starts follow the time zone, also where a day has no
  midnight). When `to` lies inside its last bucket (a plain date names the whole day, `now` is the instant), the range
  is counted until `to` and not until the end of that bucket, and the previous period until the same elapsed time after
  its own start, so a half filled month is compared with the same share of the month before. Series and totals of a
  compared request are cut the same way.
- **Time in status.** Only history of the statuses of the project's current workflow counts (archived statuses
  included): history of a deleted workflow, or of the project a task was moved in from, is ignored. A visit lasts until
  the next history entry of the task; the last one is open (runs until now or the end of the window) only when it is
  the task's current status, otherwise it has no known end and is left out. A task without counted history is counted
  from the time it entered its current status. When the first counted entry of a task leaves a status the history never
  entered (the workflow was added to a project with tasks, which gives them a status but no history), that status is
  counted as a leading visit that starts at the later of the workflow's creation and the task's creation, and not
  before the previous history entry of the task: no more precise start is stored, as the entry time of the current
  status is overwritten by every change. A plain date `to` includes that day. Assignees and labels that no longer exist
  or cannot be seen are counted with the tasks that have none. The rows read (history, current statuses, assignees or
  labels) are bounded by `reports.maxrows`.
- **Limits.** At most two dimensions plus time, `reports.maxbuckets` (366) buckets (`23002`), `reports.maxgroups`
  (200) groups (`23003`), a timeout (`reports.timeout`, 20 s) that cancels the database queries (`23009`, 503),
  a 30 s in-memory cache (`reports.cachettl`) per auth identity and normalized request, at most 256 entries, no
  invalidation.
- **Errors.** Request problems are 400 with code `23001` and `details.param` naming the parameter (`group_by`,
  `time.from`, ...). Enum and range violations of the schema are 422 from the schema validation.

### Time bucketing and time zones

The database is never asked to truncate dates. The buckets are computed in Go in the requested IANA time zone
(`time.LoadLocation`, so daylight saving is exact: a day can be 23 or 25 hours long and starts at local
midnight, or at the clock change where daylight saving starts at midnight and that hour does not exist, as in
America/Santiago and America/Havana; weeks start Monday; months on the 1st) and the boundaries are passed to the query as a balanced tree of
`CASE WHEN column < 'boundary'` expressions (depth log2(buckets), at most 9). That is plain SQL on SQLite, PostgreSQL
and MySQL, with no per-dialect date function and no time zone support tables. The only dialect specific expression
is the length of a time entry in seconds (`time_spent`). Limits: the boundaries are formatted as UTC literals
`YYYY-MM-DD HH:MM:SS`, so second precision; the time zone is one per request (a user whose setting changes sees old
data re-bucketed, nothing is stored); the week start is not configurable.

### Statements per request

Without `group_by`: 1 (totals) plus 1 for a time series (plus 1 to count `excluded`). Each dimension level is one
grouped statement, a time series on groups one more, a folded multi valued first level one more. The count never
depends on the number of groups or tasks.

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

### Dashboards API

- `GET /api/v2/dashboards` (own personal dashboards plus those of every readable project, no widgets, `widget_count`;
  filter `project_id`, `q`), `POST`, `GET/PUT/PATCH/DELETE /api/v2/dashboards/{dashboard}`.
- The widgets are part of the dashboard document: they come with a read and a `PUT` with `widgets: [...]` replaces
  them as a whole (an id of this dashboard keeps and updates the widget, no id creates, missing ones are deleted;
  `null` or no `widgets` keeps them; an id of another dashboard is 404 `23007`).
- `PUT /api/v2/dashboards/{dashboard}/layout` with `{"items":[{"id","x","y","w","h"}]}` only moves and resizes
  (12 columns, `x + w <= 12`); same permission as a change; bumps the dashboard's `updated`/ETag.
- Widget `type` is one of `number, bar, donut, line, created_vs_done, task_list, workload, status_time,
  time_tracking, text`. `config` is strictly decoded per type (unknown keys are an error): `number` `{request,
  compare}`, `bar`/`donut`/`line` `{request}` (a report request; `number` cannot group, `bar` needs `group_by`, `donut`
  one level, `line` needs `time`), `created_vs_done` `{project_id, filter, bucket, from, to}`, `task_list`
  `{project_id, filter, sort_by, order_by, columns, limit<=100}`, `workload` `{project_id, filter, estimate_field}`,
  `status_time` `{project_id, filter, group_by: assignee|label, from, to}`, `time_tracking` `{project_id, from, to, group_by: user|project|label|day}`, `text`
  `{text}`. A report inside a config is validated like `POST /reports/tasks` validates it (so cf fields have to be
  readable by the author) but is not run on save. Problems are `23008` with the path (`widgets[1].config.request.group_by`).
  On a project dashboard every `project_id` is pinned to the project (another one is an error).
- Permissions: a personal dashboard is read and changed by its owner only; a project dashboard is read by everyone
  who can read the project (link shares of that project included) and changed, created and deleted by project admins.
  Link shares never change dashboards. `max_permission` is 2 for those who may change it.
- Lifecycle: purging a project deletes its dashboards; deleting a user deletes their personal dashboards (project
  dashboards they created stay); duplicating a project copies its dashboards (owner = the duplicating user) and remaps
  `cf_<id>`, option ids compared to select fields and `status` ids in `filter` strings and `group_by`/`then_by`/
  `field`/`estimate_field`/`sort_by` values, and `project_id` (`dashboards_lifecycle.go`).

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

## Deferred

- Not in 4a: `status_time`, `workload` and `time_tracking` have a validated config but no data endpoint (4c); CSV
  export; `created_vs_done` has no server side running open count (the client combines two reports: `created` and
  `done_at`); saved filters
  (project ids below -1) are not a report scope; `include_subprojects` is not offered.
- Nested time series carry series on leaf groups only; `other` leaves have none.

## UI status (4b)

What exists (`frontend/src/components/{charts,dashboards}`, `frontend/src/views/dashboards`):

- Pages: `/dashboards` (personal and project dashboards grouped, create, rename, delete), `/dashboards/:id` (view and
  edit), `/projects/:id/dashboards` (project menu entry; opens the only dashboard, lists several, lets a project admin
  create the default one) and a "Dashboards" sidebar link. A new dashboard starts with: open tasks number, tasks by
  status (or done, without a workflow), created vs completed over 30 days, open tasks by assignee, overdue task list.
- Charts are hand-written SVG (bar incl. stacked and grouped, donut, line, number tile), no dependency: viewBox from a
  ResizeObserver, nice ticks, tooltip on hover and focus, `aria-label` per mark, a visually hidden data table, empty
  state, loading skeleton, a note for `multi_valued` reports, a neutral colour for the folded "other" group.
- Grid: 12 columns, CSS grid, one column under 768px. Edit mode: drag by the header, resize from the corner, keyboard
  (arrows move, Shift+arrows resize), vertical compaction (`helpers/dashboardLayout.ts`), debounced layout save with a
  visible unsaved / saving / failed state and retry, undo of the last layout changes. Widgets are added, changed and
  removed through the document `PUT`.
- Widget editor: type gallery, then a form built from `GET /reports/tasks/options` (labels from `title_key`, custom
  fields by title), the task filter editor of views, relative period presets, live preview of the unsaved config,
  typed validation errors (`details.param`, 23xxx and the filter errors) shown on the matching control.
- Drill-down (`helpers/dashboardDrilldown.ts`): a click on a bar, slice, point or number lists exactly the tasks behind
  it by AND-ing the widget filter with the group and bucket. Not expressible in the filter syntax, so the mark is inert
  with an explanation: the folded "other" group; the `status_entered` time field; "no value" of assignee, label and
  user fields together with a widget filter that touches nullable columns (it needs `filter_include_nulls`, which
  would widen the whole filter; the filter may only use `done`, `project`, `created`), and two such groups at once; any
  group when the widget itself sets `filter_include_nulls`. "No value" of status, priority and select fields is
  expressed without the flag. Assignees and creators are filtered by username, which the report does not return: the
  panel looks the user up by the label shown.

Deferred or different from the plan:

- Display options (bar direction, stacked, legend) are not stored: widget configs are decoded strictly and have no room
  for them, so they are a per-viewer setting in the browser.
- `created_vs_done` has no running open count; a `text` widget stores the editor's HTML (sanitized on render).
- The `line` chart does not offer a second dimension.
- Colours without an API colour come from a hash of the group's key and label (same option, same colour in every widget); the empty and folded groups are named in the user's language. In a stack over a multi valued dimension a task counts once per child, so the stack can be taller than the distinct tasks (the tooltip shows both).

## Steps

- 4a (done): aggregation endpoint (all dimensions/measures/time, permission scoping,
  limits, cache) + dashboards/widgets CRUD + tests.
- 4b (done): dashboard pages, SVG chart components, widget gallery and editor,
  drill-down.
- 4c (done, checked in a browser): `status_time`, `workload`, `time_tracking` widgets, CSV export from the widget
  menu and the drill-down, previous period delta on `number` widgets. Open: default dashboards created for new
  projects with a workflow.
