import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url));
const inventory = JSON.parse(read("docs/skill-integration/inventory.json"));
test("source inventory uses explicit manifest denominators and never treats names as body review", () => {
  assert.equal(inventory.sources.BugHunter.commit, "9bdb0a81658bc25f09c0b52d31fcde923d126e4f");
  assert.equal(inventory.sources.BugHunter.tree_truncated, false);
  assert.equal(inventory.entries.filter((e) => e.source === "BugHunter").length, 83);
  assert.equal(inventory.entries.filter((e) => e.source === "BugHunter" && e.name.startsWith("hunt-")).length, 58);
  assert.equal(inventory.entries.filter((e) => e.source === "Drive").length, 46);
  assert.equal(inventory.entries.filter((e) => e.source === "Drive" && e.content_review !== "not_read").length, 3);
  for (const entry of inventory.entries) {
    if (entry.content_review === "not_read") {
      assert.equal(entry.sha256, null);
      assert.equal(entry.artex_skill, null);
      assert.equal(entry.disposition, "not_selected_body_unreviewed");
    }
    if (entry.source === "Drive") {
      assert.equal(entry.artex_skill, null);
      assert.notEqual(entry.disposition, "adapted_this_pr");
    }
  }
});
test("three installed guides have real bytes, explicit licensing, no runtime resources, no default enable or invented roles", () => {
  assert.deepEqual(inventory.new_skills.map((e) => e.name).sort(), [
    "quality-independent-review",
    "quality-scope-brief",
    "quality-task-handoff",
  ]);
  for (const entry of inventory.new_skills) {
    const path = entry.adapted_path ?? entry.path;
    assert.match(path, /^skills\/quality-[a-z-]+\/SKILL\.md$/);
    const raw = read(path);
    assert.equal(createHash("sha256").update(raw).digest("hex"), entry.adapted_sha256 ?? entry.sha256);
    const text = raw.toString("utf8");
    assert.match(text, new RegExp(`name: ${entry.name}`));
    assert.match(text, /license: (CC-BY-4.0|AGPL-3.0-only)/);
    assert.doesNotMatch(text, /^role:|^enabled:|^mcps?:|\$\{SKILL_DIR\}/m);
    assert.deepEqual(readdirSync(new URL(`../../skills/${entry.name}/`, import.meta.url)), ["SKILL.md"]);
    assert.equal(entry.default_enabled, false);
    assert.deepEqual(entry.default_agent_bindings, []);
  }
});
test("Drive influence is disclosed as original general organization, not licensed text reuse", () => {
  const original = JSON.parse(read("third_party/quality-task-handoff.json"));
  assert.equal(original.implementation, "original_not_copied_or_translated_from_Drive");
  assert.match(read(original.path).toString("utf8"), /没有复制、翻译/);
  assert.match(read("third_party/bughunter/LICENSE-CONTENT").toString("utf8"), /Creative Commons/);
  assert.match(read("LICENSE").toString("utf8"), /GNU AFFERO GENERAL PUBLIC LICENSE/);
});
