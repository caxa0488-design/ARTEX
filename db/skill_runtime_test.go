package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func runtimeFixture(t *testing.T, d *DB) (ManagedSkillVersion, []byte) {
	t.Helper()
	in := phase2Skill(t, d)
	raw := []byte("---\nname: " + in.SkillName + "\ndescription: Synthetic quality fixture.\nlicense: Synthetic fixture\n---\nOnly inspect provided synthetic evidence.\n")
	in.ContentHash = ManagedManifestHash(raw)
	in.TotalBytes = int64(len(raw))
	v, err := d.RecordManagedSkill(t.Context(), in, "operator")
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.GetAgentByKey("planner")
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if err = d.ToggleSkillVisibility(a.ID, in.SkillName, true); err != nil {
		t.Fatal(err)
	}
	if err = d.BindManagedSkill(t.Context(), in.SkillName, "planner", v.ID, 0, v.ContentHash, "operator"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := d.Exec(`DELETE FROM managed_skill_runtime_pins WHERE skill_name=$1`, in.SkillName); err != nil {
			t.Error(err)
		}
	})
	return *v, raw
}
func TestRuntimeSkillPinPersistenceAndAuthorization(t *testing.T) {
	d := catalogTestDB(t)
	ctx := t.Context()
	task := phase2Task(t, d)
	v, raw := runtimeFixture(t, d)
	input := RuntimeSkillPin{TaskID: task.ID, AgentKey: "planner", Name: v.SkillName, VersionID: v.ID, Hash: v.ContentHash, Manifest: raw}
	p, err := d.PinRuntimeSkill(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.PinRuntimeSkill(ctx, input)
	if err != nil || again.ID != p.ID {
		t.Fatalf("idempotent restore: %v", err)
	}
	if err = d.RecordRuntimeSkillLoaded(ctx, p); err != nil {
		t.Fatal(err)
	}
	metadata, err := d.RuntimeSkillMetadata(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"manifest":`)) || bytes.Contains(b, raw) {
		t.Fatal("private manifest exposed by metadata")
	}
	var assembled, loaded int
	if err = d.QueryRow(`SELECT count(*) FILTER(WHERE stage='assembled'),count(*) FILTER(WHERE stage='loaded') FROM managed_skill_runtime_events WHERE pin_id=$1`, p.ID).Scan(&assembled, &loaded); err != nil {
		t.Fatal(err)
	}
	if assembled != 2 || loaded != 1 {
		t.Fatal("assembled confused with instruction load")
	}
	if _, err = d.Exec(`UPDATE managed_skill_runtime_pins SET manifest=manifest WHERE id=$1`, p.ID); err == nil {
		t.Fatal("snapshot update was not rejected")
	}
	broken := p
	broken.Manifest = []byte("changed")
	if err = d.ValidateRuntimeSkill(ctx, broken); !errors.Is(err, ErrSkillIntegrity) {
		t.Fatalf("corrupt content: %v", err)
	}
	newer := v
	newer.ID = 0
	newer.Label = "v2"
	newRaw := append(append([]byte{}, raw...), []byte("Additional harmless note.\n")...)
	newer.ContentHash = ManagedManifestHash(newRaw)
	newer.TotalBytes = int64(len(newRaw))
	v2, err := d.RecordManagedSkill(ctx, newer, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.BindManagedSkill(ctx, v.SkillName, "planner", v2.ID, 1, v2.ContentHash, "operator"); err != nil {
		t.Fatal(err)
	}
	if err = d.ValidateRuntimeSkill(ctx, p); !errors.Is(err, ErrSkillIntegrity) {
		t.Fatalf("rebound old pin accepted: %v", err)
	}
	changed := input
	changed.VersionID = v2.ID
	changed.Hash = v2.ContentHash
	changed.Manifest = newRaw
	if _, err = d.PinRuntimeSkill(ctx, changed); !errors.Is(err, ErrSkillIntegrity) {
		t.Fatalf("silently replaced existing snapshot: %v", err)
	}
	fresh := phase2Task(t, d)
	changed.TaskID = fresh.ID
	if _, err = d.PinRuntimeSkill(ctx, changed); err != nil {
		t.Fatal(err)
	}
	if err = d.RevokeManagedSkill(ctx, v.SkillName, v2.ID, 1, "Synthetic revocation", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err = d.PinRuntimeSkill(ctx, changed); !errors.Is(err, ErrSkillIntegrity) {
		t.Fatalf("revoked version accepted: %v", err)
	}
}
func TestRuntimeSkillHistoricalAndChatScopes(t *testing.T) {
	d := catalogTestDB(t)
	task := phase2Task(t, d)
	v, raw := runtimeFixture(t, d)
	ctx := t.Context()
	if err := d.InsertSkillUsage(&SkillUsage{Skill: v.SkillName, AgentKey: "planner", TaskID: task.ID, Found: true}); err != nil {
		t.Fatal(err)
	}
	p := RuntimeSkillPin{TaskID: task.ID, AgentKey: "planner", Name: v.SkillName, VersionID: v.ID, Hash: v.ContentHash, Manifest: raw}
	if _, err := d.PinRuntimeSkill(ctx, p); !errors.Is(err, ErrSkillIntegrity) {
		t.Fatalf("retroactive historical pin: %v", err)
	}
	p.TaskID = 0
	p.SessionID = "synthetic-chat-" + v.SkillName
	pin, err := d.PinRuntimeSkill(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if pin.RunKey != "chat:"+p.SessionID {
		t.Fatal("chat scope lost")
	}
	if _, err = d.PinRuntimeSkill(ctx, RuntimeSkillPin{}); err == nil {
		t.Fatal("unattributed runtime pin accepted")
	}
	if err = d.BindManagedSkill(ctx, v.SkillName, "planner", 0, 1, "", "operator"); err != nil {
		t.Fatal(err)
	}
	if err = d.ValidateRuntimeSkill(ctx, pin); !errors.Is(err, ErrSkillIntegrity) {
		t.Fatalf("unbound pin accepted: %v", err)
	}
}
func TestRuntimeSkillConcurrentPinIsIdempotent(t *testing.T) {
	d := catalogTestDB(t)
	task := phase2Task(t, d)
	v, raw := runtimeFixture(t, d)
	p := RuntimeSkillPin{TaskID: task.ID, AgentKey: "planner", Name: v.SkillName, VersionID: v.ID, Hash: v.ContentHash, Manifest: raw}
	type outcome struct {
		pin RuntimeSkillPin
		err error
	}
	out := make(chan outcome, 2)
	for range 2 {
		go func() { pin, err := d.PinRuntimeSkill(context.Background(), p); out <- outcome{pin, err} }()
	}
	a, b := <-out, <-out
	if a.err != nil || b.err != nil || a.pin.ID != b.pin.ID {
		t.Fatalf("concurrent pin mismatch: %v %v", a.err, b.err)
	}
}
