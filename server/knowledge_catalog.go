package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/Autumn-27/artex/db"
)

const maxKnowledgeBody = 32 << 10

// These routes are registered on Handler's existing authenticated API mux.
// No source fetching, file reads, imports, or agent tool bindings are added.
func (s *Server) registerKnowledgeRoutes(mux *http.ServeMux) {
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/knowledge-catalog", wrap(s.listKnowledge))
	mux.HandleFunc("POST /api/knowledge-catalog", wrap(s.createKnowledge))
	mux.HandleFunc("GET /api/knowledge-catalog/{id}", wrap(s.getKnowledge))
	mux.HandleFunc("PUT /api/knowledge-catalog/{id}", wrap(s.updateKnowledge))
	mux.HandleFunc("GET /api/knowledge-catalog/{id}/history", wrap(s.historyKnowledge))
	mux.HandleFunc("POST /api/knowledge-catalog/{id}/review", wrap(s.reviewKnowledge))
}

func decodeKnowledge(w http.ResponseWriter, r *http.Request, dst any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxKnowledgeBody)
	dec := json.NewDecoder(r.Body)
	var raw json.RawMessage
	err = dec.Decode(&raw)
	if err == nil {
		var extra any
		if next := dec.Decode(&extra); next != io.EOF {
			err = next
			if err == nil {
				err = errors.New("multiple JSON values")
			}
		}
	}
	if err == nil {
		if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
			err = errors.New("catalog JSON must be an object")
		} else {
			object := json.NewDecoder(bytes.NewReader(raw))
			object.DisallowUnknownFields()
			err = object.Decode(dst)
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "catalog JSON exceeds 32 KiB")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid catalog JSON: expected one object with known fields")
		}
		return false
	}
	return true
}
func knowledgeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "invalid catalog entry id")
		return 0, false
	}
	return id, true
}
func knowledgePage(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, offset := 50, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 100 {
		writeErr(w, 400, "limit must be between 1 and 100")
		return 0, 0, false
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
	}
	if err != nil || offset < 0 || offset > 1000000 {
		writeErr(w, 400, "offset must be between 0 and 1000000")
		return 0, 0, false
	}
	return limit, offset, true
}
func knowledgeError(w http.ResponseWriter, err error) {
	var invalid *db.CatalogValidationError
	switch {
	case errors.As(err, &invalid):
		writeErr(w, 400, invalid.Error())
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, 404, "catalog entry not found")
	case errors.Is(err, db.ErrCatalogConflict), errors.Is(err, db.ErrCatalogDuplicate), errors.Is(err, db.ErrCatalogRevoked):
		writeErr(w, 409, err.Error())
	default:
		writeErr(w, 500, "catalog operation failed")
	}
}
func (s *Server) listKnowledge(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := knowledgePage(w, r)
	if !ok {
		return
	}
	items, total, err := s.m.PG().ListKnowledge(r.Context(), db.KnowledgeFilter{Kind: r.URL.Query().Get("kind"), ReviewStatus: r.URL.Query().Get("review_status"), Limit: limit, Offset: offset})
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}
func (s *Server) getKnowledge(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	item, err := s.m.PG().KnowledgeEntry(r.Context(), id)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) createKnowledge(w http.ResponseWriter, r *http.Request) {
	var in db.KnowledgeMetadata
	if !decodeKnowledge(w, r, &in) {
		return
	}
	item, err := s.m.PG().CreateKnowledge(r.Context(), in, s.approver(r))
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 201, item)
}
func (s *Server) updateKnowledge(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	var in struct {
		db.KnowledgeMetadata
		Revision int64 `json:"revision"`
	}
	if !decodeKnowledge(w, r, &in) {
		return
	}
	item, err := s.m.PG().UpdateKnowledge(r.Context(), id, in.Revision, in.KnowledgeMetadata, s.approver(r))
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) reviewKnowledge(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	var in struct {
		Revision int64  `json:"revision"`
		Status   string `json:"status"`
		Note     string `json:"note"`
	}
	if !decodeKnowledge(w, r, &in) {
		return
	}
	item, err := s.m.PG().ReviewKnowledge(r.Context(), id, in.Revision, in.Status, in.Note, s.approver(r))
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) historyKnowledge(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	limit, offset, ok := knowledgePage(w, r)
	if !ok {
		return
	}
	items, err := s.m.PG().KnowledgeHistory(r.Context(), id, limit, offset)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "limit": limit, "offset": offset})
}
