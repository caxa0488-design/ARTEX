export const REVIEW_DECISIONS = ["candidate", "confirmed", "rejected", "inconclusive"] as const;
export const WORK_STATES = ["untested", "blocked", "failed", "completed"] as const;
export type ReviewDecision = (typeof REVIEW_DECISIONS)[number];
export type WorkState = (typeof WORK_STATES)[number];
export function reviewReferences(text: string) {
  return text
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean);
}
export function findingReviewPayload(fingerprint: string, decision: ReviewDecision, reason: string, refs: string[]) {
  if (!/^[0-9a-f]{64}$/.test(fingerprint) || !REVIEW_DECISIONS.includes(decision) || !reason.trim())
    throw new Error("Load a current snapshot and provide a review reason.");
  if (decision === "confirmed" && refs.length === 0)
    throw new Error("Confirmation needs evidence references, not model confidence.");
  return { fingerprint, decision, reason: reason.trim(), evidence_refs: [...refs] };
}
export function coverageReviewPayload(
  surface: string,
  risk: string,
  fingerprint: string,
  state: WorkState,
  reason: string,
  refs: string[],
) {
  if (
    !WORK_STATES.includes(state) ||
    !surface.trim() ||
    !risk.trim() ||
    !reason.trim() ||
    !/^[0-9a-f]{64}$/.test(fingerprint)
  )
    throw new Error("Inspect the exact coverage key before recording a work annotation.");
  return { surface, risk_area: risk, fingerprint, state, reason: reason.trim(), evidence_refs: [...refs] };
}
export interface ManagedVersion {
  id: number;
  label: string;
  source_ref: string;
  license: string;
  content_hash: string;
  revoked: boolean;
  revision: number;
  file_count: number;
  total_bytes: number;
}
export interface ManagedBinding {
  agent_key: string;
  version_id: number | null;
  revision: number;
  visible: boolean;
}
export interface SkillManagement {
  versions: ManagedVersion[];
  agents: ManagedBinding[];
  checks: Record<string, string>;
  current_error: string;
  runtime_locking: false;
  events: unknown[];
  limits: string;
}
export interface FindingReview {
  name: string;
  task_id: number | null;
  finding_id: number;
  status: string;
  verification_status: string;
  revision: number;
  fingerprint: string;
  history: { revision: number; result_fingerprint: string; [key: string]: unknown }[];
}
export interface CoverageReview {
  task_id: number;
  surface: string;
  risk_area: string;
  fingerprint: string;
  ledger_outcome: string;
  revision: number;
  coverage_enabled: boolean;
}
export interface ReviewDesk {
  task_id: number;
  ledger: {
    gaps: { kind: string; surface?: string; risk_area: string }[];
    unexamined: string[];
    entries: unknown[];
    completeness: unknown;
  };
  coverage_reviews: { work_state: string; stale: boolean; [key: string]: unknown }[];
  findings: { id: number; name: string; status: string; verification_status: string }[];
  finding_reviews: unknown[];
  run_scope: string;
  limits: string;
}

export function managedBindingPayload(version: number, revision: number) {
  if (
    !Number.isSafeInteger(version) ||
    version < 0 ||
    !Number.isSafeInteger(revision) ||
    revision < 0 ||
    (version === 0 && revision === 0)
  )
    throw new Error("Choose a real version and current binding revision, or clear an existing binding.");
  return { version_id: version, revision };
}
export function managedVersionPayload(label: string, source: string, license: string) {
  if (!label.trim() || !source.trim() || !license.trim())
    throw new Error("Version label, provenance reference and license statement are required.");
  return { label: label.trim(), source_ref: source.trim(), license: license.trim() };
}
