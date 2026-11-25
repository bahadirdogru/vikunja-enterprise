---
name: fork-delegate
description: Hand a piece of work on this fork to another agent and take its result back safely — what the brief must contain, what must not run in parallel, and how to check the hand-back before merging. Use when splitting work among agents, writing a brief for one, or receiving an agent's report on a branch.
---

# Delegating work on the fork

Each rule below is here because its absence cost something in this repository.

## The brief

An agent knows only what the brief says. Put in it:

- **Where:** the worktree path and branch; "do not touch the main checkout, do not push, do not merge"; where scratch files and logs go (outside the repository).
- **What to read first:** `AGENTS.md`, `.agents/docs/fork.md`, the module page in `docs/modules/`, `API.md`, and the skills that apply (`fork-change`, `fork-i18n`, `fork-db-matrix`, `fork-e2e`).
- **The goal and the documented behaviour** the tests must assert. Not your hypothesis about the cause: an agent handed a conclusion returns it confirmed.
- **What a product bug means:** small and within the documented behaviour, fix it in its own commit with a test; otherwise do not fix, write the reproduction.
- **The fork's rules, spelled out**, because they contradict upstream's `AGENTS.md`: never `lint:fix`; `en.json` and `tr-TR.json` in the same commit; Conventional Commits with the repository's identity as the only author and no trailers of any kind; new routes on `/api/v2`; generated files are regenerated, never edited; upstream-owned files touched as little as possible and listed in the report.
- **Machine facts:** `LC_ALL=C` for Go and mage; `GOFLAGS=-count=1` when a database is set through the environment; `pnpm` through `npx pnpm`; `TMPDIR` and `GOTMPDIR` under `/var/tmp` when `/tmp` is a small tmpfs; which containers exist and that no container may be started, stopped or removed unless the brief says so.
- **Verification to perform and to report with numbers and log paths**, on the final tree, and a bite check: break the code, see the test fail, restore.
- **The shape of the report:** what was done, bugs found, what was not covered and why, every upstream-owned file touched, what was not verified.
- **Priority order and what to do when the budget runs out:** finish and verify what exists instead of leaving half-written work.

## What must not overlap

- Two test suites on one machine: the script-process tests are timing-sensitive, and two runs against one database overwrite each other's fixtures. One agent at a time may run Go suites or e2e.
- Two agents in one worktree, or two branches that edit the same package.
- Two agents adding strings: both language files are single JSON documents. Two agents once added the same keys with different texts; the conflict could only be resolved by reading the code to see which text was true.

Work that only reads (a review) can run beside anything.

## Taking the result back

An agent's report is a claim.

1. Read the commits: authors, trailers (`git log --format=%B <range> | grep -ciE 'co-authored-by|generated with'` must be 0), and the file list (`git diff --stat`). A dependency cache (`node_modules/`), a `.pyc`, a coverage file or a report directory has been committed by mistake more than once; remove it from the branch's history before merging, not with a follow-up commit.
2. Read every change to product code yourself, line by line. Changes to tests that reverse what an older test asserted are a decision for the owner: report them as such.
3. Rebase onto the current `main`. Resolve a conflict in a language file per key, from what the code does.
4. Run the verification yourself on the rebased tree (`fork-change`, step 6). Results the agent reported for another tree do not count.
5. Permissions, limits, concurrency, signatures, untrusted input: independent review before merging (`fork-change`, step 5).
6. Fast-forward `main`, push, and bring the worktree's branch up to `main` for the next task.

## Small things that went wrong

- `pkill -f <pattern>` also matches the shell that runs it, when the pattern appears in its own command line. Kill by process id.
- A killed `mage test:*` leaves `go test` and the test binary running; look for them before starting the next suite.
- Stale `go-build*` directories fill a tmpfs `/tmp` until a run dies with "no space left on device". Delete them only while no Go process is running, and only with the owner's consent on a shared machine.
- If an agent reports that it was refused permission for an action, do not perform the action for it; bring it to the owner.
