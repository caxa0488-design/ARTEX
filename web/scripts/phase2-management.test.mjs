import {
  coverageReviewPayload,
  findingReviewPayload,
  managedBindingPayload,
  managedVersionPayload,
  REVIEW_DECISIONS,
  reviewReferences,
  skillRuntimeEventMeaning,
  WORK_STATES,
} from "../src/lib/phase2-management.ts";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import test from "node:test";

const fp = "a".repeat(64);
test("review and work vocabularies are deliberately distinct", () => {
  assert.deepEqual(REVIEW_DECISIONS, ["candidate", "confirmed", "rejected", "inconclusive"]);
  assert.deepEqual(WORK_STATES, ["untested", "blocked", "failed", "completed"]);
  assert.ok(!WORK_STATES.includes("confirmed"));
});
test("human confirmation requires a snapshot and declared references, not confidence", () => {
  assert.throws(() => findingReviewPayload(fp, "confirmed", "reason", []));
  assert.throws(() => findingReviewPayload("bad", "candidate", "reason", []));
  assert.throws(() => findingReviewPayload(fp, "verified", "reason", []));
  const refs = ["urn:fixture:evidence"];
  const payload = findingReviewPayload(fp, "confirmed", " human reason ", refs);
  assert.deepEqual(payload, { fingerprint: fp, decision: "confirmed", reason: "human reason", evidence_refs: refs });
  payload.evidence_refs.push("urn:second");
  assert.equal(refs.length, 1);
  assert.equal("verification_status" in payload, false);
});
test("work annotations preserve the exact inspected key and do not claim execution", () => {
  assert.throws(() => coverageReviewPayload("", "config", fp, "blocked", "reason", []));
  assert.throws(() => coverageReviewPayload("surface", "config", fp, "clean", "reason", []));
  assert.throws(() => coverageReviewPayload("surface", "config", "bad", "blocked", "reason", []));
  assert.deepEqual(coverageReviewPayload("surface", "config", fp, "blocked", " reason ", []), {
    surface: "surface",
    risk_area: "config",
    fingerprint: fp,
    state: "blocked",
    reason: "reason",
    evidence_refs: [],
  });
});
test("references are split but not fetched or silently deduplicated", () => {
  assert.deepEqual(reviewReferences(" urn:one \r\n\nurn:two\nurn:one "), ["urn:one", "urn:two", "urn:one"]);
});

test("binding requests carry only server-owned version/revision references, never client permissions", () => {
  assert.deepEqual(managedBindingPayload(42, 0), { version_id: 42, revision: 0 });
  assert.deepEqual(managedBindingPayload(0, 3), { version_id: 0, revision: 3 });
  for (const [id, rev] of [
    [0, 0],
    [-1, 1],
    [1, -1],
    [1.5, 0],
    [NaN, 1],
    [1, Infinity],
  ])
    assert.throws(() => managedBindingPayload(id, rev));
});
test("registration whitelist never supplies content hash, role or runtime fields", () => {
  assert.deepEqual(managedVersionPayload(" v1 ", " urn:fixture:source ", " Synthetic fixture "), {
    label: "v1",
    source_ref: "urn:fixture:source",
    license: "Synthetic fixture",
  });
  assert.throws(() => managedVersionPayload("", "urn:fixture:source", "license"));
});

test("runtime event labels never equate loading with execution or verification", () => {
  assert.match(skillRuntimeEventMeaning("assembled"), /not necessarily loaded/);
  assert.match(skillRuntimeEventMeaning("loaded"), /no action, test result or coverage/);
  for (const value of ["executed", "confirmed", undefined, null, 5])
    assert.match(skillRuntimeEventMeaning(value), /do not infer execution or verification/);
});
test("bundled quality adaptations pin source and actual adapted bytes, with explicit attribution and opt-in", () => {
  const entries = JSON.parse(
    readFileSync(new URL("../../third_party/bughunter/adaptations.json", import.meta.url), "utf8"),
  );
  assert.deepEqual(entries.map((e) => e.name).sort(), [
    "quality-evidence-hygiene",
    "quality-evidence-report",
    "quality-independent-review",
    "quality-scope-brief",
  ]);
  for (const entry of entries) {
    assert.equal(entry.source_commit, "9bdb0a81658bc25f09c0b52d31fcde923d126e4f");
    assert.match(entry.source_sha256, /^[0-9a-f]{64}$/);
    assert.equal(entry.content_license, "CC-BY-4.0");
    assert.match(entry.author, /Sachin Sharma/);
    assert.equal(entry.default_enabled, false);
    assert.deepEqual(entry.default_agent_bindings, []);
    assert.match(entry.adapted_path, /^skills\/quality-[a-z-]+\/SKILL\.md$/);
    const raw = readFileSync(new URL(`../../${entry.adapted_path}`, import.meta.url));
    assert.equal(createHash("sha256").update(raw).digest("hex"), entry.adapted_sha256);
    assert.match(raw.toString("utf8"), /license: CC-BY-4.0/);
    assert.match(raw.toString("utf8"), /9bdb0a81658bc25f09c0b52d31fcde923d126e4f/);
  }
});
test("Drive review records exclusion, not an unlicensed installed skill", () => {
  const review = JSON.parse(readFileSync(new URL("../../third_party/drive-review.json", import.meta.url), "utf8"));
  assert.equal(review.items[0].status, "reviewed_not_imported");
  assert.equal(review.items[0].body_copied, false);
  assert.equal(review.items[0].runtime_enabled, false);
});
