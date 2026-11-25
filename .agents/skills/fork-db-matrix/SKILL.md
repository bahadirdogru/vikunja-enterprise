---
name: fork-db-matrix
description: Run the Go test suites of this fork against SQLite and PostgreSQL, the two databases the fork tests and supports. Use before merging anything that touches models, migrations, raw SQL, locking or transactions, or when asked to verify a change "on all databases".
---

# Running the database matrix

The fork tests and supports SQLite and PostgreSQL. MySQL and MariaDB are compatible by design (portable SQL, identifiers of at most 64 characters) but are not tested and not guaranteed; do not spend a run on them unless asked.

A green SQLite run does not show locking problems or transaction behaviour. In this fork, real bugs were found only on a server database: a deadlock between two lock orders, test fixtures that were not reset.

## Databases

SQLite is the default and needs nothing. For PostgreSQL, start a throwaway container on a port that does not collide with anything else on the machine:

```bash
docker run -d --name vke-test-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_USER=vikunjatest -e POSTGRES_PASSWORD=vikunjatest -e POSTGRES_DB=vikunjatest \
  postgres:18
```

Use the newest stable PostgreSQL major; check whether a newer one than 18 exists. Only start, stop or remove containers you created. Never touch other containers on the machine and never run `docker system prune`.

The tests read the connection from the environment:

```bash
# PostgreSQL
export VIKUNJA_TESTS_USE_CONFIG=1
export VIKUNJA_DATABASE_TYPE=postgres
export VIKUNJA_DATABASE_HOST=127.0.0.1:55432
export VIKUNJA_DATABASE_USER=vikunjatest
export VIKUNJA_DATABASE_PASSWORD=vikunjatest
export VIKUNJA_DATABASE_DATABASE=vikunjatest
export VIKUNJA_DATABASE_SSLMODE=disable
export VIKUNJA_SERVICE_PUBLICURL=http://127.0.0.1:3456
```

Keep the set in its own file and `source` it in a subshell, so the variables never leak into the SQLite run.

## Running

```bash
export LC_ALL=C GOFLAGS=-count=1
out=/path/to/logs

run() { # name, env file (empty for SQLite)
  ( [ -n "$2" ] && . "$2"
    go run github.com/magefile/mage test:feature > "$out/$1-feature.log" 2>&1; echo "$1 feature exit=$?"
    go run github.com/magefile/mage test:web     > "$out/$1-web.log"     2>&1; echo "$1 web exit=$?" )
}
run sqlite ""; run pg env-pg.sh
grep -hE '^(FAIL|--- FAIL)' "$out"/*.log
```

- `GOFLAGS=-count=1` is mandatory. The Go test cache does not know about `VIKUNJA_DATABASE_*` and otherwise prints `(cached)` results from the previous database.
- `LC_ALL=C` is needed on machines with a Turkish locale.
- Run one suite at a time. The script-process tests in `pkg/automation/script` are timing-sensitive, and two suites against one database overwrite each other's fixtures.
- Write the output to files and read the files. Do not re-run a suite to look at it differently.
- `test:feature` should report 59 `ok` packages (count them: `grep -c '^ok'`); `test:web` reports one, `pkg/webtests`, and takes about six minutes.
- Run the matrix on the final tree, after the last edit, including lint-only edits.

## Reading failures

- **Fails on PostgreSQL but not on SQLite, in many tests of one package:** a table is missing from the fixture reset. New tables need a fixture file in `pkg/db/fixtures/` and registration in the test setup of the packages that use them.
- **A table has the wrong columns:** the test database kept a table from an earlier run of a different branch. Drop that table in your test containers and run again. This is an artifact of the local database, not a bug.
- **`40P01` on PostgreSQL:** a deadlock. Two paths take the same locks in a different order. Fix the order; do not add a retry.
- **`current transaction is aborted` on PostgreSQL:** a statement failed earlier in the same transaction and the code carried on. Do the follow-up in a new session.
- **A test passes alone and fails in the suite:** look for state shared between tests (fixtures, files in the test storage, pooled processes) before widening a timeout.
- **A test fails once in several runs:** treat it as a bug until the cause is known. Loop the package until it fails, find the cause, then show a long green run. In this fork such a failure turned out to be a real fault in failure attribution, not a slow machine.

## Reporting

Report per database: exit code of each suite and the number of `ok` packages. Say which databases and versions were not run (PostgreSQL versions other than the one started, upstream's minimum versions; MySQL and MariaDB are never run). Record the result in `docs/enterprise/ROADMAP.md`.
