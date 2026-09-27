import { useState } from "react";
import { Link } from "react-router";
import { Square, ThumbsDown, ThumbsUp } from "lucide-react";
import { toast } from "sonner";
import { errorMessage, frameThumbUrl } from "@/api/client";
import { useCloseVisit, useVisit, useVisitFeedback, useVisits } from "@/api/visits";
import type { Visit } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Badge, Card, EmptyState, Skeleton } from "@/components/ui/primitives";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { FrameViewer } from "@/features/frames/FrameViewer";
import { VlmBadge } from "@/features/frames/VlmBits";
import { cn, formatDateTime, formatTime, useNow, useQueryParam } from "@/lib/utils";

export function visitMinutes(v: Pick<Visit, "started_at" | "ended_at" | "last_seen_at">, now: number) {
  const end = v.ended_at ? new Date(v.ended_at).getTime() : now;
  return Math.round((end - new Date(v.started_at).getTime()) / 60000);
}

const endReason: Record<string, string> = {
  max_hours: "закрыт по времени",
  manual: "закрыт вручную",
};

function Feedback({ v }: { v: Visit }) {
  const fb = useVisitFeedback();
  const set = (value: "up" | "down") =>
    fb.mutate({ id: v.id, value: v.feedback === value ? null : value }, { onError: (e) => toast.error(errorMessage(e)) });
  return (
    <div className="flex gap-1">
      <Button
        size="icon"
        variant={v.feedback === "up" ? "default" : "ghost"}
        className="size-8"
        title="Верно"
        onClick={(e) => (e.stopPropagation(), set("up"))}
      >
        <ThumbsUp />
      </Button>
      <Button
        size="icon"
        variant={v.feedback === "down" ? "destructive" : "ghost"}
        className="size-8"
        title="Ошибка"
        onClick={(e) => (e.stopPropagation(), set("down"))}
      >
        <ThumbsDown />
      </Button>
    </div>
  );
}

/** Карточка визита (docs/06-panel.md §9): кадры по времени, ответы модели, оценка, закрытие вручную. */
function VisitDialog({ id, onClose }: { id: number | null; onClose: () => void }) {
  const q = useVisit(id);
  const close = useCloseVisit();
  const [open, setOpen] = useState<string | null>(null);
  const now = useNow(10_000);
  const v = q.data?.visit;
  const frames = q.data?.frames ?? [];
  const idx = open ? frames.findIndex((f) => f.id === open) : -1;
  return (
    <Dialog open={id != null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        title={v ? `Визит · ${v.device_name}` : "Визит"}
        description={
          v
            ? `${formatDateTime(v.started_at)} — ${v.ended_at ? formatTime(v.ended_at) : "сейчас"} · ${visitMinutes(v, now)} мин`
            : undefined
        }
        className="max-w-4xl"
      >
        {!v ? (
          <Skeleton className="h-40" />
        ) : (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              {v.state === "PRESENT" ? <Badge tone="info">идёт</Badge> : <Badge>завершён</Badge>}
              {v.end_reason && endReason[v.end_reason] && <Badge tone="warning">{endReason[v.end_reason]}</Badge>}
              <div className="flex-1" />
              <Feedback v={v} />
              {v.state === "PRESENT" && (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    window.confirm("Закрыть визит вручную? Уведомление об отъезде не придёт.") &&
                    close.mutate(v.id, { onError: (e) => toast.error(errorMessage(e)) })
                  }
                >
                  <Square /> Закрыть визит
                </Button>
              )}
            </div>
            <div className="grid max-h-[60vh] grid-cols-2 gap-2 overflow-y-auto sm:grid-cols-3 md:grid-cols-4">
              {frames.map((f) => (
                <button key={f.id} type="button" onClick={() => setOpen(f.id)} className="overflow-hidden rounded-md border text-left">
                  <div className="relative aspect-video bg-muted">
                    <img src={frameThumbUrl(f.id)} alt="" loading="lazy" className="size-full object-cover" />
                    <VlmBadge o={f.observation} className="absolute top-1 right-1 shadow" />
                  </div>
                  <p className="px-2 py-1 text-xs text-muted-foreground">
                    {formatTime(f.taken_at)} {f.observation?.note && `· ${f.observation.note}`}
                  </p>
                </button>
              ))}
            </div>
          </div>
        )}
        {v && (
          <FrameViewer
            frameId={open}
            deviceId={v.device_id}
            onClose={() => setOpen(null)}
            onNavigate={setOpen}
            prevId={idx > 0 ? frames[idx - 1].id : undefined}
            nextId={idx >= 0 && idx < frames.length - 1 ? frames[idx + 1].id : undefined}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

/** Список визитов: на странице /visits (все устройства) и на вкладке устройства. */
export function VisitList({ deviceId }: { deviceId?: string }) {
  const [state, setState] = useState("");
  const list = useVisits({ device: deviceId, state: state || undefined });
  const [visitParam, setVisitParam] = useQueryParam("visit");
  const open = visitParam ? Number(visitParam) : null;
  const setOpen = (id: number | null) => setVisitParam(id == null ? null : String(id));
  const now = useNow(30_000);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="space-y-3">
      <select className="h-9 rounded-md border bg-card px-2 text-sm" value={state} onChange={(e) => setState(e.target.value)} aria-label="Статус">
        <option value="">Все визиты</option>
        <option value="PRESENT">Идут сейчас</option>
        <option value="LEFT">Завершённые</option>
      </select>
      {list.isPending ? (
        <Skeleton className="h-48" />
      ) : items.length === 0 ? (
        <EmptyState title="Визитов пока не было">
          <p className="max-w-sm text-sm text-muted-foreground">
            Визит появится, когда модель дважды увидит бензовоз в зоне. Устройство должно быть в режиме «Мониторинг».
          </p>
        </EmptyState>
      ) : (
        <Card className="divide-y">
          {items.map((v) => (
            <div
              key={v.id}
              role="button"
              tabIndex={0}
              onClick={() => setOpen(v.id)}
              onKeyDown={(e) => e.key === "Enter" && setOpen(v.id)}
              className="flex cursor-pointer flex-wrap items-center gap-3 px-3 py-2 hover:bg-muted/60"
            >
              {v.first_frame_id ? (
                <img src={frameThumbUrl(v.first_frame_id)} alt="" className="h-12 w-20 rounded object-cover" />
              ) : (
                <div className="h-12 w-20 rounded bg-muted" />
              )}
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">
                  {formatDateTime(v.started_at)} — {v.ended_at ? formatTime(v.ended_at) : "сейчас"}
                  <span className="ml-2 font-normal text-muted-foreground">{visitMinutes(v, now)} мин</span>
                </p>
                {!deviceId && (
                  <Link to={`/devices/${v.device_id}`} onClick={(e) => e.stopPropagation()} className="text-xs text-muted-foreground hover:underline">
                    {v.device_name}
                  </Link>
                )}
              </div>
              <span className={cn(v.state === "PRESENT" && "animate-pulse")}>
                {v.state === "PRESENT" ? <Badge tone="info">идёт</Badge> : v.end_reason && endReason[v.end_reason] ? <Badge tone="warning">{endReason[v.end_reason]}</Badge> : null}
              </span>
              <Feedback v={v} />
            </div>
          ))}
        </Card>
      )}
      {list.hasNextPage && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Показать ещё
          </Button>
        </div>
      )}
      <VisitDialog id={open} onClose={() => setOpen(null)} />
    </div>
  );
}

export function VisitsPage() {
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Визиты</h1>
      <VisitList />
    </div>
  );
}
