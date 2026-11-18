"use client";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { http } from "@/lib/api";
import {
  type CoverageReview,
  coverageReviewPayload,
  type FindingReview,
  findingReviewPayload,
  REVIEW_DECISIONS,
  type ReviewDecision,
  type ReviewDesk,
  reviewReferences,
  WORK_STATES,
  type WorkState,
} from "@/lib/phase2-management";
export default function ReviewDeskPage() {
  const [task, setTask] = useState("");
  const [desk, setDesk] = useState<ReviewDesk | null>(null);
  const [filter, setFilter] = useState("");
  const [gapKind, setGapKind] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [finding, setFinding] = useState("");
  const [review, setReview] = useState<FindingReview | null>(null);
  const [decision, setDecision] = useState<ReviewDecision | "">("");
  const [findingReason, setFindingReason] = useState("");
  const [findingRefs, setFindingRefs] = useState("");
  const [surface, setSurface] = useState("");
  const [risk, setRisk] = useState("");
  const [coverage, setCoverage] = useState<CoverageReview | null>(null);
  const [work, setWork] = useState<WorkState>("untested");
  const [coverageReason, setCoverageReason] = useState("");
  const [coverageRefs, setCoverageRefs] = useState("");
  useEffect(() => {
    const q = new URLSearchParams(window.location.search);
    setTask(q.get("task") ?? "");
    setFinding(q.get("finding") ?? "");
  }, []);
  async function action(run: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await run();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Review operation failed");
    } finally {
      setBusy(false);
    }
  }
  async function loadDesk() {
    setDesk(
      await http<ReviewDesk>(`/operator/tasks/${encodeURIComponent(task)}${filter ? `?work_state=${filter}` : ""}`),
    );
  }
  async function loadFinding() {
    setReview(await http<FindingReview>(`/operator/findings/${encodeURIComponent(finding)}`));
    setDecision("");
    setFindingReason("");
    setFindingRefs("");
  }
  function selectKey(s: string, r: string) {
    setSurface(s);
    setRisk(r);
    setCoverage(null);
    setWork("untested");
    setCoverageReason("");
    setCoverageRefs("");
  }
  let reviewSnapshotLabel = "legacy state is not an explicit review";
  if (review?.history[0])
    reviewSnapshotLabel =
      review.history[0].result_fingerprint === review.fingerprint
        ? "matches loaded snapshot"
        : "stale snapshot; review again";
  return (
    <div className="space-y-5">
      <h1 className="font-semibold text-2xl">Review Desk</h1>
      <p className="rounded border p-3 text-sm">
        Human disposition, machine verification, coverage results, and operator work annotations are separate.
        References are declarations, not automatically validated evidence. Task history is never labelled as current-run
        testing; old ledger records can span revivals. No testing action is started here.
      </p>
      {error && (
        <p role="alert" className="text-destructive">
          {error} Reload the exact snapshot after conflicts; do not blindly retry.
        </p>
      )}
      <fieldset disabled={busy} className="space-y-5">
        <div className="flex flex-wrap gap-2">
          <Input
            aria-label="Task ID"
            value={task}
            placeholder="Task ID"
            onChange={(e) => {
              setTask(e.target.value);
              setDesk(null);
              setCoverage(null);
            }}
          />
          <NativeSelect
            aria-label="Work-state filter"
            value={filter}
            onChange={(e) => {
              setFilter(e.target.value);
              setDesk(null);
            }}
          >
            <NativeSelectOption value="">All work annotations</NativeSelectOption>
            {WORK_STATES.map((v) => (
              <NativeSelectOption key={v} value={v}>
                {v}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <Button disabled={!task} onClick={() => action(loadDesk)}>
            Load task / gaps
          </Button>
        </div>
        {desk && (
          <>
            <p className="text-sm">
              {desk.run_scope} · {desk.limits}
            </p>
            <Button
              variant="outline"
              onClick={() =>
                action(async () => {
                  const data = await http<ReviewDesk>(`/operator/tasks/${encodeURIComponent(task)}/report`);
                  const url = URL.createObjectURL(
                    new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }),
                  );
                  const a = document.createElement("a");
                  a.href = url;
                  a.download = `task-${task}-operator-review.json`;
                  document.body.appendChild(a);
                  a.click();
                  a.remove();
                  URL.revokeObjectURL(url);
                })
              }
            >
              Download supplemental review / gap report (JSON)
            </Button>
            <Card>
              <CardHeader>
                <CardTitle>Existing ledger gaps</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <p className="text-sm">
                  These are the existing coverage ledger gaps, not a second coverage truth. Operator annotations do not
                  clear these gaps or mark assets tested.
                </p>
                <NativeSelect aria-label="Gap-kind filter" value={gapKind} onChange={(e) => setGapKind(e.target.value)}>
                  <NativeSelectOption value="">All gap kinds</NativeSelectOption>
                  {[...new Set(desk.ledger.gaps.map((g) => g.kind))].map((k) => (
                    <NativeSelectOption key={k} value={k}>
                      {k}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                {desk.ledger.gaps
                  .filter((g) => !gapKind || g.kind === gapKind)
                  .map((g) => (
                    <div
                      key={`${g.kind}:${g.surface}:${g.risk_area}`}
                      className="flex flex-wrap items-center gap-2 rounded border p-2 text-sm"
                    >
                      <span>
                        {g.kind} · {g.surface ?? "task scope"} · {g.risk_area}
                      </span>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => selectKey(g.surface ?? "[task scope]", g.risk_area)}
                      >
                        Use this key
                      </Button>
                    </div>
                  ))}
                <details>
                  <summary>Ledger entries and completeness (original semantics)</summary>
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap text-xs">
                    {JSON.stringify(desk.ledger, null, 2)}
                  </pre>
                </details>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Findings in this task (up to 100)</CardTitle>
              </CardHeader>
              <CardContent>
                {desk.findings.map((f) => (
                  <div key={f.id} className="flex flex-wrap items-center gap-2 p-2 text-sm">
                    <span>
                      #{f.id} {f.name} · disposition: {f.status} · machine: {f.verification_status}
                    </span>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setFinding(String(f.id));
                        setReview(null);
                      }}
                    >
                      Select for review
                    </Button>
                  </div>
                ))}
              </CardContent>
            </Card>
          </>
        )}
        <Card>
          <CardHeader>
            <CardTitle>Human finding review</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <Input
              aria-label="Finding ID"
              value={finding}
              placeholder="Finding ID"
              onChange={(e) => {
                setFinding(e.target.value);
                setReview(null);
              }}
            />
            <Button disabled={!finding} variant="outline" onClick={() => action(loadFinding)}>
              Load finding snapshot
            </Button>
            {review && (
              <>
                <p className="text-sm">
                  Finding #{review.finding_id} {review.name} · task {review.task_id ?? "unassigned"} · stored
                  disposition: {review.status} · machine verification: {review.verification_status} · human review
                  revision {review.revision}. This form never changes machine verification or POE.
                </p>
                <p className="text-sm">
                  Latest explicit decision: {String(review.history[0]?.decision ?? "none recorded")} ·{" "}
                  {reviewSnapshotLabel}
                </p>
                <NativeSelect
                  aria-label="Human decision"
                  value={decision}
                  onChange={(e) => setDecision(e.target.value as ReviewDecision)}
                >
                  <NativeSelectOption value="">Choose an explicit human disposition</NativeSelectOption>
                  {REVIEW_DECISIONS.map((v) => (
                    <NativeSelectOption key={v} value={v}>
                      {v}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <Label htmlFor="finding-reason">Explicit reason (single paragraph)</Label>
                <Input
                  id="finding-reason"
                  maxLength={2000}
                  value={findingReason}
                  onChange={(e) => setFindingReason(e.target.value)}
                />
                <Label htmlFor="finding-refs">Evidence references (HTTPS without query or URN, one per line)</Label>
                <Textarea id="finding-refs" value={findingRefs} onChange={(e) => setFindingRefs(e.target.value)} />
                <Button
                  disabled={!decision || !findingReason.trim()}
                  onClick={() =>
                    action(async () => {
                      if (!decision) throw new Error("Choose a human disposition.");
                      const payload = findingReviewPayload(
                        review.fingerprint,
                        decision,
                        findingReason,
                        reviewReferences(findingRefs),
                      );
                      setReview(
                        await http<FindingReview>(`/operator/findings/${review.finding_id}`, {
                          method: "POST",
                          body: JSON.stringify(payload),
                        }),
                      );
                      if (desk) await loadDesk();
                    })
                  }
                >
                  Record human disposition
                </Button>
                <p className="text-sm">
                  candidate→pending; confirmed→confirmed; rejected→false_positive; inconclusive→in_progress. A model
                  opinion does not call this endpoint automatically.
                </p>
                <details>
                  <summary>Human review history (latest 100)</summary>
                  {review.history.map((h) => (
                    <pre
                      key={h.revision}
                      className="max-h-60 overflow-auto whitespace-pre-wrap break-all border p-2 text-xs"
                    >
                      {JSON.stringify({ ...h, stale: h.result_fingerprint !== review.fingerprint }, null, 2)}
                    </pre>
                  ))}
                </details>
              </>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Coverage work annotation</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <p className="text-sm">
              Untested, blocked, failed, and completed describe operator work only. Completed requires an existing
              settled ledger result plus references, and is not proof of current-run execution.
            </p>
            <Input
              aria-label="Coverage surface"
              placeholder="Surface"
              value={surface}
              maxLength={200}
              onChange={(e) => selectKey(e.target.value, risk)}
            />
            <Input
              aria-label="Coverage risk area"
              placeholder="Risk area"
              value={risk}
              maxLength={64}
              onChange={(e) => selectKey(surface, e.target.value)}
            />
            <Button
              variant="outline"
              disabled={!task || !surface || !risk}
              onClick={() =>
                action(async () => {
                  const q = new URLSearchParams({ surface, risk_area: risk });
                  setCoverage(
                    await http<CoverageReview>(`/operator/tasks/${encodeURIComponent(task)}/coverage-key?${q}`),
                  );
                })
              }
            >
              Inspect exact ledger snapshot
            </Button>
            {coverage && (
              <>
                <p className="text-sm">
                  Ledger outcome: {coverage.ledger_outcome || "unrecorded / untested"} · coverage enabled:{" "}
                  {String(coverage.coverage_enabled)} · annotation revision {coverage.revision}
                </p>
                <NativeSelect
                  aria-label="Operator work state"
                  value={work}
                  onChange={(e) => setWork(e.target.value as WorkState)}
                >
                  {WORK_STATES.map((v) => (
                    <NativeSelectOption key={v} value={v}>
                      {v}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <Label htmlFor="coverage-reason">Reason (single paragraph)</Label>
                <Input
                  id="coverage-reason"
                  maxLength={2000}
                  value={coverageReason}
                  onChange={(e) => setCoverageReason(e.target.value)}
                />
                <Label htmlFor="coverage-refs">Evidence references (one per line; not fetched)</Label>
                <Textarea id="coverage-refs" value={coverageRefs} onChange={(e) => setCoverageRefs(e.target.value)} />
                <Button
                  disabled={!coverage.coverage_enabled || !coverageReason.trim()}
                  onClick={() =>
                    action(async () => {
                      const payload = coverageReviewPayload(
                        coverage.surface,
                        coverage.risk_area,
                        coverage.fingerprint,
                        work,
                        coverageReason,
                        reviewReferences(coverageRefs),
                      );
                      setCoverage(
                        await http<CoverageReview>(`/operator/tasks/${coverage.task_id}/coverage`, {
                          method: "POST",
                          body: JSON.stringify(payload),
                        }),
                      );
                      if (desk) await loadDesk();
                    })
                  }
                >
                  Record work annotation only
                </Button>
              </>
            )}
            {desk && (
              <details>
                <summary>Filtered work annotation history (stale snapshots labelled)</summary>
                <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
                  {JSON.stringify(desk.coverage_reviews, null, 2)}
                </pre>
              </details>
            )}
          </CardContent>
        </Card>
      </fieldset>
    </div>
  );
}
