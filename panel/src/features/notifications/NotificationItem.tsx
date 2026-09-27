import { useNavigate } from "react-router";
import { CircleAlert, Fuel, Check, TriangleAlert, Send } from "lucide-react";
import type { Notification, Severity } from "@/api/types";
import { useMarkRead } from "@/api/hooks";
import { cn, formatDateTime, stripEmoji } from "@/lib/utils";

const icon: Record<Severity, typeof Check> = {
  info: Fuel,
  warning: TriangleAlert,
  critical: CircleAlert,
  ok: Check,
};

const color: Record<Severity, string> = {
  info: "text-sev-info",
  warning: "text-sev-warning",
  critical: "text-sev-critical",
  ok: "text-sev-ok",
};


export function NotificationItem({ n, compact = false }: { n: Notification; compact?: boolean }) {
  const nav = useNavigate();
  const markRead = useMarkRead();
  const Icon = icon[n.severity] ?? CircleAlert;
  const unread = !n.read_at;

  const open = () => {
    if (unread) markRead.mutate(n.id);
    // Открыть связанный объект: визит, иначе кадр, иначе устройство.
    if (!n.device_id) return;
    if (n.visit_id) nav(`/devices/${n.device_id}/visits?visit=${n.visit_id}`);
    else if (n.frame_id) nav(`/devices/${n.device_id}/frames?frame=${n.frame_id}`);
    else nav(`/devices/${n.device_id}`);
  };

  return (
    <button
      type="button"
      onClick={open}
      className={cn(
        "flex w-full items-start gap-3 rounded-md px-3 py-2 text-left transition-colors hover:bg-muted",
        unread && "bg-primary/5",
      )}
    >
      <Icon className={cn("mt-0.5 size-4 shrink-0", color[n.severity])} />
      <div className="min-w-0 flex-1">
        <p className={cn("text-sm", unread && "font-medium", compact && "line-clamp-2")}>{stripEmoji(n.title)}</p>
        {n.body && !compact && <p className="text-sm text-muted-foreground">{n.body}</p>}
        <p className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
          {formatDateTime(n.created_at)}
          {n.telegram_status === "sent" && <Send className="size-3" aria-label="отправлено в Telegram" />}
          {n.telegram_status === "failed" && <Send className="size-3 text-sev-critical" aria-label="ошибка Telegram" />}
        </p>
      </div>
      {unread && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary" aria-label="не прочитано" />}
    </button>
  );
}
