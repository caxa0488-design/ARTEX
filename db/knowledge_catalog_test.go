package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func sampleKnowledge() KnowledgeMetadata {
	return KnowledgeMetadata{Title: "Synthetic reference", Kind: "reference", SourceRef: "urn:artex:catalog:test", SourceVersion: "1", SHA256: strings.Repeat("a", 64), License: "Synthetic test data", EvidenceRefs: []string{"urn:artex:synthetic:evidence:1"}}
}
func TestNormalizeKnowledgeMetadata(t *testing.T) {
	in := sampleKnowledge()
	in.Title = "  Synthetic title  "
	in.SHA256 = strings.Repeat("A", 64)
	got, err := NormalizeKnowledgeMetadata(in)
	if err != nil || got.Title != "Synthetic title" || got.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("normalization: %+v %v", got, err)
	}
	for _, source := range []string{"https://example.invalid/docs#section", "urn:artex:source:1"} {
		in := sampleKnowledge()
		in.SourceRef = source
		if _, err := NormalizeKnowledgeMetadata(in); err != nil {
			t.Fatalf("valid source rejected: %v", err)
		}
	}
	cases := map[string]func(*KnowledgeMetadata){
		"empty title":        func(k *KnowledgeMetadata) { k.Title = " " },
		"long title":         func(k *KnowledgeMetadata) { k.Title = strings.Repeat("界", 201) },
		"control":            func(k *KnowledgeMetadata) { k.Title = "one\x00two" },
		"unknown kind":       func(k *KnowledgeMetadata) { k.Kind = "executable" },
		"file ref":           func(k *KnowledgeMetadata) { k.SourceRef = "file:///synthetic.txt" },
		"script ref":         func(k *KnowledgeMetadata) { k.SourceRef = "javascript:synthetic" },
		"userinfo":           func(k *KnowledgeMetadata) { k.SourceRef = "https://user@example.invalid/docs" },
		"query":              func(k *KnowledgeMetadata) { k.SourceRef = "https://example.invalid/docs?key=synthetic" },
		"empty query":        func(k *KnowledgeMetadata) { k.SourceRef = "https://example.invalid/docs?" },
		"backslash":          func(k *KnowledgeMetadata) { k.SourceRef = "https://example.invalid/\\name" },
		"empty urn":          func(k *KnowledgeMetadata) { k.SourceRef = "urn:" },
		"missing version":    func(k *KnowledgeMetadata) { k.SourceVersion = "" },
		"invalid hash":       func(k *KnowledgeMetadata) { k.SHA256 = "not-a-hash" },
		"long license":       func(k *KnowledgeMetadata) { k.License = strings.Repeat("a", 257) },
		"bad evidence":       func(k *KnowledgeMetadata) { k.EvidenceRefs = []string{"/synthetic/local/file"} },
		"duplicate evidence": func(k *KnowledgeMetadata) { k.EvidenceRefs = []string{"urn:one", " urn:one "} },
		"many references":    func(k *KnowledgeMetadata) { k.EvidenceRefs = make([]string, 17) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			in := sampleKnowledge()
			change(&in)
			_, err := NormalizeKnowledgeMetadata(in)
			var invalid *CatalogValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
	in = sampleKnowledge()
	in.SHA256 = ""
	in.License = ""
	in.EvidenceRefs = nil
	got, err = NormalizeKnowledgeMetadata(in)
	if err != nil || got.EvidenceRefs == nil {
		t.Fatalf("draft metadata / JSON array: %+v %v", got, err)
	}
}
func TestValidateKnowledgeReview(t *testing.T) {
	for _, status := range []string{"reviewed", "rejected", "revoked"} {
		if err := ValidateKnowledgeReview(status, "Synthetic review"); err != nil {
			t.Fatal(err)
		}
		if err := ValidateKnowledgeReview(status, ""); err == nil {
			t.Fatalf("%s needs a note", status)
		}
	}
	if err := ValidateKnowledgeReview("draft", ""); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "confirmed", "enabled", "UNKNOWN"} {
		if ValidateKnowledgeReview(status, "note") == nil {
			t.Fatalf("accepted %s", status)
		}
	}
	if ValidateKnowledgeReview("reviewed", "one\x00two") == nil {
		t.Fatal("control accepted")
	}
}
func catalogTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
func createCatalogFixture(t *testing.T, d *DB) *KnowledgeEntry {
	t.Helper()
	in := sampleKnowledge()
	in.SourceRef = fmt.Sprintf("urn:artex:catalog:%d", time.Now().UnixNano())
	k, err := d.CreateKnowledge(t.Context(), in, "test-operator")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := d.Exec(`DELETE FROM knowledge_catalog_history WHERE entry_id=$1`, k.ID); err != nil {
			t.Error(err)
		}
		if _, err := d.Exec(`DELETE FROM knowledge_catalog WHERE id=$1`, k.ID); err != nil {
			t.Error(err)
		}
	})
	return k
}
func TestKnowledgePersistenceReviewRevocation(t *testing.T) {
	d := catalogTestDB(t)
	ctx := t.Context()
	k := createCatalogFixture(t, d)
	if k.ReviewStatus != "draft" || k.Revision != 1 {
		t.Fatalf("unexpected initial state %+v", k)
	}
	if _, err := d.CreateKnowledge(ctx, k.KnowledgeMetadata, "test-operator"); !errors.Is(err, ErrCatalogDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := d.ReviewKnowledge(ctx, k.ID, 1, "confirmed", "not a catalog state", "test-operator"); err == nil {
		t.Fatal("unknown review status accepted")
	}
	reviewed, err := d.ReviewKnowledge(ctx, k.ID, 1, "reviewed", "Synthetic metadata checked", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Revision != 2 || reviewed.ReviewStatus != "reviewed" {
		t.Fatalf("review: %+v", reviewed)
	}
	unchanged, err := d.UpdateKnowledge(ctx, k.ID, 2, reviewed.KnowledgeMetadata, "editor")
	if err != nil || unchanged.Revision != 2 || unchanged.ReviewStatus != "reviewed" {
		t.Fatalf("no-op changed review: %+v %v", unchanged, err)
	}
	in := reviewed.KnowledgeMetadata
	in.Title = "Updated synthetic reference"
	updated, err := d.UpdateKnowledge(ctx, k.ID, 2, in, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 3 || updated.ReviewStatus != "draft" || updated.ReviewNote != "" {
		t.Fatalf("review not invalidated: %+v", updated)
	}
	if _, err := d.UpdateKnowledge(ctx, k.ID, 2, in, "editor"); !errors.Is(err, ErrCatalogConflict) {
		t.Fatalf("stale write: %v", err)
	}
	revoked, err := d.ReviewKnowledge(ctx, k.ID, 3, "revoked", "Synthetic source withdrawn", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpdateKnowledge(ctx, k.ID, 4, in, "editor"); !errors.Is(err, ErrCatalogRevoked) {
		t.Fatalf("edit revoked: %v", err)
	}
	if _, err := d.ReviewKnowledge(ctx, k.ID, 4, "draft", "", "editor"); !errors.Is(err, ErrCatalogRevoked) {
		t.Fatalf("reactivate revoked: %v", err)
	}
	got, err := d.KnowledgeEntry(ctx, k.ID)
	if err != nil || got.Revision != revoked.Revision || got.ReviewStatus != "revoked" {
		t.Fatalf("persisted: %+v %v", got, err)
	}
	history, err := d.KnowledgeHistory(ctx, k.ID, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].Entry.Revision != 4 || history[3].Entry.ReviewStatus != "draft" || history[2].Actor != "reviewer" {
		t.Fatalf("history mismatch: %+v", history)
	}
	page, total, err := d.ListKnowledge(ctx, KnowledgeFilter{ReviewStatus: "revoked", Limit: 100})
	if err != nil || total < 1 {
		t.Fatalf("list: %v %d", err, total)
	}
	found := false
	for _, item := range page {
		if item.ReviewStatus != "revoked" {
			t.Fatal("filter not applied")
		}
		found = found || item.ID == k.ID
	}
	if !found {
		t.Fatal("new revoked record absent")
	}
	if _, _, err := d.ListKnowledge(ctx, KnowledgeFilter{ReviewStatus: "confirmed"}); err == nil {
		t.Fatal("unknown filter accepted")
	}
	if _, err := d.KnowledgeEntry(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing entry: %v", err)
	}
}
func TestKnowledgeReviewRequirementsAndRollback(t *testing.T) {
	d := catalogTestDB(t)
	ctx := t.Context()
	k := createCatalogFixture(t, d)
	in := k.KnowledgeMetadata
	in.SHA256 = ""
	in.License = ""
	k, err := d.UpdateKnowledge(ctx, k.ID, k.Revision, in, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReviewKnowledge(ctx, k.ID, k.Revision, "reviewed", "Reviewed metadata", "reviewer"); err == nil {
		t.Fatal("review accepted without hash/license")
	}
	if _, err := d.UpdateKnowledge(ctx, k.ID, 0, in, "editor"); err == nil {
		t.Fatal("missing revision accepted")
	}
	if _, err := d.CreateKnowledge(ctx, in, ""); err == nil {
		t.Fatal("missing operator accepted")
	}
	other := createCatalogFixture(t, d)
	same := other.KnowledgeMetadata
	same.SourceRef = k.SourceRef
	same.SourceVersion = k.SourceVersion
	if _, err := d.UpdateKnowledge(ctx, other.ID, other.Revision, same, "editor"); !errors.Is(err, ErrCatalogDuplicate) {
		t.Fatalf("duplicate update: %v", err)
	}
	got, err := d.KnowledgeEntry(ctx, other.ID)
	if err != nil || got.SourceRef != other.SourceRef || got.Revision != 1 {
		t.Fatalf("failed update was not rolled back: %+v %v", got, err)
	}
	h, err := d.KnowledgeHistory(ctx, other.ID, 50, 0)
	if err != nil || len(h) != 1 {
		t.Fatalf("failed update added history: %d %v", len(h), err)
	}
}
func TestKnowledgeConcurrentRevision(t *testing.T) {
	d := catalogTestDB(t)
	k := createCatalogFixture(t, d)
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			in := k.KnowledgeMetadata
			in.Title = fmt.Sprintf("Concurrent edit %d", i)
			_, err := d.UpdateKnowledge(context.Background(), k.ID, 1, in, "editor")
			results <- err
		}()
	}
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrCatalogConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
func TestKnowledgeMigrationReopenPreservesData(t *testing.T) {
	d := catalogTestDB(t)
	k := createCatalogFixture(t, d)
	var agentsBefore int
	if err := d.QueryRow(`SELECT count(*) FROM agents`).Scan(&agentsBefore); err != nil {
		t.Fatal(err)
	}
	d2, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	got, err := d2.KnowledgeEntry(t.Context(), k.ID)
	if err != nil || got.Title != k.Title || got.Revision != 1 {
		t.Fatalf("reopen lost metadata: %+v %v", got, err)
	}
	var migrationCount, agentsAfter int
	if err := d2.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=8 AND name='0008_knowledge_catalog.sql'`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if err := d2.QueryRow(`SELECT count(*) FROM agents`).Scan(&agentsAfter); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 || agentsBefore != agentsAfter {
		t.Fatalf("migration repeated/agents changed: %d %d %d", migrationCount, agentsBefore, agentsAfter)
	}
	if _, err := d2.Exec(`UPDATE knowledge_catalog SET review_status='confirmed' WHERE id=$1`, k.ID); err == nil {
		t.Fatal("database accepted unknown review status")
	}
}
