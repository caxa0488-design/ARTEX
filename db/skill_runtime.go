package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

const MaxManagedManifest = 128 << 10

var ErrSkillIntegrity = errors.New("managed skill integrity: start a new task/session after changing versions, or restore the pinned content and authorization")

// ManagedManifestHash is the existing tree-v1 hash for a single SKILL.md file.
func ManagedManifestHash(raw []byte) string {
	file := sha256.Sum256(raw)
	sum := sha256.Sum256([]byte(fmt.Sprintf("8:SKILL.md:%d:%x\n", len(raw), file)))
	return hex.EncodeToString(sum[:])
}

type RuntimeSkillBinding struct {
	Name      string
	VersionID int64
	Enabled   bool
}
type RuntimeSkillPin struct {
	ID        int64
	RunKey    string
	TaskID    int64
	SessionID string
	AgentKey  string
	Name      string
	VersionID int64
	Hash      string
	Manifest  []byte
}

func SkillRunKey(task int64, session string) (string, error) {
	if task > 0 && session == "" {
		return fmt.Sprintf("task:%d", task), nil
	}
	if task == 0 && catalogText(session, 256, true) {
		return "chat:" + session, nil
	}
	return "", catalogInvalid("managed skills require one attributed task or chat session")
}
func (d *DB) RuntimeSkillBindings(ctx context.Context, key string) ([]RuntimeSkillBinding, error) {
	rows, err := d.QueryContext(ctx, `SELECT b.skill_name,COALESCE(b.version_id,0),EXISTS(SELECT 1 FROM agent_skill_visibility s WHERE s.agent_id=a.id AND s.skill_name=b.skill_name AND s.enabled) FROM managed_skill_bindings b JOIN agents a ON a.id=b.agent_id WHERE a.key=$1 ORDER BY b.skill_name`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuntimeSkillBinding{}
	for rows.Next() {
		var b RuntimeSkillBinding
		if err := rows.Scan(&b.Name, &b.VersionID, &b.Enabled); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (d *DB) RuntimeSkillPins(ctx context.Context, run, key string) ([]RuntimeSkillPin, error) {
	rows, err := d.QueryContext(ctx, `SELECT p.id,COALESCE(p.task_id,0),p.session_id,p.skill_name,p.version_id,p.content_hash,p.manifest FROM managed_skill_runtime_pins p JOIN agents a ON a.id=p.agent_id WHERE p.run_key=$1 AND a.key=$2 ORDER BY p.skill_name`, run, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuntimeSkillPin{}
	for rows.Next() {
		p := RuntimeSkillPin{RunKey: run, AgentKey: key}
		if err := rows.Scan(&p.ID, &p.TaskID, &p.SessionID, &p.Name, &p.VersionID, &p.Hash, &p.Manifest); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func validateRuntimePin(ctx context.Context, q reviewQuery, p RuntimeSkillPin) error {
	var hash string
	var count int
	var size int64
	err := q.QueryRowContext(ctx, `SELECT v.content_hash,v.file_count,v.total_bytes FROM managed_skill_bindings b JOIN agents a ON a.id=b.agent_id JOIN managed_skill_versions v ON v.id=b.version_id JOIN agent_skill_visibility s ON s.agent_id=a.id AND s.skill_name=b.skill_name AND s.enabled WHERE a.key=$1 AND b.skill_name=$2 AND b.version_id=$3 AND v.skill_name=b.skill_name AND NOT v.revoked`, p.AgentKey, p.Name, p.VersionID).Scan(&hash, &count, &size)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSkillIntegrity
		}
		return err
	}
	if hash != p.Hash || count != 1 || size != int64(len(p.Manifest)) || len(p.Manifest) == 0 || len(p.Manifest) > MaxManagedManifest || !utf8.Valid(p.Manifest) || ManagedManifestHash(p.Manifest) != hash {
		return ErrSkillIntegrity
	}
	return nil
}
func (d *DB) ValidateRuntimeSkill(ctx context.Context, p RuntimeSkillPin) error {
	return validateRuntimePin(ctx, d, p)
}
func (d *DB) PinRuntimeSkill(ctx context.Context, p RuntimeSkillPin) (RuntimeSkillPin, error) {
	run, err := SkillRunKey(p.TaskID, p.SessionID)
	if err != nil {
		return p, err
	}
	p.RunKey = run
	if !validManagedSkillName(p.Name) || !catalogText(p.AgentKey, 128, true) {
		return p, ErrSkillIntegrity
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	// Serialize binding/revocation while the snapshot is validated and recorded.
	var agent int64
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM agents a JOIN managed_skill_bindings b ON b.agent_id=a.id JOIN managed_skill_versions v ON v.id=b.version_id JOIN agent_skill_visibility s ON s.agent_id=a.id AND s.skill_name=b.skill_name AND s.enabled WHERE a.key=$1 AND b.skill_name=$2 AND v.id=$3 AND NOT v.revoked FOR SHARE OF b,v,s`, p.AgentKey, p.Name, p.VersionID).Scan(&agent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, ErrSkillIntegrity
		}
		return p, err
	}
	if err = validateRuntimePin(ctx, tx, p); err != nil {
		return p, err
	}
	if p.TaskID > 0 {
		var id int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, p.TaskID).Scan(&id); err != nil {
			return p, err
		}
	}
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM managed_skill_runtime_pins WHERE run_key=$1 AND agent_id=$2 AND skill_name=$3`, run, agent, p.Name).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		var legacy bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM skill_usage WHERE agent_key=$1 AND skill=$2 AND found AND (($3::bigint>0 AND task_id=$3) OR ($3::bigint=0 AND session_id=$4)))`, p.AgentKey, p.Name, p.TaskID, p.SessionID).Scan(&legacy); err != nil {
			return p, err
		}
		if legacy {
			return p, fmt.Errorf("%w: historical unversioned skill usage cannot be pinned retroactively", ErrSkillIntegrity)
		}
	} else if err != nil {
		return p, err
	}
	var task any
	if p.TaskID > 0 {
		task = p.TaskID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_skill_runtime_pins(run_key,task_id,session_id,agent_id,skill_name,version_id,content_hash,manifest) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(run_key,agent_id,skill_name) DO NOTHING`, run, task, p.SessionID, agent, p.Name, p.VersionID, p.Hash, p.Manifest)
	if err != nil {
		return p, err
	}
	var id, version int64
	var hash string
	var manifest []byte
	err = tx.QueryRowContext(ctx, `SELECT id,version_id,content_hash,manifest FROM managed_skill_runtime_pins WHERE run_key=$1 AND agent_id=$2 AND skill_name=$3`, run, agent, p.Name).Scan(&id, &version, &hash, &manifest)
	if err != nil {
		return p, err
	}
	if version != p.VersionID || hash != p.Hash || !bytes.Equal(manifest, p.Manifest) {
		return p, ErrSkillIntegrity
	}
	p.ID = id
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_skill_runtime_events(pin_id,stage) VALUES($1,'assembled')`, id)
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (d *DB) RecordRuntimeSkillLoaded(ctx context.Context, p RuntimeSkillPin) error {
	if err := d.ValidateRuntimeSkill(ctx, p); err != nil {
		return err
	}
	result, err := d.ExecContext(ctx, `INSERT INTO managed_skill_runtime_events(pin_id,stage) SELECT id,'loaded' FROM managed_skill_runtime_pins WHERE id=$1 AND version_id=$2 AND content_hash=$3`, p.ID, p.VersionID, p.Hash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSkillIntegrity
	}
	return nil
}

// RuntimeSkillMetadata never returns manifest content, even to authenticated callers.
func (d *DB) RuntimeSkillMetadata(ctx context.Context, task int64) (map[string]any, error) {
	rows, err := d.QueryContext(ctx, `SELECT jsonb_build_object('id',p.id,'agent_key',a.key,'skill',p.skill_name,'version_id',p.version_id,'content_hash',p.content_hash,'label',v.label,'source_ref',v.source_ref,'license',v.license,'revoked',v.revoked,'created_at',p.created_at) FROM managed_skill_runtime_pins p JOIN agents a ON a.id=p.agent_id JOIN managed_skill_versions v ON v.id=p.version_id WHERE p.task_id=$1 ORDER BY p.id DESC LIMIT 100`, task)
	if err != nil {
		return nil, err
	}
	pins := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		pins = append(pins, raw)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = d.QueryContext(ctx, `SELECT jsonb_build_object('id',e.id,'pin_id',e.pin_id,'stage',e.stage,'recorded_at',e.recorded_at) FROM managed_skill_runtime_events e JOIN managed_skill_runtime_pins p ON p.id=e.pin_id WHERE p.task_id=$1 ORDER BY e.id DESC LIMIT 100`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		events = append(events, raw)
	}
	return map[string]any{"pins": pins, "events": events, "scope": "task_agent_manifest_only", "limits": "Latest 100 pins/events. assembled is not loaded; loaded means instructions returned, not actions executed. No per-attempt testing coverage is inferred."}, rows.Err()
}
