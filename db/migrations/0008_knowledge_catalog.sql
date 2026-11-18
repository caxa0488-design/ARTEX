-- Metadata only: no source content, executable instructions, or credentials.
CREATE TABLE knowledge_catalog (
    id BIGSERIAL PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    kind TEXT NOT NULL CHECK (kind IN ('reference', 'advisory', 'lesson', 'skill_metadata')),
    source_ref TEXT NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 2048),
    source_version TEXT NOT NULL CHECK (length(btrim(source_version)) BETWEEN 1 AND 128),
    sha256 TEXT NOT NULL DEFAULT '' CHECK (sha256 = '' OR sha256 ~ '^[0-9a-f]{64}$'),
    license TEXT NOT NULL DEFAULT '' CHECK (length(license) <= 256),
    evidence_refs JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(evidence_refs) = 'array'),
    review_status TEXT NOT NULL DEFAULT 'draft' CHECK (review_status IN ('draft', 'reviewed', 'rejected', 'revoked')),
    review_note TEXT NOT NULL DEFAULT '' CHECK (length(review_note) <= 2000),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, source_ref, source_version),
    CHECK (review_status = 'draft' OR length(btrim(review_note)) > 0),
    CHECK (review_status <> 'reviewed' OR (sha256 <> '' AND length(btrim(license)) > 0))
);
CREATE INDEX knowledge_catalog_review_idx ON knowledge_catalog (review_status, id DESC);

-- No update/delete API: preserve snapshots even after revocation. This is
-- application revision history, not a tamper-proof audit log against DB admins.
CREATE TABLE knowledge_catalog_history (
    entry_id BIGINT NOT NULL REFERENCES knowledge_catalog(id) ON DELETE RESTRICT,
    revision BIGINT NOT NULL,
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    actor TEXT NOT NULL CHECK (length(btrim(actor)) BETWEEN 1 AND 128),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entry_id, revision)
);
