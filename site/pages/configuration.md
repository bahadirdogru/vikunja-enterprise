# Configuration

Every setting works as in upstream Vikunja: it comes from `config.yml` or from an environment variable, and the environment variable wins. This page covers what the fork adds. For the generic settings (database, mail, authentication, CORS, rate limits and so on) see upstream's [configuration options](https://vikunja.io/docs/config-options/); they are upstream's documentation and describe upstream's releases.

The complete, current list of keys with their comments is [`config-raw.json`](../../config-raw.json) in the repository. `config.yml.sample` is generated from it, so the JSON is the file to read.

## Settings you should always look at

These are upstream keys, but a working install of the fork depends on them:

| Key | Why it matters |
|---|---|
| `service.publicurl` | The address people use. Links in e-mails and incoming webhook addresses are built from it. |
| `service.secret` | Signs tokens. If it is unset, a random one is created at every start and all tokens become invalid on restart. Encrypted project secrets of automation rules depend on it as well. |
| `service.ipextractionmethod`, `service.trustedproxies` | Which client address the server uses. The per-IP limits of public forms and webhooks rely on it. See [behind a reverse proxy](getting-started.md#behind-a-reverse-proxy). |
| `database.type` and the other `database.*` keys | SQLite by default; MySQL and PostgreSQL are supported. See [choosing a database](getting-started.md#choosing-a-database). |
| `files.basepath` | Where attachments are stored. The image sets the root path to `/app/vikunja/`. |

{{include docs/modules/README.md | Configuration keys at a glance}}

## No license key

Upstream gates a few optional features behind `license.key`. In this fork nothing reads the key, so the setting has no effect even though it is still listed in `config-raw.json`.
