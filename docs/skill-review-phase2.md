> Historical phase-2 contract. Runtime binding is now extended by `docs/skill-runtime-phase3.md`; see that document for the manifest-only pinning scope.

# Phase 2: skill version references and operator review

This PR stacks on the knowledge metadata catalog (PR 1). It adds two working
management surfaces; it does not replace task scheduling, graph state, evidence
verification, skill loading, or tool permissions. Implementation is original:
no BugHunter skill contents, deployment-package scripts, databases, credentials,
or third-party binaries are copied.

## Skill Versions (System navigation)

- The installed-skill list is combined with up to 500 registered names, so missing
  local content remains discoverable. Inspecting a name shows actual agent keys,
  existing enabled visibility, registered versions, binding revisions, current
  content checks and the latest 100 management events/versions.
- Registering requires a unique label, source reference and license statement.
  These provenance fields are operator declarations, not verified provenance or
  a legal assessment. HTTP callers cannot supply their own digest or file path.
- The server hashes the selected real skill tree: lexical directory traversal of relative paths, path
  byte lengths, file sizes and individual SHA-256 file digests are encoded as
  `length:path:size:file_digest_hex\n` and hashed with SHA-256 (tree scheme v1).
  The result is not the raw SHA-256 of SKILL.md. Every regular file participates,
  and nonempty SKILL.md is required. No file contents are stored or returned.
- Limits: 512 tree entries, 256 regular files, 16 MiB. os.Root confines resolution;
  links/special files are rejected. Unix opens are no-follow and nonblocking;
  inode/size/mtime checks detect observed changes, including a final metadata
  pass. Concurrent malicious local writers are not an atomic filesystem snapshot
  guarantee. The server must control the skill directory. Other platforms reject
  safe registration instead of falling back to an unsafe file reader.
- Binding uses the actual agent key and a revision CAS, rehashes current content,
  and requires an existing enabled visibility record. Unknown agent/version,
  changed/missing content and revoked versions cannot silently bind. Registration
  and binding never enable a skill. Clearing a binding does not remove visibility.
- A registered version is immutable except revocation reason/revision. Revocation
  is final and audited; a new version label is needed for subsequent registration.
  Bindings to older versions outside the list still get explicit content checks.
- This is **management binding, not per-run version locking**: no immutable content
  artifact is retained, no version is injected into the agent loader, and no
  running or future task is disabled automatically by revocation. Legacy visible,
  unversioned skills retain their behavior. No role field is invented.

## Review Desk (Function navigation)

Load a task to inspect the existing coverage ledger, its derived gaps, unexamined
classes, and completeness. Filter gaps by kind and operator history by work state.
Finding IDs can also be loaded directly; name/task/current human and machine states
are shown before explicit disposition. No background agent calls these endpoints.

Human decisions update the **existing** findings.status only:

| Explicit decision | Existing stored status |
| --- | --- |
| candidate | pending |
| confirmed | confirmed |
| rejected | false_positive |
| inconclusive | in_progress |

They do not modify verification_status, proof/evidence versions, POE or report_state.
All require an authenticated actor, a single-paragraph reason and a current
snapshot fingerprint; confirmation additionally needs evidence references.
References are declarations, not proof that evidence exists or has been verified.
The original machine verification pipeline remains authoritative on verification.

Coverage annotations have a separate purpose: untested / blocked / failed /
completed describe operator work against an exact normalized surface/risk key.
They never rewrite coverage_ledger outcomes, clear derived gaps, or mark assets
tested. Completed needs an existing settled ledger outcome and references, not
just absence of a finding. Coverage-disabled tasks reject annotations.

Both kinds record append-only application history with reason, references, actor,
revision, and before/result fingerprints. Updates use row locks and fingerprint
CAS; stale writes return 409. Historical annotations are labelled stale if their
result snapshot no longer matches. These tables are not tamper-proof audit storage
against a database administrator. Existing finding editors can still change the
original status; such changes invalidate old review fingerprints, not disappear.

There is no reliable per-revival execution epoch in the inherited schema. All
review exports therefore say `task_history_not_run_verified`; old ledger/history
is never relabelled current-run testing. A later execution-epoch feature is
explicitly outside this PR.

## API, report and persistence

11 routes under `/api/managed-skills` and `/api/operator` use the unchanged JWT
middleware and `Cache-Control: no-store`. Mutations use the existing strict 32 KiB
JSON decoder (unknown fields rejected). Text, states, lengths, IDs, references,
CAS, duplicate versions, enabled visibility and revoked content are server-checked.
No URL fetching, arbitrary file read, import, execution or new permission default
is exposed. This inherits the platform's current authenticated-user policy; it
is not a new multi-user RBAC or independent reviewer identity system.

The supplemental JSON report is available in Review Desk and at
`GET /api/operator/tasks/{id}/report`. It adds human history/work annotations to
the original ledger/gaps and machine-status summaries; the original reports are
untouched. It is explicitly bounded to 100 findings, 100 finding-review records
(grouped by finding), and 100 coverage annotations; not a complete archival audit.
Individual finding history is separately available (latest 100). No model provider
or report-generation task is invoked.

0009 only adds management/history tables and lookup indexes. Existing schema,
visibility settings, statuses and task state are not rewritten. Migrations remain
forward-only; apply after a normal backup under the existing migration runner.
No data-deleting downgrade or deployment is performed by this PR.

## Verification approach

Synthetic non-executable filesystem fixtures exercise hash stability, content/path
changes, unsafe names, links, special files, missing manifest and resource limits.
Fresh Unix-socket PostgreSQL databases test migration/reopen, CRUD constraints,
revocation, legacy visibility, concurrent CAS, human/machine-state separation,
ledger preservation, stale history and authenticated HTTP round-trips. Browser
contracts test separate vocabularies and snapshot-bound payloads; TypeScript,
scoped Biome, Node scripts and static Next build cover wiring/build contracts.
Actual executed commands/results and omissions are recorded in the PR; these are
not browser E2E or proof of live skill execution.
