import { useMemo, useState } from "react";
import { useFrames, useTimeline } from "@/api/frames";
import { frameThumbUrl } from "@/api/client";
import type { FrameKind } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Badge, Card, CardContent, CardHeader, CardTitle, EmptyState, Skeleton } from "@/components/ui/primitives";
import { formatDateTime, formatTime, useQueryParam } from "@/lib/utils";
import { FrameViewer } from "./FrameViewer";
import { kindColor, kindLabel, kindTone } from "./labels";
import { VlmBadge } from "./VlmBits";

const DAY_MS = 86_400_000;

function localDay(offsetDays: number) {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  d.setDate(d.getDate() + offsetDays);
  return toDateInput(d);
}

function toDateInput(d: Date) {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Начало и конец выбранных суток в локальном времени браузера → RFC 3339. */
function dayRange(date: string) {
  const start = new Date(`${date}T00:00:00`);
  return { from: start.toISOString(), to: new Date(start.getTime() + DAY_MS).toISOString(), start };
}

/** Лента за сутки (docs/06-panel.md §7.2): отметки кадров по типу и интервалы offline. */
function TimelineStrip({ deviceId, date, onOpen }: { deviceId: string; date: string; onOpen: (id: string) => void }) {
  const { from, to, start } = dayRange(date);
  const tl = useTimeline(deviceId, from, to);
  const x = (iso: string) => ((new Date(iso).getTime() - start.getTime()) / DAY_MS) * 100;
  const now = Date.now();
  return (
    <div className="space-y-1">
      <div className="relative h-10 overflow-hidden rounded-md border bg-muted/40">
        {tl.data?.offline.map((o, i) => {
          const a = Math.max(0, x(o.from));
          const b = Math.min(100, o.to ? x(o.to) : ((now - start.getTime()) / DAY_MS) * 100);
          return (
            <div
              key={i}
              className="absolute inset-y-0 bg-sev-critical/20"
              style={{ left: `${a}%`, width: `${Math.max(0.3, b - a)}%` }}
              title={`нет связи ${formatTime(o.from)}–${o.to ? formatTime(o.to) : "сейчас"}`}
            />
          );
        })}
        {tl.data?.visits.map((v, i) => {
          const a = Math.max(0, x(v.from));
          const b = Math.min(100, v.to ? x(v.to) : ((now - start.getTime()) / DAY_MS) * 100);
          return (
            <div
              key={`v${i}`}
              className="absolute inset-y-0 bg-sev-info/25"
              style={{ left: `${a}%`, width: `${Math.max(0.3, b - a)}%` }}
              title={`бензовоз ${formatTime(v.from)}–${v.to ? formatTime(v.to) : "сейчас"}`}
            />
          );
        })}
        {tl.data?.frames.map((f) => (
          <button
            key={f.id}
            type="button"
            className="absolute inset-y-1 w-[3px] -translate-x-1/2 rounded-full hover:inset-y-0 hover:w-[5px]"
            style={{ left: `${x(f.taken_at)}%`, background: kindColor[f.kind] }}
            title={`${formatTime(f.taken_at)} · ${kindLabel[f.kind]}`}
            onClick={() => onOpen(f.id)}
          />
        ))}
      </div>
      <div className="flex justify-between text-[10px] text-muted-foreground">
        {[0, 3, 6, 9, 12, 15, 18, 21, 24].map((h) => (
          <span key={h}>{String(h).padStart(2, "0")}</span>
        ))}
      </div>
      <div className="flex flex-wrap gap-3 text-xs text-muted-foreground">
        {(Object.keys(kindLabel) as FrameKind[]).map((k) => (
          <span key={k} className="flex items-center gap-1">
            <span className="inline-block size-2 rounded-full" style={{ background: kindColor[k] }} /> {kindLabel[k]}
          </span>
        ))}
        <span className="flex items-center gap-1">
          <span className="inline-block h-2 w-3 bg-sev-info/40" /> визит
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block h-2 w-3 bg-sev-critical/30" /> нет связи
        </span>
        {tl.data && <span>· кадров: {tl.data.frames.length}</span>}
      </div>
    </div>
  );
}

/** Вкладка «Кадры» (docs/06-panel.md §7): лента за сутки, галерея с фильтрами, просмотр кадра. */
export function FramesTab({ deviceId }: { deviceId: string }) {
  const [date, setDate] = useState(localDay(0));
  const [kind, setKind] = useState("");
  const [tanker, setTanker] = useState("");
  const [open, setOpen] = useQueryParam("frame");
  const range = useMemo(() => dayRange(date), [date]);
  const frames = useFrames({ device: deviceId, kind: kind || undefined, tanker: tanker || undefined, from: range.from, to: range.to });
  const items = frames.data?.pages.flatMap((p) => p.items) ?? [];
  const idx = open ? items.findIndex((f) => f.id === open) : -1;

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex-wrap">
          <CardTitle>Лента за сутки</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => setDate(toDateInput(new Date(range.start.getTime() - DAY_MS)))}>
              ←
            </Button>
            <input type="date" className="h-8 rounded-md border bg-card px-2 text-sm" value={date} max={localDay(0)} onChange={(e) => e.target.value && setDate(e.target.value)} />
            <Button variant="ghost" size="sm" disabled={date >= localDay(0)} onClick={() => setDate(toDateInput(new Date(range.start.getTime() + DAY_MS)))}>
              →
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <TimelineStrip deviceId={deviceId} date={date} onOpen={setOpen} />
        </CardContent>
      </Card>

      <div className="flex flex-wrap items-center gap-2">
        <select className="h-9 rounded-md border bg-card px-2 text-sm" value={kind} onChange={(e) => setKind(e.target.value)} aria-label="Тип кадра">
          <option value="">Все типы</option>
          {(Object.keys(kindLabel) as FrameKind[]).map((k) => (
            <option key={k} value={k}>
              {kindLabel[k]}
            </option>
          ))}
        </select>
        <select className="h-9 rounded-md border bg-card px-2 text-sm" value={tanker} onChange={(e) => setTanker(e.target.value)} aria-label="Ответ модели">
          <option value="">Любой ответ VLM</option>
          <option value="yes">Бензовоз в зоне</option>
          <option value="no">Нет бензовоза</option>
          <option value="unchecked">Не проверялся</option>
          <option value="error">Ошибка VLM</option>
        </select>
        <span className="text-sm text-muted-foreground">{items.length ? `показано ${items.length}` : ""}</span>
      </div>

      {frames.isPending ? (
        <Skeleton className="h-64" />
      ) : items.length === 0 ? (
        <EmptyState title="За эти сутки кадров нет" />
      ) : (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
          {items.map((f) => (
            <button key={f.id} type="button" onClick={() => setOpen(f.id)} className="group overflow-hidden rounded-md border bg-card text-left">
              <div className="relative aspect-video bg-muted">
                <img src={frameThumbUrl(f.id)} alt="" loading="lazy" className="size-full object-cover transition-transform group-hover:scale-105" />
                <Badge tone={kindTone[f.kind]} className="absolute top-1 left-1 shadow">
                  {kindLabel[f.kind]}
                </Badge>
                <VlmBadge o={f.observation} className="absolute top-1 right-1 shadow" />
                {f.diff != null && (
                  <span className="absolute right-1 bottom-1 rounded bg-black/60 px-1 text-[10px] text-white">Δ {Math.round(f.diff * 100)}%</span>
                )}
              </div>
              <p className="px-2 py-1 text-xs text-muted-foreground">{formatDateTime(f.taken_at)}</p>
            </button>
          ))}
        </div>
      )}
      {frames.hasNextPage && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => frames.fetchNextPage()} disabled={frames.isFetchingNextPage}>
            Показать ещё
          </Button>
        </div>
      )}

      <FrameViewer
        frameId={open}
        deviceId={deviceId}
        onClose={() => setOpen(null)}
        onNavigate={setOpen}
        prevId={idx > 0 ? items[idx - 1].id : undefined}
        nextId={idx >= 0 && idx < items.length - 1 ? items[idx + 1].id : undefined}
      />
    </div>
  );
}
