package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// This exercises the installed manifest, authenticated management API, actual
// host assembly, Norma Skill tool and persisted runtime pin without any model.
func TestSkillContentBatch2ManagementAndRuntime(t *testing.T) {
	skipWithoutDSN(t)
	for _, sample := range []struct{ name, license, marker string }{
		{"quality-scope-brief", "CC-BY-4.0", "scope_brief/v1"},
		{"quality-independent-review", "CC-BY-4.0", "review_packet/v1"},
		{"quality-task-handoff", "AGPL-3.0-only", "task_handoff/v1"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			old := agent.ToolAugment()
			t.Cleanup(func() { agent.SetToolAugment(old) })
			m, err := NewManager(t.TempDir(), "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { m.Close() })
			dir := t.TempDir()
			raw := copyQualityFixture(t, dir, sample.name)
			files, err := os.ReadDir(filepath.Join(dir, sample.name))
			if err != nil || len(files) != 1 || files[0].Name() != "SKILL.md" {
				t.Fatal("runtime resource missing or unexpectedly introduced")
			}
			sk, err := parseManagedManifest(sample.name, raw)
			if err != nil || sk.License != sample.license || sk.Dir != "" || len(sk.MCPs) != 0 {
				t.Fatalf("native discovery: %v", err)
			}
			a, err := m.pg.GetAgentByKey("planner")
			if err != nil || a == nil {
				t.Fatal(err)
			}
			task, err := m.pg.CreateTaskWithOptions("Offline content integration", "Only supplied synthetic records", db.TaskCreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				m.pg.DeleteTask(task.ID)
				for _, table := range []string{"managed_skill_runtime_pins", "managed_skill_events", "managed_skill_bindings", "managed_skill_versions", "agent_skill_visibility"} {
					if _, err := m.pg.Exec(`DELETE FROM `+table+` WHERE skill_name=$1`, sample.name); err != nil {
						t.Error(err)
					}
				}
				m.pg.Exec(`DELETE FROM skill_usage WHERE skill=$1`, sample.name)
			})
			s := &Server{m: m, skillDir: dir, jwtKey: bytes.Repeat([]byte{7}, 32)}
			token, err := signJWT(s.jwtKey)
			if err != nil {
				t.Fatal(err)
			}
			handler := s.Handler()
			request := func(method, path string, payload any, authenticated bool) *httptest.ResponseRecorder {
				t.Helper()
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(method, path, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if authenticated {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				return w
			}
			path := "/api/managed-skills/" + sample.name
			payload := map[string]any{"label": "batch2-v1", "source_ref": "urn:artex:offline-content:" + sample.name, "license": sample.license}
			if w := request("POST", path, payload, false); w.Code != 401 {
				t.Fatal("registration missing authentication")
			}
			w := request("POST", path, payload, true)
			if w.Code != 201 {
				t.Fatalf("registration status %d", w.Code)
			}
			var version db.ManagedSkillVersion
			if err := json.Unmarshal(w.Body.Bytes(), &version); err != nil || version.ID == 0 {
				t.Fatalf("version response: %v", err)
			}
			hash := db.ManagedManifestHash(raw)
			if version.ContentHash != hash || version.FileCount != 1 || version.TotalBytes != int64(len(raw)) {
				t.Fatal("management hash does not identify installed content")
			}
			binding := map[string]any{"version_id": version.ID, "revision": 0}
			if w := request("PUT", path+"/bindings/planner", binding, false); w.Code != 401 {
				t.Fatal("binding missing authentication")
			}
			if w := request("PUT", path+"/bindings/planner", binding, true); w.Code != 400 {
				t.Fatalf("hidden skill binding should be rejected without enabling it: %d", w.Code)
			}
			names, err := m.pg.AgentSkillNames(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range names {
				if n == sample.name {
					t.Fatal("installation/registration/binding enabled a skill")
				}
			}
			wireAgentAugment(m.pg, dir, nil)
			ctx := agent.WithRunInfo(t.Context(), agent.RunInfo{TaskID: task.ID, ExplorationID: task.ExplorationID})
			initial, def, closeInitial := agent.AugmentTools(ctx, "planner", nil)
			closeInitial()
			if def.AssemblyError != nil {
				t.Fatal(def.AssemblyError)
			}
			for _, tool := range initial {
				if tool.Name() == "Skill" {
					t.Fatal("default-disabled content entered agent registry")
				}
			}
			if err := m.pg.ToggleSkillVisibility(a.ID, sample.name, true); err != nil {
				t.Fatal(err)
			}
			if w := request("PUT", path+"/bindings/planner", binding, true); w.Code != 200 {
				t.Fatalf("explicit visible binding status %d", w.Code)
			}
			tools, def, cleanup := agent.AugmentTools(ctx, "planner", nil)
			defer cleanup()
			if def.AssemblyError != nil {
				t.Fatal(def.AssemblyError)
			}
			var selected actool.CoreTool
			for _, tool := range tools {
				if tool.Name() == "Skill" {
					selected = tool
				}
			}
			if selected == nil {
				t.Fatal("explicitly enabled/bound content not discoverable")
			}
			input, _ := json.Marshal(map[string]string{"name": sample.name})
			result, err := selected.Call(ctx, input, nil)
			if err != nil || result.IsError || len(result.Extra) == 0 {
				t.Fatalf("managed Skill load failed: %v", err)
			}
			rendered, _ := json.Marshal(result.Extra)
			if !strings.Contains(string(rendered), sample.marker) {
				t.Fatal("actual guidance not returned")
			}
			pins, err := m.pg.RuntimeSkillPins(ctx, fmt.Sprintf("task:%d", task.ID), "planner")
			if err != nil || len(pins) != 1 || pins[0].VersionID != version.ID || pins[0].Hash != hash || !bytes.Equal(pins[0].Manifest, raw) {
				t.Fatalf("persistent snapshot mismatch: %v", err)
			}
			metadata, err := m.pg.RuntimeSkillMetadata(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(metadata)
			if !bytes.Contains(encoded, []byte(`"stage":"loaded"`)) || bytes.Contains(encoded, []byte(`"manifest":`)) {
				t.Fatal("runtime metadata/loaded event incorrect")
			}
			if err := os.WriteFile(filepath.Join(dir, sample.name, "SKILL.md"), append(append([]byte{}, raw...), []byte("\nSynthetic changed source\n")...), 0600); err != nil {
				t.Fatal(err)
			}
			rejected, err := selected.Call(ctx, input, nil)
			if err != nil || !rejected.IsError || len(rejected.Extra) != 0 {
				t.Fatal("changed source was not rejected")
			}
			if err := os.WriteFile(filepath.Join(dir, sample.name, "SKILL.md"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if w := request("POST", fmt.Sprintf("%s/versions/%d/revoke", path, version.ID), map[string]any{"revision": version.Revision, "reason": "Offline fixture revoked"}, true); w.Code != 200 {
				t.Fatalf("revoke status %d", w.Code)
			}
			rejected, err = selected.Call(ctx, input, nil)
			if err != nil || !rejected.IsError || len(rejected.Extra) != 0 {
				t.Fatal("revoked content was not rejected")
			}
			_, blocked, closeBlocked := agent.AugmentTools(ctx, "planner", nil)
			defer closeBlocked()
			if blocked.AssemblyError == nil {
				t.Fatal("revoked content restored during actual assembly")
			}
		})
	}
}
