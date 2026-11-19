import { http } from "@/lib/api";
import {
  type KnowledgeEntry,
  type KnowledgeHistoryPage,
  type KnowledgeKind,
  type KnowledgeMetadata,
  type KnowledgePage,
  type KnowledgeReviewState,
  knowledgeMetadata,
  knowledgeReviewPayload,
} from "@/lib/knowledge-catalog";

export const knowledgeCatalogApi = {
  list(
    filters: { kind: KnowledgeKind | ""; review_status: KnowledgeReviewState | ""; offset: number },
    signal?: AbortSignal,
  ) {
    const query = new URLSearchParams({ limit: "25", offset: String(filters.offset) });
    if (filters.kind) query.set("kind", filters.kind);
    if (filters.review_status) query.set("review_status", filters.review_status);
    return http<KnowledgePage>(`/knowledge-catalog?${query}`, { signal });
  },
  get(id: number, signal?: AbortSignal) {
    return http<KnowledgeEntry>(`/knowledge-catalog/${id}`, { signal });
  },
  history(id: number, offset = 0, signal?: AbortSignal) {
    return http<KnowledgeHistoryPage>(`/knowledge-catalog/${id}/history?limit=25&offset=${offset}`, { signal });
  },
  create(metadata: KnowledgeMetadata) {
    return http<KnowledgeEntry>("/knowledge-catalog", {
      method: "POST",
      body: JSON.stringify(knowledgeMetadata(metadata)),
    });
  },
  update(entry: KnowledgeEntry, metadata: KnowledgeMetadata) {
    return http<KnowledgeEntry>(`/knowledge-catalog/${entry.id}`, {
      method: "PUT",
      body: JSON.stringify({ ...knowledgeMetadata(metadata), revision: entry.revision }),
    });
  },
  review(entry: KnowledgeEntry, status: KnowledgeReviewState, note: string) {
    return http<KnowledgeEntry>(`/knowledge-catalog/${entry.id}/review`, {
      method: "POST",
      body: JSON.stringify(knowledgeReviewPayload(entry.revision, status, note)),
    });
  },
};
