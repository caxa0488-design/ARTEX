package server

import (
	"encoding/json"
	"github.com/Autumn-27/artex/db"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) registerPhase2Routes(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		"GET /api/managed-skills":                                   s.listManagedSkills,
		"GET /api/managed-skills/{name}":                            s.viewManagedSkill,
		"POST /api/managed-skills/{name}":                           s.recordManagedSkill,
		"PUT /api/managed-skills/{name}/bindings/{key}":             s.bindManagedSkill,
		"POST /api/managed-skills/{name}/versions/{version}/revoke": s.revokeManagedSkill,
		"GET /api/operator/findings/{id}":                           s.operatorFinding,
		"POST /api/operator/findings/{id}":                          s.operatorFinding,
		"GET /api/operator/tasks/{id}":                              s.operatorTask,
		"GET /api/operator/tasks/{id}/report":                       s.operatorTask,
		"GET /api/operator/tasks/{id}/coverage-key":                 s.operatorCoverage,
		"POST /api/operator/tasks/{id}/coverage":                    s.operatorCoverage,
	}
	for route, h := range routes {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Cache-Control", "no-store"); h(w, r) })
	}
}
func (s *Server) listManagedSkills(w http.ResponseWriter, r *http.Request) {
	names, err := s.m.pg.ManagedSkillNames(r.Context())
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"names": names, "limit": 500, "possibly_truncated": len(names) == 500})
}
func managedName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if !validSkillName(name) {
		writeErr(w, 400, "invalid skill directory name")
		return "", false
	}
	return name, true
}
func (s *Server) viewManagedSkill(w http.ResponseWriter, r *http.Request) {
	name, ok := managedName(w, r)
	if !ok {
		return
	}
	versions, bindings, err := s.m.pg.ManagedSkillVersions(r.Context(), name)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	digest, digestErr := digestManagedSkill(r.Context(), s.skillDir, name)
	checks := map[int64]string{}
	for _, v := range versions {
		state := "matches"
		if digestErr != nil {
			state = "unavailable"
		} else if v.ContentHash != digest.Hash {
			state = "content_changed"
		}
		if v.Revoked {
			state = "revoked"
		}
		checks[v.ID] = state
	}
	// Older bound versions can fall outside the 100-version list: report their
	// checks explicitly rather than presenting an uninspected binding as valid.
	for _, b := range bindings {
		if b.VersionID == nil {
			continue
		}
		if _, ok := checks[*b.VersionID]; ok {
			continue
		}
		v, e := s.m.pg.ManagedSkillVersion(r.Context(), *b.VersionID)
		if e != nil {
			checks[*b.VersionID] = "unavailable"
			continue
		}
		state := "matches"
		if digestErr != nil {
			state = "unavailable"
		} else if v.ContentHash != digest.Hash {
			state = "content_changed"
		}
		if v.Revoked {
			state = "revoked"
		}
		checks[v.ID] = state
	}
	events, err := s.m.pg.ManagedSkillEvents(r.Context(), name)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	digestError := ""
	if digestErr != nil {
		digestError = digestErr.Error()
	}
	writeJSON(w, 200, map[string]any{"versions": versions, "agents": bindings, "checks": checks, "current": digest, "current_error": digestError, "events": events, "runtime_locking": false, "limits": "Latest 100 versions/events. Management bindings do not enable, disable, or freeze runtime skills; verify changes explicitly."})
}
func (s *Server) recordManagedSkill(w http.ResponseWriter, r *http.Request) {
	name, ok := managedName(w, r)
	if !ok {
		return
	}
	var in struct {
		Label     string `json:"label"`
		SourceRef string `json:"source_ref"`
		License   string `json:"license"`
	}
	if !decodeKnowledge(w, r, &in) {
		return
	}
	digest, err := digestManagedSkill(r.Context(), s.skillDir, name)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	v, err := s.m.pg.RecordManagedSkill(r.Context(), db.ManagedSkillVersion{SkillName: name, Label: in.Label, SourceRef: in.SourceRef, License: in.License, ContentHash: digest.Hash, FileCount: digest.Files, TotalBytes: digest.Bytes}, s.approver(r))
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) bindManagedSkill(w http.ResponseWriter, r *http.Request) {
	name, ok := managedName(w, r)
	if !ok {
		return
	}
	var in struct {
		VersionID int64 `json:"version_id"`
		Revision  int64 `json:"revision"`
	}
	if !decodeKnowledge(w, r, &in) {
		return
	}
	hash := ""
	if in.VersionID > 0 {
		digest, err := digestManagedSkill(r.Context(), s.skillDir, name)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		hash = digest.Hash
	}
	if err := s.m.pg.BindManagedSkill(r.Context(), name, r.PathValue("key"), in.VersionID, in.Revision, hash, s.approver(r)); err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "runtime_locking": false})
}
func (s *Server) revokeManagedSkill(w http.ResponseWriter, r *http.Request) {
	name, ok := managedName(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || id < 1 {
		writeErr(w, 400, "invalid version id")
		return
	}
	var in struct {
		Revision int64  `json:"revision"`
		Reason   string `json:"reason"`
	}
	if !decodeKnowledge(w, r, &in) {
		return
	}
	if err := s.m.pg.RevokeManagedSkill(r.Context(), name, id, in.Revision, in.Reason, s.approver(r)); err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) operatorFinding(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	if r.Method == "POST" {
		var in struct {
			Fingerprint  string   `json:"fingerprint"`
			Decision     string   `json:"decision"`
			Reason       string   `json:"reason"`
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if !decodeKnowledge(w, r, &in) {
			return
		}
		if err := s.m.pg.ReviewFinding(r.Context(), id, in.Fingerprint, in.Decision, in.Reason, in.EvidenceRefs, s.approver(r)); err != nil {
			knowledgeError(w, err)
			return
		}
	}
	state, err := s.m.pg.FindingReviewState(r.Context(), id)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, state)
}
func (s *Server) operatorCoverage(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	surface, risk := r.URL.Query().Get("surface"), r.URL.Query().Get("risk_area")
	if r.Method == "POST" {
		var in struct {
			Surface      string   `json:"surface"`
			RiskArea     string   `json:"risk_area"`
			Fingerprint  string   `json:"fingerprint"`
			State        string   `json:"state"`
			Reason       string   `json:"reason"`
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if !decodeKnowledge(w, r, &in) {
			return
		}
		surface, risk = in.Surface, in.RiskArea
		if err := s.m.pg.ReviewCoverage(r.Context(), id, surface, risk, in.Fingerprint, in.State, in.Reason, in.EvidenceRefs, s.approver(r)); err != nil {
			knowledgeError(w, err)
			return
		}
	}
	state, err := s.m.pg.CoverageReviewState(r.Context(), id, surface, risk)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	writeJSON(w, 200, state)
}
func (s *Server) operatorTask(w http.ResponseWriter, r *http.Request) {
	id, ok := knowledgeID(w, r)
	if !ok {
		return
	}
	task, err := s.m.pg.GetTask(id)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	if task == nil {
		writeErr(w, 404, "task not found")
		return
	}
	ledger, err := s.m.pg.Assets().CoverageLedger(id)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	history, err := s.m.pg.CoverageReviewHistory(r.Context(), id, r.URL.Query().Get("work_state"))
	if err != nil {
		knowledgeError(w, err)
		return
	}
	rows, err := s.m.pg.QueryContext(r.Context(), `SELECT id,name,status,verification_status FROM findings WHERE task_id=$1 ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		knowledgeError(w, err)
		return
	}
	findings := []map[string]any{}
	for rows.Next() {
		var fid int64
		var name, status, verification string
		if err = rows.Scan(&fid, &name, &status, &verification); err != nil {
			break
		}
		findings = append(findings, map[string]any{"id": fid, "name": name, "status": status, "verification_status": verification})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		knowledgeError(w, err)
		return
	}
	reviews := []json.RawMessage{}
	for _, f := range findings {
		if len(reviews) >= 100 {
			break
		}
		state, err := s.m.pg.FindingReviewState(r.Context(), f["id"].(int64))
		if err != nil {
			knowledgeError(w, err)
			return
		}
		for _, raw := range state.History {
			var h map[string]any
			if err := json.Unmarshal(raw, &h); err != nil {
				knowledgeError(w, err)
				return
			}
			h["stale"] = h["result_fingerprint"] != state.Fingerprint
			v, _ := json.Marshal(h)
			reviews = append(reviews, v)
			if len(reviews) >= 100 {
				break
			}
		}
	}
	if strings.HasSuffix(r.URL.Path, "/report") {
		w.Header().Set("Content-Disposition", `attachment; filename="task-`+strconv.FormatInt(id, 10)+`-operator-review.json"`)
	}
	writeJSON(w, 200, map[string]any{"task_id": id, "ledger": ledger, "coverage_reviews": history, "findings": findings, "finding_reviews": reviews, "run_scope": "task_history_not_run_verified", "limits": "Up to 100 findings, 100 finding reviews grouped by finding, and 100 coverage annotations; truncated histories are not complete. Operator annotations never set asset-tested flags or prove current-run execution. Existing ledger gaps remain authoritative."})
}
