# Module reference

One reference page per module the fork adds. They are written for operators and contributors: what the module is for, its concepts and data model, who may do what, the API, the configuration keys, the UI, how it interacts with the other modules, its limits, what is still open, and where the code lives.

For the big picture see [ARCHITECTURE.md](../../ARCHITECTURE.md); for HTTP conventions, authentication, errors and the endpoint tables see [API.md](../../API.md); for screens see [DESIGN.md](../../DESIGN.md). Status and open points are tracked in [docs/enterprise/ROADMAP.md](../enterprise/ROADMAP.md), which is the single source of truth.

| Module | Page | Design document (history and rationale) |
|---|---|---|
| Custom fields | [custom-fields.md](custom-fields.md) | [docs/enterprise/custom-fields.md](../enterprise/custom-fields.md) |
| Workflows | [workflows.md](workflows.md) | [docs/enterprise/workflow.md](../enterprise/workflow.md) |
| Approvals | [approvals.md](approvals.md) | [docs/enterprise/forms-approvals.md](../enterprise/forms-approvals.md) (Approvals) |
| Automation | [automation.md](automation.md) | [docs/enterprise/automation.md](../enterprise/automation.md) |
| Dashboards and reports | [dashboards-reports.md](dashboards-reports.md) | [docs/enterprise/dashboards.md](../enterprise/dashboards.md) |
| Forms | [forms.md](forms.md) | [docs/enterprise/forms-approvals.md](../enterprise/forms-approvals.md) (Forms) |

## How the modules fit together

- **Custom fields** add typed data to tasks. Workflows, automation, forms and reports all refer to a field as `cf_<id>`.
- **Workflows** add statuses and transitions to a project. **Approvals** make a transition wait for named people.
- **Automation** reacts to events from all of the others (task changes, status changes, field changes, approvals, form submissions, time) and acts through the normal model functions as the rule's owner.
- **Forms** turn submissions into tasks through the normal create path, so workflows, custom fields and automation apply.
- **Dashboards and reports** count tasks, statuses and history, always limited to what the viewer can read.

Every page follows the same outline: Overview, Concepts, Data model, Permissions, API, Configuration keys, UI, Events and interaction with other modules, Limits and safety, Known gaps and open decisions, Where the code lives.

## Configuration keys at a glance

Settings come from `config.yml` or from environment variables; environment variables override the file. The variable name is `VIKUNJA_` plus the key in upper case with dots as underscores, so `automation.ticker.interval` is `VIKUNJA_AUTOMATION_TICKER_INTERVAL`. The full list with comments is [`config-raw.json`](../../config-raw.json). `config.yml.sample` is generated from it with `mage generate:config-yaml`; edit the JSON, not the sample.

The fork's keys, with their defaults (the meaning of each is on the module page):

| Key | Default | Module |
|---|---|---|
| `forms.enabled` | `true` | [Forms](forms.md) |
| `forms.maxbodysize` | `262144` | [Forms](forms.md) |
| `forms.maxattachments` | `5` | [Forms](forms.md) |
| `forms.maxattachmentsize` | `10MB` | [Forms](forms.md) |
| `forms.public.enabled` | `true` | [Forms](forms.md) |
| `forms.public.attachments` | `false` | [Forms](forms.md) |
| `forms.public.ratelimit` | `10` | [Forms](forms.md) |
| `forms.public.formratelimit` | `300` | [Forms](forms.md) |
| `forms.public.requestratelimit` | `60` | [Forms](forms.md) |
| `forms.users.ratelimit` | `60` | [Forms](forms.md) |
| `automation.enabled` | `true` | [Automation](automation.md) |
| `automation.maxdepth` | `3` | [Automation](automation.md) |
| `automation.maxactions` | `25` | [Automation](automation.md) |
| `automation.steptimeout` | `10s` | [Automation](automation.md) |
| `automation.runtimeout` | `60s` | [Automation](automation.md) |
| `automation.ratelimit` | `60` | [Automation](automation.md) |
| `automation.workers` | `4` | [Automation](automation.md) |
| `automation.maxattempts` | `5` | [Automation](automation.md) |
| `automation.maxconsecutivefailures` | `20` | [Automation](automation.md) |
| `automation.pollinterval` | `1s` | [Automation](automation.md) |
| `automation.cachettl` | `30s` | [Automation](automation.md) |
| `automation.retention` | `30` | [Automation](automation.md) |
| `automation.ticker.interval` | `30s` | [Automation](automation.md) |
| `automation.ticker.maxtasks` | `100` | [Automation](automation.md) |
| `automation.ticker.maxscan` | `5000` | [Automation](automation.md) |
| `automation.waitmax` | `720h` | [Automation](automation.md) |
| `automation.maxpendingwaits` | `1000` | [Automation](automation.md) |
| `automation.maxrulesperproject` | `100` | [Automation](automation.md) |
| `automation.webhook.maxbody` | `65536` | [Automation](automation.md) |
| `automation.webhook.tolerance` | `5m` | [Automation](automation.md) |
| `automation.webhook.ratelimit` | `60` | [Automation](automation.md) |
| `automation.scripts.enabled` | `true` | [Automation](automation.md) |
| `automation.scripts.timeout` | `5s` | [Automation](automation.md) |
| `automation.scripts.conditiontimeout` | `1s` | [Automation](automation.md) |
| `automation.scripts.maxsource` | `65536` | [Automation](automation.md) |
| `automation.scripts.maxapicalls` | `100` | [Automation](automation.md) |
| `automation.scripts.maxhttprequests` | `5` | [Automation](automation.md) |
| `automation.scripts.maxresponsesize` | `1048576` | [Automation](automation.md) |
| `automation.scripts.maxlogsize` | `16384` | [Automation](automation.md) |
| `automation.scripts.maxcallstack` | `512` | [Automation](automation.md) |
| `automation.scripts.maxresults` | `200` | [Automation](automation.md) |
| `automation.scripts.httptimeout` | `10s` | [Automation](automation.md) |
| `customfields.maxfieldsperproject` | `100` | [Custom fields](custom-fields.md) |
| `customfields.maxoptionsperfield` | `200` | [Custom fields](custom-fields.md) |
| `reports.cachettl` | `30s` | [Dashboards and reports](dashboards-reports.md) |
| `reports.maxgroups` | `200` | [Dashboards and reports](dashboards-reports.md) |
| `reports.maxbuckets` | `366` | [Dashboards and reports](dashboards-reports.md) |
| `reports.maxrows` | `50000` | [Dashboards and reports](dashboards-reports.md) |
| `reports.maxcells` | `50000` | [Dashboards and reports](dashboards-reports.md) |
| `reports.timeout` | `20s` | [Dashboards and reports](dashboards-reports.md) |

The modules without keys of their own (custom fields, workflows, approvals) share the instance's database and file settings. Besides these keys, `service.publicurl`, `service.secret`, `service.timezone`, `service.ipextractionmethod`, `service.trustedproxies`, `outgoingrequests.allownonroutableips` and `outgoingrequests.proxyurl` come from upstream and are used by the fork as described on the module pages.
