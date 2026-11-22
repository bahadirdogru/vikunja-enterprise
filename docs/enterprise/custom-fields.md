# Custom fields — design

Status: backend in progress (step 1a). Frontend, filtering and table/kanban
display follow in later steps.

## Model

Field **definitions** belong to a project. Field **values** belong to a task.
Definition and value are separate tables; values are stored in typed columns
(not one JSON blob) so they can be filtered and sorted in SQL later.

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
copies the definitions; duplicating a task inside the same project copies the
values.
