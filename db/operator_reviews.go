package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

type reviewQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type FindingReviewState struct {
	Name         string            `json:"name"`
	TaskID       *int64            `json:"task_id"`
	FindingID    int64             `json:"finding_id"`
	Status       string            `json:"status"`
	Verification string            `json:"verification_status"`
	Revision     int64             `json:"revision"`
	Fingerprint  string            `json:"fingerprint"`
	History      []json.RawMessage `json:"history"`
}
type CoverageReviewState struct {
	TaskID      int64  `json:"task_id"`
	Surface     string `json:"surface"`
	RiskArea    string `json:"risk_area"`
	Revision    int64  `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	Outcome     string `json:"ledger_outcome"`
	Enabled     bool   `json:"coverage_enabled"`
}

func reviewFingerprint(raw []byte, revision int64) string {
	h := sha256.New()
	h.Write(raw)
	v, _ := json.Marshal(revision)
	h.Write(v)
	return hex.EncodeToString(h.Sum(nil))
}
func validReviewInput(reason string, refs []string, actor string) error {
	if !catalogText(reason, 2000, true) || validateCatalogActor(actor) != nil || len(refs) > 16 {
		return catalogInvalid("authenticated operator, single-paragraph reason (1–2000 characters) and at most 16 references required")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if !catalogReference(ref, 512) || seen[ref] {
			return catalogInvalid("invalid or duplicate evidence reference")
		}
		seen[ref] = true
	}
	return nil
}
func findingSnapshot(ctx context.Context, q reviewQuery, id int64) (FindingReviewState, []byte, error) {
	s := FindingReviewState{FindingID: id}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT f.name,f.task_id,f.status,f.verification_status,to_jsonb(f),COALESCE((SELECT max(revision) FROM finding_operator_reviews WHERE finding_id=f.id),0) FROM findings f WHERE f.id=$1`, id).Scan(&s.Name, &s.TaskID, &s.Status, &s.Verification, &raw, &s.Revision)
	s.Fingerprint = reviewFingerprint(raw, s.Revision)
	return s, raw, err
}
func (d *DB) FindingReviewState(ctx context.Context, id int64) (FindingReviewState, error) {
	s, _, err := findingSnapshot(ctx, d, id)
	if err != nil {
		return s, err
	}
	rows, err := d.QueryContext(ctx, `SELECT to_jsonb(r) FROM finding_operator_reviews r WHERE finding_id=$1 ORDER BY revision DESC LIMIT 100`, id)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	s.History = []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return s, err
		}
		s.History = append(s.History, raw)
	}
	return s, rows.Err()
}
func (d *DB) ReviewFinding(ctx context.Context, id int64, expected, decision, reason string, refs []string, actor string) error {
	mapping := map[string]string{"candidate": FindingPending, "confirmed": FindingConfirmed, "rejected": FindingFalsePositive, "inconclusive": FindingInProgress}
	status, ok := mapping[decision]
	if !ok || !catalogHashRE.MatchString(expected) {
		return catalogInvalid("invalid review decision or fingerprint")
	}
	if err := validReviewInput(reason, refs, actor); err != nil {
		return err
	}
	if decision == "confirmed" && len(refs) == 0 {
		return catalogInvalid("human confirmation requires evidence references; they remain operator declarations")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM findings WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return err
	}
	before, _, err := findingSnapshot(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.Fingerprint != expected {
		return ErrCatalogConflict
	}
	// Never promote verification_status, POE, report_state or evidence versions.
	if _, err = tx.ExecContext(ctx, `UPDATE findings SET status=$2 WHERE id=$1`, id, status); err != nil {
		return err
	}
	_, raw, err := findingSnapshot(ctx, tx, id)
	if err != nil {
		return err
	}
	after := reviewFingerprint(raw, before.Revision+1)
	data, _ := json.Marshal(refs)
	if refs == nil {
		data = []byte("[]")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO finding_operator_reviews(finding_id,revision,decision,previous_status,resulting_status,reason,evidence_refs,actor,source_fingerprint,result_fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`, id, before.Revision+1, decision, before.Status, status, reason, string(data), actor, expected, after)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func coverageSnapshot(ctx context.Context, q reviewQuery, task int64, surface, risk string) (CoverageReviewState, []byte, error) {
	s := CoverageReviewState{TaskID: task, Surface: surface, RiskArea: risk}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT t.coverage_enabled,COALESCE((SELECT outcome FROM coverage_ledger WHERE task_id=t.id AND surface=$2 AND risk_area=$3),''),jsonb_build_object('task_status',t.status,'paused',t.paused,'coverage_enabled',t.coverage_enabled,'deadline_at',t.deadline_at,'completed_at',t.completed_at,'first_run_at',t.first_run_at,'entry',(SELECT to_jsonb(c) FROM coverage_ledger c WHERE c.task_id=t.id AND c.surface=$2 AND c.risk_area=$3)),COALESCE((SELECT max(revision) FROM coverage_operator_reviews WHERE task_id=t.id AND surface=$2 AND risk_area=$3),0) FROM tasks t WHERE t.id=$1 AND t.deleted_at IS NULL`, task, surface, risk).Scan(&s.Enabled, &s.Outcome, &raw, &s.Revision)
	s.Fingerprint = reviewFingerprint(raw, s.Revision)
	return s, raw, err
}
func normalizeReviewKey(surface, risk string) (string, string, error) {
	surface = NormalizeCoverageField(surface)
	risk = normRiskArea(risk)
	if !catalogText(surface, 200, true) || !catalogText(risk, 64, true) {
		return "", "", catalogInvalid("surface and risk_area are required and bounded")
	}
	return surface, risk, nil
}
func (d *DB) CoverageReviewState(ctx context.Context, task int64, surface, risk string) (CoverageReviewState, error) {
	surface, risk, err := normalizeReviewKey(surface, risk)
	if err != nil {
		return CoverageReviewState{}, err
	}
	s, _, err := coverageSnapshot(ctx, d, task, surface, risk)
	return s, err
}
func (d *DB) ReviewCoverage(ctx context.Context, task int64, surface, risk, expected, state, reason string, refs []string, actor string) error {
	switch state {
	case "untested", "blocked", "failed", "completed":
	default:
		return catalogInvalid("unknown operator work state")
	}
	if !catalogHashRE.MatchString(expected) {
		return catalogInvalid("snapshot fingerprint required")
	}
	if err := validReviewInput(reason, refs, actor); err != nil {
		return err
	}
	surface, risk, err := normalizeReviewKey(surface, risk)
	if err != nil {
		return err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked int64
	// Existing ledger writers use this same task lock. No scheduler state changes.
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, task).Scan(&locked); err != nil {
		return err
	}
	before, raw, err := coverageSnapshot(ctx, tx, task, surface, risk)
	if err != nil {
		return err
	}
	if before.Fingerprint != expected {
		return ErrCatalogConflict
	}
	if !before.Enabled {
		return catalogInvalid("coverage is disabled for this task")
	}
	if state == "completed" && (before.Outcome == "" || before.Outcome == OutcomeNeedsFollowUp || len(refs) == 0) {
		return catalogInvalid("completion annotation requires an existing settled ledger outcome and evidence references; it never marks an asset tested")
	}
	// The ledger and its outcomes are not rewritten by a work annotation.
	after := reviewFingerprint(raw, before.Revision+1)
	data, _ := json.Marshal(refs)
	if refs == nil {
		data = []byte("[]")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO coverage_operator_reviews(task_id,surface,risk_area,revision,work_state,reason,evidence_refs,actor,source_fingerprint,result_fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`, task, surface, risk, before.Revision+1, state, reason, string(data), actor, expected, after)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) CoverageReviewHistory(ctx context.Context, task int64, state string) ([]json.RawMessage, error) {
	if state != "" {
		switch state {
		case "untested", "blocked", "failed", "completed":
		default:
			return nil, catalogInvalid("unknown work-state filter")
		}
	}
	rows, err := d.QueryContext(ctx, `SELECT to_jsonb(r) FROM coverage_operator_reviews r WHERE task_id=$1 AND ($2='' OR work_state=$2) ORDER BY id DESC LIMIT 100`, task, state)
	if err != nil {
		return nil, err
	}
	rawRows := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, raw)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []json.RawMessage{}
	cache := map[string]string{}
	for _, raw := range rawRows {
		var record struct {
			Surface  string `json:"surface"`
			RiskArea string `json:"risk_area"`
			Result   string `json:"result_fingerprint"`
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		key := record.Surface + "\x00" + record.RiskArea
		current, ok := cache[key]
		if !ok {
			s, _, e := coverageSnapshot(ctx, d, task, record.Surface, record.RiskArea)
			if e != nil {
				return nil, e
			}
			current = s.Fingerprint
			cache[key] = current
		}
		var annotated map[string]any
		if err := json.Unmarshal(raw, &annotated); err != nil {
			return nil, err
		}
		annotated["stale"] = current != record.Result
		annotated["run_scope"] = "task_history_not_run_verified"
		b, _ := json.Marshal(annotated)
		out = append(out, b)
	}
	return out, nil
}
