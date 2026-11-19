-- Management records only. Existing skill visibility and runtime loading are unchanged.
CREATE TABLE managed_skill_versions (
 id BIGSERIAL PRIMARY KEY, skill_name TEXT NOT NULL, label TEXT NOT NULL,
 source_ref TEXT NOT NULL, license TEXT NOT NULL, content_hash TEXT NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 file_count INTEGER NOT NULL CHECK(file_count BETWEEN 1 AND 256), total_bytes BIGINT NOT NULL CHECK(total_bytes BETWEEN 1 AND 16777216),
 revoked BOOLEAN NOT NULL DEFAULT false, revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 note TEXT NOT NULL DEFAULT '', actor TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(skill_name,label)
);
CREATE TABLE managed_skill_bindings (
 agent_id BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE, skill_name TEXT NOT NULL,
 version_id BIGINT REFERENCES managed_skill_versions(id), revision BIGINT NOT NULL CHECK(revision>0),
 actor TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(agent_id,skill_name)
);
CREATE TABLE managed_skill_events (
 id BIGSERIAL PRIMARY KEY, skill_name TEXT NOT NULL, version_id BIGINT REFERENCES managed_skill_versions(id),
 agent_key TEXT NOT NULL DEFAULT '', action TEXT NOT NULL, actor TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Human disposition uses findings.status; machine verification is never promoted.
CREATE TABLE finding_operator_reviews (
 id BIGSERIAL PRIMARY KEY, finding_id BIGINT NOT NULL REFERENCES findings(id) ON DELETE CASCADE,
 revision BIGINT NOT NULL CHECK(revision>0), decision TEXT NOT NULL CHECK(decision IN ('candidate','confirmed','rejected','inconclusive')),
 previous_status TEXT NOT NULL, resulting_status TEXT NOT NULL, reason TEXT NOT NULL,
 evidence_refs JSONB NOT NULL CHECK(jsonb_typeof(evidence_refs)='array'), actor TEXT NOT NULL,
 source_fingerprint TEXT NOT NULL, result_fingerprint TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(finding_id,revision)
);
-- Work annotations point to a ledger snapshot; they never replace its outcomes
-- or asset-tested flags. No per-run execution completeness is inferred.
CREATE TABLE coverage_operator_reviews (
 id BIGSERIAL PRIMARY KEY, task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
 surface TEXT NOT NULL, risk_area TEXT NOT NULL, revision BIGINT NOT NULL CHECK(revision>0),
 work_state TEXT NOT NULL CHECK(work_state IN ('untested','blocked','failed','completed')),
 reason TEXT NOT NULL, evidence_refs JSONB NOT NULL CHECK(jsonb_typeof(evidence_refs)='array'), actor TEXT NOT NULL,
 source_fingerprint TEXT NOT NULL, result_fingerprint TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(task_id,surface,risk_area,revision)
);

CREATE INDEX managed_skill_events_lookup ON managed_skill_events(skill_name,id DESC);
CREATE INDEX coverage_operator_reviews_lookup ON coverage_operator_reviews(task_id,id DESC);
