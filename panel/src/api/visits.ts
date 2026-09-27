import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { CostBucket, Frame, ObservationResult, Page, Visit } from "./types";

// Ключи начинаются с "visits": SSE visit.updated инвалидирует их разом.
export function useVisits(f: { device?: string; state?: string } = {}, limit = 30) {
  return useInfiniteQuery({
    queryKey: ["visits", "list", f, limit],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: String(limit) });
      if (f.device) q.set("device", f.device);
      if (f.state) q.set("state", f.state);
      if (pageParam) q.set("cursor", pageParam);
      return api<Page<Visit>>(`/visits?${q}`);
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useVisit(id: number | null) {
  return useQuery({
    queryKey: ["visits", "one", id],
    queryFn: () => api<{ visit: Visit; frames: Frame[] }>(`/visits/${id}`),
    enabled: id != null,
  });
}

export function useVisitFeedback() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: number; value: "up" | "down" | null }) =>
      api(`/visits/${v.id}/feedback`, { method: "POST", body: { value: v.value } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["visits"] }),
  });
}

export function useCloseVisit() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api(`/visits/${id}/close`, { method: "POST" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["visits"] });
      qc.invalidateQueries({ queryKey: ["devices"] });
    },
  });
}

export interface ClassifyResult {
  observation_id: number;
  provider: string;
  model: string;
  prompt_version: string;
  cost: number;
  tokens_in: number;
  tokens_out: number;
  latency_ms: number;
  result?: ObservationResult;
  error?: string;
}

/** Проверка модели на кадре (dry-run): на визиты не влияет. */
export function useClassifyFrame() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { frameId: string; model?: string }) =>
      api<ClassifyResult>(`/frames/${v.frameId}/classify`, { method: "POST", body: v.model ? { model: v.model } : {} }),
    onSettled: (_d, _e, v) => qc.invalidateQueries({ queryKey: ["frames", "one", v.frameId] }),
  });
}

export function useCosts(from: string, to: string) {
  return useQuery({
    queryKey: ["costs", from, to],
    queryFn: () =>
      api<{ by_day: CostBucket[]; by_model: CostBucket[]; by_device: CostBucket[] }>(`/costs?${new URLSearchParams({ from, to })}`),
  });
}

export function useTelegramStatus() {
  return useQuery({ queryKey: ["telegram"], queryFn: () => api<{ configured: boolean }>("/notify/telegram"), staleTime: Infinity });
}

export function useTelegramTest() {
  return useMutation({ mutationFn: () => api<{ sent: number }>("/notify/telegram/test", { method: "POST" }) });
}
