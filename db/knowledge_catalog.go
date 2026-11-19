package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrCatalogConflict  = errors.New("catalog revision changed; reload before saving")
	ErrCatalogDuplicate = errors.New("this source version already exists for this kind")
	ErrCatalogRevoked   = errors.New("revoked catalog entries are read-only; create a new source version")
	catalogHashRE       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type CatalogValidationError struct{ Message string }

func (e *CatalogValidationError) Error() string { return e.Message }
func catalogInvalid(message string) error       { return &CatalogValidationError{Message: message} }

// KnowledgeMetadata describes a source, not its contents or an execution grant.
type KnowledgeMetadata struct {
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	SourceRef     string   `json:"source_ref"`
	SourceVersion string   `json:"source_version"`
	SHA256        string   `json:"sha256"`
	License       string   `json:"license"`
	EvidenceRefs  []string `json:"evidence_refs"`
}

type KnowledgeEntry struct {
	KnowledgeMetadata
	ID           int64     `json:"id"`
	ReviewStatus string    `json:"review_status"`
	ReviewNote   string    `json:"review_note"`
	Revision     int64     `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type KnowledgeRevision struct {
	Entry      KnowledgeEntry `json:"entry"`
	Actor      string         `json:"actor"`
	RecordedAt time.Time      `json:"recorded_at"`
}

type KnowledgeFilter struct {
	Kind         string
	ReviewStatus string
	Limit        int
	Offset       int
}

func ValidKnowledgeKind(s string) bool {
	switch s {
	case "reference", "advisory", "lesson", "skill_metadata":
		return true
	}
	return false
}
func ValidKnowledgeReview(s string) bool {
	switch s {
	case "draft", "reviewed", "rejected", "revoked":
		return true
	}
	return false
}
func catalogText(s string, max int, required bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || (required && strings.TrimSpace(s) == "") {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}
func catalogReference(s string, max int) bool {
	if !catalogText(s, max, true) || strings.ContainsAny(s, " \\<>\"") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Hostname() != "" && u.Opaque == ""
	case "urn":
		return u.Opaque != "" && u.Host == "" && u.Fragment == ""
	default:
		return false
	}
}

// NormalizeKnowledgeMetadata is shared by HTTP and direct database callers.
// References remain inert strings; this package never opens or fetches them.
func NormalizeKnowledgeMetadata(in KnowledgeMetadata) (KnowledgeMetadata, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.SourceRef = strings.TrimSpace(in.SourceRef)
	in.SourceVersion = strings.TrimSpace(in.SourceVersion)
	in.License = strings.TrimSpace(in.License)
	in.SHA256 = strings.ToLower(strings.TrimSpace(in.SHA256))
	if !catalogText(in.Title, 200, true) {
		return in, catalogInvalid("title must contain 1–200 printable characters")
	}
	if !ValidKnowledgeKind(in.Kind) {
		return in, catalogInvalid("unknown knowledge kind")
	}
	if !catalogReference(in.SourceRef, 2048) {
		return in, catalogInvalid("source_ref must be a credential-free HTTPS URL without query parameters, or a URN")
	}
	if !catalogText(in.SourceVersion, 128, true) {
		return in, catalogInvalid("source_version must contain 1–128 printable characters")
	}
	if in.SHA256 != "" && !catalogHashRE.MatchString(in.SHA256) {
		return in, catalogInvalid("sha256 must be empty or 64 hexadecimal characters")
	}
	if !catalogText(in.License, 256, false) {
		return in, catalogInvalid("license must contain at most 256 printable characters")
	}
	if len(in.EvidenceRefs) > 16 {
		return in, catalogInvalid("at most 16 evidence references are allowed")
	}
	refs := make([]string, 0, len(in.EvidenceRefs))
	seen := map[string]bool{}
	for _, ref := range in.EvidenceRefs {
		ref = strings.TrimSpace(ref)
		if !catalogReference(ref, 512) {
			return in, catalogInvalid("invalid evidence reference; use a credential-free HTTPS URL without query parameters, or a URN")
		}
		if seen[ref] {
			return in, catalogInvalid("duplicate evidence reference")
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	in.EvidenceRefs = refs
	return in, nil
}

func ValidateKnowledgeReview(status, note string) error {
	if !ValidKnowledgeReview(status) {
		return catalogInvalid("unknown metadata review status")
	}
	if !catalogText(note, 2000, status != "draft") {
		return catalogInvalid("review note must be printable, at most 2000 characters, and nonempty for a review decision")
	}
	return nil
}
func validateCatalogActor(actor string) error {
	if !catalogText(actor, 128, true) {
		return catalogInvalid("authenticated operator is required")
	}
	return nil
}

const knowledgeColumns = `id,title,kind,source_ref,source_version,sha256,license,evidence_refs,review_status,review_note,revision,created_at,updated_at`

type catalogScanner interface{ Scan(...any) error }

func scanKnowledge(row catalogScanner) (*KnowledgeEntry, error) {
	var k KnowledgeEntry
	var refs []byte
	err := row.Scan(&k.ID, &k.Title, &k.Kind, &k.SourceRef, &k.SourceVersion, &k.SHA256, &k.License, &refs, &k.ReviewStatus, &k.ReviewNote, &k.Revision, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(refs, &k.EvidenceRefs); err != nil {
		return nil, err
	}
	return &k, nil
}
func catalogDBError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrCatalogDuplicate
	}
	return err
}
func snapshotKnowledge(ctx context.Context, tx *sql.Tx, id int64, actor string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_catalog_history(entry_id,revision,snapshot,actor) SELECT id,revision,to_jsonb(k),$2 FROM knowledge_catalog k WHERE id=$1`, id, actor)
	return err
}
func (d *DB) KnowledgeEntry(ctx context.Context, id int64) (*KnowledgeEntry, error) {
	return scanKnowledge(d.QueryRowContext(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_catalog WHERE id=$1`, id))
}
func (d *DB) ListKnowledge(ctx context.Context, f KnowledgeFilter) ([]KnowledgeEntry, int, error) {
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Limit < 1 || f.Limit > 100 || f.Offset < 0 || f.Offset > 1000000 {
		return nil, 0, catalogInvalid("invalid catalog pagination")
	}
	if (f.Kind != "" && !ValidKnowledgeKind(f.Kind)) || (f.ReviewStatus != "" && !ValidKnowledgeReview(f.ReviewStatus)) {
		return nil, 0, catalogInvalid("unknown catalog filter")
	}
	const where = ` FROM knowledge_catalog WHERE ($1='' OR kind=$1) AND ($2='' OR review_status=$2)`
	var total int
	if err := d.QueryRowContext(ctx, `SELECT count(*)`+where, f.Kind, f.ReviewStatus).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT `+knowledgeColumns+where+` ORDER BY id DESC LIMIT $3 OFFSET $4`, f.Kind, f.ReviewStatus, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []KnowledgeEntry{}
	for rows.Next() {
		k, err := scanKnowledge(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *k)
	}
	return items, total, rows.Err()
}
func (d *DB) CreateKnowledge(ctx context.Context, in KnowledgeMetadata, actor string) (*KnowledgeEntry, error) {
	in, err := NormalizeKnowledgeMetadata(in)
	if err != nil {
		return nil, err
	}
	if err := validateCatalogActor(actor); err != nil {
		return nil, err
	}
	refs, err := json.Marshal(in.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	k, err := scanKnowledge(tx.QueryRowContext(ctx, `INSERT INTO knowledge_catalog(title,kind,source_ref,source_version,sha256,license,evidence_refs) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) RETURNING `+knowledgeColumns, in.Title, in.Kind, in.SourceRef, in.SourceVersion, in.SHA256, in.License, string(refs)))
	if err != nil {
		return nil, catalogDBError(err)
	}
	if err := snapshotKnowledge(ctx, tx, k.ID, actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return k, nil
}
func lockKnowledge(ctx context.Context, tx *sql.Tx, id, revision int64) (*KnowledgeEntry, error) {
	if revision < 1 {
		return nil, catalogInvalid("positive revision is required")
	}
	k, err := scanKnowledge(tx.QueryRowContext(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_catalog WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	if k.Revision != revision {
		return nil, ErrCatalogConflict
	}
	if k.ReviewStatus == "revoked" {
		return nil, ErrCatalogRevoked
	}
	return k, nil
}

// UpdateKnowledge resets review when metadata changes. It never grants execution
// or changes existing skill bindings; a no-op preserves the current revision.
func (d *DB) UpdateKnowledge(ctx context.Context, id, revision int64, in KnowledgeMetadata, actor string) (*KnowledgeEntry, error) {
	in, err := NormalizeKnowledgeMetadata(in)
	if err != nil {
		return nil, err
	}
	if err := validateCatalogActor(actor); err != nil {
		return nil, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := lockKnowledge(ctx, tx, id, revision)
	if err != nil {
		return nil, err
	}
	if reflect.DeepEqual(current.KnowledgeMetadata, in) {
		return current, nil
	}
	refs, err := json.Marshal(in.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	k, err := scanKnowledge(tx.QueryRowContext(ctx, `UPDATE knowledge_catalog SET title=$2,kind=$3,source_ref=$4,source_version=$5,sha256=$6,license=$7,evidence_refs=$8::jsonb,review_status='draft',review_note='',revision=revision+1,updated_at=now() WHERE id=$1 RETURNING `+knowledgeColumns, id, in.Title, in.Kind, in.SourceRef, in.SourceVersion, in.SHA256, in.License, string(refs)))
	if err != nil {
		return nil, catalogDBError(err)
	}
	if err := snapshotKnowledge(ctx, tx, id, actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return k, nil
}
func (d *DB) ReviewKnowledge(ctx context.Context, id, revision int64, status, note, actor string) (*KnowledgeEntry, error) {
	note = strings.TrimSpace(note)
	if err := ValidateKnowledgeReview(status, note); err != nil {
		return nil, err
	}
	if err := validateCatalogActor(actor); err != nil {
		return nil, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := lockKnowledge(ctx, tx, id, revision)
	if err != nil {
		return nil, err
	}
	if status == "reviewed" && (current.SHA256 == "" || current.License == "") {
		return nil, catalogInvalid("a declared source hash and license are required before metadata review")
	}
	if current.ReviewStatus == status && current.ReviewNote == note {
		return current, nil
	}
	k, err := scanKnowledge(tx.QueryRowContext(ctx, `UPDATE knowledge_catalog SET review_status=$2,review_note=$3,revision=revision+1,updated_at=now() WHERE id=$1 RETURNING `+knowledgeColumns, id, status, note))
	if err != nil {
		return nil, err
	}
	if err := snapshotKnowledge(ctx, tx, id, actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return k, nil
}
func (d *DB) KnowledgeHistory(ctx context.Context, id int64, limit, offset int) ([]KnowledgeRevision, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, catalogInvalid("invalid history pagination")
	}
	if _, err := d.KnowledgeEntry(ctx, id); err != nil {
		return nil, err
	}
	rows, err := d.QueryContext(ctx, `SELECT snapshot,actor,recorded_at FROM knowledge_catalog_history WHERE entry_id=$1 ORDER BY revision DESC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []KnowledgeRevision{}
	for rows.Next() {
		var item KnowledgeRevision
		var raw []byte
		if err := rows.Scan(&raw, &item.Actor, &item.RecordedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Entry); err != nil {
			return nil, fmt.Errorf("decode catalog revision: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
