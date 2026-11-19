package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/skill"
	actool "github.com/Autumn-27/norma/tool"
)

// Capture uses the existing tree-v1 reader and then proves the exact returned
// bytes produce that digest. Runtime never reads instructions from the live path.
func captureManagedManifest(ctx context.Context, dir, name string) ([]byte, string, error) {
	before, err := digestManagedSkill(ctx, dir, name)
	if err != nil {
		return nil, "", err
	}
	if before.Files != 1 || before.Bytes > db.MaxManagedManifest {
		return nil, "", errors.New("managed runtime supports one SKILL.md up to 128 KiB, without resource files")
	}
	parent, err := os.OpenRoot(dir)
	if err != nil {
		return nil, "", errors.New("skill root unavailable")
	}
	defer parent.Close()
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, "", errors.New("skill directory unavailable")
	}
	defer root.Close()
	f, err := openDigestFile(root, "SKILL.md")
	if err != nil {
		return nil, "", errors.New("manifest unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("unsafe manifest")
	}
	raw, err := io.ReadAll(io.LimitReader(f, db.MaxManagedManifest+1))
	if err != nil {
		return nil, "", errors.New("manifest unreadable")
	}
	if len(raw) > db.MaxManagedManifest || !utf8.Valid(raw) || db.ManagedManifestHash(raw) != before.Hash {
		return nil, "", errors.New("manifest changed or is not bounded UTF-8")
	}
	after, err := digestManagedSkill(ctx, dir, name)
	if err != nil || after != before {
		return nil, "", errors.New("manifest tree changed during capture")
	}
	if _, err = parseManagedManifest(name, raw); err != nil {
		return nil, "", err
	}
	return raw, before.Hash, nil
}

// Use Norma's real parser/discovery without retaining a live filesystem Dir.
// Resource substitution/MCP activation remain legacy-only, not silently ignored.
func parseManagedManifest(name string, raw []byte) (skill.Skill, error) {
	var empty skill.Skill
	if !validSkillName(name) || !utf8.Valid(raw) || len(raw) == 0 || len(raw) > db.MaxManagedManifest || bytes.Contains(raw, []byte("${SKILL_DIR}")) {
		return empty, errors.New("managed manifest invalid or requires unsupported filesystem resources")
	}
	tmp, err := os.MkdirTemp("", "artex-managed-parse-")
	if err != nil {
		return empty, errors.New("cannot create manifest parser workspace")
	}
	defer os.RemoveAll(tmp)
	if err = os.Mkdir(filepath.Join(tmp, name), 0700); err != nil {
		return empty, err
	}
	if err = os.WriteFile(filepath.Join(tmp, name, "SKILL.md"), raw, 0600); err != nil {
		return empty, err
	}
	reg, err := skill.LoadDir(tmp)
	if err != nil {
		return empty, errors.New("manifest parse failed")
	}
	sk, ok := reg.Get(name)
	if !ok || sk.Name != name || strings.TrimSpace(sk.Description) == "" || strings.TrimSpace(sk.Instructions) == "" || len(sk.MCPs) > 0 {
		return empty, errors.New("managed manifest needs matching directory/name and description; MCP/resource packages are not supported")
	}
	sk.Dir = ""
	return sk, nil
}

type managedRuntime struct {
	pg   *db.DB
	dir  string
	pins map[string]db.RuntimeSkillPin
}

func loadAgentSkills(ctx context.Context, pg *db.DB, dir, key string, agentID int64) (*skill.Registry, *managedRuntime, error) {
	names, err := pg.AgentSkillNames(agentID)
	if err != nil {
		return nil, nil, err
	}
	visible := map[string]bool{}
	for _, name := range names {
		visible[name] = true
	}
	bindings, err := pg.RuntimeSkillBindings(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	managed := map[string]db.RuntimeSkillBinding{}
	for _, b := range bindings {
		if b.VersionID > 0 && b.Enabled {
			managed[b.Name] = b
		}
	}
	ri := agent.RunInfoFrom(ctx)
	run, keyErr := db.SkillRunKey(ri.TaskID, ri.SessionID)
	if len(managed) > 0 && keyErr != nil {
		return nil, nil, keyErr
	}
	if keyErr == nil {
		pins, err := pg.RuntimeSkillPins(ctx, run, key)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range pins {
			b, ok := managed[p.Name]
			if !ok || !visible[p.Name] || b.VersionID != p.VersionID {
				return nil, nil, fmt.Errorf("%w: pinned skill binding or visibility changed (%s)", db.ErrSkillIntegrity, p.Name)
			}
		}
	}
	reg := skill.NewRegistry()
	legacy := map[string]bool{}
	for _, n := range names {
		if _, ok := managed[n]; !ok {
			legacy[n] = true
		}
	}
	if len(legacy) > 0 {
		if all, err := skill.LoadDir(dir); err == nil && all != nil {
			for _, sk := range all.List() {
				if sk.Dir != "" && legacy[filepath.Base(sk.Dir)] {
					if _, collision := managed[sk.Name]; collision {
						return nil, nil, errors.New("managed/legacy display name collision")
					}
					reg.Add(sk)
				}
			}
		}
	}
	rt := &managedRuntime{pg: pg, dir: dir, pins: map[string]db.RuntimeSkillPin{}}
	for _, name := range names {
		binding, ok := managed[name]
		if !ok {
			continue
		}
		raw, hash, err := captureManagedManifest(ctx, dir, name)
		if err != nil {
			return nil, nil, fmt.Errorf("managed skill %s: %w", name, err)
		}
		pin, err := pg.PinRuntimeSkill(ctx, db.RuntimeSkillPin{TaskID: ri.TaskID, SessionID: ri.SessionID, AgentKey: key, Name: name, VersionID: binding.VersionID, Hash: hash, Manifest: raw})
		if err != nil {
			return nil, nil, fmt.Errorf("managed skill %s: %w", name, err)
		}
		// PinRuntimeSkill compares immutable historical content; never substitutes it
		// with a newly-bound version when a task or conversation is restored.
		sk, err := parseManagedManifest(name, pin.Manifest)
		if err != nil {
			return nil, nil, err
		}
		reg.Add(sk)
		rt.pins[name] = pin
	}
	if len(reg.List()) == 0 {
		return nil, rt, nil
	}
	return reg, rt, nil
}

type managedRuntimeTool struct {
	actool.CoreTool
	runtime *managedRuntime
}

func (rt *managedRuntime) guard(t actool.CoreTool) actool.CoreTool {
	if rt == nil || len(rt.pins) == 0 {
		return t
	}
	return &managedRuntimeTool{CoreTool: t, runtime: rt}
}
func (t *managedRuntimeTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	var input struct {
		Name string `json:"name"`
		Args string `json:"args"`
	}
	if err := json.Unmarshal(in, &input); err != nil {
		return t.CoreTool.Call(ctx, in, tc)
	}
	p, managed := t.runtime.pins[input.Name]
	if !managed {
		return t.CoreTool.Call(ctx, in, tc)
	}
	if err := t.runtime.pg.ValidateRuntimeSkill(ctx, p); err != nil {
		return actool.Errorf("Managed skill blocked: " + err.Error()), nil
	}
	_, hash, err := captureManagedManifest(ctx, t.runtime.dir, p.Name)
	if err != nil || hash != p.Hash {
		return actool.Errorf("Managed skill blocked: source content missing or changed; restore the pinned source or start a new run after rebinding."), nil
	}
	result, err := t.CoreTool.Call(ctx, in, tc)
	if err != nil {
		return result, err
	}
	if !result.IsError {
		if err := t.runtime.pg.RecordRuntimeSkillLoaded(ctx, p); err != nil {
			return actool.Errorf("Managed skill instructions withheld: runtime record could not be persisted."), nil
		}
	}
	return result, nil
}
