# vikunja-enterprise

vikunja-enterprise is an independent community fork of [Vikunja](https://github.com/go-vikunja/vikunja), the self-hosted to-do and project app. It keeps upstream's Go API, Vue 3 frontend and single-binary deployment, and adds custom fields, workflows, approvals, automation, dashboards and reports, and forms.

[![License: AGPL-3.0-or-later](https://img.shields.io/badge/License-AGPL--3.0--or--later-blue.svg)](LICENSE)

> [!IMPORTANT]
> **This is not an official Vikunja product.** It is not affiliated with, endorsed by or supported by the Vikunja project or its maintainers. Do not ask them for help with this fork.
>
> Almost everything here is their work. If Vikunja is useful to you, please [support the upstream project](https://vikunja.io/support/).

- Website: <https://vikunja.bahadirdogru.com>
- Fork: <https://github.com/bahadirdogru/vikunja-enterprise>
- Upstream: <https://github.com/go-vikunja/vikunja>
- Image: `ghcr.io/bahadirdogru/vikunja-enterprise` (linux/amd64 and linux/arm64)

## Why this fork exists

The aim of this fork is to offer a copy of Vikunja that is complete and properly built in every respect up to enterprise level, stays true to the spirit of the original Vikunja, and is accessible to everyone.

That is an aim, not a claim. The fork is a development project and has not reached it. [Status](#status) says where it stands and defers to the [roadmap](docs/enterprise/ROADMAP.md) for the details. Please do not read "enterprise" in the name as "production-ready".

"True to the spirit of Vikunja" means, in practice:

- **A small difference to upstream.** New features live in their own packages, files and database tables. Upstream files are touched as little as possible so that upstream changes can still be merged. See [ARCHITECTURE.md](ARCHITECTURE.md#repository-and-package-map).
- **The same stack.** Go and xorm on the backend, Vue 3 and TypeScript on the frontend, SQLite, MySQL/MariaDB or PostgreSQL. No new service is required.
- **The same permission model.** Everything the fork adds is gated by the existing project rights (read, write, admin), link shares and API token scopes. Nothing runs with more rights than the person who set it up has. See [ARCHITECTURE.md](ARCHITECTURE.md#permission-model).
- **Self-hosted, one binary.** One image, one process, the API and the web app on port `3456`. Background work uses a database outbox inside the same process, not a queue server or a worker container.
- **AGPL.** The code is licensed [AGPL-3.0-or-later](LICENSE), as upstream.

## What it adds

Each module has a reference page in [docs/modules/](docs/modules/README.md). All new API routes are on `/api/v2`; `/api/v1` is frozen. See [API.md](API.md).

- **[Custom fields](docs/modules/custom-fields.md).** Per-project fields on tasks in thirteen types (text, long text, number, currency, date, checkbox, select, multi select, user, url, email, rating, progress). They can be filtered and sorted in the API as `cf_<id>`, typed by name in the filter input, shown as table columns and on kanban cards, and required on workflow transitions.
- **[Workflows](docs/modules/workflows.md).** Real task statuses, independent of kanban buckets: a category per status (open, in progress, done, cancelled), optional restriction to listed transitions, per-transition permission, team, required-field and comment rules, status history, and a kanban mode whose columns are statuses.
- **[Approvals](docs/modules/approvals.md).** A transition can require approval by named people, teams, assignees or a user field, in mode "any" or "all", with a deadline and a status after rejection. Ad-hoc approvals work on any task.
- **[Automation](docs/modules/automation.md).** When / If / Then rules with 19 triggers (task events, status and field changes, approvals, form submissions, schedule, overdue, due soon, manual, incoming webhook), nested conditions, 19 actions, sandboxed JavaScript, signed and SSRF-guarded HTTP requests, encrypted project secrets, a dry run and a run history. Rules run as their owner and are bounded by configurable limits.
- **[Dashboards and reports](docs/modules/dashboards-reports.md).** A task aggregation API and dashboards of widgets over task filters. Every number is computed over the tasks the viewer may read. Hand-written SVG charts, drill-down to the tasks behind a value, CSV export.
- **[Forms](docs/modules/forms.md).** A form builder whose submissions become tasks, for project members, signed-in users or the public (a standalone page, no login), with branding, rate limits, a honeypot and size limits.

## "All features enabled without a license key"

Upstream gates a few optional features (`admin_panel`, `time_tracking`, `audit_logs`, `user_invites`) behind a license key. In this fork `license.Init` in [`pkg/license`](pkg/license/) neither reads a key nor contacts a license server: `IsFeatureEnabled` is true for every known feature and `MaxUsersReached` is always false. The upstream validation code is still in the package but nothing calls it, and the `license.key` setting is still listed in `config-raw.json` without effect. That is all it means. It says nothing about the quality or completeness of those features.

## Quick start with Docker

The image is built from the repository's `Dockerfile` as one binary on a `scratch` base. The API and the web frontend are served by the same process on port `3456`, and the container runs as user `1000`.

```bash
mkdir -p vikunja/db vikunja/files
sudo chown -R 1000:1000 vikunja

docker run -d --name vikunja \
  -p 3456:3456 \
  -e VIKUNJA_SERVICE_PUBLICURL=https://tasks.example.com/ \
  -e VIKUNJA_SERVICE_SECRET=change-me-to-a-long-random-string \
  -v "$PWD/vikunja/db:/db" \
  -v "$PWD/vikunja/files:/app/vikunja/files" \
  ghcr.io/bahadirdogru/vikunja-enterprise:main
```

CI publishes the image with `.github/workflows/enterprise-docker.yml`: the tags `main` and `sha-<short>` on pushes to `main`, and `<tag>` plus `latest` for tags that start with `e`.

- **Database.** Without further settings the image uses SQLite at `/db/vikunja.db` (`VIKUNJA_DATABASE_PATH`), so mount `/db` and make it writable for user `1000`. For MySQL or PostgreSQL set `VIKUNJA_DATABASE_TYPE` and the connection settings.
- **Files.** Attachments go to `files` (`files.basepath`) relative to the root path, which the image sets to `/app/vikunja/`.
- **Secret.** Set `service.secret`. It signs tokens, and if it is unset a random one is generated at every start, which invalidates all issued tokens on each restart. Project secrets of automation rules are encrypted with it as well, so changing it makes them unreadable until they are set again.
- **Public URL.** Set `service.publicurl` to the address people really use. Incoming webhook addresses and e-mail links are built from it. Public forms are served from `/forms/<token>` on the same origin.
- **Reverse proxy.** Put TLS in front, and check `service.ipextractionmethod` and `service.trustedproxies`: the per-IP limits of public forms and webhooks use the address the router determines.
- **arm64.** The arm64 image is built in CI but has not been started on real ARM hardware yet.

Settings come from `config.yml` or `VIKUNJA_*` environment variables. The fork's own keys, their defaults and their meaning are listed per module in [docs/modules/](docs/modules/README.md#configuration-keys-at-a-glance); the full list is [`config-raw.json`](config-raw.json). For everything generic (mail, authentication, reverse proxy examples, upgrades, backups) the [upstream installation docs](https://vikunja.io/docs/installing/) apply.

## Documentation

| Document | What is in it |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | How the system is built and why: deployment, packages, request lifecycle, data model, permissions, the automation engine, database portability, frontend, and the architecture decisions with their trade-offs. |
| [DESIGN.md](DESIGN.md) | UI and visual design decisions: the patterns the fork inherits from Vikunja, and the screen structure of each feature. |
| [API.md](API.md) | The API: versions, authentication, envelopes, filter syntax, errors, rate limits, public endpoints, and every endpoint the fork adds. |
| [docs/modules/](docs/modules/README.md) | One reference page per module for operators and contributors. |
| [Custom fields](docs/modules/custom-fields.md), [Workflows](docs/modules/workflows.md), [Approvals](docs/modules/approvals.md), [Automation](docs/modules/automation.md), [Dashboards and reports](docs/modules/dashboards-reports.md), [Forms](docs/modules/forms.md) | The module pages. |
| [docs/enterprise/ROADMAP.md](docs/enterprise/ROADMAP.md) | Milestones, the log, and what is open. The single source of truth for status. |
| [docs/enterprise/](docs/enterprise/) | The original design documents and their history. |
| [Website](https://vikunja.bahadirdogru.com) | The project site. |
| [.agents/docs/](.agents/docs/api.md) | Conventions for contributors: API design, testing, code style, translations, supported databases. |

## Status

This is a development fork. All six modules are implemented in the backend and the web client, but that is not the same as finished or proven. Which suites have been run on which database, which parts of the interface were checked in a browser, and what is still open are all recorded in the [roadmap](docs/enterprise/ROADMAP.md), which is the single source of truth and is updated more often than this file. Read it before relying on the fork. Organisation / multi-tenant hierarchy and AI features are deferred.

The fork's endpoints are not exposed over MCP, and the incoming webhook signature has no replay protection; both are documented in [API.md](API.md).

## Relationship to upstream

- The fork was created from upstream `main` (v2.8.0-187). The repository has an `upstream` remote next to `origin`, and the aim is to keep the difference small so that upstream can be merged. There is no promised sync schedule.
- `/api/v1` is frozen and only gets bug fixes and ports to v2. Everything the fork adds is on `/api/v2`.
- Upstream's documentation (installation, configuration, user guide, webhooks, CalDAV) applies to everything these pages do not cover.
- Upstream's GitHub workflows inherited by the fork are disabled; only `enterprise-docker.yml` builds the image.
- Changes that belong in Vikunja itself are better sent upstream.

## Security reports

[SECURITY.md](SECURITY.md) is upstream's policy and points at upstream's advisories and contact page. That is right for problems that also exist in upstream Vikunja. For issues in the fork's own code (custom fields, workflows, approvals, forms, automation, dashboards and reports), use the [security advisories of this repository](https://github.com/bahadirdogru/vikunja-enterprise/security/advisories) if private reporting is available there; otherwise open an [issue](https://github.com/bahadirdogru/vikunja-enterprise/issues) that says you have a security report, without exploit details. Please do not send fork-specific problems to the upstream maintainers. The trust model and the list of non-issues in SECURITY.md apply here too.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) is inherited from upstream and its development parts apply. In short:

- Use [Conventional Commits](https://www.conventionalcommits.org/).
- Run the linters before committing, and run tests through `mage test:*` (plain `go test` does not work). Permission paths need a positive and a negative test.
- Every new API route goes on `/api/v2`, and the frontend reaches the API only through the generated client. See [API.md](API.md#the-generated-frontend-client).
- New user-facing strings go into `frontend/src/i18n/lang/en.json` or `pkg/i18n/lang/en.json`.
- Open an issue before starting a larger feature.

The development commands and conventions are in [.agents/docs/](.agents/docs/dev-commands.md) and in [ARCHITECTURE.md](ARCHITECTURE.md).

## License

Most of this repository is licensed under [AGPL-3.0-or-later](LICENSE), as upstream. The contents of [`desktop/`](desktop/) are licensed under [GPL-3.0-or-later](desktop/LICENSE).

### Unsplash images

Background images from Unsplash are distributed under the [Unsplash License](https://unsplash.com/license). The license requires giving credit to the photographer and Unsplash. See [Unsplash's terms](https://unsplash.com/terms) for more information.
