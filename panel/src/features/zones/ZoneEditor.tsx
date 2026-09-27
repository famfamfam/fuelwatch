import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { Circle, Image as KImage, Layer, Line, Stage } from "react-konva";
import type Konva from "konva";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  Check,
  Eraser,
  LoaderCircle,
  Minus,
  Plus,
  Redo2,
  RotateCcw,
  Save,
  Trash2,
  Undo2,
  ZoomIn,
} from "lucide-react";
import { ApiError, errorMessage, frameImageUrl } from "@/api/client";
import { useDevice } from "@/api/hooks";
import { useRestoreZones, useSaveZones, useZones, useZoneVersions } from "@/api/zones";
import type { Point, Zone, ZoneType } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Badge, Card, CardContent, CardHeader, CardTitle, EmptyState, Skeleton } from "@/components/ui/primitives";
import { cn, formatDateTime } from "@/lib/utils";
import { checkZones, dist, flat, midpoint, toNorm, toPx, translate } from "./geometry";

const COLORS: Record<ZoneType, { stroke: string; fill: string }> = {
  MONITOR: { stroke: "#16a34a", fill: "rgba(22,163,74,0.2)" },
  IGNORE: { stroke: "#9ca3af", fill: "rgba(107,114,128,0.35)" },
};
const MAX_SCALE = 8;
const CLOSE_RADIUS_PX = 10;
const DUP_EPS = 0.002;
const DBLCLICK_MS = 400;

type Selection = { zone: number; vertex?: number } | null;
type Drawing = { type: ZoneType; points: Point[] };
type Background = "reference" | "last" | "frame";

function useImage(url: string | null) {
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  useEffect(() => {
    if (!url) {
      setImg(null);
      return;
    }
    const el = new window.Image();
    el.onload = () => setImg(el);
    el.src = url;
    return () => {
      el.onload = null;
    };
  }, [url]);
  return img;
}

/** Ширина элемента. Callback-ref: элемент появляется не при первом рендере (сначала скелетон загрузки). */
function useWidth<T extends HTMLElement>() {
  const [width, setWidth] = useState(0);
  const roRef = useRef<ResizeObserver | null>(null);
  const ref = useCallback((el: T | null) => {
    roRef.current?.disconnect();
    roRef.current = null;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.floor(e.contentRect.width)));
    ro.observe(el);
    roRef.current = ro;
  }, []);
  return [ref, width] as const;
}

const same = (a: Zone[], b: Zone[]) => JSON.stringify(a) === JSON.stringify(b);

// История правок: одно состояние, чтобы отмена/повтор не зависели от порядка setState.
type History = { past: Zone[][]; present: Zone[]; future: Zone[][] };
type HistoryAction =
  | { type: "commit"; zones: Zone[] } // новое состояние с записью в историю
  | { type: "checkpoint" } // запомнить текущее перед перетаскиванием
  | { type: "update"; fn: (z: Zone[]) => Zone[] } // без записи (во время перетаскивания)
  | { type: "undo" }
  | { type: "redo" }
  | { type: "reset"; zones: Zone[] }; // загрузка/сохранение: история очищается

const HISTORY_LIMIT = 100;

function historyReducer(h: History, a: HistoryAction): History {
  switch (a.type) {
    case "commit":
      return { past: [...h.past, h.present].slice(-HISTORY_LIMIT), present: a.zones, future: [] };
    case "checkpoint":
      return { past: [...h.past, h.present].slice(-HISTORY_LIMIT), present: h.present, future: [] };
    case "update":
      return { ...h, present: a.fn(h.present) };
    case "undo":
      return h.past.length
        ? { past: h.past.slice(0, -1), present: h.past[h.past.length - 1], future: [h.present, ...h.future] }
        : h;
    case "redo":
      return h.future.length ? { past: [...h.past, h.present], present: h.future[0], future: h.future.slice(1) } : h;
    case "reset":
      return { past: [], present: a.zones, future: [] };
  }
}

/** Редактор зон (docs/06-panel.md §5). Координаты — нормализованные [0..1] полного повёрнутого кадра. */
export function ZoneEditor({ deviceId }: { deviceId: string }) {
  const zonesQ = useZones(deviceId);
  const device = useDevice(deviceId);
  const save = useSaveZones(deviceId);
  const [params] = useSearchParams();
  const frameParam = params.get("frame");

  const [hist, dispatch] = useReducer(historyReducer, { past: [], present: [], future: [] });
  const zones = hist.present;
  const [saved, setSaved] = useState<Zone[]>([]);
  const [baseVersion, setBaseVersion] = useState(0);
  const [sel, setSel] = useState<Selection>(null);
  // Рисуемый полигон — и в state (для отрисовки), и в ref: быстрые клики (двойной клик) приходят
  // раньше перерисовки, и обработчик должен видеть уже добавленные точки.
  const [drawing, setDrawingState] = useState<Drawing | null>(null);
  const drawingRef = useRef<Drawing | null>(null);
  const setDrawing = useCallback((next: Drawing | null | ((d: Drawing | null) => Drawing | null)) => {
    const v = typeof next === "function" ? next(drawingRef.current) : next;
    drawingRef.current = v;
    setDrawingState(v);
  }, []);
  const lastClick = useRef<{ t: number; p: Point } | null>(null);
  const [cursor, setCursor] = useState<Point | null>(null);
  const [bg, setBg] = useState<Background>(frameParam ? "frame" : "reference");
  const [overlay, setOverlay] = useState<"none" | "last" | "reference">("none");
  const [opacity, setOpacity] = useState(0.5);
  const [scale, setScale] = useState(1);
  const [pos, setPos] = useState({ x: 0, y: 0 });
  const stageRef = useRef<Konva.Stage>(null);
  const [boxRef, width] = useWidth<HTMLDivElement>();

  const dirty = !same(zones, saved);

  // Загрузка и обновление с сервера (в т. ч. по SSE zones.updated), если нет несохранённых правок.
  useEffect(() => {
    const d = zonesQ.data;
    if (!d) return;
    const z = d.zones ?? [];
    setSaved(z);
    setBaseVersion(d.version);
    if (same(zones, saved)) dispatch({ type: "reset", zones: z });
  }, [zonesQ.data]);

  const dev = device.data?.device;
  const refId = zonesQ.data?.reference_frame_id ?? null;
  const lastId = dev?.last_frame_id ?? null;
  const bgId = bg === "frame" ? frameParam : bg === "last" ? lastId : (refId ?? lastId);
  const overlayId = overlay === "last" ? lastId : overlay === "reference" ? refId : null;
  const bgImg = useImage(bgId ? frameImageUrl(bgId) : null);
  const overlayImg = useImage(overlayId ? frameImageUrl(overlayId) : null);

  const aspect = bgImg ? bgImg.naturalHeight / bgImg.naturalWidth : 9 / 16;
  const size = useMemo(() => ({ width: width || 1, height: Math.round((width || 1) * aspect) }), [width, aspect]);

  const commit = useCallback((next: Zone[]) => dispatch({ type: "commit", zones: next }), []);

  const undo = useCallback(() => {
    if (drawing) {
      // Во время рисования Ctrl+Z убирает последнюю точку.
      setDrawing((d) => (d && d.points.length > 1 ? { ...d, points: d.points.slice(0, -1) } : null));
      return;
    }
    dispatch({ type: "undo" });
    setSel(null);
  }, [drawing, setDrawing]);

  const redo = useCallback(() => {
    dispatch({ type: "redo" });
    setSel(null);
  }, []);

  const finishDrawing = useCallback(() => {
    const d = drawingRef.current;
    if (!d) return;
    const pts = d.points.filter((p, i, a) => i === 0 || dist(p, a[i - 1]) > DUP_EPS);
    if (pts.length >= 3) {
      commit([...zones, { type: d.type, points: pts }]);
      setSel({ zone: zones.length });
    } else {
      toast.warning("Нужно не меньше трёх точек");
    }
    setDrawing(null);
    setCursor(null);
    lastClick.current = null;
  }, [zones, commit, setDrawing]);

  const deleteSelection = useCallback(() => {
    if (!sel) return;
    const z = zones[sel.zone];
    if (!z) return;
    if (sel.vertex !== undefined && z.points.length > 3) {
      commit(zones.map((zz, i) => (i === sel.zone ? { ...zz, points: zz.points.filter((_, j) => j !== sel.vertex) } : zz)));
      setSel({ zone: sel.zone });
    } else {
      commit(zones.filter((_, i) => i !== sel.zone));
      setSel(null);
    }
  }, [sel, zones, commit]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t.tagName === "INPUT" || t.tagName === "SELECT" || t.tagName === "TEXTAREA") return;
      const mod = e.ctrlKey || e.metaKey;
      if (mod && e.key.toLowerCase() === "z") {
        e.preventDefault();
        if (e.shiftKey) redo();
        else undo();
      } else if (mod && e.key.toLowerCase() === "y") {
        e.preventDefault();
        redo();
      } else if (e.key === "Enter" && drawing) {
        // Иначе Enter ещё и «нажмёт» кнопку в фокусе (например, «+ IGNORE»).
        e.preventDefault();
        finishDrawing();
      } else if (e.key === "Escape") {
        if (drawing) setDrawing(null);
        else setSel(null);
      } else if ((e.key === "Delete" || e.key === "Backspace") && sel && !drawing) {
        e.preventDefault();
        deleteSelection();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [undo, redo, drawing, finishDrawing, deleteSelection, sel]);

  const pointer = (): Point | null => {
    const p = stageRef.current?.getRelativePointerPosition();
    return p ? toNorm([p.x, p.y], size) : null;
  };

  const onStageClick = (e: Konva.KonvaEventObject<MouseEvent | TouchEvent>) => {
    if ("button" in e.evt && e.evt.button !== 0) return;
    const d = drawingRef.current;
    if (d) {
      const p = pointer();
      if (!p) return;
      const near = (a: Point) => dist(toPx(a, size), toPx(p, size)) * scale < CLOSE_RADIUS_PX;
      if (d.points.length >= 3 && near(d.points[0])) {
        finishDrawing();
        return;
      }
      // Двойной клик = второй клик в ту же точку быстро. Встроенный dblclick Konva не подходит:
      // он срабатывает на два быстрых клика в РАЗНЫЕ точки одного объекта (подложки).
      const now = Date.now();
      const prev = lastClick.current;
      if (prev && now - prev.t < DBLCLICK_MS && near(prev.p)) {
        finishDrawing();
        return;
      }
      lastClick.current = { t: now, p };
      setDrawing({ ...d, points: [...d.points, p] });
      return;
    }
    if (e.target === e.target.getStage() || e.target.getClassName() === "Image") setSel(null);
  };

  const onWheel = (e: Konva.KonvaEventObject<WheelEvent>) => {
    e.evt.preventDefault();
    const stage = stageRef.current;
    const ptr = stage?.getPointerPosition();
    if (!stage || !ptr) return;
    const next = Math.min(MAX_SCALE, Math.max(1, scale * (e.evt.deltaY < 0 ? 1.15 : 1 / 1.15)));
    const at = { x: (ptr.x - pos.x) / scale, y: (ptr.y - pos.y) / scale };
    setScale(next);
    setPos(next === 1 ? { x: 0, y: 0 } : { x: ptr.x - at.x * next, y: ptr.y - at.y * next });
  };

  const zoomBy = (k: number) => {
    const next = Math.min(MAX_SCALE, Math.max(1, scale * k));
    const c = { x: size.width / 2, y: size.height / 2 };
    const at = { x: (c.x - pos.x) / scale, y: (c.y - pos.y) / scale };
    setScale(next);
    setPos(next === 1 ? { x: 0, y: 0 } : { x: c.x - at.x * next, y: c.y - at.y * next });
  };

  const moveVertex = (zi: number, vi: number, node: Konva.Node) => {
    const p = toNorm([node.x(), node.y()], size);
    const [px, py] = toPx(p, size);
    node.position({ x: px, y: py });
    dispatch({ type: "update", fn: (zs) => zs.map((z, i) => (i === zi ? { ...z, points: z.points.map((q, j) => (j === vi ? p : q)) } : z)) });
  };

  const doSave = () => {
    const check = checkZones(zones);
    if (check.errors.length) {
      toast.error(check.errors[0]);
      return;
    }
    save.mutate(
      { zones, base_version: baseVersion },
      {
        onSuccess: (d) => {
          setSaved(d.zones ?? []);
          dispatch({ type: "reset", zones: d.zones ?? [] });
          setBaseVersion(d.version);
          toast.success(`Зоны сохранены (версия ${d.version}). Телефон получит их в течение ~20 с`);
        },
        onError: (e) => {
          if (e instanceof ApiError && e.status === 409) {
            toast.error("Зоны уже изменил кто-то другой. Нажмите «Сбросить», чтобы загрузить их версию.");
            zonesQ.refetch();
          } else toast.error(errorMessage(e));
        },
      },
    );
  };

  const reset = () => {
    dispatch({ type: "reset", zones: saved });
    setSel(null);
    setDrawing(null);
  };

  if (zonesQ.isPending || device.isPending) return <Skeleton className="h-96" />;

  const check = checkZones(zones);
  const vScale = 1 / scale;
  const applied = dev && zonesQ.data && dev.config_version_device >= zonesQ.data.config_version;
  const noImage = !bgId;

  return (
    <div className="grid gap-4 xl:grid-cols-[1fr_20rem]">
      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant={drawing?.type === "MONITOR" ? "default" : "outline"} onClick={() => (setDrawing({ type: "MONITOR", points: [] }), setSel(null))}>
            <Plus /> MONITOR
          </Button>
          <Button size="sm" variant={drawing?.type === "IGNORE" ? "default" : "outline"} onClick={() => (setDrawing({ type: "IGNORE", points: [] }), setSel(null))}>
            <Plus /> IGNORE
          </Button>
          <Button size="sm" variant="ghost" onClick={undo} disabled={!hist.past.length && !drawing?.points.length} title="Отменить (Ctrl+Z)">
            <Undo2 />
          </Button>
          <Button size="sm" variant="ghost" onClick={redo} disabled={!hist.future.length} title="Повторить (Ctrl+Shift+Z)">
            <Redo2 />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={!zones.length}
            onClick={() => window.confirm("Удалить все зоны? Их можно будет вернуть отменой или из истории версий.") && (commit([]), setSel(null))}
          >
            <Eraser /> Очистить
          </Button>
          <div className="flex-1" />
          <Button size="sm" variant="ghost" onClick={() => zoomBy(1 / 1.5)} disabled={scale <= 1} title="Уменьшить">
            <Minus />
          </Button>
          <span className="w-10 text-center text-xs text-muted-foreground">{Math.round(scale * 100)}%</span>
          <Button size="sm" variant="ghost" onClick={() => zoomBy(1.5)} title="Увеличить (колесо мыши)">
            <ZoomIn />
          </Button>
        </div>

        <div ref={boxRef} className="relative overflow-hidden rounded-lg border bg-black">
          {noImage && (
            <div className="absolute inset-0 z-10 flex items-center justify-center p-6 text-center text-sm text-white/80">
              Нет кадра для подложки. Сделайте снимок или эталон на вкладке «Камера».
            </div>
          )}
          {width > 0 && (
            <Stage
              ref={stageRef}
              width={size.width}
              height={size.height}
              scaleX={scale}
              scaleY={scale}
              x={pos.x}
              y={pos.y}
              draggable={scale > 1 && !drawing}
              dragBoundFunc={(p) => ({
                x: Math.min(0, Math.max(size.width - size.width * scale, p.x)),
                y: Math.min(0, Math.max(size.height - size.height * scale, p.y)),
              })}
              onDragEnd={(e) => e.target === stageRef.current && setPos({ x: e.target.x(), y: e.target.y() })}
              onWheel={onWheel}
              onClick={onStageClick}
              onTap={onStageClick}
              onMouseMove={() => drawing && setCursor(pointer())}
              onContextMenu={(e) => e.evt.preventDefault()}
              style={{ cursor: drawing ? "crosshair" : "default", touchAction: "none" }}
            >
              <Layer>
                {bgImg && <KImage image={bgImg} width={size.width} height={size.height} />}
                {overlayImg && <KImage image={overlayImg} width={size.width} height={size.height} opacity={opacity} listening={false} />}
              </Layer>
              <Layer>
                {zones.map((z, zi) => {
                  const selected = sel?.zone === zi;
                  const c = COLORS[z.type];
                  return (
                    <Line
                      key={`poly-${zi}`}
                      points={flat(z.points, size)}
                      closed
                      fill={c.fill}
                      stroke={c.stroke}
                      strokeWidth={(selected ? 3 : 2) * vScale}
                      dash={z.type === "IGNORE" ? [8 * vScale, 5 * vScale] : undefined}
                      draggable={selected && !drawing}
                      onClick={(e) => {
                        if (drawing) return;
                        e.cancelBubble = true;
                        setSel({ zone: zi });
                      }}
                      onTap={(e) => {
                        if (drawing) return;
                        e.cancelBubble = true;
                        setSel({ zone: zi });
                      }}
                      onDragStart={(e) => {
                        e.cancelBubble = true;
                        dispatch({ type: "checkpoint" });
                      }}
                      onDragEnd={(e) => {
                        e.cancelBubble = true;
                        const node = e.target;
                        const dx = node.x() / size.width;
                        const dy = node.y() / size.height;
                        node.position({ x: 0, y: 0 });
                        dispatch({ type: "update", fn: (zs) => zs.map((zz, i) => (i === zi ? { ...zz, points: translate(zz.points, dx, dy) } : zz)) });
                      }}
                    />
                  );
                })}
                {sel &&
                  zones[sel.zone] &&
                  zones[sel.zone].points.map((p, vi, pts) => {
                    const next = pts[(vi + 1) % pts.length];
                    const [mx, my] = toPx(midpoint(p, next), size);
                    return (
                      <Circle
                        key={`mid-${vi}`}
                        x={mx}
                        y={my}
                        radius={5 * vScale}
                        fill="white"
                        opacity={0.8}
                        stroke={COLORS[zones[sel.zone].type].stroke}
                        strokeWidth={1.5 * vScale}
                        onClick={(e) => {
                          e.cancelBubble = true;
                          const z = zones[sel.zone];
                          const pts2 = [...z.points];
                          pts2.splice(vi + 1, 0, midpoint(p, next));
                          commit(zones.map((zz, i) => (i === sel.zone ? { ...zz, points: pts2 } : zz)));
                          setSel({ zone: sel.zone, vertex: vi + 1 });
                        }}
                        onTap={(e) => {
                          e.cancelBubble = true;
                          const z = zones[sel.zone];
                          const pts2 = [...z.points];
                          pts2.splice(vi + 1, 0, midpoint(p, next));
                          commit(zones.map((zz, i) => (i === sel.zone ? { ...zz, points: pts2 } : zz)));
                        }}
                      />
                    );
                  })}
                {zones.map((z, zi) =>
                  z.points.map((p, vi) => {
                    const [x, y] = toPx(p, size);
                    const active = sel?.zone === zi && sel.vertex === vi;
                    return (
                      <Circle
                        key={`v-${zi}-${vi}`}
                        x={x}
                        y={y}
                        radius={(active ? 8 : sel?.zone === zi ? 7 : 5) * vScale}
                        fill={active ? COLORS[z.type].stroke : "white"}
                        stroke={COLORS[z.type].stroke}
                        strokeWidth={2 * vScale}
                        draggable={!drawing}
                        onMouseDown={(e) => (e.cancelBubble = true)}
                        onClick={(e) => {
                          e.cancelBubble = true;
                          if (!drawing) setSel({ zone: zi, vertex: vi });
                        }}
                        onTap={(e) => {
                          e.cancelBubble = true;
                          if (!drawing) setSel({ zone: zi, vertex: vi });
                        }}
                        onContextMenu={(e) => {
                          e.evt.preventDefault();
                          e.cancelBubble = true;
                          if (z.points.length > 3) {
                            commit(zones.map((zz, i) => (i === zi ? { ...zz, points: zz.points.filter((_, j) => j !== vi) } : zz)));
                            setSel({ zone: zi });
                          } else toast.warning("У зоны должно быть не меньше трёх точек");
                        }}
                        onDragStart={(e) => {
                          e.cancelBubble = true;
                          dispatch({ type: "checkpoint" });
                          setSel({ zone: zi, vertex: vi });
                        }}
                        onDragMove={(e) => {
                          e.cancelBubble = true;
                          moveVertex(zi, vi, e.target);
                        }}
                        onDragEnd={(e) => (e.cancelBubble = true)}
                      />
                    );
                  }),
                )}
                {drawing && drawing.points.length > 0 && (
                  <>
                    <Line
                      points={flat(cursor ? [...drawing.points, cursor] : drawing.points, size)}
                      stroke={COLORS[drawing.type].stroke}
                      strokeWidth={2 * vScale}
                      dash={[6 * vScale, 4 * vScale]}
                      listening={false}
                    />
                    {drawing.points.map((p, i) => {
                      const [x, y] = toPx(p, size);
                      return (
                        <Circle
                          key={`d-${i}`}
                          x={x}
                          y={y}
                          radius={(i === 0 ? 7 : 4) * vScale}
                          fill={i === 0 ? COLORS[drawing.type].stroke : "white"}
                          stroke={COLORS[drawing.type].stroke}
                          strokeWidth={2 * vScale}
                          listening={false}
                        />
                      );
                    })}
                  </>
                )}
              </Layer>
            </Stage>
          )}
        </div>

        <p className="text-xs text-muted-foreground">
          {drawing
            ? "Кликайте по точкам полигона. Замкнуть — клик по первой точке, двойной клик или Enter. Esc — отмена, Ctrl+Z — убрать последнюю точку."
            : "Клик по зоне — выбрать, перетаскивание — сдвинуть. Точки: тянуть — двигать, «+» на ребре — добавить, правый клик или Delete — удалить. Колесо — масштаб."}
        </p>
      </div>

      <div className="space-y-4">
        <Card>
          <CardHeader>
            <CardTitle>Зоны · версия {baseVersion}</CardTitle>
            {dirty ? (
              <Badge tone="warning">не сохранено</Badge>
            ) : applied ? (
              <Badge tone="ok">
                <Check className="size-3" /> на телефоне
              </Badge>
            ) : (
              <Badge tone="info">
                <LoaderCircle className="size-3 animate-spin" /> ждём телефон
              </Badge>
            )}
          </CardHeader>
          <CardContent className="space-y-3">
            {zones.length === 0 ? (
              <p className="text-sm text-muted-foreground">Зон нет. Нарисуйте MONITOR там, где встаёт бензовоз.</p>
            ) : (
              <ul className="space-y-1">
                {zones.map((z, i) => (
                  <li
                    key={i}
                    className={cn("flex items-center gap-2 rounded-md px-2 py-1 text-sm", sel?.zone === i && "bg-muted")}
                    onClick={() => setSel({ zone: i })}
                  >
                    <span className="size-3 rounded-sm" style={{ background: COLORS[z.type].stroke }} />
                    <select
                      className="h-7 rounded border bg-card px-1 text-xs"
                      value={z.type}
                      onChange={(e) => commit(zones.map((zz, j) => (j === i ? { ...zz, type: e.target.value as ZoneType } : zz)))}
                    >
                      <option value="MONITOR">MONITOR</option>
                      <option value="IGNORE">IGNORE</option>
                    </select>
                    <span className="flex-1 text-xs text-muted-foreground">{z.points.length} точ.</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7"
                      title="Удалить зону"
                      onClick={(e) => {
                        e.stopPropagation();
                        commit(zones.filter((_, j) => j !== i));
                        setSel(null);
                      }}
                    >
                      <Trash2 />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
            {check.errors.map((m) => (
              <p key={m} className="text-xs text-sev-critical">
                {m}
              </p>
            ))}
            {check.warnings.map((m) => (
              <p key={m} className="text-xs text-sev-warning">
                {m}
              </p>
            ))}
            <div className="flex gap-2">
              <Button className="flex-1" onClick={doSave} disabled={!dirty || check.errors.length > 0 || save.isPending}>
                <Save /> Сохранить
              </Button>
              <Button variant="outline" onClick={reset} disabled={!dirty} title="Вернуть сохранённую версию">
                <RotateCcw /> Сбросить
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Подложка</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <label className="block space-y-1">
              <span className="text-muted-foreground">Фон</span>
              <select className="h-9 w-full rounded-md border bg-card px-2" value={bg} onChange={(e) => setBg(e.target.value as Background)}>
                <option value="reference">Эталон{refId ? "" : " (нет — последний кадр)"}</option>
                <option value="last">Последний кадр</option>
                {frameParam && <option value="frame">Кадр из галереи</option>}
              </select>
            </label>
            <label className="block space-y-1">
              <span className="text-muted-foreground">Наложить сверху (проверить сдвиг камеры)</span>
              <select
                className="h-9 w-full rounded-md border bg-card px-2"
                value={overlay}
                onChange={(e) => setOverlay(e.target.value as typeof overlay)}
              >
                <option value="none">Ничего</option>
                <option value="last">Последний кадр</option>
                <option value="reference">Эталон</option>
              </select>
            </label>
            {overlay !== "none" && (
              <label className="flex items-center gap-2">
                <span className="text-muted-foreground">Прозрачность</span>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={opacity}
                  onChange={(e) => setOpacity(Number(e.target.value))}
                  className="flex-1 accent-primary"
                />
              </label>
            )}
          </CardContent>
        </Card>

        <ZoneVersions deviceId={deviceId} current={baseVersion} disabled={dirty} />
      </div>
    </div>
  );
}

function ZoneVersions({ deviceId, current, disabled }: { deviceId: string; current: number; disabled: boolean }) {
  const versions = useZoneVersions(deviceId);
  const restore = useRestoreZones(deviceId);
  if (!versions.data?.length) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>История версий</CardTitle>
      </CardHeader>
      <CardContent className="max-h-72 space-y-1 overflow-y-auto">
        {versions.data.map((v) => (
          <div key={v.version} className="flex items-center gap-2 text-sm">
            <span className="w-10 font-medium">v{v.version}</span>
            <span className="flex-1 text-xs text-muted-foreground">
              {formatDateTime(v.created_at)} · {v.created_by ?? "—"} · {v.zones.length} зон
            </span>
            {v.version === current ? (
              <Badge tone="ok">текущая</Badge>
            ) : (
              <Button
                size="sm"
                variant="ghost"
                disabled={disabled || restore.isPending}
                title={disabled ? "Сначала сохраните или сбросьте изменения" : "Сделать текущей (сохранится как новая версия)"}
                onClick={() =>
                  restore.mutate(v.version, {
                    onSuccess: (d) => toast.success(`Версия ${v.version} восстановлена как v${d.version}`),
                    onError: (e) => toast.error(errorMessage(e)),
                  })
                }
              >
                Вернуть
              </Button>
            )}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

export function ZonesTab({ deviceId }: { deviceId: string }) {
  const device = useDevice(deviceId);
  if (device.data && !device.data.device.last_frame_id && !device.data.reference_frame_id) {
    return (
      <EmptyState title="Нет кадров">
        <p className="text-sm text-muted-foreground">Сделайте снимок или эталон — зоны рисуются поверх кадра.</p>
      </EmptyState>
    );
  }
  return <ZoneEditor deviceId={deviceId} />;
}

