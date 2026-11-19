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

func copyQualityFixture(t *testing.T, dir, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, name, "SKILL.md"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestQualitySkillAdaptersAreDiscoverableAndNonOperational(t *testing.T) {
	for _, name := range []string{"quality-evidence-hygiene", "quality-evidence-report"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			raw := copyQualityFixture(t, dir, name)
			captured, hash, err := captureManagedManifest(t.Context(), dir, name)
			if err != nil || !bytes.Equal(raw, captured) || hash != db.ManagedManifestHash(raw) {
				t.Fatalf("capture: %v", err)
			}
			sk, err := parseManagedManifest(name, captured)
			if err != nil || sk.Name != name || sk.License != "CC-BY-4.0" || sk.Dir != "" || len(sk.MCPs) != 0 {
				t.Fatalf("discovery: %v", err)
			}
			if !strings.Contains(sk.Instructions, "9bdb0a81658bc25f09c0b52d31fcde923d126e4f") || !strings.Contains(sk.Instructions, "未") {
				t.Fatal("attribution or explicit boundaries missing")
			}
		})
	}
}
func TestManagedRuntimeRejectsUnsupportedPackages(t *testing.T) {
	dir := t.TempDir()
	name := "quality-evidence-report"
	raw := copyQualityFixture(t, dir, name)
	if err := os.WriteFile(filepath.Join(dir, name, "resource.txt"), []byte("Synthetic extra resource"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureManagedManifest(t.Context(), dir, name); err == nil {
		t.Fatal("multi-file package silently treated as immutable manifest")
	}
	if _, err := parseManagedManifest(name, bytes.ReplaceAll(raw, []byte("name: "+name), []byte("name: another-name"))); err == nil {
		t.Fatal("directory/name mismatch accepted")
	}
	withMCP := bytes.Replace(raw, []byte("description:"), []byte("mcps: [offline-not-started]\ndescription:"), 1)
	if _, err := parseManagedManifest(name, withMCP); err == nil {
		t.Fatal("MCP package was accepted or could be started")
	}
	if _, err := parseManagedManifest(name, append(append([]byte{}, raw...), []byte("\n${SKILL_DIR}\n")...)); err == nil {
		t.Fatal("resource dependency accepted")
	}
}
func TestManagedRuntimeActualAgentAssemblyAndRevocation(t *testing.T) {
	skipWithoutDSN(t)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	old := agent.ToolAugment()
	t.Cleanup(func() { agent.SetToolAugment(old) })
	name := "quality-evidence-report"
	dir := t.TempDir()
	raw := copyQualityFixture(t, dir, name)
	a, err := m.pg.GetAgentByKey("planner")
	if err != nil || a == nil {
		t.Fatal(err)
	}
	names, err := m.pg.AgentSkillNames(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == name {
			t.Fatal("adapted skill was automatically enabled")
		}
	}
	task, err := m.pg.CreateTaskWithOptions("Offline skill consistency fixture", "No external test", db.TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.pg.DeleteTask(task.ID)
		for _, table := range []string{"managed_skill_runtime_pins", "managed_skill_events", "managed_skill_bindings", "managed_skill_versions", "agent_skill_visibility"} {
			if _, err := m.pg.Exec(`DELETE FROM `+table+` WHERE skill_name=$1`, name); err != nil {
				t.Error(err)
			}
		}
		m.pg.Exec(`DELETE FROM skill_usage WHERE skill=$1`, name)
	})
	hash := db.ManagedManifestHash(raw)
	v, err := m.pg.RecordManagedSkill(t.Context(), db.ManagedSkillVersion{SkillName: name, Label: "test-v1", SourceRef: "urn:artex:synthetic:adaptation", License: "CC-BY-4.0", ContentHash: hash, FileCount: 1, TotalBytes: int64(len(raw))}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.pg.ToggleSkillVisibility(a.ID, name, true); err != nil {
		t.Fatal(err)
	}
	if err = m.pg.BindManagedSkill(t.Context(), name, "planner", v.ID, 0, hash, "operator"); err != nil {
		t.Fatal(err)
	}
	wireAgentAugment(m.pg, dir, nil)
	ctx := agent.WithRunInfo(t.Context(), agent.RunInfo{TaskID: task.ID, ExplorationID: task.ExplorationID})
	assemble := func() (actool.CoreTool, func(), error) {
		tools, def, cleanup := agent.AugmentTools(ctx, "planner", nil)
		for _, tool := range tools {
			if tool.Name() == "Skill" {
				return tool, cleanup, def.AssemblyError
			}
		}
		return nil, cleanup, def.AssemblyError
	}
	tool, cleanup, err := assemble()
	if err != nil || tool == nil {
		t.Fatalf("real assembly failed: %v", err)
	}
	defer cleanup()
	result, err := tool.Call(ctx, json.RawMessage(`{"name":"quality-evidence-report"}`), nil)
	if err != nil || result.IsError || len(result.Extra) == 0 {
		t.Fatalf("real Skill invocation did not return adapted content: %v", err)
	}
	encoded, _ := json.Marshal(result.Extra)
	if !bytes.Contains(encoded, []byte("9bdb0a81658bc25f09c0b52d31fcde923d126e4f")) {
		t.Fatal("wrong instructions loaded")
	}
	// A new assembly for the same task restores exactly the existing snapshot.
	restored, closeRestored, err := assemble()
	if err != nil || restored == nil {
		t.Fatalf("restore: %v", err)
	}
	defer closeRestored()
	pins, err := m.pg.RuntimeSkillPins(ctx, fmt.Sprintf("task:%d", task.ID), "planner")
	if err != nil || len(pins) != 1 || pins[0].VersionID != v.ID || pins[0].Hash != hash || !bytes.Equal(pins[0].Manifest, raw) {
		t.Fatalf("actual pin mismatch: %v", err)
	}
	s := &Server{m: m, jwtKey: bytes.Repeat([]byte{1}, 32)}
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", fmt.Sprintf("/api/operator/tasks/%d/skill-runtime", task.ID), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"manifest":`)) || bytes.Contains(w.Body.Bytes(), raw) {
		t.Fatal("metadata API unavailable or exposes body")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"stage":"loaded"`)) {
		t.Fatal("actual load not recorded")
	}
	changed := append(append([]byte{}, raw...), []byte("\nChanged fixture\n")...)
	if err = os.WriteFile(filepath.Join(dir, name, "SKILL.md"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	blocked, err := tool.Call(ctx, json.RawMessage(`{"name":"quality-evidence-report"}`), nil)
	if err != nil || !blocked.IsError || len(blocked.Extra) != 0 {
		t.Fatal("source change did not withhold instructions")
	}
	if _, cl, err := assemble(); err == nil {
		cl()
		t.Fatal("changed content did not reject actual agent assembly")
	} else {
		cl()
	}
	if err = os.WriteFile(filepath.Join(dir, name, "SKILL.md"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Missing source blocks both a previously assembled tool and restoration.
	manifestPath := filepath.Join(dir, name, "SKILL.md")
	if err = os.Rename(manifestPath, manifestPath+".missing"); err != nil {
		t.Fatal(err)
	}
	blocked, err = tool.Call(ctx, json.RawMessage(`{"name":"quality-evidence-report"}`), nil)
	if err != nil || !blocked.IsError || len(blocked.Extra) != 0 {
		t.Fatal("missing source did not withhold instructions")
	}
	if _, cl, err := assemble(); err == nil {
		cl()
		t.Fatal("missing source accepted during assembly")
	} else {
		cl()
	}
	if err = os.Rename(manifestPath+".missing", manifestPath); err != nil {
		t.Fatal(err)
	}
	// Clearing a management binding must not downgrade an already-pinned task.
	if err = m.pg.BindManagedSkill(ctx, name, "planner", 0, 1, "", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, cl, err := assemble(); err == nil {
		cl()
		t.Fatal("cleared binding downgraded a pinned task")
	} else {
		cl()
	}
	if err = m.pg.BindManagedSkill(ctx, name, "planner", v.ID, 2, hash, "operator"); err != nil {
		t.Fatal(err)
	}
	if err = m.pg.ToggleSkillVisibility(a.ID, name, false); err != nil {
		t.Fatal(err)
	}
	if _, cl, err := assemble(); err == nil {
		cl()
		t.Fatal("hidden skill was restored into pinned task")
	} else {
		cl()
	}
	if err = m.pg.ToggleSkillVisibility(a.ID, name, true); err != nil {
		t.Fatal(err)
	}
	if err = m.pg.RevokeManagedSkill(ctx, name, v.ID, 1, "Synthetic source revocation", "operator"); err != nil {
		t.Fatal(err)
	}
	blocked, err = tool.Call(ctx, json.RawMessage(`{"name":"quality-evidence-report"}`), nil)
	if err != nil || !blocked.IsError {
		t.Fatal("revocation did not block an existing tool wrapper")
	}
	if _, cl, err := assemble(); err == nil {
		cl()
		t.Fatal("revocation ignored by actual assembly")
	} else {
		cl()
	}
}

func TestManagedRuntimePreservesUnmanagedSkillLoading(t *testing.T) {
	skipWithoutDSN(t)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	old := agent.ToolAugment()
	t.Cleanup(func() { agent.SetToolAugment(old) })
	name := "runtime-legacy-fixture"
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("---\nname: runtime-legacy-fixture\ndescription: Offline compatibility fixture\n---\nOnly summarize already provided synthetic evidence.\n")
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := m.pg.GetAgentByKey("planner")
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if err := m.pg.ToggleSkillVisibility(a.ID, name, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.pg.Exec(`DELETE FROM agent_skill_visibility WHERE agent_id=$1 AND skill_name=$2`, a.ID, name)
		m.pg.Exec(`DELETE FROM skill_usage WHERE skill=$1`, name)
	})
	wireAgentAugment(m.pg, dir, nil)
	// No task/session attribution was historically required for unmanaged skills.
	tools, def, cleanup := agent.AugmentTools(t.Context(), "planner", nil)
	defer cleanup()
	if def.AssemblyError != nil {
		t.Fatal(def.AssemblyError)
	}
	for _, tool := range tools {
		if tool.Name() != "Skill" {
			continue
		}
		result, err := tool.Call(t.Context(), json.RawMessage(`{"name":"runtime-legacy-fixture"}`), nil)
		if err != nil || result.IsError || len(result.Extra) == 0 {
			t.Fatalf("unmanaged compatibility: %v", err)
		}
		return
	}
	t.Fatal("legacy Skill tool was removed")
}

func TestManagedRuntimeConfigurationFailureStopsAssembly(t *testing.T) {
	skipWithoutDSN(t)
	dsn, _, err := db.DSN()
	if err != nil {
		t.Fatal(err)
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Close(); err != nil {
		t.Fatal(err)
	}
	old := agent.ToolAugment()
	t.Cleanup(func() { agent.SetToolAugment(old) })
	wireAgentAugment(pg, t.TempDir(), nil)
	tools, def, cleanup := agent.AugmentTools(t.Context(), "planner", nil)
	defer cleanup()
	if def.AssemblyError == nil || len(tools) != 0 {
		t.Fatal("unavailable skill configuration was silently ignored")
	}
}
