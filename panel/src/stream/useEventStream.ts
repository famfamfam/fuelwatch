import { useEffect, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { qk } from "@/api/hooks";
import type { DeviceDetail, DeviceSummary, Notification } from "@/api/types";
import { alertNotification } from "@/features/notifications/alerts";

export type StreamState = "connecting" | "open" | "reconnecting";

/**
 * Один EventSource на приложение (docs/06-panel.md §11). События обновляют кэш TanStack Query —
 * без опроса по таймеру. После переподключения активные запросы перечитываются.
 */
export function useEventStream() {
  const qc = useQueryClient();
  const [state, setState] = useState<StreamState>("connecting");

  useEffect(() => {
    let es: EventSource | null = null;
    let dropped = false;

    const connect = () => {
      es = new EventSource("/api/panel/stream");
      es.onopen = () => {
        setState("open");
        if (dropped) qc.invalidateQueries();
        dropped = false;
      };
      es.onerror = () => {
        dropped = true;
        setState("reconnecting");
      };

      const src = es;
      const on = <T,>(name: string, fn: (data: T) => void) =>
        src.addEventListener(name, (e) => fn(JSON.parse((e as MessageEvent).data) as T));

      on<DeviceSummary>("device.status", (d) => applyDeviceStatus(qc, d));
      on<{ device_id: string }>("device.deleted", (e) => {
        qc.setQueryData<DeviceSummary[]>(qk.devices, (list) => list?.filter((x) => x.id !== e.device_id));
        qc.invalidateQueries({ queryKey: qk.notifications });
      });
      on<{ device_id: string }>("frame.created", () => qc.invalidateQueries({ queryKey: ["frames"] }));
      on<{ device_id: string }>("command.updated", (c) => qc.invalidateQueries({ queryKey: qk.commands(c.device_id) }));
      on<{ device_id: string }>("issue.updated", (i) => {
        qc.invalidateQueries({ queryKey: qk.device(i.device_id) });
        qc.invalidateQueries({ queryKey: qk.devices });
      });
      on<unknown>("settings.updated", () => qc.invalidateQueries({ queryKey: ["settings"] }));
      on<{ device_id: string }>("zones.updated", (z) => qc.invalidateQueries({ queryKey: ["zones", z.device_id] }));
      on<{ device_id: string }>("event.created", (e) => qc.invalidateQueries({ queryKey: ["log", e.device_id] }));
      on<unknown>("observation.created", () => qc.invalidateQueries({ queryKey: ["frames"] }));
      on<{ device_id: string }>("visit.updated", (v) => {
        qc.invalidateQueries({ queryKey: ["visits"] });
        qc.invalidateQueries({ queryKey: qk.devices });
        qc.invalidateQueries({ queryKey: qk.device(v.device_id) });
      });
      on<Notification>("notification.created", (n) => {
        qc.invalidateQueries({ queryKey: qk.notifications });
        if (n.device_id) qc.invalidateQueries({ queryKey: ["log", n.device_id] });
        showToast(n);
        alertNotification(n);
      });
    };

    // При уходе со страницы браузер держит её в кэше «назад/вперёд» вместе с открытым потоком ~минуту.
    // По HTTP/1.1 на адрес всего 6 соединений: несколько переходов — и новые страницы ждут. Поэтому
    // поток закрывается на pagehide и открывается заново при возврате (с перечитыванием данных).
    const onHide = () => {
      es?.close();
      es = null;
    };
    const onShow = (e: PageTransitionEvent) => {
      if (e.persisted && !es) {
        connect();
        qc.invalidateQueries();
      }
    };
    connect();
    window.addEventListener("pagehide", onHide);
    window.addEventListener("pageshow", onShow);
    return () => {
      window.removeEventListener("pagehide", onHide);
      window.removeEventListener("pageshow", onShow);
      onHide();
    };
  }, [qc]);

  return state;
}

function applyDeviceStatus(qc: QueryClient, d: DeviceSummary) {
  qc.setQueryData<DeviceSummary[]>(qk.devices, (list) => {
    if (!list) return list;
    return list.some((x) => x.id === d.id) ? list.map((x) => (x.id === d.id ? d : x)) : [...list, d];
  });
  qc.setQueryData<DeviceDetail>(qk.device(d.id), (old) => (old ? { ...old, device: d } : old));
}

function showToast(n: Notification) {
  const opts = { description: n.body || undefined, duration: n.severity === "critical" ? Infinity : 8000 };
  switch (n.severity) {
    case "critical":
      toast.error(n.title, opts);
      break;
    case "warning":
      toast.warning(n.title, opts);
      break;
    case "ok":
      toast.success(n.title, opts);
      break;
    default:
      toast.info(n.title, opts);
  }
}
