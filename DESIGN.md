# Design

This document records the user interface and visual design decisions of vikunja-enterprise: how the fork's screens are structured, which patterns they follow, and why. It is about the web client in `frontend/`. How the system is built is in [ARCHITECTURE.md](ARCHITECTURE.md); the HTTP surface is in [API.md](API.md); behaviour and data are in the [module pages](docs/modules/README.md).

The sketches below are schematic. They show structure, not appearance, and they are not screenshots.

## Contents

- [Principles](#principles)
- [Foundations: tokens, dark mode, layout, i18n, accessibility](#foundations-tokens-dark-mode-layout-i18n-accessibility)
- [Custom fields](#custom-fields)
- [Workflows](#workflows)
- [Approvals](#approvals)
- [Automation](#automation)
- [Dashboards and reports](#dashboards-and-reports)
- [Forms](#forms)
- [Empty, loading and error states](#empty-loading-and-error-states)
- [Security-relevant UX](#security-relevant-ux)
- [Not yet verified in a browser](#not-yet-verified-in-a-browser)

## Principles

1. **Follow the patterns that already exist.** The fork inherits Vikunja's interface and adds to it rather than redesigning it. New settings go on project settings pages next to the existing ones, task properties go in the task detail's property list, new filter fields go into the existing filter input, new notifications use the existing notification list, and new dialogs use the existing modal. A person who knows Vikunja should find the new screens where they expect them.
2. **Reuse the building blocks.** Buttons (`BaseButton`), checkboxes, modal, dropdown, popup, cards, avatars, pagination and the `Loading` and `Nothing` states in `components/base` and `components/misc` are used instead of one-off versions. Styling uses the existing custom properties, not hard-coded colours (the one deliberate exception, the accent colour of a public form, is described below).
3. **The server is the authority; the client mirrors it.** Forms validate in the browser what the server will validate, for fast feedback, and then show the server's typed errors on the control they belong to. The workflow, form and automation editors are generated from, or checked against, what the server reports: the automation editor from the catalog, the report widget editor from the report options, the form builder from the form options. A new action, dimension or form target does not need a new screen.
4. **Say what is missing, offer what to do.** A refused status change lists the missing fields and opens a dialog to fill them. A refused "done" that needs an approval offers "Request approval". An empty page explains what the feature is and offers a way to start.
5. **Never lose work silently.** Editors with a draft (workflow, form builder, automation rule) ask before leaving with unsaved changes. Dashboards show whether the layout is saved, saving or failed.
6. **Secrets are shown once, destructive things are confirmed.** See [Security-relevant UX](#security-relevant-ux).

## Foundations: tokens, dark mode, layout, i18n, accessibility

**Tokens.** Colours, shadows and spacing come from CSS custom properties defined in `frontend/src/styles/custom-properties/` (`colors.scss`, `shadows.scss`), documented in `frontend/src/styles/README.md`. The base is Bulma (through `bulma-css-variables`), mapped to semantic tokens such as `--primary`, `--text`, `--text-light`, `--grey-*`, `--card-border-color` and `--site-background`. Component styles are scoped to their `.vue` file; global styles are limited to the material in `styles/`. The fork's components use logical properties (`inset-inline-start`, `block-size`) so right-to-left languages work, as in upstream. Tailwind utilities exist with a `tw` prefix and are not the main styling mechanism.

**Dark mode.** The `useColorScheme` composable reads the user's `color_schema` setting (`auto`, `light` or `dark`; `auto` follows `prefers-color-scheme`) and toggles a `dark` class on the `html` element. The grey scale is reversed under `.dark` in `colors.scss`, so every component that uses tokens is themed for free. Charts, badges and the dashboard grid use tokens (`--grey-200` for grid lines, `--text-light` for axis text) and therefore follow. Colours that come from the data (a status colour, an option colour, a form accent) are the exception; see the sections below. The standalone public form page has no logged-in user, so it follows `prefers-color-scheme` directly.

**Responsiveness.** Desktop layouts use side-by-side panels; narrow layouts stack. The dashboard grid is 12 columns and becomes one column at 768 px and below. The form builder shows its live preview beside the editor at the desktop breakpoint and below it on narrow screens. The public form page is mobile first and was checked down to 320 px width. Dialogs and tables are the existing, already responsive components.

**Internationalization.** Strings are in `frontend/src/i18n/lang/en.json` (English is the source and the fallback). The fork's strings are also translated into Turkish in `tr-TR.json`, kept in the repository; other languages are synced from Crowdin and show the fork's strings in English until they are translated. Server errors carry a numeric code and `i18n_params`, and the client maps codes to localized messages. The public form page uses `navigator.language` when nobody is logged in, like the app's own pre-login rule. Dates and numbers use the user's locale (the custom field display component formats numbers and currency by locale and dates with the app's date format).

**Accessibility, as present in the code.**

- Charts: one `aria-label` per mark, a visually hidden data table as an alternative (`ChartTable`), tooltips on hover and on focus, `aria-live` for status messages, selectable marks are `role="button"` and others `role="img"`, and transitions are gated by `prefers-reduced-motion`.
- Dashboard grid: drag by the widget header or move and resize with the keyboard (arrow keys move, Shift plus arrow keys resize).
- Public forms: a skip link, labels above inputs and help below, required fields marked with an asterisk and "(required)" for screen readers, errors tied to inputs with `aria-describedby`, an `aria-live` error summary, focus moved to the first invalid field, 44 px touch targets, a honeypot input that is `tabindex="-1"`, `autocomplete="off"` and inside an `aria-hidden` container, and `role="alert"` for failure states.
- Status and category badges carry an icon as well as a colour, so category is not conveyed by colour alone.

Accessibility has been designed in, not audited: no automated or manual audit against WCAG is recorded in the repository, and the claims above are limited to what the code does.

## Custom fields

Custom fields appear in four places.

```text
Project settings > Custom fields            Task detail                          Table view / kanban card
+----------------------------------+        +-------------------------------+    +------------------+
| [+ New field]                    |        | Status      [ In review  v ]  |    | Title | Budget   |
| 1. Budget      number   [edit]   |        | Due date    ...               |    | ...   | 1,200.00 |
| 2. Stage       select   [edit]   |        | -- custom fields ----------   |    +------------------+
| 3. Go live     date     [edit]   |        | Budget      [ 1200       ]    |    Card: label + value,
+----------------------------------+        | Stage       ( Review ) ( ... )|    a line is omitted when
  type fixed after creation                 | Rating      * * * . .         |    the card has no value
                                            +-------------------------------+
```

- **Definition editor** (project settings, project admins). Title, type, required flag, per-type configuration (min, max, precision, currency, maximum rating) and, for select and multi select, an option list with colours. The type cannot be changed after creation, so the type control is disabled when editing.
- **Value editors** (task detail) are one component per type, chosen by field type: text and long text (a long text shows a single clipped line until focused), number and currency, date, checkbox, select as chips or a select, multi select, user with avatars, url, email, rating as stars, progress as a bar. Values are validated in the client with the same ranges as the server and saved per field, so one invalid value never blocks the others.
- **Display** is a separate read-only component (`CustomFieldValueDisplay`) used in table cells and kanban cards: truncated text, locale-formatted numbers and currency, dates, a check mark, option chips in the option's colour with a text colour chosen for contrast, avatars, links, stars and a progress bar.
- **Columns and card fields are opt-in.** The Columns menu of a table view lists the project's custom fields under their own heading, off by default; a kanban view has a "Card fields" menu. The choice is stored in the browser (`localStorage`, per project or per view), not on the server, because it is a display preference. The view asks for `expand=custom_fields` only while at least one is shown.
- **Filter input shows names, not ids.** The filter input accepts and shows the field name (`Budget > 100`) and the option title (`Stage in Review`), and sends `cf_<id>` and option ids. Names with spaces or punctuation are double quoted; built-in names such as `done` and `priority` always win unless quoted; the field name is suggested after one typed character, option titles after the operator of a select field, users after the operator of a user field. Saved filters keep the id form, so renaming a field does not break them. When two fields share a name, each gets its project title appended. The rationale: people should not have to know ids, and stored data should not break on a rename.

## Workflows

The workflow editor is in project settings (project admins). Without a workflow the page says what a workflow is and offers "Enable workflow", which pre-fills To do (initial), In progress and Done in the user's language. Everything is edited as one draft and saved with one request, so partial states are never stored.

```text
Workflow editor                                          Transition rules (opens on a ticked cell)
+-----------------------------------------------+        +-----------------------------------+
| Statuses              [ ] Only listed moves   |        | Button label   [ Send to design ] |
|  o To do      open        initial   [up][dn]  |        | Min. permission ( ) write ( ) admin|
|  o In progress in progress          [up][dn]  |        | Allowed teams   [ Editors  x ]    |
|  o Done       done                  [up][dn]  |        | Required fields [ Due date ] [..] |
|  [+ Add status]                               |        | [ ] Require a comment             |
|                                               |        | [ ] Require approval  (see below) |
| Transitions: [ List | Matrix ]                |        +-----------------------------------+
|           to: To do | In prog. | Done         |
| from To do      -   |   [x]    |  [ ]         |
|   In progress  [ ]  |    -     |  [x]         |
+-----------------------------------------------+
```

- **Statuses**: colour, title, category (each category has its own icon and colour), a radio for the initial status, up and down buttons for order, and remove. Removing a status that still has tasks asks where the tasks should go and retries with that choice. Statuses with history are shown as archived instead of vanishing.
- **Transitions** can be edited as a list or as a matrix (rows are the from status, columns the to status); a ticked cell opens that transition's rules.
- **Client checks mirror the server**: exactly one initial status, at least one done status, unique titles, no self or duplicate transitions. Server errors are shown inline on the control they name.
- **Task detail.** When the project has a workflow a Status row sits at the top of the property list. Its menu lists the transitions the person may perform now, with the button label or target status; transitions with unmet requirements say what is missing. A transition that needs fields or a comment opens a dialog with editors for exactly the missing fields and a comment box, then saves the fields and posts the change. A collapsible history shows who moved the task from where to where, when, and the comment. The done button of a task in a workflow project performs the transition to the first done status (or back to the initial one) through the same path, so its rules and dialog apply.
- **Views.** A kanban view offers "Workflow statuses" next to manual and filter columns, with a one-line explanation. In that mode the column header is the status badge with a task count, a drop is a transition and snaps back if rejected, and "Add a task" in a column creates the task there (with the same dialog when the transition needs input). Bucket management controls are hidden because the columns are edited in the workflow editor. A table view gets a sortable Status column, on by default in projects with a workflow; list rows and manual kanban cards get a small status badge.
- **Filter input** accepts `status` and `status_category`, suggests status titles and the four category names, shows ids as titles and sends titles as ids.

**Why a status is not a bucket in the UI.** Status mode is an option of a kanban view, not a replacement: manual boards keep working and keep their buckets, and a status is visible wherever a task is, not only on a board.

## Approvals

```text
Task detail                                              Approvals page  (/approvals)
+---------------------------------------------+          +------------------------------------+
| Approval requested: Send to print           |          | [ Waiting for me | Requested by me | History ]
| by Ada, mode: all, due in 2 days            |          | project filter  v                  |
|   Bo      approved  "fine"       10:02      |          |  Task #12  Send to print  [Approve] [Reject]
|   Cem     waiting                           |          |  ...                               |
|   [ Approve ]  [ Reject ]  (comment needed) |          +------------------------------------+
|   [ Cancel request ]  (requester or admin)  |          sidebar entry with a count badge
+---------------------------------------------+
```

- **Where approvals are configured.** In the workflow editor, a transition has an "Approval" section: a switch, the mode (any or all, each with one line of explanation), the approver sources (a person, a team the project is shared with, the assignees of the task, a person in a user field), the status after a rejection, the duration to decide within (`12h`, `2d`, `1w`) and a self-approval switch. Transitions with an approval carry a tag in the list and an icon in the matrix.
- **Requesting.** The status menu labels such transitions "Request approval" and previews who will be asked. A successful request (HTTP `202`) is shown as "Approval requested", never as an error. "Request approval" in the task's actions menu opens the ad-hoc dialog for a decision on any task.
- **Banner on the task.** Above the property list: title, requester, mode, due (overdue marked), state, and per approver the decision, comment and time. Approvers who have not decided get approve and reject; a rejection needs a comment and the missing comment is shown at the field. The requester and project admins can cancel, with a confirmation. Resolved requests are in a collapsible section, with the cancel reason in words. A status history entry made by an approval links to that section.
- **Markers.** Cards, list rows and the table status cell show a "Waiting for approval" marker, requested through `expand=approvals` only for projects with a workflow. A status-mode drop that answers `202` leaves the card where it is and shows a toast.
- **Refused plain "done".** When a checkbox in a list row asks for done and the transition needs approval, the server answers `409` with code `24007`. The client rolls back the optimistic change (the checkbox goes back) and shows a message naming the status with the action "Request approval"; if a request is already pending the message says so and offers to open the task. The design choice is that nothing changes silently: no request is created as a side effect of a failed update.
- **Approvals page** (`/approvals`, sidebar entry with a count badge): waiting for me, requested by me, history, with a project filter, paging and approve or reject in the first tab. The badge refreshes on window focus, when an `approval.*` notification arrives over the WebSocket, and by polling every minute without a socket.

## Automation

Project settings, Automations (project admins). The page has a rule list and a Secrets section.

```text
Rule list                                                  Rule editor (When / If / Then)
+--------------------------------------------------+       +---------------------------------------------+
| [+ New rule]  starts from a template gallery     |       | Title [ ________________ ]   [x] Enabled    |
| (x) Urgent first       last run: success  12:04  |       | WHEN   [ Task status changed  v ]           |
| ( ) Weekly digest      last run: failed   Mon  ! |       |        options generated from the catalog   |
|     disabled by the system: too_many_failures    |       | IF     all of                               |
+--------------------------------------------------+       |          [ Priority  >=  4 ]  [+ condition] |
                                                           |          any of ... / not ...   [+ group]   |
Runs of a rule                                             | THEN   1. [ Set field ... ]  [up][dn][dup][x]|
+--------------------------------------------------+       |        2. [ HTTP request ... ]               |
| status  started  task   steps   [Retry]          |       |        [+ action]                           |
|  > run opens: each step, output, error, duration |       | [ Test with a task ]    [ Save ]            |
+--------------------------------------------------+       +---------------------------------------------+
```

- **Three stacked blocks: When, If, Then.** The structure mirrors the rule document (a trigger, a condition tree, an ordered list of actions) and the sentence a person would say. Nothing in the layout implies code.
- **Generated from the server catalog.** The trigger and action pickers, the options and parameters, the condition fields and the operators of each come from `GET /api/v2/automations/catalog`. One generic renderer per parameter kind (string, text, template, number, duration, enum, user, users, team, label, status, custom field, project, secret, map, code, condition) draws the control. Custom fields of the project appear as condition fields. This is why a new action or trigger needs no UI work, and why the catalog's translation keys are checked against the English and Turkish files by a unit test.
- **Conditions** are a nested builder of all, any and not groups over comparisons; operators are those valid for the field's kind; "is empty" operators hide the value input.
- **Actions** are an ordered list of cards with move, duplicate and remove (buttons; there is no drag-and-drop reordering). Text parameters accept `{{placeholders}}` through a placeholder menu; `{{secrets.NAME}}` is offered only for `http.request` header values and body.
- **Time triggers.** The schedule trigger has a cron input with presets (hourly, daily, weekly, monthly), a check in the browser that mirrors the server's parser, and a note that times are in the server's time zone. Overdue and due soon show a hint that tasks that are already overdue or inside the window when the rule is created or enabled do not fire. Wait takes a duration of at least one minute.
- **Scripts.** A code editor with line numbers, a Tab key that indents, an error marker on the line the server reports (the server compiles the script on save, the editor does not compile while typing), a collapsible and filterable API reference that inserts at the cursor, example snippets and a collapsible limits table. When scripts are switched off on the instance, the script action and condition are not offered and existing ones are shown read-only with a note.
- **Dry run.** "Test with a task" shows the plan: which conditions held, what each action would do, warnings for placeholders that do not resolve, and for scripts the "would call" lines and `log()` output in a monospace box. It tests only the saved rule, so the editor asks to save first and disables the test for a changed draft.
- **Runs.** A table of runs with a status badge that refreshes automatically while a run is `running`, and a run opens to its steps with output, error and duration. Failed runs have a Retry action. The runs list has no server-side status filter; the filter narrows the loaded page.
- **Webhook address panel.** For `webhook.incoming` the save that creates the address returns it once; the editor shows it with a copy button and the warning that it cannot be shown again, with notes on the signature header and on the missing replay protection. Later it only says the address exists and offers "Create a new address" behind a confirmation (the old address stops working; disabled while the rule has unsaved changes). The address lives only in the component that shows it: closing the editor forgets it.
- **Manual run.** A task's actions menu has "Run automation..." when the project has enabled manual rules and the person can write; a dialog lists the rules by their button text and queues the run; failures use the usual error toast with the localized code and the dialog stays open.
- **Secrets section.** Names, timestamps, an "unreadable" badge, add, replace and delete with a confirmation. A value is typed into a password input, cleared after the request, and never fetched or logged.
- **Disabled-by-system badge.** A rule disabled by the system shows a warning badge with the reason in words, so a rule that stopped is noticed.

## Dashboards and reports

```text
/dashboards/:id  (view mode)                         Widget editor (side panel)
+------------------------------------------------+   +------------------------------+
| Title    [Edit] [+ Widget]   saved / saving / !|   | Type gallery > form from the |
| +--------+ +------------------+ +-------------+|   | report options               |
| | Open   | | By status (donut)| | Created vs  ||   | Filter [ done = false     ]  |
| |  42    | |                  | | done (line) ||   | Group by [ assignee v ]      |
| | +5 vs  | +------------------+ |             ||   | Range  [ last 30 days v ]    |
| | prev.  | +------------------+ +-------------+|   |                              |
| +--------+ | Overdue (list)   |                |   | Live preview of the unsaved  |
|            +------------------+                |   | configuration                |
+------------------------------------------------+   +------------------------------+
12-column grid, one column at 768 px and below
```

- **Pages.** `/dashboards` lists personal and project dashboards, grouped, with create, rename and delete. `/dashboards/:id` shows and edits one. Each project has a Dashboards entry in its menu (`/projects/:id/dashboards`: it opens the only dashboard, lists several, and lets a project admin create the default one), and the sidebar has a Dashboards link. A new dashboard starts with a useful set of widgets: open tasks, tasks by status (or by done without a workflow), created vs completed over 30 days, open tasks by assignee and an overdue task list. The first version is a separate page rather than a new project view kind, because a new view kind touches a lot of upstream code.
- **Grid and edit mode.** A 12-column CSS grid. In edit mode a widget is moved by dragging its header and resized from its corner, or with the keyboard; the layout is compacted vertically, saved after a short delay with a visible saved, saving or failed state and a retry, and the last layout changes can be undone. A project dashboard seen by someone who is not an admin is read-only.
- **Widget editor.** A gallery of widget types first, then a form built from `GET /api/v2/reports/tasks/options` (labels from translation keys; custom fields by title), the same filter editor used in views, relative period presets, and a **live preview of the unsaved configuration**, so a configuration is seen before it is stored. Typed validation errors (`details.param`, the 23xxx codes and the filter errors) are shown on the matching control.
- **Charts are hand-written SVG** (bar including stacked and grouped, donut, line, number tile). The viewBox follows a `ResizeObserver`, ticks are "nice" numbers, and there are a tooltip, a legend, a neutral colour for the folded "other" group and a visually hidden data table. The reason for no charting library is dependency weight and control over theming and accessibility (decision 6 in [ARCHITECTURE.md](ARCHITECTURE.md#6-charts-are-hand-written-svg)). A note appears under a chart whose report is `multi_valued` (a task with several assignees counts in each group, so the sum of the groups can exceed the number of tasks).
- **Colours.** Data colours come from the API where one exists (status and option colours); otherwise they come from a hash of the group's key and label, so the same option has the same colour in every widget. Colours of the empty and folded groups are neutral. Colours that come from data are used as given in both themes.
- **Drill-down.** Clicking a bar, slice, point or number opens a panel that lists exactly the tasks behind it, by combining the widget's filter with the group and bucket. Marks that cannot be expressed in the filter syntax (the folded "other" group, the `status_entered` time field, some "no value" cases) are inert and say why, instead of showing a wrong list.
- **Export.** CSV from the widget menu and from the drill-down panel. Previous-period comparison on number widgets shows a delta against the same elapsed share of the previous period.
- **Per-viewer display options.** Bar direction, stacking and legend are stored in the browser, not in the widget, because widget configs are decoded strictly and have no room for them.

## Forms

Forms are built by project admins (project settings, Forms) and used from a standalone page.

```text
Form builder (project settings)                                   Public form page  /forms/:token
+------------------------------------+-----------------------+   +------------------------------+
| Tabs: [Fields] [Settings] [Share]  |  Live preview         |   | [logo] Brand name            |
| Fields               [+ Add field v]|  +-----------------+ |   | Description                  |
|  1. Title      label help  [req]    |  | What do you need?| |   | What do you need? *          |
|  2. Stage      label help  [ ]      |  | [______________] | |   | [______________________]     |
|  3. Budget     label help  [hide]   |  | Stage  ( ) ( )   | |   | Stage  ( ) Open  ( ) Closed  |
| per field: label, help, required,   |  | [ Submit ]       | |   | [ Submit ]  (accent colour)  |
| default, "do not show"              |  +-----------------+ |   | footer: Powered by Vikunja   |
+------------------------------------+-----------------------+   +------------------------------+
 preview sits below on narrow screens                              no app header, sidebar or login
```

- **List.** Title, an access badge (members, signed-in users, public), an open and closed switch that saves at once, the submission count, a warning badge with the translated reason when the system switched the form off, and copy link, open, edit, submissions, duplicate (the copy starts closed) and delete (with confirmation; the tasks stay). With no forms the page says what forms do and offers three starters built from what the project offers (a simple request, a request with a due date, a bug report).
- **Builder.** Three tabs: Fields, Settings and Share, with a live preview next to them (below on narrow screens). Everything comes from `GET .../forms/options`; no target list is hard-coded. Fields are added from a menu grouped into task fields and custom fields, each target once; reordered with buttons; and have a label, help, required flag, "do not show" (a hidden field takes its default, which is how a status, label or custom field is pre-set), and a typed default (custom fields reuse the custom field editors). On a public form a target that may not be asked but may be preset (labels, user fields) is added as a hidden field; one that can be neither (attachments when the instance forbids them) is disabled with the reason; switching to public lists the fields that block it. The title rule (asked and required, or hidden with a value) is explained inline together with the placeholders.
- **Settings tab.** Access, with a warning box for public; initial status (only with a workflow); default assignees; success message; redirect; "submit another"; close date; maximum submissions; who is notified; whether to ask for name and e-mail (off, optional, required; public forms only); and the branding: brand name, accent colour, hide the footer, and logo (PNG, JPEG, GIF or WebP, no SVG).
- **Share tab.** The link with a copy button, "create a new link" behind a confirmation (the old link stops working), and an iframe snippet. There is no QR code, because it would need a new dependency.
- **Validation and drafts.** Client checks mirror the server. Server issues (`400` with code `25002`, `details.issues[].path`) are mapped to the control and the tab: the tab is switched, the field opened, the control focused, and tabs show a badge with the count. Unsaved changes are guarded (route leave, tab close, back to the list). Saving keeps the builder open.
- **Public page.** A route with `meta.standalone`, rendered without header, sidebar or the login layout; it loads the definition without credentials. Fields are native inputs chosen by kind (radios or checkboxes up to six options, selects above, stars as radio inputs, range, date or date-time input, a textarea with a counter near the maximum length, a file picker that honours the limits). Dates without a time are sent as noon in the viewer's time zone. Success shows the message or a default, "submit another" resets the form, and a redirect (http or https only) shows the target host and goes after four seconds. Failure states each have their own screen: not found, closed, full or disabled (with the form title), login required (a button that returns to the form after login), forbidden, rate limited (with the retry time) and network errors.
- **Branding and accent colour.** The page shows the brand name and logo and can hide the "Powered by Vikunja" footer. The accent colour is `#rrggbb`, validated in the builder and on the server. The page cannot just change one variable, because `--primary` and its derived colours are resolved on `:root`; instead `accentStyle` converts the accent to hue, saturation and lightness and emits every derived variable already resolved (`--primary`, `--primary-light`, `--primary-dark`, `--link`, `--link-hover`) on the form container. **Contrast:** the text on buttons is chosen from the accent's relative luminance, black above a luminance of 0.4 and white otherwise (`--fp-button-text`). That guarantees readable button text; it does not check the accent against the page background, so a very light accent can give low-contrast links and outlines on a light page. An invalid or empty accent leaves the normal theme untouched.

## Empty, loading and error states

| Place | Empty | Loading | Error |
|---|---|---|---|
| Workflow editor | Explains workflows, offers "Enable workflow" | The shared loading component | A 404 from the workflow route is treated as "no workflow" and renders nothing extra |
| Task status row | Not shown without a workflow | - | Typed errors become plain messages; unmet requirements open the status dialog |
| Charts | A localized empty state | A skeleton | A per-widget error state with the server's message (each widget has its own queries) |
| Dashboards list | Offers creating the default dashboard (project admins) | Loading component | Request errors as toasts |
| Automation rule list | A short explanation and the template gallery | Loading component | Server issues mapped to the exact input by path (`actions[1].params.status_id`) |
| Form list | What forms do, three starters | Loading component | Server issues mapped to the control and the tab |
| Public form page | - | Loading state before the definition arrives | Separate screens for 404, 410 (closed, full, disabled), 401, 403, 429 and network errors; 422 answers are mapped to the inputs |
| Approvals page | An empty tab | Loading component | Request errors as toasts; a decision racing another shows the `409` message |

Failed queries toast through a global handler unless the consumer renders its own error state. A request that fails because the session or API base changed mid-request is ignored rather than shown.

## Security-relevant UX

- **Secrets are shown once.** The incoming webhook address is returned only by the request that creates or regenerates it; the editor shows it once with a copy button and a warning, and it is kept only in the component, never in a query cache (the save and the address request use `useSecretMutation`), never logged. Automation secret values are write-only: password input, cleared after the request, never fetched. Form tokens are visible to project admins (they can read forms) but can be regenerated.
- **Confirmations on destructive or link-breaking actions.** Deleting a form, a secret, a rule, a dashboard, disabling a workflow, creating a new form link or webhook address, and cancelling an approval each ask first. Leaving an editor with unsaved changes asks.
- **Honest warnings.** Choosing public access on a form shows a warning box. The webhook panel states that the signature has no replay protection. The "unreadable" badge on a secret tells the admin that `service.secret` changed.
- **Nothing is hidden by the UI alone.** Controls are hidden or disabled for people who cannot use them, but the server enforces every permission; a forbidden answer is shown as a plain message.
- **Little traffic from the public form page.** A logged-out visitor sends only the instance `/info` the app always loads, the definition, and later the submission.
- **Abuse protection stays out of the way.** The honeypot is invisible to people and assistive technology; a rate-limit answer shows the retry time.

## Not yet verified in a browser

Parts of the interface have been type-checked and unit-tested but have **not** been checked manually in a browser, as the [roadmap](docs/enterprise/ROADMAP.md) records: the form branding (brand name, accent colour, footer, logo), the newest dashboard widgets (time in status, workload, time tracking), CSV export and the previous-period comparison, and the automation editor support for the schedule, overdue, due soon, manual and incoming webhook triggers and the wait, create task and notify actions, including the manual run dialog. Earlier parts were exercised in a browser at the time they were built; the roadmap log lists what was and was not covered each time (for example, a server-side 422 on a form was only shown with a mocked response, and forbidden and team-restricted transitions were not exercised). The contrast behaviour of accent colours on real pages has not been checked, and no accessibility audit has been done. Treat the descriptions above of those newest parts as design intent confirmed by unit tests, and check the roadmap for the current state.
