import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { qk } from "./hooks";
import type { Zone, ZonesState, ZoneVersion } from "./types";

export const zoneKeys = {
  zones: (id: string) => ["zones", id] as const,
  versions: (id: string) => ["zones", id, "versions"] as const,
};

export function useZones(id: string) {
  return useQuery({ queryKey: zoneKeys.zones(id), queryFn: () => api<ZonesState>(`/devices/${id}/zones`) });
}

export function useZoneVersions(id: string) {
  return useQuery({
    queryKey: zoneKeys.versions(id),
    queryFn: () => api<{ items: ZoneVersion[] }>(`/devices/${id}/zones/versions`).then((r) => r.items),
  });
}

function onZonesSaved(qc: ReturnType<typeof useQueryClient>, id: string, data: ZonesState) {
  qc.setQueryData(zoneKeys.zones(id), data);
  qc.invalidateQueries({ queryKey: zoneKeys.versions(id) });
}

/** Сохранить новую версию. 409 — кто-то сохранил раньше (base_version устарела). */
export function useSaveZones(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { zones: Zone[]; base_version: number }) => api<ZonesState>(`/devices/${id}/zones`, { method: "PUT", body: v }),
    onSuccess: (data) => onZonesSaved(qc, id, data),
  });
}

export function useRestoreZones(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (version: number) => api<ZonesState>(`/devices/${id}/zones/restore`, { method: "POST", body: { version } }),
    onSuccess: (data) => onZonesSaved(qc, id, data),
  });
}

export function useSetReference(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (frameId: string) => api(`/devices/${id}/reference`, { method: "POST", body: { frame_id: frameId } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.device(id) });
      qc.invalidateQueries({ queryKey: zoneKeys.zones(id) });
    },
  });
}

/** Живой режим: без аргумента — на live.duration_s, 0 — выключить. */
export function useSetLive(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (durationS?: number) =>
      api<{ live_until: string | null }>(`/devices/${id}/live`, {
        method: "POST",
        body: durationS === undefined ? {} : { duration_s: durationS },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.device(id) }),
  });
}
