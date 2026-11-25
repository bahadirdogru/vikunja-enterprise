# Getting started

vikunja-enterprise is one container image: the API, the web app and the background workers run in one process on port `3456`. You need Docker (or any OCI runtime), a place to keep two directories, and, if you do not want SQLite, a database server.

> [!IMPORTANT]
> This is a development fork. Read the [roadmap and status](../../docs/enterprise/ROADMAP.md) before you rely on it for anything that matters, and take a backup before every upgrade. It is not an official Vikunja product; see [the relationship to upstream](upstream.md).

{{include README.md | Quick start with Docker}}

## Docker Compose

The same setup as a Compose file, with PostgreSQL. It is an example assembled from the settings above, not a file the project ships or tests: change the host name, the passwords and the secret, and check the image tag.

```yaml
services:
  vikunja:
    image: ghcr.io/bahadirdogru/vikunja-enterprise:main
    ports:
      - "3456:3456"
    environment:
      VIKUNJA_SERVICE_PUBLICURL: https://tasks.example.com/
      VIKUNJA_SERVICE_SECRET: change-me-to-a-long-random-string
      VIKUNJA_DATABASE_TYPE: postgres
      VIKUNJA_DATABASE_HOST: db
      VIKUNJA_DATABASE_USER: vikunja
      VIKUNJA_DATABASE_PASSWORD: change-me
      VIKUNJA_DATABASE_DATABASE: vikunja
    volumes:
      - ./files:/app/vikunja/files
    depends_on:
      - db
    restart: unless-stopped
  db:
    image: postgres:18
    environment:
      POSTGRES_USER: vikunja
      POSTGRES_PASSWORD: change-me
      POSTGRES_DB: vikunja
    volumes:
      - ./db:/var/lib/postgresql
    restart: unless-stopped
```

The `files` directory must be writable for user `1000`, as in the `docker run` example. Create it first and change its owner.

## Choosing a database

| Database | Status in this fork |
|---|---|
| SQLite | The default and the simplest choice for a small group. The backend test suites pass on it. |
| PostgreSQL | Tested on 18 and 19; 18 or newer is recommended. |
| MySQL, MariaDB | Compatible by design, not guaranteed. The fork's code follows upstream's rules for portable SQL, and its suites passed on MySQL 8 until November 2026, but they are no longer run there. Use it at your own risk and test before upgrading. |
| Older versions | Upstream documents lower minimum versions. The code is written for them, but they are not tested here. |

Which suites were run on which database, and when, is recorded in the [roadmap](../../docs/enterprise/ROADMAP.md); that page is the source of truth, not this table. Raw SQL and migrations in the fork follow upstream's [supported databases](../../.agents/docs/databases.md).

Pick the database once. Moving an existing instance between engines is not something the fork provides or has tested.

## Behind a reverse proxy

Put TLS in front of the container with any reverse proxy and forward everything to port `3456`. Three settings matter:

- `service.publicurl` must be the address people really use, with the scheme. Links in e-mails and the addresses of incoming webhooks are built from it.
- `service.ipextractionmethod` and `service.trustedproxies` decide which client address the server sees. Public forms and incoming webhooks have per-IP rate limits, so a wrong value either blocks everybody behind one address or lets a caller choose their own.
- Forward the `Host` header and, for `xff`, `X-Forwarded-For`.

A minimal nginx location, as an example for you to adapt:

```nginx
location / {
    proxy_pass http://127.0.0.1:3456;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 20m;
}
```

Run the container with `VIKUNJA_SERVICE_IPEXTRACTIONMETHOD=xff` and `VIKUNJA_SERVICE_TRUSTEDPROXIES` set to the address range of your proxy. Upstream has more reverse proxy examples in its [installation documentation](https://vikunja.io/docs/installing/) (upstream's page, not this fork's).

## Upgrading

1. Take a backup (below).
2. Pull the image tag you run. The tag `main` moves with every push to `main`; to stay on a known build, use the `sha-<short>` tag of a commit instead.
3. Recreate the container. The database schema is migrated when the process starts, as in upstream, so the first start after an upgrade can take longer.
4. Check the log and open the app.

There is no downgrade path. If an upgrade goes wrong, restore the backup and start the old image. The fork merges upstream's `main` from time to time with no promised schedule; see the [roadmap](../../docs/enterprise/ROADMAP.md) for what has been verified.

## Backups

Back up three things together, and keep the secret with them:

- **The database.** For SQLite, stop the container and copy the file (or use `sqlite3 vikunja.db ".backup backup.db"` on a running instance). For PostgreSQL use `pg_dump`.
- **The files directory** (`/app/vikunja/files`), which holds attachments.
- **Your settings**, in particular `service.secret`. It signs login tokens, and the encrypted project secrets of [automation](../../docs/modules/automation.md) rules are encrypted with a key derived from it. A restored database with a different secret cannot read them.

Test the restore at least once on a spare machine.

## Where to go next

- [Configuration](configuration.md): the fork's own keys and their defaults.
- [Modules](../../docs/modules/README.md): what each module does and how to set it up.
- [API reference](../../API.md) and [architecture](../../ARCHITECTURE.md).
- For anything generic (mail, authentication providers, CalDAV, the user guide) upstream's [documentation](https://vikunja.io/docs/) applies. It is upstream's, written for upstream's releases.
