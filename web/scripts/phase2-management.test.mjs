import {
  coverageReviewPayload,
  findingReviewPayload,
  managedBindingPayload,
  managedVersionPayload,
  REVIEW_DECISIONS,
  reviewReferences,
  WORK_STATES,
} from "../src/lib/phase2-management.ts";
import assert from "node:assert/strict";
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

test("binding is a revision-bound management reference, never an enable or runtime lock", () => {
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
