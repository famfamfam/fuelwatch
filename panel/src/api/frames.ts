import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { Frame, Observation, Page, Timeline } from "./types";

// Все ключи начинаются с "frames": SSE frame.created инвалидирует их разом.
export interface FrameFilter {
  device?: string;
  kind?: string;
  from?: string;
  to?: string;
  tanker?: string; // yes | no | unchecked | error
}

export function useFrames(f: FrameFilter, limit = 48) {
  return useInfiniteQuery({
    queryKey: ["frames", "list", f, limit],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: String(limit) });
      for (const [k, v] of Object.entries(f)) if (v) q.set(k, v);
      if (pageParam) q.set("cursor", pageParam);
      return api<Page<Frame>>(`/frames?${q}`);
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useFrame(id: string | null) {
  return useQuery({
    queryKey: ["frames", "one", id ?? ""],
    queryFn: () => api<{ frame: Frame; observations: Observation[] }>(`/frames/${id}`),
    enabled: !!id,
  });
}

export function useTimeline(id: string, from: string, to: string) {
  return useQuery({
    queryKey: ["frames", "timeline", id, from, to],
    queryFn: () => api<Timeline>(`/devices/${id}/timeline?${new URLSearchParams({ from, to })}`),
  });
}
