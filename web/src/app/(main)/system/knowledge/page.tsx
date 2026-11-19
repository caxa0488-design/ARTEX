"use client";

import { type FormEvent, useEffect, useState } from "react";

import { BookOpen, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import {
  KNOWLEDGE_KINDS,
  KNOWLEDGE_REVIEW_STATES,
  type KnowledgeEntry,
  type KnowledgeKind,
  type KnowledgeMetadata,
  type KnowledgePage,
  type KnowledgeReviewState,
  type KnowledgeRevision,
  knowledgeEvidenceRefs,
  knowledgeMetadata,
} from "@/lib/knowledge-catalog";
import { knowledgeCatalogApi } from "@/lib/knowledge-catalog-api";

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "Catalog operation failed.";
}

export default function KnowledgeCatalogPage() {
  const [page, setPage] = useState<KnowledgePage | null>(null);
  // Request objects encode explicit reloads without unused effect dependencies.
  const [listQuery, setListQuery] = useState<{
    kind: KnowledgeKind | "";
    review_status: KnowledgeReviewState | "";
    offset: number;
  }>({ kind: "", review_status: "", offset: 0 });
  const { kind, review_status: status, offset } = listQuery;
  const [detailQuery, setDetailQuery] = useState<{ id: number | null }>({ id: null });
  const selectedId = detailQuery.id;
  const [entry, setEntry] = useState<KnowledgeEntry | null>(null);
  const [form, setForm] = useState<KnowledgeMetadata>(() => knowledgeMetadata());
  const [evidenceText, setEvidenceText] = useState("");
  const [reviewStatus, setReviewStatus] = useState<KnowledgeReviewState>("draft");
  const [reviewNote, setReviewNote] = useState("");
  const [history, setHistory] = useState<KnowledgeRevision[]>([]);
  const [historyOffset, setHistoryOffset] = useState(0);
  const [listError, setListError] = useState("");
  const [detailError, setDetailError] = useState("");
  const [mutationError, setMutationError] = useState("");
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setListError("");
    knowledgeCatalogApi
      .list(listQuery, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) setPage(result);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          setPage(null);
          setListError(errorMessage(error));
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [listQuery]);

  useEffect(() => {
    setMutationError("");
    setDetailError("");
    setHistoryOffset(0);
    setHistory([]);
    if (detailQuery.id === null) {
      setEntry(null);
      setForm(knowledgeMetadata());
      setEvidenceText("");
      setReviewStatus("draft");
      setReviewNote("");
      setDetailLoading(false);
      return;
    }
    const controller = new AbortController();
    setDetailLoading(true);
    setEntry(null);
    knowledgeCatalogApi
      .get(detailQuery.id, controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setEntry(result);
        setForm(knowledgeMetadata(result));
        setEvidenceText(result.evidence_refs.join("\n"));
        setReviewStatus(result.review_status);
        setReviewNote(result.review_note);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setDetailError(errorMessage(error));
      })
      .finally(() => {
        if (!controller.signal.aborted) setDetailLoading(false);
      });
    return () => controller.abort();
  }, [detailQuery]);

  useEffect(() => {
    if (detailQuery.id === null) return;
    const controller = new AbortController();
    setHistory([]);
    knowledgeCatalogApi
      .history(detailQuery.id, historyOffset, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) setHistory(result.items);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setDetailError(`History: ${errorMessage(error)}`);
      });
    return () => controller.abort();
  }, [detailQuery, historyOffset]);

  const readOnly =
    busy || detailLoading || entry?.review_status === "revoked" || (selectedId !== null && entry === null);
  const metadataDirty =
    !detailLoading &&
    JSON.stringify({ ...form, evidence_refs: knowledgeEvidenceRefs(evidenceText) }) !==
      JSON.stringify(knowledgeMetadata(entry ?? undefined));
  function selectEntry(id: number | null) {
    if (metadataDirty && !window.confirm("Discard unsaved metadata changes?")) return;
    setDetailQuery({ id });
  }
  function field(name: Exclude<keyof KnowledgeMetadata, "evidence_refs">, value: string) {
    setForm((current) => ({ ...current, [name]: value }));
  }
  function saved(result: KnowledgeEntry) {
    setEntry(result);
    setDetailQuery({ id: result.id });
    setListQuery((current) => ({ ...current }));
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (readOnly) return;
    setBusy(true);
    setMutationError("");
    try {
      const metadata = { ...form, evidence_refs: knowledgeEvidenceRefs(evidenceText) };
      const result = entry
        ? await knowledgeCatalogApi.update(entry, metadata)
        : await knowledgeCatalogApi.create(metadata);
      saved(result);
      toast.success("Metadata saved. Source content was not fetched or executed.");
    } catch (error) {
      setMutationError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  async function review() {
    if (!entry || readOnly || metadataDirty) return;
    if (
      reviewStatus === "revoked" &&
      !window.confirm("Revoke this catalog entry? It becomes read-only. A replacement needs a new source version.")
    )
      return;
    setBusy(true);
    setMutationError("");
    try {
      saved(await knowledgeCatalogApi.review(entry, reviewStatus, reviewNote));
      toast.success("Metadata review recorded; no execution permission was granted.");
    } catch (error) {
      setMutationError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  const submitLabel = selectedId === null ? "Create draft" : "Save metadata";
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 font-semibold text-2xl">
            <BookOpen className="size-6" />
            Knowledge Catalog
          </h1>
          <p className="text-muted-foreground text-sm">
            Manage source metadata, declared versions, review decisions, and revision history.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" disabled={busy} onClick={() => setListQuery((current) => ({ ...current }))}>
            <RefreshCw className="size-4" />
            Refresh list
          </Button>
          <Button disabled={busy} onClick={() => selectEntry(null)}>
            <Plus className="size-4" />
            New entry
          </Button>
        </div>
      </div>
      <p className="rounded-lg border p-3 text-sm">
        Metadata only. Do not enter credentials or payloads. References are not fetched, uploaded, or executed. Hashes
        and evidence references are declarations. A metadata review does not confirm a finding or enable any skill/tool.
      </p>
      <div className="grid gap-6 xl:grid-cols-[minmax(260px,1fr)_minmax(0,2fr)]">
        <Card>
          <CardHeader>
            <CardTitle>Entries</CardTitle>
            <CardDescription>Authenticated administrator catalog; not a public skill marketplace.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <NativeSelect
                aria-label="Filter catalog kind"
                value={kind}
                disabled={busy}
                onChange={(event) => {
                  setListQuery((current) => ({
                    ...current,
                    kind: event.target.value as KnowledgeKind | "",
                    offset: 0,
                  }));
                }}
              >
                <NativeSelectOption value="">All kinds</NativeSelectOption>
                {KNOWLEDGE_KINDS.map((value) => (
                  <NativeSelectOption key={value} value={value}>
                    {value}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <NativeSelect
                aria-label="Filter metadata review"
                value={status}
                disabled={busy}
                onChange={(event) => {
                  setListQuery((current) => ({
                    ...current,
                    review_status: event.target.value as KnowledgeReviewState | "",
                    offset: 0,
                  }));
                }}
              >
                <NativeSelectOption value="">All review states</NativeSelectOption>
                {KNOWLEDGE_REVIEW_STATES.map((value) => (
                  <NativeSelectOption key={value} value={value}>
                    {value}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            {loading && <p role="status">Loading catalog…</p>}
            {listError && (
              <p role="alert" className="text-destructive text-sm">
                {listError}
              </p>
            )}
            {!loading && page?.items.length === 0 && (
              <p className="text-muted-foreground text-sm">
                No entries match. Create a metadata entry or adjust filters.
              </p>
            )}
            {!loading &&
              page?.items.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  disabled={busy}
                  aria-pressed={selectedId === item.id}
                  onClick={() => selectEntry(item.id)}
                  className="w-full rounded-lg border p-3 text-left hover:bg-muted disabled:opacity-50 aria-pressed:border-primary"
                >
                  <span className="block break-words font-medium">{item.title}</span>
                  <span className="block text-muted-foreground text-xs">
                    {item.kind} · {item.source_version} · revision {item.revision}
                  </span>
                  <Badge variant="outline">{item.review_status}</Badge>
                </button>
              ))}
            <div className="flex items-center justify-between gap-2 text-sm">
              <Button
                variant="outline"
                disabled={busy || loading || offset === 0}
                onClick={() => setListQuery((current) => ({ ...current, offset: Math.max(0, current.offset - 25) }))}
              >
                Previous
              </Button>
              <span>
                {page ? `${offset + (page.items.length ? 1 : 0)}–${offset + page.items.length} / ${page.total}` : "—"}
              </span>
              <Button
                variant="outline"
                disabled={busy || loading || !page || offset + page.items.length >= page.total}
                onClick={() => setListQuery((current) => ({ ...current, offset: current.offset + 25 }))}
              >
                Next
              </Button>
            </div>
          </CardContent>
        </Card>
        <div className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>{selectedId === null ? "New metadata entry" : `Entry #${selectedId}`}</CardTitle>
              <CardDescription>
                Changes to metadata reset review to draft. Revoked entries remain read-only; create a new source version
                instead.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {detailLoading && <p role="status">Loading entry…</p>}
              {detailError && (
                <p role="alert" className="text-destructive text-sm">
                  {detailError}
                </p>
              )}
              {mutationError && (
                <p role="alert" className="text-destructive text-sm">
                  {mutationError} Reload the entry before retrying a revision conflict.
                </p>
              )}
              {entry && (
                <div className="flex items-center gap-2 text-sm">
                  <Badge variant="outline">{entry.review_status}</Badge>
                  <span>Revision {entry.revision}</span>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busy}
                    onClick={() => setDetailQuery((current) => ({ ...current }))}
                  >
                    Reload entry (discard edits)
                  </Button>
                </div>
              )}
              <form onSubmit={save}>
                <fieldset disabled={readOnly} className="space-y-4 disabled:opacity-60">
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-title">Title</Label>
                    <Input
                      id="knowledge-title"
                      value={form.title}
                      maxLength={200}
                      required
                      onChange={(event) => field("title", event.target.value)}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-kind">Kind</Label>
                    <NativeSelect
                      id="knowledge-kind"
                      value={form.kind}
                      onChange={(event) => field("kind", event.target.value)}
                    >
                      {KNOWLEDGE_KINDS.map((value) => (
                        <NativeSelectOption key={value} value={value}>
                          {value}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-source">
                      Source reference (credential-free HTTPS without query parameters, or URN)
                    </Label>
                    <Input
                      id="knowledge-source"
                      value={form.source_ref}
                      maxLength={2048}
                      required
                      onChange={(event) => field("source_ref", event.target.value)}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-version">Declared source version</Label>
                    <Input
                      id="knowledge-version"
                      value={form.source_version}
                      maxLength={128}
                      required
                      onChange={(event) => field("source_version", event.target.value)}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-hash">Declared source SHA-256 (not automatically verified)</Label>
                    <Input
                      id="knowledge-hash"
                      value={form.sha256}
                      maxLength={64}
                      onChange={(event) => field("sha256", event.target.value)}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-license">License / provenance statement</Label>
                    <Input
                      id="knowledge-license"
                      value={form.license}
                      maxLength={256}
                      onChange={(event) => field("license", event.target.value)}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-evidence">
                      Evidence references (one HTTPS reference or URN per line; at most 16; not verified)
                    </Label>
                    <Textarea
                      id="knowledge-evidence"
                      value={evidenceText}
                      onChange={(event) => setEvidenceText(event.target.value)}
                    />
                  </div>
                  <Button type="submit">{busy ? "Saving…" : submitLabel}</Button>
                </fieldset>
              </form>
            </CardContent>
          </Card>
          {entry && (
            <Card>
              <CardHeader>
                <CardTitle>Explicit metadata review</CardTitle>
                <CardDescription>
                  Review applies to the saved revision, not unsaved form edits. Reviewed requires a declared hash,
                  license statement, and note. This is not finding verification.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <fieldset disabled={readOnly} className="space-y-3 disabled:opacity-60">
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-review">Review state</Label>
                    <NativeSelect
                      id="knowledge-review"
                      value={reviewStatus}
                      onChange={(event) => setReviewStatus(event.target.value as KnowledgeReviewState)}
                    >
                      {KNOWLEDGE_REVIEW_STATES.map((value) => (
                        <NativeSelectOption key={value} value={value}>
                          {value}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="knowledge-review-note">Decision note (single paragraph)</Label>
                    <Input
                      id="knowledge-review-note"
                      value={reviewNote}
                      maxLength={2000}
                      onChange={(event) => setReviewNote(event.target.value)}
                    />
                  </div>
                  {metadataDirty && (
                    <p className="text-muted-foreground text-sm">
                      Save or discard metadata edits before reviewing the saved revision.
                    </p>
                  )}
                  <Button
                    disabled={metadataDirty}
                    variant={reviewStatus === "revoked" ? "destructive" : "outline"}
                    onClick={review}
                  >
                    Record review decision
                  </Button>
                </fieldset>
              </CardContent>
            </Card>
          )}
          {entry && (
            <Card>
              <CardHeader>
                <CardTitle>Revision history</CardTitle>
                <CardDescription>
                  Saved metadata snapshots and authenticated operator. Not a tamper-proof log against database
                  administrators.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {history.map((item) => (
                  <details key={item.entry.revision} className="rounded border p-3">
                    <summary className="cursor-pointer text-sm">
                      Revision {item.entry.revision} · {item.entry.review_status} · {item.actor} ·{" "}
                      {new Date(item.recorded_at).toLocaleString()}
                    </summary>
                    <pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">
                      {JSON.stringify(item.entry, null, 2)}
                    </pre>
                  </details>
                ))}
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    disabled={busy || historyOffset === 0}
                    onClick={() => setHistoryOffset(Math.max(0, historyOffset - 25))}
                  >
                    Newer revisions
                  </Button>
                  <Button
                    variant="outline"
                    disabled={busy || history.length < 25 || historyOffset + history.length >= entry.revision}
                    onClick={() => setHistoryOffset(historyOffset + 25)}
                  >
                    Older revisions
                  </Button>
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}
