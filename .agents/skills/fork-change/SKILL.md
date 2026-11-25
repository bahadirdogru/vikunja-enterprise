---
name: fork-change
description: The path a change takes into `main` in this fork — worktree, failing test first, independent review, fixes, verification on SQLite and PostgreSQL, fast-forward. Use when implementing a feature or fix in the fork's modules, when delegating such work to another agent, or when deciding whether a branch is ready to merge.
---

# Getting a change into main

This is the process that worked while the fork's modules were built. Each step exists because skipping it let a real bug through.

## 1. Work in a worktree, on a branch

Never on `main` directly. One concern per branch. Two pieces of work that touch the same package run one after the other, not in parallel; work that also needs the PostgreSQL test database does not overlap with other work that needs it.

## 2. Failing test first

For a bug: a test that reproduces it and fails before the fix. Keep the failing output. For a concurrency bug the test must run on PostgreSQL, because SQLite has no row locks. If a test cannot fail against the old code because it uses new API, prove it bites by reverting the fix and showing the failure.

Permission paths need a positive and a negative test. A limit needs a test at the limit, one above it, and one showing that data already above the limit keeps working.

## 3. Keep the difference to upstream small

See `.agents/docs/fork.md`. Fork-owned files first; a registration line or a hook call in upstream files; list every upstream-owned file touched.

## 4. Commit as you go

One concern per commit, each commit builds, Conventional Commits, no attribution trailers. Strings in `en.json` and `tr-TR.json` in the same commit (`fork-i18n` skill). Docs in the same branch: the module page, `API.md`, `config-raw.json`, the roadmap log.

## 5. Independent review before merging

Anything that touches permissions, limits, concurrency, signatures, process boundaries or data from untrusted senders gets a review by a reviewer who has not seen the author's reasoning.

- Give the reviewer the diff range and the threat model, not the author's conclusions. A reviewer handed a conclusion tends to confirm it.
- Ask for evidence: file and line, and whether the finding was reproduced by running something or inferred by reading.
- Ask for a verdict in three classes: must fix before merge, should fix, nit.
- The review is read-only. Fixes go back to the author.
- After fixes, review the delta again when the fix was more than mechanical. A fix to a security finding has introduced a new variant of the same hole here more than once; stop when a review of the last delta finds nothing that blocks.

An author's report is a claim, not a result. "All fixed, tests pass" has been wrong in ways only a second pair of eyes or a run on another database found.

## 6. Verify on the final tree

After the last edit, including lint-only edits:

- `go build ./...`
- full `test:feature` and `test:web` on SQLite and PostgreSQL (`fork-db-matrix` skill)
- `mage lint` (never `lint:fix`), and for frontend changes `pnpm lint`, `vue-tsc --noEmit`, `vitest`
- i18n parity check
- for interface changes: look at them in a browser (`run-e2e-tests` skill), or say that this was not done

A test that fails once and passes on re-run is a finding. Find the cause before merging.

## 7. Merge and push

Rebase the branch onto the current `main`, fast-forward `main`, push. Check before pushing that every commit has the repository's identity as its only author and no trailers:

```bash
git log --format='%an | %s' origin/main..HEAD
git log --format=%B origin/main..HEAD | grep -ciE 'co-authored-by|generated with'   # must be 0
```

## 8. Say what is true

In the roadmap and in the report: what changed, what was verified and on which databases, and what was not verified. A documented limit ("two projects together can still hold both slots") is better than a claim the code does not keep. When the docs promise more than the code does, fix the docs in the same change.

## Briefing another agent

When this work is delegated, the brief states: the worktree and branch, what not to touch, the files to read first, the test commands with `LC_ALL=C GOFLAGS=-count=1`, which databases may be used, the i18n rule, the commit rule, "no push, no merge", and what the report must contain, including what was not done.
