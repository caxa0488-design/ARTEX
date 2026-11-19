package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type ManagedSkillVersion struct {
	ID          int64     `json:"id"`
	SkillName   string    `json:"skill_name"`
	Label       string    `json:"label"`
	SourceRef   string    `json:"source_ref"`
	License     string    `json:"license"`
	ContentHash string    `json:"content_hash"`
	FileCount   int       `json:"file_count"`
	TotalBytes  int64     `json:"total_bytes"`
	Revoked     bool      `json:"revoked"`
	Revision    int64     `json:"revision"`
	Note        string    `json:"note"`
	Actor       string    `json:"actor"`
	CreatedAt   time.Time `json:"created_at"`
}
type ManagedSkillBinding struct {
	AgentKey  string `json:"agent_key"`
	VersionID *int64 `json:"version_id"`
	Revision  int64  `json:"revision"`
	Visible   bool   `json:"visible"`
}

func validManagedSkillName(s string) bool {
	return catalogText(s, 64, true) && !strings.ContainsAny(s, "./\\ ")
}

const managedSkillColumns = `id,skill_name,label,source_ref,license,content_hash,file_count,total_bytes,revoked,revision,note,actor,created_at`

func scanManagedSkill(row catalogScanner) (*ManagedSkillVersion, error) {
	var v ManagedSkillVersion
	err := row.Scan(&v.ID, &v.SkillName, &v.Label, &v.SourceRef, &v.License, &v.ContentHash, &v.FileCount, &v.TotalBytes, &v.Revoked, &v.Revision, &v.Note, &v.Actor, &v.CreatedAt)
	return &v, err
}
func (d *DB) RecordManagedSkill(ctx context.Context, v ManagedSkillVersion, actor string) (*ManagedSkillVersion, error) {
	v.Label = strings.TrimSpace(v.Label)
	v.License = strings.TrimSpace(v.License)
	if !validManagedSkillName(v.SkillName) || !catalogText(v.Label, 128, true) || !catalogReference(v.SourceRef, 2048) || !catalogText(v.License, 256, true) || !catalogHashRE.MatchString(v.ContentHash) || v.FileCount < 1 || v.FileCount > 256 || v.TotalBytes < 1 || v.TotalBytes > 16<<20 {
		return nil, catalogInvalid("invalid managed skill metadata or digest")
	}
	if err := validateCatalogActor(actor); err != nil {
		return nil, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out, err := scanManagedSkill(tx.QueryRowContext(ctx, `INSERT INTO managed_skill_versions(skill_name,label,source_ref,license,content_hash,file_count,total_bytes,actor) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+managedSkillColumns, v.SkillName, v.Label, v.SourceRef, v.License, v.ContentHash, v.FileCount, v.TotalBytes, actor))
	if err != nil {
		return nil, catalogDBError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO managed_skill_events(skill_name,version_id,action,actor) VALUES($1,$2,'record',$3)`, v.SkillName, out.ID, actor); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func (d *DB) ManagedSkillVersions(ctx context.Context, name string) ([]ManagedSkillVersion, []ManagedSkillBinding, error) {
	versions := []ManagedSkillVersion{}
	bindings := []ManagedSkillBinding{}
	rows, err := d.QueryContext(ctx, `SELECT `+managedSkillColumns+` FROM managed_skill_versions WHERE skill_name=$1 ORDER BY id DESC LIMIT 100`, name)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		v, e := scanManagedSkill(rows)
		if e != nil {
			rows.Close()
			return nil, nil, e
		}
		versions = append(versions, *v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = d.QueryContext(ctx, `SELECT a.key,b.version_id,COALESCE(b.revision,0),EXISTS(SELECT 1 FROM agent_skill_visibility v WHERE v.agent_id=a.id AND v.skill_name=$1 AND v.enabled) FROM agents a LEFT JOIN managed_skill_bindings b ON a.id=b.agent_id AND b.skill_name=$1 ORDER BY a.key`, name)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b ManagedSkillBinding
		if err := rows.Scan(&b.AgentKey, &b.VersionID, &b.Revision, &b.Visible); err != nil {
			return nil, nil, err
		}
		bindings = append(bindings, b)
	}
	return versions, bindings, rows.Err()
}
func (d *DB) ManagedSkillVersion(ctx context.Context, id int64) (*ManagedSkillVersion, error) {
	return scanManagedSkill(d.QueryRowContext(ctx, `SELECT `+managedSkillColumns+` FROM managed_skill_versions WHERE id=$1`, id))
}
func (d *DB) RevokeManagedSkill(ctx context.Context, name string, id, revision int64, note, actor string) error {
	if revision < 1 || id < 1 || !validManagedSkillName(name) || !catalogText(note, 2000, true) || validateCatalogActor(actor) != nil {
		return catalogInvalid("revocation reason and authenticated operator required")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var found int64
	err = tx.QueryRowContext(ctx, `UPDATE managed_skill_versions SET revoked=true,revision=revision+1,note=$4 WHERE id=$1 AND skill_name=$2 AND revision=$3 AND NOT revoked RETURNING id`, id, name, revision, note).Scan(&found)
	if err == sql.ErrNoRows {
		return ErrCatalogConflict
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_skill_events(skill_name,version_id,action,actor,note) VALUES($1,$2,'revoke',$3,$4)`, name, id, actor, note)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Binding is management-only and never enables a skill or changes visibility.
// expectedHash is computed by the trusted platform adapter, never accepted from HTTP.
func (d *DB) BindManagedSkill(ctx context.Context, name, key string, versionID, revision int64, expectedHash, actor string) error {
	if versionID == 0 && revision == 0 {
		return catalogInvalid("no existing version binding to clear")
	}
	if !validManagedSkillName(name) || !catalogText(key, 128, true) || revision < 0 || validateCatalogActor(actor) != nil {
		return catalogInvalid("invalid skill binding")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var agentID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE key=$1`, key).Scan(&agentID); err != nil {
		return err
	}
	var target any
	if versionID > 0 {
		v, err := scanManagedSkill(tx.QueryRowContext(ctx, `SELECT `+managedSkillColumns+` FROM managed_skill_versions WHERE id=$1 FOR SHARE`, versionID))
		if err != nil {
			return err
		}
		if v.SkillName != name || v.Revoked || v.ContentHash != expectedHash {
			return catalogInvalid("version is revoked, mismatched, or content changed")
		}
		var visible bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_skill_visibility WHERE agent_id=$1 AND skill_name=$2 AND enabled)`, agentID, name).Scan(&visible); err != nil {
			return err
		}
		if !visible {
			return catalogInvalid("skill is not visible to this agent; manage existing visibility separately")
		}
		target = versionID
	} else if versionID < 0 {
		return catalogInvalid("invalid version id")
	}
	var next int64
	if revision == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO managed_skill_bindings(agent_id,skill_name,version_id,revision,actor) VALUES($1,$2,$3,1,$4) ON CONFLICT DO NOTHING RETURNING revision`, agentID, name, target, actor).Scan(&next)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE managed_skill_bindings SET version_id=$3,revision=revision+1,actor=$4,updated_at=now() WHERE agent_id=$1 AND skill_name=$2 AND revision=$5 RETURNING revision`, agentID, name, target, actor, revision).Scan(&next)
	}
	if err == sql.ErrNoRows {
		return ErrCatalogConflict
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_skill_events(skill_name,version_id,agent_key,action,actor) VALUES($1,$2,$3,'bind',$4)`, name, target, key, actor)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) ManagedSkillEvents(ctx context.Context, name string) ([]json.RawMessage, error) {
	rows, err := d.QueryContext(ctx, `SELECT to_jsonb(e) FROM managed_skill_events e WHERE skill_name=$1 ORDER BY id DESC LIMIT 100`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}

// ManagedSkillNames keeps orphaned/missing local skill records discoverable.
// The index is bounded; it does not read directories or expose skill content.
func (d *DB) ManagedSkillNames(ctx context.Context) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT DISTINCT skill_name FROM managed_skill_versions ORDER BY skill_name LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
