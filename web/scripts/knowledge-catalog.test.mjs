import {
  KNOWLEDGE_KINDS,
  KNOWLEDGE_REVIEW_STATES,
  knowledgeEvidenceRefs,
  knowledgeMetadata,
  knowledgeReviewPayload,
} from "../src/lib/knowledge-catalog.ts";
import assert from "node:assert/strict";
import test from "node:test";

test("metadata whitelist never carries review, identity, or execution fields", () => {
  const entry = {
    ...knowledgeMetadata(),
    id: 7,
    revision: 3,
    review_status: "reviewed",
    enabled: true,
    role: "planner",
  };
  const draft = knowledgeMetadata(entry);
  assert.deepEqual(
    Object.keys(draft).sort(),
    ["title", "kind", "source_ref", "source_version", "sha256", "license", "evidence_refs"].sort(),
  );
  assert.equal(draft.kind, "reference");
  draft.evidence_refs.push("urn:synthetic:evidence:1");
  assert.deepEqual(entry.evidence_refs, []);
});
test("evidence input accepts lines but preserves duplicates for server validation", () => {
  assert.deepEqual(knowledgeEvidenceRefs(" urn:one \r\n\nurn:two\nurn:one "), ["urn:one", "urn:two", "urn:one"]);
});
test("review is explicit and revision-bound, never an execution enable", () => {
  assert.deepEqual(knowledgeReviewPayload(4, "reviewed", " Source metadata checked "), {
    revision: 4,
    status: "reviewed",
    note: "Source metadata checked",
  });
  for (const revision of [0, -1, NaN, 1.2]) assert.throws(() => knowledgeReviewPayload(revision, "draft", ""));
  assert.throws(() => knowledgeReviewPayload(1, "unknown", "note"));
  for (const state of ["reviewed", "rejected", "revoked"]) assert.throws(() => knowledgeReviewPayload(1, state, " "));
  assert.deepEqual(knowledgeReviewPayload(1, "draft", ""), { revision: 1, status: "draft", note: "" });
});
test("metadata vocabulary does not claim finding confirmation or skill enablement", () => {
  assert.deepEqual(KNOWLEDGE_REVIEW_STATES, ["draft", "reviewed", "rejected", "revoked"]);
  assert.ok(KNOWLEDGE_KINDS.includes("advisory"));
  assert.ok(!KNOWLEDGE_REVIEW_STATES.includes("confirmed"));
  assert.ok(!KNOWLEDGE_REVIEW_STATES.includes("enabled"));
});
