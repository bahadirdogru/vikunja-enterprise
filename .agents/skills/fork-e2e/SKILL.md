---
name: fork-e2e
description: Write and run Playwright end-to-end tests for the fork's modules (custom fields, workflow, approvals, automation, dashboards, forms, Turkish walk). Use when a change alters what a user sees or does in one of those modules, when asked to test "in the browser" or "end to end", and before calling an interface change verified.
---

# End-to-end tests of the fork's modules

Unit tests did not find these; a browser did: a filter made only of a custom field condition was sent as search text, and one click on a submit button sent two requests (a `@click` on a `type="submit"` button inside a form with `@submit`). Interface changes are not verified until a browser has run them.

Upstream's `run-e2e-tests` skill still applies: only `mage test:e2e`, never `pnpm test:e2e`, output to a file.

## Running

```bash
export LC_ALL=C
export PATH=/path/to/shim:$PATH              # only if `pnpm` is not on PATH: a script `exec npx --yes pnpm "$@"`
export TMPDIR=/var/tmp/vke-e2e GOTMPDIR=/var/tmp/vke-e2e; mkdir -p "$TMPDIR"   # when /tmp is a small tmpfs
go run github.com/magefile/mage test:e2e \
  "tests/e2e/custom-fields tests/e2e/workflow tests/e2e/approvals tests/e2e/automation tests/e2e/dashboards tests/e2e/forms tests/e2e/i18n" \
  > "$out/e2e.log" 2>&1
grep -E '^\s+[0-9]+ (passed|failed|flaky|skipped)' "$out/e2e.log"
```

- The fork's specs take about five minutes together. `VIKUNJA_E2E_SKIP_BUILD=true` skips the API build when only specs or the frontend changed.
- The run uses its own SQLite and its own server. It needs no container.
- Never at the same time as a Go suite or a second e2e run.
- Remove the directory under `/var/tmp` afterwards. Reports and traces in `frontend/test-results` and `frontend/playwright-report` are ignored by git; never commit them.
- The full upstream suite takes thirty minutes. On a machine without a mail server and an OpenID provider about thirteen upstream specs fail (admin panel, invite links, OpenID login, registration mail, e-mail and linked-account settings, API token scopes, unlicensed time tracking). Before blaming a change, run the same specs on the unchanged tree.

## Writing

- New specs go into the fork's directories under `frontend/tests/e2e/`; shared helpers into `frontend/tests/support/forkModules.ts` and new factories into new files. Do not edit upstream specs, helpers or the Playwright config.
- Set up through the API and the factories, act through the interface, assert in the interface and, where it is cheap, through the API (the stored document, the number of requests sent).
- Create projects through the real API when the test needs default views; a factory row has none.
- Locate by role and label; add `data-test` to a fork component when that is not possible.
- No `waitForTimeout`. Wait for a condition.
- A second user needs their own browser context; so does a signed-out visitor of a public form.
- Kanban drags: move the mouse in steps and use a viewport wide enough for all columns (1920 for four); see `workflow/workflow-kanban.spec.ts`.
- Count requests when a duplicate would be a bug (`page.on('request')`), and assert that no error toast (`.vue-notification`) appears where the page explains the state itself.
- Turkish: set the user's language through the API, use Turkish test data, assert the Turkish text, the absence of the English text and of raw keys (a visible text like `a.b.c`). The public form page uses the formal register, the application the informal one.

## What cannot be tested here

- An outgoing HTTP request that succeeds: the address guard is on and there is no network. Test building, dry run, the refusal and the retry.
- A schedule trigger firing: the ticker needs minutes.
- File answers on a public form while `forms.public.attachments` is off; test the upload on a `users` form.

Say so in the report instead of weakening the server's configuration for the test.

## Before reporting

- Every new spec passes alone, and all fork specs pass together twice in a row on the final tree. A test that fails once is a finding.
- One bite check per new area: break the feature, see the test fail, restore with `git checkout`.
- A failing test may be a product bug. Confirm it by hand in the trace, fix it in its own `fix(...)` commit when it is small and within the documented behaviour, otherwise write down the steps.
- Do not claim a journey is covered when the test only checks that something renders.
