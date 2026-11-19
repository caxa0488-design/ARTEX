// Metadata contracts only. References are inert and hashes are declarations,
// not content verification, execution permission, or confirmed findings.
export const KNOWLEDGE_KINDS = ["reference", "advisory", "lesson", "skill_metadata"] as const;
export const KNOWLEDGE_REVIEW_STATES = ["draft", "reviewed", "rejected", "revoked"] as const;
export type KnowledgeKind = (typeof KNOWLEDGE_KINDS)[number];
export type KnowledgeReviewState = (typeof KNOWLEDGE_REVIEW_STATES)[number];

export interface KnowledgeMetadata {
  title: string;
  kind: KnowledgeKind;
  source_ref: string;
  source_version: string;
  sha256: string;
  license: string;
  evidence_refs: string[];
}
export interface KnowledgeEntry extends KnowledgeMetadata {
  id: number;
  revision: number;
  review_status: KnowledgeReviewState;
  review_note: string;
  created_at: string;
  updated_at: string;
}
export interface KnowledgeRevision {
  entry: KnowledgeEntry;
  actor: string;
  recorded_at: string;
}
export interface KnowledgePage {
  items: KnowledgeEntry[];
  total: number;
  limit: number;
  offset: number;
}
export interface KnowledgeHistoryPage {
  items: KnowledgeRevision[];
  limit: number;
  offset: number;
}

// Whitelist metadata fields so editing never forwards review state, identity,
// timestamps, or revision history into the create/update payload.
export function knowledgeMetadata(
  value: KnowledgeMetadata = {
    title: "",
    kind: "reference",
    source_ref: "",
    source_version: "",
    sha256: "",
    license: "",
    evidence_refs: [],
  },
): KnowledgeMetadata {
  return {
    title: value.title,
    kind: value.kind,
    source_ref: value.source_ref,
    source_version: value.source_version,
    sha256: value.sha256,
    license: value.license,
    evidence_refs: [...value.evidence_refs],
  };
}
export function knowledgeEvidenceRefs(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}
export function knowledgeReviewPayload(revision: number, status: KnowledgeReviewState, note: string) {
  if (!Number.isSafeInteger(revision) || revision < 1) throw new Error("Reload the entry before reviewing.");
  if (!KNOWLEDGE_REVIEW_STATES.includes(status)) throw new Error("Unknown metadata review status.");
  if (status !== "draft" && !note.trim()) throw new Error("A review note is required.");
  return { revision, status, note: note.trim() };
}
