package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestKnowledgeRoutesRequireAuthentication(t *testing.T) {
	s := &Server{}
	h := s.Handler()
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/knowledge-catalog"}, {"POST", "/api/knowledge-catalog"},
		{"GET", "/api/knowledge-catalog/1"}, {"PUT", "/api/knowledge-catalog/1"},
		{"GET", "/api/knowledge-catalog/1/history"}, {"POST", "/api/knowledge-catalog/1/review"},
	} {
		for _, token := range []string{"", "invalid-synthetic-token"} {
			r := httptest.NewRequest(endpoint.method, endpoint.path, nil)
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s: status %d", endpoint.method, endpoint.path, w.Code)
			}
		}
	}
}
func TestKnowledgeJSONBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"valid", "application/json", `{"title":"Synthetic"}`, 200},
		{"charset", "application/json; charset=utf-8", `{"title":"Synthetic"}`, 200},
		{"content type", "text/plain", `{"title":"Synthetic"}`, 415},
		{"unknown field", "application/json", `{"title":"Synthetic","enabled":true}`, 400},
		{"trailing value", "application/json", `{"title":"Synthetic"} {}`, 400},
		{"bad json", "application/json", `{"title":`, 400},
		{"null", "application/json", `null`, 400},
		{"array", "application/json", `[]`, 400},
		{"over limit", "application/json", `{"title":"` + strings.Repeat("a", maxKnowledgeBody) + `"}`, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/knowledge-catalog", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			var in struct {
				Title string `json:"title"`
			}
			ok := decodeKnowledge(w, r, &in)
			if tc.status == 200 {
				if !ok || in.Title != "Synthetic" {
					t.Fatalf("valid JSON rejected: %d", w.Code)
				}
			} else if ok || w.Code != tc.status {
				t.Fatalf("ok=%v status=%d want %d", ok, w.Code, tc.status)
			}
		})
	}
}
func TestKnowledgeAuthenticatedAPI(t *testing.T) {
	skipWithoutDSN(t)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	// Construct only the API surface: no engine, task, provider, startup key file,
	// setup token, external tools, or background server is created by this test.
	s := &Server{m: m, jwtKey: bytes.Repeat([]byte{1}, 32)}
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("HTTP %d want %d: %s", w.Code, status, w.Body.String())
		}
	}
	decode := func(w *httptest.ResponseRecorder) db.KnowledgeEntry {
		t.Helper()
		var k db.KnowledgeEntry
		if err := json.Unmarshal(w.Body.Bytes(), &k); err != nil {
			t.Fatal(err)
		}
		return k
	}
	in := db.KnowledgeMetadata{Title: "Synthetic API reference", Kind: "advisory", SourceRef: fmt.Sprintf("urn:artex:http:%d", time.Now().UnixNano()), SourceVersion: "1", SHA256: strings.Repeat("b", 64), License: "Synthetic test data", EvidenceRefs: []string{"urn:artex:synthetic:observation"}}
	w := call("POST", "/api/knowledge-catalog", in)
	check(w, 201)
	k := decode(w)
	t.Cleanup(func() {
		if _, err := m.pg.Exec(`DELETE FROM knowledge_catalog_history WHERE entry_id=$1`, k.ID); err != nil {
			t.Error(err)
		}
		if _, err := m.pg.Exec(`DELETE FROM knowledge_catalog WHERE id=$1`, k.ID); err != nil {
			t.Error(err)
		}
	})
	base := "/api/knowledge-catalog/" + strconv.FormatInt(k.ID, 10)
	if k.ReviewStatus != "draft" || k.Revision != 1 {
		t.Fatalf("unexpected creation state: %+v", k)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("metadata response can be cached")
	}
	check(call("POST", "/api/knowledge-catalog", in), 409)
	check(call("GET", base, nil), 200)
	w = call("GET", "/api/knowledge-catalog?kind=advisory&review_status=draft&limit=100", nil)
	check(w, 200)
	var list struct {
		Items []db.KnowledgeEntry `json:"items"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list.Items {
		found = found || item.ID == k.ID
	}
	if !found {
		t.Fatal("created entry not in list")
	}
	for _, path := range []string{"?limit=0", "?limit=101", "?offset=-1", "?offset=x", "?kind=unknown", "?review_status=confirmed"} {
		check(call("GET", "/api/knowledge-catalog"+path, nil), 400)
	}
	check(call("GET", "/api/knowledge-catalog/-1", nil), 400)
	check(call("GET", "/api/knowledge-catalog/9223372036854775807", nil), 404)
	check(call("GET", base+"/history?limit=0", nil), 400)
	check(call("POST", "/api/knowledge-catalog", map[string]any{"title": "Synthetic", "review_status": "reviewed"}), 400)
	bad := in
	bad.SourceRef = "file:///synthetic"
	check(call("POST", "/api/knowledge-catalog", bad), 400)
	check(call("POST", base+"/review", map[string]any{"revision": 1, "status": "confirmed", "note": "not a catalog state"}), 400)
	check(call("POST", base+"/review", map[string]any{"revision": 1, "status": "reviewed", "note": ""}), 400)
	w = call("POST", base+"/review", map[string]any{"revision": 1, "status": "reviewed", "note": "Source metadata checked"})
	check(w, 200)
	reviewed := decode(w)
	if reviewed.Revision != 2 || reviewed.ReviewStatus != "reviewed" {
		t.Fatalf("reviewed: %+v", reviewed)
	}
	check(call("POST", base+"/review", map[string]any{"revision": 1, "status": "revoked", "note": "stale"}), 409)
	metadata := in
	metadata.Title = "Updated synthetic metadata"
	w = call("PUT", base, struct {
		db.KnowledgeMetadata
		Revision int64 `json:"revision"`
	}{metadata, 2})
	check(w, 200)
	updated := decode(w)
	if updated.ReviewStatus != "draft" || updated.Revision != 3 {
		t.Fatalf("metadata did not invalidate review: %+v", updated)
	}
	check(call("PUT", base, struct {
		db.KnowledgeMetadata
		Revision int64 `json:"revision"`
	}{metadata, 2}), 409)
	w = call("POST", base+"/review", map[string]any{"revision": 3, "status": "revoked", "note": "Source withdrawn"})
	check(w, 200)
	check(call("PUT", base, struct {
		db.KnowledgeMetadata
		Revision int64 `json:"revision"`
	}{metadata, 4}), 409)
	check(call("POST", base+"/review", map[string]any{"revision": 4, "status": "draft", "note": ""}), 409)
	w = call("GET", base+"/history", nil)
	check(w, 200)
	var history struct {
		Items []db.KnowledgeRevision `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 4 || history.Items[0].Entry.ReviewStatus != "revoked" || history.Items[0].Actor != "ARTEX" {
		t.Fatalf("history/actor: %+v", history)
	}
	w = call("GET", base+"/history?limit=1&offset=1", nil)
	check(w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || history.Items[0].Entry.Revision != 3 {
		t.Fatal("history pagination failed")
	}
}
