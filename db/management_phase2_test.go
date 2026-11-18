package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func phase2Task(t *testing.T, d *DB) *Task {
	t.Helper()
	task, err := d.CreateTaskWithOptions("Synthetic management fixture", "No actions", TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	return task
}
func phase2Skill(t *testing.T, d *DB) ManagedSkillVersion {
	t.Helper()
	name := fmt.Sprintf("sample-%d", time.Now().UnixNano())
	v := ManagedSkillVersion{SkillName: name, Label: "v1", SourceRef: "urn:artex:synthetic:source", License: "Synthetic test fixture", ContentHash: strings.Repeat("b", 64), FileCount: 1, TotalBytes: 32}
	t.Cleanup(func() {
		for _, table := range []string{"managed_skill_events", "managed_skill_bindings", "managed_skill_versions", "agent_skill_visibility"} {
			if _, err := d.Exec(`DELETE FROM `+table+` WHERE skill_name=$1`, name); err != nil {
				t.Error(err)
			}
		}
	})
	return v
}
func TestManagedSkillVersionBindingLifecycle(t *testing.T) {
	d := catalogTestDB(t)
	ctx := t.Context()
	in := phase2Skill(t, d)
	agent, err := d.GetAgentByKey("planner")
	if err != nil || agent == nil {
		t.Fatalf("planner: %v", err)
	}
	v, err := d.RecordManagedSkill(ctx, in, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.RecordManagedSkill(ctx, in, "operator"); !errors.Is(err, ErrCatalogDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "unknown-key", v.ID, 0, in.ContentHash, "operator"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown agent: %v", err)
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", v.ID, 0, in.ContentHash, "operator"); err == nil {
		t.Fatal("binding enabled invisible skill")
	}
	if err = d.ToggleSkillVisibility(agent.ID, in.SkillName, false); err != nil {
		t.Fatal(err)
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", v.ID, 0, in.ContentHash, "operator"); err == nil {
		t.Fatal("explicitly disabled visibility accepted")
	}
	if err = d.ToggleSkillVisibility(agent.ID, in.SkillName, true); err != nil {
		t.Fatal(err)
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", v.ID, 0, strings.Repeat("c", 64), "operator"); err == nil {
		t.Fatal("changed content accepted")
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", v.ID, 0, in.ContentHash, "operator"); err != nil {
		t.Fatal(err)
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", v.ID, 0, in.ContentHash, "operator"); !errors.Is(err, ErrCatalogConflict) {
		t.Fatalf("stale bind: %v", err)
	}
	versions, bindings, err := d.ManagedSkillVersions(ctx, in.SkillName)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions: %v", err)
	}
	found := false
	for _, b := range bindings {
		if b.AgentKey == "planner" {
			found = b.Visible && b.Revision == 1 && b.VersionID != nil && *b.VersionID == v.ID
		}
	}
	if !found {
		t.Fatal("actual agent-key binding missing")
	}
	if err = d.RevokeManagedSkill(ctx, in.SkillName, v.ID, 1, "Source withdrawn", "reviewer"); err != nil {
		t.Fatal(err)
	}
	if err = d.RevokeManagedSkill(ctx, in.SkillName, v.ID, 1, "stale", "reviewer"); !errors.Is(err, ErrCatalogConflict) {
		t.Fatalf("stale revoke: %v", err)
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", v.ID, 1, in.ContentHash, "operator"); err == nil {
		t.Fatal("revoked version accepted")
	}
	if err = d.BindManagedSkill(ctx, in.SkillName, "planner", 0, 1, "", "operator"); err != nil {
		t.Fatal(err)
	}
	names, err := d.AgentSkillNames(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, n := range names {
		visible = visible || n == in.SkillName
	}
	if !visible {
		t.Fatal("revocation or unbinding changed legacy visibility")
	}
	events, err := d.ManagedSkillEvents(ctx, in.SkillName)
	if err != nil || len(events) != 4 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
}
func TestManagedSkillConcurrentBinding(t *testing.T) {
	d := catalogTestDB(t)
	in := phase2Skill(t, d)
	ctx := t.Context()
	v, err := d.RecordManagedSkill(ctx, in, "operator")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := d.GetAgentByKey("planner")
	if err != nil || agent == nil {
		t.Fatal(err)
	}
	if err = d.ToggleSkillVisibility(agent.ID, in.SkillName, true); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- d.BindManagedSkill(context.Background(), in.SkillName, "planner", v.ID, 0, in.ContentHash, "operator")
		}()
	}
	ok, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			ok++
		} else if errors.Is(err, ErrCatalogConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}
func TestManagedSkillMetadataValidation(t *testing.T) {
	d := catalogTestDB(t)
	in := phase2Skill(t, d)
	for _, change := range []func(*ManagedSkillVersion){func(v *ManagedSkillVersion) { v.SkillName = "../bad" }, func(v *ManagedSkillVersion) { v.ContentHash = "bad" }, func(v *ManagedSkillVersion) { v.SourceRef = "file:///no-read" }, func(v *ManagedSkillVersion) { v.Label = strings.Repeat("x", 129) }, func(v *ManagedSkillVersion) { v.License = "" }, func(v *ManagedSkillVersion) { v.FileCount = 257 }, func(v *ManagedSkillVersion) { v.TotalBytes = 17 << 20 }} {
		bad := in
		change(&bad)
		if _, err := d.RecordManagedSkill(t.Context(), bad, "operator"); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
func phase2Finding(t *testing.T, d *DB, task int64) int64 {
	t.Helper()
	var id int64
	err := d.QueryRow(`INSERT INTO findings(task_id,name,summary,severity,vulnclass,status) VALUES($1,'Synthetic finding','Harmless test fixture','low','config','pending') RETURNING id`, task).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := d.Exec(`DELETE FROM findings WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
	})
	return id
}
func TestOperatorFindingReviewsDoNotPromoteMachineEvidence(t *testing.T) {
	d := catalogTestDB(t)
	ctx := t.Context()
	task := phase2Task(t, d)
	id := phase2Finding(t, d, task.ID)
	var machineBefore []byte
	if err := d.QueryRow(`SELECT to_jsonb(f)-'status' FROM findings f WHERE id=$1`, id).Scan(&machineBefore); err != nil {
		t.Fatal(err)
	}
	initial, err := d.FindingReviewState(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		decision, reason string
		refs             []string
	}{{"unknown", "reason", nil}, {"confirmed", "reason", nil}, {"candidate", "", nil}, {"candidate", "reason", []string{"file:///no-read"}}, {"candidate", "reason", []string{"urn:one", "urn:one"}}} {
		if err := d.ReviewFinding(ctx, id, initial.Fingerprint, bad.decision, bad.reason, bad.refs, "operator"); err == nil {
			t.Fatal("invalid review accepted")
		}
	}
	for decision, status := range map[string]string{"candidate": FindingPending, "confirmed": FindingConfirmed, "rejected": FindingFalsePositive, "inconclusive": FindingInProgress} {
		before, err := d.FindingReviewState(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = d.ReviewFinding(ctx, id, before.Fingerprint, decision, "Human fixture review", []string{"urn:artex:synthetic:evidence"}, "reviewer"); err != nil {
			t.Fatal(err)
		}
		after, err := d.FindingReviewState(ctx, id)
		if err != nil || after.Status != status || after.Verification != initial.Verification || after.Revision != before.Revision+1 || after.Fingerprint == before.Fingerprint {
			t.Fatalf("review: %+v %v", after, err)
		}
		var latest map[string]any
		if err = json.Unmarshal(after.History[0], &latest); err != nil {
			t.Fatal(err)
		}
		if latest["result_fingerprint"] != after.Fingerprint || latest["actor"] != "reviewer" {
			t.Fatal("history does not match source fingerprint")
		}
		if err = d.ReviewFinding(ctx, id, before.Fingerprint, decision, "stale fixture", []string{"urn:artex:synthetic:evidence"}, "reviewer"); !errors.Is(err, ErrCatalogConflict) {
			t.Fatalf("stale: %v", err)
		}
	}
	var machineAfter []byte
	if err := d.QueryRow(`SELECT to_jsonb(f)-'status' FROM findings f WHERE id=$1`, id).Scan(&machineAfter); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(machineBefore, machineAfter) {
		t.Fatal("human review changed machine evidence, proof, or reporting fields")
	}
	before, err := d.FindingReviewState(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec(`UPDATE findings SET summary='Changed fixture' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = d.ReviewFinding(ctx, id, before.Fingerprint, "candidate", "stale evidence", nil, "reviewer"); !errors.Is(err, ErrCatalogConflict) {
		t.Fatalf("machine/source change not detected: %v", err)
	}
}
func TestOperatorFindingConcurrentReview(t *testing.T) {
	d := catalogTestDB(t)
	task := phase2Task(t, d)
	id := phase2Finding(t, d, task.ID)
	before, err := d.FindingReviewState(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- d.ReviewFinding(context.Background(), id, before.Fingerprint, "candidate", "concurrent fixture", nil, "operator")
		}()
	}
	ok, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			ok++
		} else if errors.Is(err, ErrCatalogConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}
func TestOperatorCoverageAnnotationsPreserveLedger(t *testing.T) {
	d := catalogTestDB(t)
	ctx := t.Context()
	task := phase2Task(t, d)
	surface, risk := "synthetic-surface", "configuration"
	initial, err := d.CoverageReviewState(ctx, task.ID, surface, risk)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ReviewCoverage(ctx, task.ID, surface, risk, initial.Fingerprint, "completed", "not executed", []string{"urn:fixture:evidence"}, "operator"); err == nil {
		t.Fatal("completion allowed without ledger")
	}
	if err = d.ReviewCoverage(ctx, task.ID, surface, risk, initial.Fingerprint, "unknown", "reason", nil, "operator"); err == nil {
		t.Fatal("unknown state accepted")
	}
	for _, state := range []string{"untested", "blocked", "failed"} {
		before, err := d.CoverageReviewState(ctx, task.ID, surface, risk)
		if err != nil {
			t.Fatal(err)
		}
		if err = d.ReviewCoverage(ctx, task.ID, surface, risk, before.Fingerprint, state, "Synthetic work annotation", nil, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	ledger, err := d.Assets().CoverageLedger(task.ID)
	if err != nil || len(ledger.Entries) != 0 {
		t.Fatalf("annotation rewrote ledger: %v", err)
	}
	if err = d.Assets().RecordCoverage(task.ID, CoverageEntry{Surface: surface, RiskArea: risk, Outcome: OutcomeNoIssueFound, Evidence: "Synthetic fixture evidence"}); err != nil {
		t.Fatal(err)
	}
	history, err := d.CoverageReviewHistory(ctx, task.ID, "")
	if err != nil || len(history) != 3 {
		t.Fatalf("history %d %v", len(history), err)
	}
	for _, raw := range history {
		var h map[string]any
		if err = json.Unmarshal(raw, &h); err != nil {
			t.Fatal(err)
		}
		if h["stale"] != true || h["run_scope"] != "task_history_not_run_verified" {
			t.Fatal("old annotation represented as current execution")
		}
	}
	before, err := d.CoverageReviewState(ctx, task.ID, surface, risk)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ReviewCoverage(ctx, task.ID, surface, risk, before.Fingerprint, "completed", "Referenced existing ledger", nil, "reviewer"); err == nil {
		t.Fatal("completion without references")
	}
	if err = d.ReviewCoverage(ctx, task.ID, surface, risk, before.Fingerprint, "completed", "Referenced existing ledger", []string{"urn:fixture:evidence"}, "reviewer"); err != nil {
		t.Fatal(err)
	}
	if err = d.ReviewCoverage(ctx, task.ID, surface, risk, before.Fingerprint, "blocked", "stale", nil, "operator"); !errors.Is(err, ErrCatalogConflict) {
		t.Fatalf("stale: %v", err)
	}
	ledger, err = d.Assets().CoverageLedger(task.ID)
	if err != nil || len(ledger.Entries) != 1 || ledger.Entries[0].Outcome != OutcomeNoIssueFound {
		t.Fatalf("ledger changed: %v", err)
	}
	history, err = d.CoverageReviewHistory(ctx, task.ID, "completed")
	if err != nil || len(history) != 1 {
		t.Fatalf("filter %d %v", len(history), err)
	}
	var h map[string]any
	_ = json.Unmarshal(history[0], &h)
	if h["stale"] != false {
		t.Fatal("new snapshot incorrectly stale")
	}
	if _, err = d.CoverageReviewHistory(ctx, task.ID, "clean"); err == nil {
		t.Fatal("unknown filter accepted")
	}
	if _, err = d.Exec(`UPDATE tasks SET coverage_enabled=false WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	disabled, err := d.CoverageReviewState(ctx, task.ID, surface, risk)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ReviewCoverage(ctx, task.ID, surface, risk, disabled.Fingerprint, "untested", "disabled", nil, "operator"); err == nil {
		t.Fatal("disabled coverage accepted annotation")
	}
}
func TestPhase2MigrationReopenAndConstraints(t *testing.T) {
	d := catalogTestDB(t)
	in := phase2Skill(t, d)
	v, err := d.RecordManagedSkill(t.Context(), in, "operator")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, err := again.ManagedSkillVersion(t.Context(), v.ID)
	if err != nil || got.ContentHash != v.ContentHash {
		t.Fatalf("reopen: %v", err)
	}
	var count int
	if err = again.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=9 AND name='0009_skill_review_management.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration: %d %v", count, err)
	}
	if _, err = again.Exec(`UPDATE managed_skill_versions SET file_count=257 WHERE id=$1`, v.ID); err == nil {
		t.Fatal("DB digest limit constraint missing")
	}
}

func TestOperatorCoverageConcurrentAnnotation(t *testing.T) {
	d := catalogTestDB(t)
	task := phase2Task(t, d)
	before, err := d.CoverageReviewState(t.Context(), task.ID, "synthetic", "config")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- d.ReviewCoverage(context.Background(), task.ID, before.Surface, before.RiskArea, before.Fingerprint, "blocked", "Concurrent synthetic annotation", nil, "operator")
		}()
	}
	ok, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			ok++
		} else if errors.Is(err, ErrCatalogConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}
