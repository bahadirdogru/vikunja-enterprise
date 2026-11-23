# API

This document describes the HTTP API of vikunja-enterprise with the focus on what the fork adds. For everything that is inherited from upstream (projects, tasks, labels, teams, users, CalDAV, ...) the OpenAPI document of your instance is the reference, and [upstream's documentation](https://vikunja.io/docs/) applies.

Related documents: [ARCHITECTURE.md](ARCHITECTURE.md) (how requests are processed), the [module pages](docs/modules/README.md) (behaviour and data per module), [DESIGN.md](DESIGN.md) (how the UI uses the API).

## Contents

- [Versions](#versions)
- [Base URL and documentation](#base-url-and-documentation)
- [Authentication](#authentication)
- [Conventions](#conventions)
- [Filter syntax](#filter-syntax)
- [Errors](#errors)
- [Rate limits](#rate-limits)
- [The automation request marker](#the-automation-request-marker)
- [Unauthenticated endpoints](#unauthenticated-endpoints)
- [Endpoints added by the fork](#endpoints-added-by-the-fork)
- [Examples](#examples)
- [The generated frontend client](#the-generated-frontend-client)
- [MCP](#mcp)

## Versions

- **`/api/v1`** is the original Echo-based API with swaggo annotations. It is **frozen**: it keeps running and is supported, but it does not grow. It gets bug fixes and ports of resources to v2, nothing else. None of the fork's features exists on v1.
- **`/api/v2`** is built with [Huma](https://huma.rocks). Every new route goes here, including everything the fork adds. What that implies:
  - The OpenAPI document is generated from the Go types; there are no hand-written annotations.
  - Verbs are REST-style: `POST` creates, `PUT` replaces, `PATCH` updates partially. (v1 used `PUT` to create and `POST` to update.)
  - URLs are kebab-case, JSON is snake_case.
  - Errors are RFC 9457 `application/problem+json` with Vikunja's numeric `code`.
  - Lists have one envelope, reads send ETags, and a `PATCH` is synthesised for every resource that has a `GET` and a `PUT` on the same path.
  - A successful `PUT` that sets a value in place answers `200`; a `POST` that creates answers `201`; a `DELETE` answers `204`; operations that start asynchronous work answer `202`.

## Base URL and documentation

| What | Where |
|---|---|
| API base | `<service.publicurl>/api/v2` (for example `https://tasks.example.com/api/v2`) |
| OpenAPI 3.1 document | `/api/v2/openapi.json` and `/api/v2/openapi.yaml` |
| OpenAPI 3.0 downgrade | `/api/v2/openapi-3.0.json` and `/api/v2/openapi-3.0.yaml` |
| Interactive reference (Scalar, served from the binary, no CDN) | `/api/v2/docs` |
| JSON schemas referenced by `$schema` links in responses | `/api/v2/schemas/{schema}` |
| Instance information (version, enabled features) | `GET /api/v2/info` |
| Health check | `GET /api/v2/health` |
| v1 reference (Redoc) and document | `/api/v1/docs` and `/api/v1/docs.json` |

All of these are public. Responses of the API carry `Cache-Control: no-store`.

## Authentication

Three kinds of credential are accepted, always as `Authorization: Bearer <token>`:

| Credential | How to get it | Notes |
|---|---|---|
| **User JWT** | `POST /api/v2/login` with `username`, `password`, optionally `totp_passcode` and `long_token` (needs local or LDAP authentication to be enabled). The answer is `{"token": "..."}`. A refresh token is set as an `HttpOnly` cookie scoped to the refresh endpoint (`POST /api/v2/user/token/refresh`). OpenID Connect and OAuth flows are also available. | Short-lived. Full rights of the user. Sessions are listed at `GET /api/v2/user/sessions`. |
| **API token** | `POST /api/v2/tokens` with a `title`, an `expires_at` and `permissions`. The cleartext token (prefix `tk_`) is returned once, in that response. | Limited to the permission groups it was created with. `GET /api/v2/routes` lists the groups and permissions that exist, and the available groups are generated from the registered routes. |
| **Link share JWT** | `POST /api/v2/shares/{share}/auth` with the share's hash (and a password for password-protected shares). | Scoped to one project with the share's permission. |

`GET /api/v2/token/test` checks that a bearer token is valid.

**Which of the fork's routes accept which credential.** Every authenticated route of the fork accepts a user JWT and, where an API token has the matching permission group, an API token. The model's `Can*` methods decide the rest:

| Module | Link shares | API tokens |
|---|---|---|
| Custom fields | Read follows the share; changing values follows the share's write permission; definitions need project admin, so never | Groups `projects_custom_fields` and `tasks_custom_field_values` |
| Workflows | Read yes; changing the definition needs admin, so no; status changes follow write permission and the transition's rules | Routes under `projects` and `tasks` (the `status` permission of the `tasks` group) |
| Approvals | **No access at all** (403 on every route) | Groups `approvals` and `tasks_approvals` |
| Automation | No: needs project admin | Groups `projects_automations`, `projects_automations_runs`, and the other automation routes by path |
| Dashboards and reports | Project dashboards: read yes, change never; reports yes, except the time tracking report and the `time_spent` measure (403, code 23005) | Group `dashboards`; report routes by path |
| Forms (admin) | Never | Groups `projects_forms` and `projects_forms_submissions` |
| Forms (public) and the incoming webhook | Not applicable, see [Unauthenticated endpoints](#unauthenticated-endpoints) | Not applicable |

Reading expanded data needs the permission of what is embedded: `expand=custom_fields` needs `tasks_custom_field_values` `read_all`, `expand=status` needs the `status` permission of `tasks`, and `expand=approvals` needs `tasks_approvals` `read_all`. Use `GET /api/v2/routes` for the authoritative list.

## Conventions

**Media types.** Requests and responses are `application/json`. `PATCH` takes `application/merge-patch+json`. Form bodies (`application/x-www-form-urlencoded`) are refused with `415`, except on the OAuth token endpoint. File uploads are `multipart/form-data`.

**Resources.** Single resources are returned as the resource itself, without an envelope. Reads send an `ETag` (it includes the caller's permission) and accept `If-None-Match` for a `304`. A single read of a resource that has permissions includes `max_permission` (0 read, 1 read and write, 2 admin) where the operation documents it.

**Lists** are paginated with one envelope:

```json
{"items": [], "total": 0, "page": 1, "per_page": 50, "total_pages": 0}
```

`page` is 1-based. `per_page` defaults to and is silently capped at `service.maxitemsperpage` (default 50, reported as `max_items_per_page` by `/info`); asking for more is not an error, so drive paging from `total_pages` and `per_page` in the response. Many lists accept `q` for a search string.

**Task lists** (`GET /tasks`, `GET /projects/{project}/tasks`, `GET /projects/{project}/views/{view}/tasks`, `GET /projects/{project}/views/{view}/buckets/tasks`) take `q` (search), `filter`, `filter_timezone`, `filter_include_nulls`, `include_subprojects`, `sort_by` and `order_by` (repeatable, paired by position), and `expand` (repeatable), besides `page` and `per_page`. The accepted `expand` values are `subtasks`, `buckets`, `reactions`, `comments`, `comment_count`, `time_entries_count`, `is_unread`, and the fork's `custom_fields`, `status` and `approvals`. Single task reads (`GET /tasks/{task}`, `GET /projects/{project}/tasks/by-index/{index}`) accept the same `expand` values.

- `expand=custom_fields` adds `custom_fields: [{"custom_field_id": 1, "type": "text", "value": "REF-1"}, ...]` with one entry per field that has a value, in field order.
- `expand=status` adds `status: {"id", "title", "color", "category"}` to tasks that have a status.
- `expand=approvals` adds `approvals: {"has_pending", "pending_count"}`.

The values of a page are loaded with a fixed number of queries, independent of the page size.

**Kanban status mode.** A project view with `bucket_configuration_mode: "status"` has the workflow statuses as its columns. Its bucket ids are negated status ids (status 7 is bucket `-7`); `filter=bucket_id = -7` loads one column and `POST /projects/{project}/tasks` with `bucket_id: -7` creates a task in that column. See [Workflows](docs/modules/workflows.md).

## Filter syntax

The `filter` parameter of task lists (and saved filters and view filters) is one or more conditions `field operator value`, combined with `&&` and `||` and grouped with parentheses. The words `and` and `or` are not operators. Operators are `=`, `!=`, `>`, `>=`, `<`, `<=`, `like`, `in` and `not in`. Values containing spaces or `&`, `|`, `(`, `)` are quoted. Date fields accept absolute dates and date math (`now`, `now+24h`, `now/d`, `now/w+1w`, `now-1M/M`) resolved in `filter_timezone` or the server time zone.

The fork adds fields:

| Field | Comparators | Value | Notes |
|---|---|---|---|
| `status` | `=` `!=` `in` `not in` | status ids, for example `status in 1,2` | A task without a status never matches a positive comparison and always matches a negative one; with `filter_include_nulls` positive comparators match it too. |
| `status_category` | `=` `!=` `in` `not in` | `open`, `in_progress`, `done`, `cancelled`, for example `status_category not in done,cancelled` | Same rules as `status`. |
| `cf_<id>` | by field type | see below | `<id>` is the custom field id. |

Custom field comparators by type: number, currency, rating and progress use `=` `!=` `>` `>=` `<` `<=` with numbers; date uses the same comparators with the formats of `due_date` (`=` compares the exact stored instant, so use a range to match a day); checkbox uses `=` and `!=` with `true` or `false` (a task without a value counts as `false`); text, long text, url and email use `=`, `!=` and `like`; select and multi select use `=`, `!=`, `in` and `not in` with option ids (`cf_4 in 3,4`); user fields use the same with user ids or usernames (`cf_9 in 1,alice`). `!=` and `not in` also match tasks that have no value.

Sorting: `sort_by=status` orders by the position of the status in its workflow; `sort_by=cf_<id>` works for all single-value types (select fields sort by option position; `multi_select` and `user` fields cannot be sorted by and answer `400`). Tasks without a value sort last in both directions.

An unknown `cf_<id>`, one of a project the caller cannot read, and a malformed identifier all answer the same `400` with code `4016`, so field ids of other projects cannot be probed. A comparator that does not fit is `400` with `4017`, an invalid value is `400` with `4019`. The checks run when the query runs, so a saved filter is accepted as long as it parses and fails if its field was deleted. In the web client the filter input shows field names; the API always uses ids.

## Errors

Errors are `application/problem+json`:

```json
{
  "title": "Conflict",
  "status": 409,
  "detail": "Moving the task to this status needs an approval. Nothing was changed. Request it with POST /tasks/{task}/status.",
  "code": 24007,
  "details": {
    "task_id": 20,
    "from_status_id": 1,
    "from_status_title": "Todo",
    "to_status_id": 5,
    "to_status_title": "Dropped",
    "to_status_category": "cancelled",
    "pending_approval_id": 0,
    "approval_pending": false,
    "requestable": true
  }
}
```

| Field | Meaning |
|---|---|
| `status`, `title`, `detail` | HTTP status, its text, and a human-readable message |
| `code` | Vikunja's numeric error code; this is what clients should switch on |
| `i18n_params` | Values referenced by the message, for client-side localization |
| `details` | Error-specific data; its shape depends on `code` (for example `issues`, a list of `{path, message}` for invalid definitions, or `errors` on a form submission) |
| `errors` | Huma's per-field validation details for schema errors |

Schema violations (an enum, a range, a missing required property) are `422`, and so are `govalidator` failures. Server errors (`5xx`) never carry internal detail. An expired or invalid token is `401` with code `11`, which clients use to refresh.

**Ranges.** Codes below 20000 are upstream's. The fork's codes are in these ranges, each defined in `pkg/models/error_<module>.go`:

| Range | Module | File |
|---|---|---|
| 20001 - 20005 | Custom fields | `pkg/models/error_custom_fields.go` |
| 21001 - 21010 | Workflows | `pkg/models/error_workflow.go` |
| 22001 - 22013 | Automation (22005 - 22008 are scripts and secrets) | `pkg/models/error_automation.go`, `pkg/models/error_automation_secrets.go` |
| 23001 - 23010 | Reports and dashboards | `pkg/models/error_reports.go` |
| 24001 - 24010 | Approvals | `pkg/models/error_approvals.go` |
| 25001 - 25009 | Forms | `pkg/models/error_forms.go` |

**Codes a client most needs.**

| Code | HTTP | Meaning | Where |
|---|---|---|---|
| 4016 | 400 | Unknown task field (also an unreadable or unknown `cf_<id>`) | filters, sorting |
| 4017 | 400 | Invalid filter comparator | filters |
| 4019 | 400 | Invalid filter value | filters |
| 20002 | 400 | Invalid custom field definition | custom fields |
| 20003 | 400 | The type of a field cannot change | custom fields |
| 20005 | 400 | Value not valid for the field | custom field values |
| 21002 | 400 | Invalid workflow definition (`details.issues`) | workflow PUT |
| 21005 | 400 | The workflow does not allow this status change | status change |
| 21006 | 403 | Not allowed to perform this transition (permission or team) | status change |
| 21007 | 422 | Requirements not met: `details` has `task_id`, `from_status_id`, `to_status_id`, `missing_fields`, `comment_required` | status change |
| 21008 | 409 | Removed statuses still have tasks; send `move_tasks_to` | workflow PUT |
| 21009 | 400 | A status board needs a workflow | views |
| 21010 | 400 | Buckets of a status view are managed by the workflow | views |
| 22002 | 400 | Invalid rule, with `details.issues[] = {path, message}` and, for scripts, `line` and `column` | automation |
| 22005 | 400 | Scripts are disabled on this instance | automation |
| 22008 | 409 | A secret cannot be decrypted (`service.secret` changed) | secrets |
| 22009 | 409 | The rule cannot be run this way (disabled, or no manual trigger) | manual run |
| 22010 | 404 | Webhook not found (unknown token, disabled rule, other trigger) | incoming webhook |
| 22011 | 401 | Signature missing or wrong | incoming webhook |
| 22012 | 429 | Webhook rate limited, with `Retry-After` | incoming webhook |
| 22013 | 400 | The webhook body is not a JSON object | incoming webhook |
| 23001 | 400 | Invalid report parameter, `details.param` names it | reports |
| 23002 / 23003 | 400 | Too many buckets or groups | reports |
| 23005 | 403 | Not available through a link share | reports |
| 23008 | 400 | Invalid dashboard or widget, with the path | dashboards |
| 23009 | 503 | The report took longer than `reports.timeout` | reports |
| 23010 | 400 | More rows than `reports.maxrows` | reports, exports |
| 24002 | 409 | Approval already resolved or cancelled | approvals |
| 24003 | 403 | You are not an approver of this request | approvals |
| 24005 | 422 | A rejection needs a comment | approvals |
| 24006 | 422 | Nobody left to ask | approval requests |
| 24007 | 409 | The change needs an approval; nothing was changed (`details`, see above) | plain done updates |
| 25002 | 400 | Invalid form definition (`details.issues`) | forms |
| 25003 | 410 | The form takes no submissions (`details.reason`: `disabled`, `closed`, `full`) | public forms |
| 25004 | 401 | Login required for this form | forms |
| 25006 | 422 | Some answers are invalid (`details.errors[] = {field, code, message}`) | form submission |
| 25007 | 429 | Too many submissions, with `Retry-After` | public forms |

The complete lists are in the files above and in the [module pages](docs/modules/README.md).

## Rate limits

- **Global per-user or per-IP limit** for authenticated routes: off by default. `ratelimit.enabled`, `ratelimit.kind` (`user` or `ip`), `ratelimit.period` (seconds), `ratelimit.limit` and `ratelimit.store`.
- **Credential endpoints** (login, registration, password reset, email confirmation, link share auth, OpenID callback): `ratelimit.noauthlimit` requests per minute per IP, default 10. This limit cannot be disabled.
- **Session renewal** (token refresh and the OAuth token endpoint): `ratelimit.tokenrefreshlimit`, default 60 per minute per IP.
- **Public form endpoints and the incoming webhook endpoint**: a floor of `forms.public.requestratelimit` requests per client IP and minute (default 60), which applies even when `ratelimit.enabled` is off. On top of it, public form submissions are limited per IP and per form per hour (`forms.public.ratelimit`, `forms.public.formratelimit`), and webhook calls per rule and minute (`automation.webhook.ratelimit`). Limited responses are `429` with a `Retry-After` header.
- Which address counts as the client's follows `service.ipextractionmethod` and `service.trustedproxies`.

## The automation request marker

Every HTTP request that the automation engine itself sends (the `http.request` action and `http.fetch` in scripts) carries the header `X-Vikunja-Automation-Depth`. The server answers **every** `/api/v1` and `/api/v2` request that carries this header with `508 Loop Detected`, before authentication. This stops a rule from calling the task API or the incoming webhook endpoint of its own instance, whatever address or token it uses, which would create work that no run, origin or depth accounts for. The header cannot be removed or changed by a rule and survives redirects. A reverse proxy that strips the header, or a second hop through an external service that calls back without it, is outside what the guard can see. Do not send this header from your own clients.

## Unauthenticated endpoints

Besides the public endpoints of upstream (`/info`, `/health`, the OpenAPI document and docs page, login, registration, password reset, link share auth), the fork adds these. All of them are bounded as described in [ARCHITECTURE.md](ARCHITECTURE.md#public-surface).

| Method | Path | Credential |
|---|---|---|
| GET | `/api/v2/forms/{token}` | The form token. Public forms need no authentication; forms for signed-in users answer `401` / `25004` without a valid `Authorization` header. |
| GET | `/api/v2/forms/{token}/logo` | The form token; the logo is served whatever the access mode, because a browser cannot send credentials with an image request. |
| POST | `/api/v2/forms/{token}/submissions` | As above. JSON body. |
| POST | `/api/v2/forms/{token}/submissions/multipart` | As above. `multipart/form-data` with the JSON in the part `data` and files in the parts `files`. |
| POST | `/api/v2/automation/hooks/{token}` | The secret token in the URL. |

The form routes are *optional-auth*: with no `Authorization` header the caller is anonymous; with a header it is validated like anywhere (an invalid or expired token is `401` code `11`). Link shares are not users: they get `403` on `users` and `members` forms and are treated as anonymous on `public` forms.

**Incoming webhook.** `POST /api/v2/automation/hooks/{token}` with a JSON object body (`{}` for none; at most `automation.webhook.maxbody` bytes, `413` above) starts the rule asynchronously and answers `202` with `{"queued": true}`. The body becomes `event.payload` for templates and conditions. Unknown tokens, disabled rules and rules with another trigger are all the same `404` (code 22010). If the rule names a project secret, the call must carry the header

```text
X-Vikunja-Signature: sha256=<hex HMAC-SHA256 of the raw body, keyed with the secret>
```

and a missing or wrong signature is `401` (code 22011), compared in constant time and checked before the rate limit counts. **The signature has no replay protection.** It covers the body only, with no timestamp or nonce, so whoever captured a signed request can send it again until the secret or the address is changed. Rules keep no state between runs, so they cannot filter out requests they have seen. Rotate the address (`POST /api/v2/projects/{project}/automations/{rule}/webhook-token`) or the secret when a capture is suspected, and put a verifying proxy in front of Vikunja where more is needed.

## Endpoints added by the fork

The tables below list every `/api/v2` operation the fork registers, grouped by module. Paths are relative to `/api/v2`. "Permission" is the right the caller needs; "read, write and admin on project" are the project rights of the caller, "on task" means on the project of the task. A `PATCH` is also available on every path below that has both a `GET` and a `PUT` (custom fields, workflow, automation rules, dashboards, forms); it is synthesised by Huma and not counted in the tables.

**67 operations are documented**: custom fields 8, workflows 6, approvals 6, automation 17, dashboards 6, reports 10, forms 14. The list was produced by a throwaway script that read every `huma.Operation{...}` block (operation id, method, path, summary) from the fork's route files in `pkg/routes/api/v2/` (`custom_fields.go`, `workflow.go`, `approvals.go`, `automation.go`, `automation_secrets.go`, `automation_hooks.go`, `dashboards.go`, `reports.go`, `forms.go`, `forms_public.go`), and the totals were cross-checked against a count of `OperationID:` lines in those files. The script is not kept in the repository, so when a route changes, edit these tables by hand or re-extract them the same way. The permission column is written from the model's `Can*` methods and the operation descriptions.

### Custom fields

| Method | Path | Operation ID | Permission | Description |
|---|---|---|---|---|
| GET | `/projects/{project}/custom-fields` | `custom-fields-list` | read on project | List the custom fields of a project |
| GET | `/projects/{project}/custom-fields/{field}` | `custom-fields-read` | read on project | Get a custom field |
| POST | `/projects/{project}/custom-fields` | `custom-fields-create` | admin on project | Create a custom field |
| PUT | `/projects/{project}/custom-fields/{field}` | `custom-fields-update` | admin on project | Update a custom field |
| DELETE | `/projects/{project}/custom-fields/{field}` | `custom-fields-delete` | admin on project | Delete a custom field |
| GET | `/tasks/{task}/custom-field-values` | `task-custom-field-values-list` | read on task | List the custom field values of a task |
| PUT | `/tasks/{task}/custom-field-values/{field}` | `task-custom-field-values-set` | write on task | Set a custom field value on a task |
| DELETE | `/tasks/{task}/custom-field-values/{field}` | `task-custom-field-values-clear` | write on task | Clear a custom field value on a task |

### Workflows

| Method | Path | Operation ID | Permission | Description |
|---|---|---|---|---|
| GET | `/projects/{project}/workflow` | `workflow-read` | read on project | Get the workflow of a project |
| PUT | `/projects/{project}/workflow` | `workflow-replace` | admin on project | Create or replace the workflow of a project |
| DELETE | `/projects/{project}/workflow` | `workflow-delete` | admin on project | Delete the workflow of a project |
| GET | `/tasks/{task}/status` | `task-status-read` | read on task | Get the status of a task |
| POST | `/tasks/{task}/status` | `task-status-change` | write on task, plus the transition rules | Change the status of a task |
| GET | `/tasks/{task}/status/history` | `task-status-history-list` | read on task | List the status history of a task |

### Approvals

| Method | Path | Operation ID | Permission | Description |
|---|---|---|---|---|
| GET | `/approvals` | `approvals-list` | signed-in user (own requests only) | List my approvals |
| GET | `/approvals/{request}` | `approvals-read` | read on task (no link shares) | Get an approval request |
| POST | `/approvals/{request}/decision` | `approvals-decide` | an approver who has not decided | Decide on an approval request |
| POST | `/approvals/{request}/cancel` | `approvals-cancel` | the requester or project admin | Cancel an approval request |
| GET | `/tasks/{task}/approvals` | `task-approvals-list` | read on task (no link shares) | List the approval requests of a task |
| POST | `/tasks/{task}/approvals` | `task-approvals-create` | write on task (no link shares) | Request an approval for a task |

### Automation

| Method | Path | Operation ID | Permission | Description |
|---|---|---|---|---|
| GET | `/automations/catalog` | `automation-catalog` | signed-in user (read on project for project_id) | Get the automation catalog |
| GET | `/projects/{project}/automations` | `automations-list` | admin on project | List the automation rules of a project |
| GET | `/projects/{project}/automations/{rule}` | `automations-read` | admin on project | Get an automation rule |
| POST | `/projects/{project}/automations` | `automations-create` | admin on project | Create an automation rule |
| PUT | `/projects/{project}/automations/{rule}` | `automations-update` | admin on project | Update an automation rule |
| DELETE | `/projects/{project}/automations/{rule}` | `automations-delete` | admin on project | Delete an automation rule |
| POST | `/projects/{project}/automations/{rule}/test` | `automations-test` | admin on project | Test an automation rule with a task |
| POST | `/projects/{project}/automations/{rule}/run` | `automation-run-manually` | write on task | Run a manual automation rule for a task |
| GET | `/projects/{project}/manual-automations` | `automation-manual-list` | write on project | List the manual automation rules of a project |
| POST | `/projects/{project}/automations/{rule}/webhook-token` | `automation-webhook-token` | admin on project | Regenerate the webhook URL of a rule |
| GET | `/projects/{project}/automations/{rule}/runs` | `automation-runs-list` | admin on project | List the runs of an automation rule |
| GET | `/projects/{project}/automations/{rule}/runs/{run}` | `automation-runs-read` | admin on project | Get an automation run with its steps |
| POST | `/projects/{project}/automations/{rule}/runs/{run}/retry` | `automation-runs-retry` | admin on project | Retry a failed automation run |
| GET | `/projects/{project}/automation-secrets` | `automation-secrets-list` | admin on project | List the secrets of a project |
| PUT | `/projects/{project}/automation-secrets/{name}` | `automation-secrets-set` | admin on project | Create or replace a secret |
| DELETE | `/projects/{project}/automation-secrets/{name}` | `automation-secrets-delete` | admin on project | Delete a secret |
| POST | `/automation/hooks/{token}` | `automation-hook-incoming` | none, the secret token is the credential | Call an incoming webhook of an automation rule |

### Dashboards and reports

| Method | Path | Operation ID | Permission | Description |
|---|---|---|---|---|
| GET | `/dashboards` | `dashboards-list` | signed-in user (own personal and readable project dashboards) | List dashboards |
| GET | `/dashboards/{dashboard}` | `dashboards-read` | owner (personal) or read on project | Get a dashboard |
| POST | `/dashboards` | `dashboards-create` | any user (personal) or admin on project | Create a dashboard |
| PUT | `/dashboards/{dashboard}` | `dashboards-update` | owner (personal) or admin on project | Update a dashboard |
| DELETE | `/dashboards/{dashboard}` | `dashboards-delete` | owner (personal) or admin on project | Delete a dashboard |
| PUT | `/dashboards/{dashboard}/layout` | `dashboards-layout` | owner (personal) or admin on project | Move and resize widgets |
| POST | `/reports/tasks` | `reports-tasks` | signed-in user, tasks limited to what the user can read | Aggregate tasks |
| POST | `/reports/tasks/export` | `reports-tasks-export` | as reports-tasks | Export an aggregate report as CSV |
| POST | `/reports/tasks/export/rows` | `reports-tasks-export-rows` | as reports-tasks | Export the tasks behind a report as CSV |
| POST | `/reports/status-time` | `reports-status-time` | as reports-tasks | Time tasks spent in each status |
| POST | `/reports/status-time/export` | `reports-status-time-export` | as reports-tasks | Export the time in status report as CSV |
| POST | `/reports/workload` | `reports-workload` | as reports-tasks | Open tasks per assignee |
| POST | `/reports/workload/export` | `reports-workload-export` | as reports-tasks | Export the workload report as CSV |
| POST | `/reports/time-tracking` | `reports-time-tracking` | signed-in user, not for link shares | Tracked time per user, project, label or day |
| POST | `/reports/time-tracking/export` | `reports-time-tracking-export` | as reports-time-tracking | Export the time tracking report as CSV |
| GET | `/reports/tasks/options` | `reports-tasks-options` | signed-in user (read on project for project_id) | List what a task report can use |

### Forms

| Method | Path | Operation ID | Permission | Description |
|---|---|---|---|---|
| GET | `/projects/{project}/forms/options` | `forms-options` | admin on project | List what a form of the project can ask for |
| GET | `/projects/{project}/forms` | `forms-list` | admin on project | List the forms of a project |
| GET | `/projects/{project}/forms/{form}` | `forms-read` | admin on project | Get a form |
| POST | `/projects/{project}/forms` | `forms-create` | admin on project | Create a form |
| PUT | `/projects/{project}/forms/{form}` | `forms-update` | admin on project | Update a form |
| DELETE | `/projects/{project}/forms/{form}` | `forms-delete` | admin on project | Delete a form |
| POST | `/projects/{project}/forms/{form}/token` | `forms-token` | admin on project | Regenerate the token of a form |
| PUT | `/projects/{project}/forms/{form}/logo` | `forms-logo-upload` | admin on project | Upload the logo of a form |
| DELETE | `/projects/{project}/forms/{form}/logo` | `forms-logo-delete` | admin on project | Remove the logo of a form |
| GET | `/projects/{project}/forms/{form}/submissions` | `form-submissions-list` | admin on project | List the submissions of a form |
| GET | `/forms/{token}` | `form-public-read` | none for public forms, else per access mode | Get the public definition of a form |
| GET | `/forms/{token}/logo` | `form-public-logo` | none, the token is the credential | Get the logo of a form |
| POST | `/forms/{token}/submissions` | `form-public-submit` | none for public forms, else per access mode | Submit a form |
| POST | `/forms/{token}/submissions/multipart` | `form-public-submit-multipart` | none for public forms, else per access mode | Submit a form with files |

## Examples

The examples use `$BASE` for `https://tasks.example.com/api/v2` and `$TOKEN` for a user JWT or API token. Request bodies are taken from the web tests in `pkg/webtests`; response bodies are abridged (`...` stands for omitted members, and ids and timestamps are illustrative) unless stated otherwise.

### Log in

```bash
curl -sS -X POST "$BASE/login" \
  -H 'Content-Type: application/json' \
  -d '{"username": "alice", "password": "secret"}'
```

```json
{"token": "eyJhbGciOi..."}
```

### Create a custom field and set a value

```bash
curl -sS -X POST "$BASE/projects/1/custom-fields" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Severity","type":"select","required":true,"options":[{"title":"Minor","color":"#00ff00"},{"title":"Major"}]}'
```

```json
{
  "id": 6, "project_id": 1, "title": "Severity", "type": "select", "required": true,
  "options": [
    {"id": 11, "custom_field_id": 6, "title": "Minor", "color": "#00ff00", "...": "..."},
    {"id": 12, "custom_field_id": 6, "title": "Major", "...": "..."}
  ],
  "created_by": {"id": 1, "username": "alice"},
  "created": "2026-11-24T10:00:00Z", "updated": "2026-11-24T10:00:00Z"
}
```

```bash
# the shape of "value" depends on the type: an option id for select
curl -sS -X PUT "$BASE/tasks/1/custom-field-values/6" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value": 11}'
```

### Filter tasks by status category and a custom field

```bash
curl -sS -G "$BASE/projects/1/tasks" -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'filter=status_category != done && cf_6 in 11,12' \
  --data-urlencode 'sort_by=status' --data-urlencode 'order_by=asc' \
  --data-urlencode 'expand=status' --data-urlencode 'expand=custom_fields'
```

### Replace a workflow

Negative ids are temporary ids that the transitions of the same document can refer to.

```bash
curl -sS -X PUT "$BASE/projects/1/workflow" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "restrict_transitions": true,
  "statuses": [
    {"id": -1, "title": "Backlog",  "category": "open", "is_initial": true},
    {"id": -2, "title": "Working",  "category": "in_progress"},
    {"id": -3, "title": "Finished", "category": "done"}
  ],
  "transitions": [
    {"from_status_id": -1, "to_status_id": -2, "title": "Start", "required_fields": ["due_date", "cf_2"], "require_comment": true},
    {"from_status_id": -2, "to_status_id": -3, "min_permission": "admin"}
  ]
}'
```

### Change a task's status when the transition needs approval

`POST /tasks/{task}/status` answers `202` and the pending request, instead of `200` and the new status. This body is a response recorded by the approvals tests.

```bash
curl -sS -X POST "$BASE/tasks/20/status" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"to_status_id": 5, "comment": "No longer needed"}'
```

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
    "approvers": [{"id": 1, "request_id": 1, "user_id": 2, "decision": null}],
    "...": "..."
  }
}
```

The approver then decides:

```bash
curl -sS -X POST "$BASE/approvals/1/decision" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"decision": "approved", "comment": "Fine"}'
```

### Create an automation rule

```bash
curl -sS -X POST "$BASE/projects/1/automations" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "title": "Urgent first",
  "enabled": true,
  "trigger": {"type": "task.created"},
  "conditions": {"field": "title", "op": "like", "value": "urgent"},
  "actions": [{"type": "task.set_field", "params": {"field": "priority", "value": 5}}]
}'
```

The answer is `201` with the rule, including `revision`, `owner_id` (you), `enabled` and `last_run_status`. For a rule with the `webhook.incoming` trigger it also contains `webhook_token` and `webhook_url`, **only in this response** (the server stores a hash); `has_webhook` stays true afterwards. An invalid rule is `400` with code `22002` and `details.issues`, a list of `{path, message}` such as `actions[1].params.status_id`.

### Call an incoming webhook

```bash
body='{"id": 77, "customer": {"name": "Ada"}}'
sig="sha256=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/^.* //')"
curl -sS -X POST "$BASE/automation/hooks/$WEBHOOK_TOKEN" \
  -H 'Content-Type: application/json' -H "X-Vikunja-Signature: $sig" -d "$body"
```

```json
{"queued": true}
```

Without a secret on the rule, omit the signature header. A rule action can then use `{{event.payload.customer.name}}`.

### Run a report

```bash
curl -sS -X POST "$BASE/reports/tasks" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"project_id":1,"group_by":"done","time":{"field":"created","bucket":"month","from":"2018-11-01","to":"2018-12-31","timezone":"Europe/Istanbul"}}'
```

This response was recorded on the test fixtures:

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

### Read and submit a public form

```bash
curl -sS "$BASE/forms/$FORM_TOKEN"

curl -sS -X POST "$BASE/forms/$FORM_TOKEN/submissions" \
  -H 'Content-Type: application/json' \
  -d '{"values":{"title":"Hello","cf_2":1},"submitter":{},"honeypot":""}'
```

A successful submission answers `{"message": "Thanks", "redirect_url": "", "submit_another": true}` (the message is the form's success message); `task_id` is added only for signed-in submitters who can read the project, never for public ones. Invalid answers are `422` with code `25006`:

```json
{"status": 422, "code": 25006, "detail": "Some answers are not valid.",
 "details": {"errors": [{"field": "cf_12", "code": "invalid", "message": "option does not belong to this field"},
                        {"field": "title", "code": "required", "message": "This field is required."}]}}
```

## The generated frontend client

The web client does not hand-write API calls. It uses a TypeScript client generated from the OpenAPI document of the Go code with `@hey-api/openapi-ts`, in `frontend/src/client/generated`, wrapped by query modules in `frontend/src/client/queries` (TanStack Query). The rules:

- After adding or changing a v2 route or schema, run **`mage generate:frontend-client`** and commit the regenerated output. It builds the canonical OpenAPI document from the registered operations (`apiv2.NewCanonicalAPI`), writes it to a temporary file, and runs `pnpm run generate:api-client` in `frontend/`.
- **`mage check:frontend-client`** regenerates twice and fails if the generated directory differs between the two runs (generation is not repeatable) or from what is committed (`frontend/src/client/generated` has uncommitted changes). CI uses it to keep the client current.
- Never edit the generated files by hand. Use the snake_case fields directly.
- Feature code goes through the query modules (`customFields.ts`, `workflow.ts`, `approvals.ts`, `automation.ts`, `automationSecrets.ts`, `dashboards.ts`, `reports.ts`, `reportWidgets.ts`, `forms.ts`); importing `axios` or the legacy `models`, `modelTypes` or `services` paths is a lint error. Failed requests throw an `ApiError` that carries `status`, `detail` and `code`.

You can use the same OpenAPI document to generate a client in another language.

## MCP

The server exposes an MCP endpoint (a streamable HTTP endpoint at `/api/v2/mcp`; `GET /api/v2/mcp/info` returns its connection settings and the token permissions usable by exposed tools). Which operations become tools is decided by an allow-list in `pkg/modules/mcp/exposure.go`; anything not listed stays off MCP.

**None of the fork's operations is on that allow-list**: custom fields, workflows, approvals, automation, dashboards, reports and forms are not available over MCP. The roadmap lists this as open. To expose an operation, add its operation ID to the allow-list as a typed tool or a catalog action.
