import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { DeviceSettings, SettingValue, SettingsSchema } from "./types";

export type SettingsChanges = Record<string, SettingValue | null>;

export const settingsKeys = {
  schema: (device?: string) => ["settings", "schema", device ?? ""] as const,
  global: ["settings", "global"] as const,
  device: (id: string) => ["settings", "device", id] as const,
};

/** Реестр настроек; с device — диапазоны и варианты из возможностей камеры телефона. */
export function useSettingsSchema(device?: string) {
  return useQuery({
    queryKey: settingsKeys.schema(device),
    queryFn: () => api<SettingsSchema>(`/settings/schema${device ? `?device=${device}` : ""}`),
    staleTime: 5 * 60_000,
  });
}

export function useGlobalSettings() {
  return useQuery({ queryKey: settingsKeys.global, queryFn: () => api<Record<string, SettingValue>>("/settings/global") });
}

export function usePutGlobalSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (changes: SettingsChanges) =>
      api<Record<string, SettingValue>>("/settings/global", { method: "PUT", body: changes }),
    onSuccess: (data) => {
      qc.setQueryData(settingsKeys.global, data);
      qc.invalidateQueries({ queryKey: ["settings", "device"] });
    },
  });
}

export function useDeviceSettings(id: string) {
  return useQuery({ queryKey: settingsKeys.device(id), queryFn: () => api<DeviceSettings>(`/devices/${id}/settings`) });
}

export function usePutDeviceSettings(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (changes: SettingsChanges) => api<DeviceSettings>(`/devices/${id}/settings`, { method: "PUT", body: changes }),
    onSuccess: (data) => qc.setQueryData(settingsKeys.device(id), data),
  });
}
