import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, Smartphone, Terminal } from "lucide-react";
import { api } from "@/api/client";
import type { CommandStatus, CommandType, Severity } from "@/api/types";
import { Badge, Card, EmptyState, Skeleton } from "@/components/ui/primitives";
import { formatDateTime, stripEmoji } from "@/lib/utils";
import { commandLabel, commandStatusLabel, commandStatusTone } from "./labels";

interface Entry {
  kind: "notification" | "command" | "event";
  time: string;
  type: string;
  title: string;
  who?: string | null;
  state?: string;
  data?: Record<string, unknown> | null;
}

const eventLabel: Record<string, string> = {
  BOOT: "Телефон перезагрузился",
  POWER_ON: "Питание подключено",
  POWER_OFF: "Питание отключено",
  MOVED: "Телефон сдвинули",
  CAMERA_ERROR: "Камера не отвечает",
  APP_ERROR: "Ошибка приложения",
  CONFIG_REJECTED: "Настройка отклонена телефоном",
};

const kinds = [
  { id: "", label: "Всё" },
  { id: "notification", label: "Уведомления" },
  { id: "command", label: "Команды" },
  { id: "event", label: "События телефона" },
];

function eventDetails(e: Entry) {
  const d = e.data ?? {};
  if (typeof d.tilt_deg === "number") return `наклон ${d.tilt_deg.toFixed(1)}°`;
  if (typeof d.shake === "number") return `тряска ${d.shake.toFixed(1)} м/с²`;
  if (typeof d.battery === "number") return `заряд ${d.battery}%`;
  if (typeof d.age_s === "number") return `нет кадров ${d.age_s} с`;
  return "";
}

/** Журнал устройства (docs/06-panel.md §4.2): уведомления, команды, события телефона. */
export function DeviceLog({ deviceId }: { deviceId: string }) {
  const [kind, setKind] = useState("");
  const q = useQuery({
    queryKey: ["log", deviceId, kind],
    queryFn: () => api<{ items: Entry[] }>(`/devices/${deviceId}/log?${new URLSearchParams({ kind, limit: "200" })}`).then((r) => r.items),
  });
  return (
    <div className="space-y-3">
      <select className="h-9 rounded-md border bg-card px-2 text-sm" value={kind} onChange={(e) => setKind(e.target.value)} aria-label="Тип записи">
        {kinds.map((k) => (
          <option key={k.id} value={k.id}>
            {k.label}
          </option>
        ))}
      </select>
      {q.isPending ? (
        <Skeleton className="h-64" />
      ) : !q.data?.length ? (
        <EmptyState title="Записей нет" />
      ) : (
        <Card className="divide-y">
          {q.data.map((e, i) => (
            <div key={i} className="flex items-start gap-3 px-3 py-2 text-sm">
              <span className="mt-0.5 text-muted-foreground">
                {e.kind === "notification" ? <Bell className="size-4" /> : e.kind === "command" ? <Terminal className="size-4" /> : <Smartphone className="size-4" />}
              </span>
              <div className="min-w-0 flex-1">
                {e.kind === "notification" && <p>{stripEmoji(e.title)}</p>}
                {e.kind === "command" && (
                  <p>
                    {commandLabel[e.type as CommandType] ?? e.type}
                    {e.who && <span className="text-muted-foreground"> · {e.who}</span>}
                    {e.title && e.state !== "expired" && <span className="text-sev-critical"> · {e.title}</span>}
                  </p>
                )}
                {e.kind === "event" && (
                  <p>
                    {eventLabel[e.type] ?? e.type}
                    {eventDetails(e) && <span className="text-muted-foreground"> · {eventDetails(e)}</span>}
                  </p>
                )}
                <p className="text-xs text-muted-foreground">{formatDateTime(e.time)}</p>
              </div>
              {e.kind === "command" && e.state && (
                <Badge tone={commandStatusTone[e.state as CommandStatus]}>{commandStatusLabel[e.state as CommandStatus]}</Badge>
              )}
              {e.kind === "notification" && e.state === "critical" && <Badge tone={"critical" as Severity}>важно</Badge>}
            </div>
          ))}
        </Card>
      )}
    </div>
  );
}
