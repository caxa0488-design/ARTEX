-- Immutable application-level manifest snapshots, not filesystem execution sandboxes.
CREATE TABLE managed_skill_runtime_pins (
 id BIGSERIAL PRIMARY KEY, run_key TEXT NOT NULL,
 task_id BIGINT REFERENCES tasks(id) ON DELETE CASCADE, session_id TEXT NOT NULL DEFAULT '',
 agent_id BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE, skill_name TEXT NOT NULL,
 version_id BIGINT NOT NULL REFERENCES managed_skill_versions(id),
 content_hash TEXT NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 manifest BYTEA NOT NULL CHECK(octet_length(manifest) BETWEEN 1 AND 131072),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK((task_id IS NOT NULL AND session_id='') OR (task_id IS NULL AND session_id<>'')),
 UNIQUE(run_key,agent_id,skill_name)
);
CREATE TABLE managed_skill_runtime_events (
 id BIGSERIAL PRIMARY KEY, pin_id BIGINT NOT NULL REFERENCES managed_skill_runtime_pins(id) ON DELETE CASCADE,
 stage TEXT NOT NULL CHECK(stage IN ('assembled','loaded')), recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX managed_skill_runtime_events_pin ON managed_skill_runtime_events(pin_id,id DESC);

CREATE FUNCTION reject_managed_skill_pin_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'managed skill runtime snapshots are immutable; create a new task/session';
END;
$$;
CREATE TRIGGER managed_skill_pin_immutable BEFORE UPDATE ON managed_skill_runtime_pins
FOR EACH ROW EXECUTE FUNCTION reject_managed_skill_pin_update();
