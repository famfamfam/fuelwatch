import { useState } from "react";
import { Link } from "react-router";
import { ChevronLeft, ChevronRight, Crosshair, Download, LoaderCircle, PenTool, Sparkles } from "lucide-react";
import { toast } from "sonner";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { errorMessage, frameImageUrl } from "@/api/client";
import { useFrame } from "@/api/frames";
import { useSetReference, useZones } from "@/api/zones";
import { useClassifyFrame } from "@/api/visits";
import type { Frame, Zone } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Badge, Input } from "@/components/ui/primitives";
import { formatDateTime } from "@/lib/utils";
import { toCrop } from "@/features/zones/geometry";
import { kindLabel, kindTone } from "./labels";
import { ObservationFields } from "./VlmBits";

/** Контуры зон поверх кадра. Для выреза (change) зоны переводятся в его координаты. */
export function ZonesOverlay({ zones, crop }: { zones: Zone[]; crop: Frame["crop_rect"] }) {
  return (
    <svg className="pointer-events-none absolute inset-0 size-full" viewBox="0 0 1 1" preserveAspectRatio="none">
      {zones.map((z, i) => (
        <polygon
          key={i}
          points={z.points.map((p) => (crop ? toCrop(p, crop) : p).join(",")).join(" ")}
          fill={z.type === "MONITOR" ? "rgba(22,163,74,0.18)" : "rgba(107,114,128,0.35)"}
          stroke={z.type === "MONITOR" ? "#16a34a" : "#9ca3af"}
          strokeWidth={2}
          vectorEffect="non-scaling-stroke"
          strokeDasharray={z.type === "IGNORE" ? "6 4" : undefined}
        />
      ))}
    </svg>
  );
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}

const statusLabel: Record<Frame["status"], string> = {
  pending: "в очереди",
  processing: "проверяется",
  done: "проверен",
  failed: "ошибка",
  skipped: "не проверялся",
};

/** Просмотр кадра во весь экран (docs/06-panel.md §7.3): зоны, метаданные, ответы VLM, проверка модели, эталон. */
export function FrameViewer({
  frameId,
  deviceId,
  onClose,
  onNavigate,
  prevId,
  nextId,
}: {
  frameId: string | null;
  deviceId: string;
  onClose: () => void;
  onNavigate: (id: string) => void;
  prevId?: string;
  nextId?: string;
}) {
  const q = useFrame(frameId);
  const zones = useZones(deviceId);
  const setRef = useSetReference(deviceId);
  const classify = useClassifyFrame();
  const [showZones, setShowZones] = useState(true);
  const [model, setModel] = useState("");
  const f = q.data?.frame;
  const observations = q.data?.observations ?? [];
  const isRef = zones.data?.reference_frame_id === frameId;

  return (
    <DialogPrimitive.Root open={!!frameId} onOpenChange={(o) => !o && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/80" />
        <DialogPrimitive.Content
          className="fixed inset-2 z-50 flex flex-col gap-3 overflow-y-auto rounded-lg bg-card p-3 outline-none md:inset-6 lg:flex-row"
          onKeyDown={(e) => {
            if ((e.target as HTMLElement).tagName === "INPUT") return;
            if (e.key === "ArrowLeft" && prevId) onNavigate(prevId);
            if (e.key === "ArrowRight" && nextId) onNavigate(nextId);
          }}
        >
          <DialogPrimitive.Title className="sr-only">Кадр</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">Просмотр кадра</DialogPrimitive.Description>
          <div className="relative flex min-h-0 flex-1 items-center justify-center bg-black">
            {frameId && (
              <div className="relative max-h-full">
                <img src={frameImageUrl(frameId)} alt="Кадр" className="max-h-[80vh] w-auto object-contain" />
                {showZones && zones.data?.zones && f && <ZonesOverlay zones={zones.data.zones} crop={f.crop_rect} />}
              </div>
            )}
            {prevId && (
              <Button variant="secondary" size="icon" className="absolute top-1/2 left-2 -translate-y-1/2 opacity-80" onClick={() => onNavigate(prevId)} aria-label="Предыдущий">
                <ChevronLeft />
              </Button>
            )}
            {nextId && (
              <Button variant="secondary" size="icon" className="absolute top-1/2 right-2 -translate-y-1/2 opacity-80" onClick={() => onNavigate(nextId)} aria-label="Следующий">
                <ChevronRight />
              </Button>
            )}
          </div>
          <aside className="w-full shrink-0 space-y-4 lg:w-80">
            <div className="flex items-center justify-between">
              {f ? <Badge tone={kindTone[f.kind]}>{kindLabel[f.kind]}</Badge> : <span />}
              <DialogPrimitive.Close asChild>
                <Button variant="ghost" size="sm">
                  Закрыть
                </Button>
              </DialogPrimitive.Close>
            </div>
            {f && (
              <div className="space-y-1.5">
                <Meta label="Снят">{formatDateTime(f.taken_at)}</Meta>
                <Meta label="Размер">
                  {f.width}×{f.height}
                  {f.crop_rect && " (вырез зоны)"}
                </Meta>
                {f.diff != null && <Meta label="Изменение">{Math.round(f.diff * 100)}%</Meta>}
                {f.zoom != null && <Meta label="Zoom">{f.zoom.toFixed(1)}×</Meta>}
                <Meta label="VLM">{statusLabel[f.status] ?? f.status}</Meta>
              </div>
            )}

            {observations.map((o) => (
              <div key={o.id} className="space-y-2 rounded-lg border p-3">
                <div className="flex flex-wrap items-center justify-between gap-1 text-xs text-muted-foreground">
                  <span className="font-medium text-foreground">{o.dry_run ? "Проверка" : "Ответ модели"}</span>
                  <span>
                    {o.model} · {o.prompt_version} · ${o.cost.toFixed(4)} · {(o.latency_ms / 1000).toFixed(1)} с
                  </span>
                </div>
                {o.error ? <p className="text-sm text-sev-critical">{o.error}</p> : o.result && <ObservationFields r={o.result} />}
              </div>
            ))}

            <form
              className="space-y-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (!frameId) return;
                classify.mutate(
                  { frameId, model: model.trim() || undefined },
                  { onError: (err) => toast.error(errorMessage(err)) },
                );
              }}
            >
              <p className="text-sm font-medium">Проверить модель на этом кадре</p>
              <Input placeholder="модель (пусто — текущая)" value={model} onChange={(e) => setModel(e.target.value)} />
              <Button type="submit" variant="outline" className="w-full" disabled={classify.isPending}>
                {classify.isPending ? <LoaderCircle className="animate-spin" /> : <Sparkles />} Проверить
              </Button>
              <p className="text-xs text-muted-foreground">Результат сохраняется отдельно и на визиты не влияет.</p>
            </form>

            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" className="accent-primary" checked={showZones} onChange={(e) => setShowZones(e.target.checked)} />
              Показать зоны
            </label>
            <div className="grid gap-2">
              {f && !f.crop_rect && (
                <Button
                  variant="outline"
                  disabled={isRef || setRef.isPending}
                  onClick={() =>
                    setRef.mutate(f.id, {
                      onSuccess: () => toast.success("Кадр стал эталоном. Проверьте, что зоны на месте."),
                      onError: (e) => toast.error(errorMessage(e)),
                    })
                  }
                >
                  <Crosshair /> {isRef ? "Это эталон" : "Сделать эталоном"}
                </Button>
              )}
              {f && !f.crop_rect && (
                <Button variant="outline" asChild>
                  <Link to={`/devices/${deviceId}/zones?frame=${f.id}`} onClick={onClose}>
                    <PenTool /> Зоны поверх этого кадра
                  </Link>
                </Button>
              )}
              {frameId && (
                <Button variant="outline" asChild>
                  <a href={frameImageUrl(frameId)} download={`${frameId}.jpg`}>
                    <Download /> Скачать
                  </a>
                </Button>
              )}
            </div>
          </aside>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
