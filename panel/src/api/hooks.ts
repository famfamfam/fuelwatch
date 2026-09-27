import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type {
  Command,
  CommandType,
  DeviceDetail,
  DeviceSummary,
  Mode,
  Notification,
  Page,
  PairingCode,
  User,
} from "./types";

// Ключи кэша. SSE-события (stream/useEventStream) обновляют кэш по этим же ключам.
export const qk = {
  me: ["me"] as const,
  devices: ["devices"] as const,
  device: (id: string) => ["device", id] as const,
  commands: (id: string) => ["commands", id] as const,
  notifications: ["notifications"] as const,
  unread: ["notifications", "unread"] as const,
};

export function useMe() {
  return useQuery({ queryKey: qk.me, queryFn: () => api<User>("/auth/me"), retry: false, staleTime: Infinity });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { login: string; password: string }) => api<User>("/auth/login", { method: "POST", body: v }),
    onSuccess: (u) => qc.setQueryData(qk.me, u),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api("/auth/logout", { method: "POST" }),
    onSettled: () => {
      qc.clear();
      qc.setQueryData(qk.me, null);
    },
  });
}

export function useDevices() {
  return useQuery({ queryKey: qk.devices, queryFn: () => api<DeviceSummary[]>("/devices") });
}

export function useDevice(id: string) {
  return useQuery({ queryKey: qk.device(id), queryFn: () => api<DeviceDetail>(`/devices/${id}`) });
}

export function useCommands(id: string) {
  return useQuery({
    queryKey: qk.commands(id),
    queryFn: () => api<{ items: Command[] }>(`/devices/${id}/commands`).then((r) => r.items),
  });
}

export function useCreateDevice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      api<{ device: DeviceSummary } & PairingCode>("/devices", { method: "POST", body: { name } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.devices }),
  });
}

export function useNewPairingCode(id: string) {
  return useMutation({ mutationFn: () => api<PairingCode>(`/devices/${id}/pairing-code`, { method: "POST" }) });
}

export function usePatchDevice(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name?: string; disabled?: boolean }) => api(`/devices/${id}`, { method: "PATCH", body: v }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.device(id) });
      qc.invalidateQueries({ queryKey: qk.devices });
    },
  });
}

export function useSendCommand(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (type: CommandType) => api<Command>(`/devices/${id}/commands`, { method: "POST", body: { type } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.commands(id) }),
  });
}

export function useSetMode(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (mode: Exclude<Mode, "setup">) => api<Command>(`/devices/${id}/mode`, { method: "POST", body: { mode } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.commands(id) });
      qc.invalidateQueries({ queryKey: qk.device(id) });
    },
  });
}

export interface NotificationFilter {
  device?: string;
  category?: string;
  severity?: string;
  unread?: boolean;
}

export function useNotifications(f: NotificationFilter = {}, limit = 30) {
  return useInfiniteQuery({
    queryKey: [...qk.notifications, "list", f, limit],
    initialPageParam: "",
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: String(limit) });
      if (f.device) q.set("device", f.device);
      if (f.category) q.set("category", f.category);
      if (f.severity) q.set("severity", f.severity);
      if (f.unread) q.set("unread", "true");
      if (pageParam) q.set("cursor", pageParam);
      return api<Page<Notification>>(`/notifications?${q}`);
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useUnreadCount() {
  return useQuery({
    queryKey: qk.unread,
    queryFn: () => api<{ count: number }>("/notifications/unread-count").then((r) => r.count),
  });
}

export function useMarkRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number | "all") =>
      api(id === "all" ? "/notifications/read-all" : `/notifications/${id}/read`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.notifications }),
  });
}
