package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestPhase2RoutesRequireAuthentication(t *testing.T) {
	h := (&Server{}).Handler()
	for _, route := range []struct{ method, path string }{{"GET", "/api/managed-skills"}, {"GET", "/api/managed-skills/sample"}, {"POST", "/api/managed-skills/sample"}, {"PUT", "/api/managed-skills/sample/bindings/planner"}, {"POST", "/api/managed-skills/sample/versions/1/revoke"}, {"GET", "/api/operator/findings/1"}, {"POST", "/api/operator/findings/1"}, {"GET", "/api/operator/tasks/1"}, {"GET", "/api/operator/tasks/1/report"}, {"GET", "/api/operator/tasks/1/skill-runtime"}, {"GET", "/api/operator/tasks/1/coverage-key"}, {"POST", "/api/operator/tasks/1/coverage"}} {
		for _, token := range []string{"", "invalid-synthetic-token"} {
			r := httptest.NewRequest(route.method, route.path, nil)
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("%s %s: %d", route.method, route.path, w.Code)
			}
		}
	}
}
func TestPhase2AuthenticatedManagementAPI(t *testing.T) {
	skipWithoutDSN(t)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	dir := t.TempDir()
	name := fmt.Sprintf("sample-%d", time.Now().UnixNano())
	if err = os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, name, "SKILL.md")
	if err = os.WriteFile(manifest, []byte("---\nname: "+name+"\ndescription: Synthetic fixture only.\nlicense: Synthetic fixture\n---\nNon-executable fixture.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{m: m, skillDir: dir, jwtKey: bytes.Repeat([]byte{1}, 32)}
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
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
		if w.Code != status {
			t.Fatalf("%s %s: HTTP %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	base := "/api/managed-skills/" + name
	t.Cleanup(func() {
		for _, table := range []string{"managed_skill_events", "managed_skill_bindings", "managed_skill_versions", "agent_skill_visibility"} {
			if _, err := m.pg.Exec(`DELETE FROM `+table+` WHERE skill_name=$1`, name); err != nil {
				t.Error(err)
			}
		}
	})
	input := map[string]any{"label": "v1", "source_ref": "urn:artex:synthetic:source", "license": "Synthetic fixture"}
	bad := map[string]any{"label": "v1", "source_ref": "urn:artex:synthetic:source", "license": "Synthetic fixture", "content_hash": "cannot-self-declare"}
	call("POST", base, bad, 400)
	call("POST", "/api/managed-skills/UPPER", input, 400)
	call("POST", "/api/managed-skills/missing-fixture", input, 400)
	w := call("POST", base, input, 201)
	var version db.ManagedSkillVersion
	if err = json.Unmarshal(w.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if len(version.ContentHash) != 64 || version.Actor != "ARTEX" {
		t.Fatal("digest or authenticated actor missing")
	}
	call("POST", base, input, 409)
	index := call("GET", "/api/managed-skills", nil, 200)
	if !bytes.Contains(index.Body.Bytes(), []byte(name)) {
		t.Fatal("managed record not discoverable")
	}

	agent, err := m.pg.GetAgentByKey("planner")
	if err != nil || agent == nil {
		t.Fatal(err)
	}
	bind := map[string]any{"version_id": version.ID, "revision": 0}
	call("PUT", base+"/bindings/unknown-key", bind, 404)
	call("PUT", base+"/bindings/planner", bind, 400)
	if err = m.pg.ToggleSkillVisibility(agent.ID, name, true); err != nil {
		t.Fatal(err)
	}
	call("PUT", base+"/bindings/planner", bind, 200)
	call("PUT", base+"/bindings/planner", bind, 409)
	w = call("GET", base, nil, 200)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("metadata cached")
	}
	var view struct {
		RuntimeLocking bool                     `json:"runtime_locking"`
		Checks         map[string]string        `json:"checks"`
		Agents         []db.ManagedSkillBinding `json:"agents"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.RuntimeLocking || view.Checks[strconv.FormatInt(version.ID, 10)] != "matches" {
		t.Fatal("incorrect lock/digest claim")
	}
	if err = os.WriteFile(manifest, []byte("Changed harmless fixture."), 0600); err != nil {
		t.Fatal(err)
	}
	call("PUT", base+"/bindings/planner", map[string]any{"version_id": version.ID, "revision": 1}, 400)
	w = call("GET", base, nil, 200)
	if err = json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Checks[strconv.FormatInt(version.ID, 10)] != "content_changed" {
		t.Fatal("change not exposed")
	}
	if err = os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	call("PUT", base+"/bindings/planner", map[string]any{"version_id": version.ID, "revision": 1}, 400)
	call("POST", fmt.Sprintf("%s/versions/%d/revoke", base, version.ID), map[string]any{"revision": 1, "reason": "Synthetic source withdrawn"}, 200)
	call("PUT", base+"/bindings/planner", map[string]any{"version_id": 0, "revision": 1}, 200)
	task, err := m.pg.CreateTaskWithOptions("Synthetic review fixture", "No execution", db.TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.pg.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	var finding int64
	if err = m.pg.QueryRow(`INSERT INTO findings(task_id,name,summary,severity,vulnclass,status) VALUES($1,'Synthetic finding','Synthetic fixture','low','config','pending') RETURNING id`, task.ID).Scan(&finding); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := m.pg.Exec(`DELETE FROM findings WHERE id=$1`, finding); err != nil {
			t.Error(err)
		}
	})
	findingPath := fmt.Sprintf("/api/operator/findings/%d", finding)
	w = call("GET", findingPath, nil, 200)
	var state db.FindingReviewState
	if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	call("POST", findingPath, map[string]any{"fingerprint": state.Fingerprint, "decision": "confirmed", "reason": "No evidence", "evidence_refs": []string{}}, 400)
	call("POST", findingPath, map[string]any{"fingerprint": state.Fingerprint, "decision": "unknown", "reason": "No unknown state"}, 400)
	call("POST", findingPath, map[string]any{"fingerprint": state.Fingerprint, "decision": "candidate", "reason": "No actor injection", "actor": "someone-else"}, 400)
	w = call("POST", findingPath, map[string]any{"fingerprint": state.Fingerprint, "decision": "confirmed", "reason": "Human fixture review", "evidence_refs": []string{"urn:artex:synthetic:evidence"}}, 200)
	var reviewed db.FindingReviewState
	if err = json.Unmarshal(w.Body.Bytes(), &reviewed); err != nil {
		t.Fatal(err)
	}
	if reviewed.Status != "confirmed" || reviewed.Verification != state.Verification {
		t.Fatal("human review promoted machine verification")
	}
	call("POST", findingPath, map[string]any{"fingerprint": state.Fingerprint, "decision": "candidate", "reason": "stale"}, 409)
	taskPath := fmt.Sprintf("/api/operator/tasks/%d", task.ID)
	call("GET", taskPath+"?work_state=unknown", nil, 400)
	key := taskPath + "/coverage-key?surface=synthetic-surface&risk_area=configuration"
	w = call("GET", key, nil, 200)
	var snapshot db.CoverageReviewState
	if err = json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	annotation := map[string]any{"surface": snapshot.Surface, "risk_area": snapshot.RiskArea, "fingerprint": snapshot.Fingerprint, "state": "blocked", "reason": "Synthetic blocked work", "evidence_refs": []string{}}
	call("POST", taskPath+"/coverage", annotation, 200)
	call("POST", taskPath+"/coverage", annotation, 409)
	w = call("GET", taskPath+"?work_state=blocked", nil, 200)
	var desk struct {
		RunScope string             `json:"run_scope"`
		History  []json.RawMessage  `json:"coverage_reviews"`
		Ledger   *db.CoverageLedger `json:"ledger"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &desk); err != nil {
		t.Fatal(err)
	}
	if desk.RunScope != "task_history_not_run_verified" || len(desk.History) != 1 || len(desk.Ledger.Entries) != 0 {
		t.Fatal("history filtered incorrectly or annotation rewrote ledger")
	}
	w = call("GET", taskPath+"/report", nil, 200)
	if w.Header().Get("Content-Disposition") == "" {
		t.Fatal("report attachment unavailable")
	}
	for _, path := range []string{"/api/operator/tasks/9223372036854775807", "/api/operator/findings/9223372036854775807", "/api/operator/tasks/9223372036854775807/coverage-key?surface=synthetic&risk_area=config"} {
		call("GET", path, nil, 404)
	}
}
