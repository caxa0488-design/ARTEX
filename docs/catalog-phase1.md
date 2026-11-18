# Phase 1: knowledge catalog metadata

Base: BaaRai/ARTEX main at `d89cf781b790ec9a1d071bee7e79de489ed05252`.

This first vertical slice adds a metadata-only knowledge catalog. It does not
change the scheduler, graph, findings verification, coverage ledger, installed
skills, or agent bindings. Inspection found existing finding verification and
coverage ledgers; adding parallel result states would be incorrect.

## Actual modules and contract

- Add migration `0008_knowledge_catalog.sql` without changing published migrations
  or existing tables. Store metadata plus append-only revision snapshots.
- Add database validation, bounded list/detail/history, create, optimistic
  metadata update, and explicit human review. New entries start in `draft`.
  Metadata changes reset review to `draft`; revoked records are immutable.
  A duplicate `(kind, source_ref, source_version)` is a conflict, not an upsert.
- Register authenticated `/api/knowledge-catalog` endpoints through the existing
  JWT middleware. No new public endpoint, role system, file upload, file read,
  URL fetch, content execution, model call, database import or automatic review.
- Add a real System navigation entry and management page using the shared API
  client/components. Show review and edit conflicts, metadata-only limitations,
  and revision history. External references are displayed as text, not fetched.

References accept credential-free HTTPS URLs without query parameters or URNs.
Evidence references are declarations, not proof that evidence exists or has
been verified. SHA-256 is a declared source-content hash; no fetching occurs to
verify it. `reviewed` means metadata was explicitly reviewed by the logged-in
operator, not that a finding was confirmed or a tool/skill may execute. All users
currently have ARTEX's existing administrator privileges; this adds no RBAC.

## Tests and acceptance

- Unit validation: valid, invalid kind/status/hash/reference, limits, controls,
  duplicate evidence references, and missing review requirements.
- Isolated PostgreSQL: fresh/repeated migration, existing data preserved, CRUD,
  duplicates, revisions/stale writes, review invalidation, revocation and history.
- HTTP: all endpoints reject missing credentials; JSON type/size/unknown fields,
  ID/filter/paging validation; authenticated create/list/detail/update/review.
- Frontend: pure contract tests, TypeScript, scoped Biome, existing test scripts,
  production build where dependencies permit. No unrun item is called passed.

Remaining separate slices: skill source/version metadata and real agent-key
bindings (including explicit runtime-locking semantics), and improvements to
existing finding verification/coverage UI. This change does not claim either.
No offensive skill content, third-party engine, binary, configuration, credentials,
old database, remote-control integration or proxy pool is imported.

## Local validation results

- Targeted new Go unit, PostgreSQL, and HTTP integration tests: passed with race.
- Full `db` package with race: passed after appending migration 0008 to the
  existing explicit migration test expectations; original assertions preserved.
- Full `server` package with race: passed. Its initial combined invocation
  returned failure only because the earlier `db` migration expectations were
  still 1–7; these have since been updated and the full DB suite re-run.
- `go build ./...`: passed. Scoped Biome and `tsc --noEmit`: passed.
- Node tests: 29 passed, zero skipped, including four new catalog contract tests.
  `npm run test:catalog` is added to the existing web CI test sequence.
- Production Next build and `npm run build:static`: passed, including
  `/system/knowledge`. The temporary PostgreSQL instance has been stopped.
- No browser interaction/E2E, full-repository Go test run, remote CI result,
  deployment, source-content verification, skill binding change, or actual
  scanning/testing task is claimed.
- `npm ci` ran the existing prepare hook normally; upstream `husky` reports
  `.git can't be found` from the web subdirectory, so no local Git hook is
  installed. No hook bypass flag or disabled hook setting was used.
