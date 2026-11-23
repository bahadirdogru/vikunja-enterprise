# Custom fields — design

Status: backend done (step 1a), filtering and sorting done, display in task
lists, table columns and kanban cards done, filter input with field names done.

## Model

Field **definitions** belong to a project. Field **values** belong to a task.
Definition and value are separate tables; values are stored in typed columns
(not one JSON blob) so they can be filtered and sorted in SQL.

### `custom_fields`

| column | type | notes |
|---|---|---|
| id | bigint pk | |
| project_id | bigint, index, not null | owning project |
| title | varchar(250), not null | |
| description | text, null | |
| type | varchar(30), not null | see types below |
| required | bool, default false | enforced by workflow transitions later, not on every save |
| config | json/text, null | type specific: `{"min":0,"max":100,"precision":2,"currency":"TRY","max_rating":5}` |
| position | double, not null | ordering inside the project |
| created_by_id | bigint, not null | |
| created / updated | timestamps | |

### `custom_field_options` (for `select` and `multi_select`)

id, custom_field_id (index), title varchar(250), color varchar(7) null, position double.

### `task_custom_field_values`

One row per value. Single-value types have at most one row per (task, field);
`multi_select` and multi `user` have one row per selected option/user.

| column | type |
|---|---|
| id | bigint pk |
| task_id | bigint, index, not null |
| custom_field_id | bigint, index, not null |
| value_text | text null |
| value_number | double null |
| value_date | datetime null |
| value_bool | bool null |
| option_id | bigint null, index |
| user_id | bigint null, index |
| created / updated | timestamps |

### Types (v1)

`text`, `long_text`, `number`, `currency`, `date`, `checkbox`, `select`,
`multi_select`, `user`, `url`, `email`, `rating`, `progress`.

Validation on write: number/currency respect `min`/`max`; rating is an integer
`1..max_rating` (default 5); progress is `0..100`; url/email are format
checked; select/multi_select option ids must belong to the field; user ids
must have read access to the task's project. The field type cannot be changed
after creation.

## API (`/api/v2`)

- `GET  /projects/{project}/custom-fields` — list (paginated), options embedded.
- `POST /projects/{project}/custom-fields` — create (options embedded).
- `GET|PUT|DELETE /projects/{project}/custom-fields/{field}` — options are
  replaced by the submitted list on update (matched by id, new ones created,
  missing ones deleted together with the values that reference them).
- `GET  /tasks/{task}/custom-field-values` — all values of the task, one
  entry per field of the task's project (unset fields are included with a
  null value so clients can render the full form).
- `PUT  /tasks/{task}/custom-field-values/{field}` — set the value.
- `DELETE /tasks/{task}/custom-field-values/{field}` — clear the value.

The JSON value shape depends on the type: string (`text`, `long_text`, `url`,
`email`), number (`number`, `currency`, `rating`, `progress`), RFC 3339 string
(`date`), boolean (`checkbox`), option id (`select`), array of option ids
(`multi_select`), array of user ids (`user`).

## Permissions

- Definitions: read needs read access to the project, create/update/delete
  needs **admin** on the project.
- Values: read needs read access to the task, set/clear needs write access to
  the task. Link shares follow the share's permission.

## Events

- `custom-field.created|updated|deleted`
- `task.custom-field-value.updated` with task, field, old and new value and
  doer. This is the hook the automation engine will listen to.

## Lifecycle

Deleting a field deletes its options and values. Deleting a task or a project
deletes the values / definitions that belong to it. Duplicating a project
copies the definitions and the values of the copied tasks (ids remapped);
duplicating a task inside the same project copies the values. Values are
removed when a task is purged, not when it is moved to the trash.

When a task is moved to another project (single or bulk update that changes
`project_id`), its values for fields of the old project are deleted, since
those fields do not exist in the new project. Values of the new project's
fields are kept.

## Filtering and sorting

A field is addressed as `cf_<fieldID>` in the normal task `filter` and
`sort_by` parameters, for example
`cf_12 > 100 && cf_3 = 5 && done = false`. Filters work everywhere the filter
syntax does (task lists, views, saved filters) and combine freely with
built-in fields, `&&`, `||` and groups.

| Field type | Comparators | Value |
|---|---|---|
| number, currency, rating, progress | `=` `!=` `>` `>=` `<` `<=` | number |
| date | `=` `!=` `>` `>=` `<` `<=` | same formats, date math and `filter_timezone` handling as `due_date` |
| checkbox | `=` `!=` | `true` / `false`; a task without a value counts as `false` |
| text, long_text, url, email | `=` `!=` `like` | text (`like` is a substring match) |
| select, multi_select | `=` `!=` `in` `not in` | option ids, e.g. `cf_4 in 3,4`; multi_select matches if any selected option matches |
| user | `=` `!=` `in` `not in` | user ids or usernames, e.g. `cf_9 in 1,alice` |

- `!=` and `not in` also match tasks that have no value for the field.
  With `filter_include_nulls`, positive comparators match them too.
- Date `=` compares the exact stored instant, like `due_date =`; use a range
  (`>` and `<`) to match a whole day.
- An unknown field id, a field the user cannot read (its project is not
  readable) and a malformed identifier all return the same `400` as an
  unknown built-in field, so field ids of other projects cannot be probed. A
  comparator or value that does not fit the field type returns the usual
  invalid comparator / invalid value `400`.
- The check happens when the query runs, so a saved filter is accepted as long
  as it parses; it fails (and is skipped when matching tasks to views) if the
  field was deleted.

### Field names in the filter input

The filter input in the UI never shows `cf_<id>`. It shows the field name and
translates to and from the API form, also when a saved filter is loaded.
Nothing changes on the API: the stored filter string keeps `cf_<id>` and option
ids, so renaming a field does not break a saved filter.

| Shown in the input | Sent to the API |
|---|---|
| `Budget > 100` | `cf_12 > 100` |
| `"Cost center" like abc` | `cf_3 like abc` |
| `Stage in Review, "Won, closed"` | `cf_4 in 31, 32` |
| `"Go live" < now/w+1w` | `cf_5 < now/w+1w` |
| `Owner in alice, bob` | `cf_6 in alice, bob` |

- **Quoting.** A name made only of letters, digits and `_` is written as it is.
  Anything else (spaces, punctuation) is double quoted; `\"` and `\\` escape a
  quote and a backslash inside. Matching is case-insensitive. Select option
  titles follow the same rule, and a title with a comma is quoted in lists.
- **Built-in names win.** A bare `done`, `priority`, `labels`, ... is always the
  built-in field. A custom field with such a name is written quoted
  (`"Priority" > 2`); the input does this by itself when it displays the filter.
  A quoted name never means a built-in field.
- **Where a field is looked up.** In a project view the fields of that project.
  In saved filters and other cross-project places the fields of every
  non-archived project the user can read; they are fetched when the filter
  already uses one or the user starts typing in the input.
- **Name collisions.** Fields whose names are equal (ignoring case) cannot be
  addressed by that name. Each gets the project title appended,
  `Budget (Marketing)`, and if that is still equal the field id,
  `Budget (Marketing) #12`. The order that decides this is project id, field
  position, field id, so the same set of fields always gets the same names. The
  plain name of a colliding field is not resolved and the API answers with the
  usual unknown-field `400`.
- **Values.** Select and multi-select option titles are turned into option ids
  (and back); text that matches no option is sent unchanged and fails in the
  API like a wrong id. Numbers, text, checkbox values and dates are not
  touched, so date math works as before, and the date picker opens when
  clicking a date value of a date field. User values are sent as typed
  (usernames are accepted by the API); the assignee autocomplete suggests them.
  User ids from an old filter stay as ids.
- **Autocomplete.** At the start of an expression, after one typed character,
  the field names are suggested; after the operator of a select field the
  option titles, after the operator of a user field the users.
- **Fallbacks.** A raw `cf_<id>` still works when typed, and a field that is not
  known (deleted, not readable) stays as `cf_<id>` in the display.

Sorting: `sort_by=cf_<fieldID>` with `order_by=asc|desc` works for all
single-value types. Select fields sort by option position, not option id.
`multi_select` and `user` fields cannot be sorted by (`400`). Tasks without a
value sort last in both directions, like the built-in nullable columns.

## Display

### `expand=custom_fields`

Task list and single task reads (`/api/v2/tasks`, `/projects/{project}/tasks`,
`/projects/{project}/views/{view}/tasks`, `/projects/{project}/views/{view}/buckets/tasks`,
`/tasks/{task}`, `/projects/{project}/tasks/by-index/{index}`) accept
`expand=custom_fields`. Each task then carries `custom_fields`, a compact list
with one entry per field that has a value, ordered by the field position:

```json
"custom_fields": [
  {"custom_field_id": 1, "type": "text", "value": "REF-1"},
  {"custom_field_id": 2, "type": "select", "value": 1},
  {"custom_field_id": 4, "type": "multi_select", "value": [3, 4]}
]
```

`value` has the same shape as in `/tasks/{task}/custom-field-values`. Fields
without a value are left out (a checkbox set to `false` counts as a value). The
values of a page are loaded with two queries, independent of the page size.
Without the expand, and for tasks that have no value at all, the key is absent. API tokens need
`tasks_custom_field_values` `read_all` to use it, like `comment_count` needs
the comment scope.

### Table view

The columns menu of a table view lists the project's custom fields under a
"Custom fields" heading. They are off by default; the choice is stored in the
browser (`localStorage` key `tableViewCustomColumns`, field ids per project id).
The view requests `expand=custom_fields` only while at least one custom column
is shown. Cells are read-only; headers of all types except `multi_select` and
`user` sort through `sort_by=cf_<id>`.

### Kanban cards

A kanban view has a "Card fields" menu next to the filter. Selected fields are
shown on every card as label and value; a card without a value for a field
omits that line. The selection is stored in the browser (`localStorage` key
`kanbanCardFields`, field ids per view id). The expand is added to the bucket
requests only when a field is selected.

Both use the read-only `CustomFieldValueDisplay` component, which renders text
truncated, numbers and currency by locale, dates with the app's date format,
checkboxes as a check mark, select values as coloured chips, users with
avatar and name, links, rating stars and a progress bar.
