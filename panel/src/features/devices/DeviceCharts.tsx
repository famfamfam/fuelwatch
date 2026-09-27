import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api } from "@/api/client";
import { Card, CardContent, CardHeader, CardTitle, Skeleton } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

interface Point {
  t: string;
  battery: number | null;
  charging: number | null;
  battery_temp_c: number | null;
  thermal_status: number | null;
  tx_bytes_today: number | null;
  queue_items: number | null;
  count: number;
}

const RANGES = {
  "24h": { label: "24 ч", ms: 86_400_000, step: "10m", stepMs: 600_000, tickMs: 3 * 3_600_000 },
  "7d": { label: "7 дней", ms: 7 * 86_400_000, step: "1h", stepMs: 3_600_000, tickMs: 86_400_000 },
} as const;

type Row = Point & { ts: number };

/** Метки оси — ровные часы/сутки по местному времени внутри диапазона. */
function timeTicks([from, to]: [number, number], stepMs: number): number[] {
  const d = new Date(from);
  d.setMinutes(0, 0, 0);
  if (stepMs >= 86_400_000) d.setHours(0);
  else d.setHours(Math.ceil(d.getHours() / (stepMs / 3_600_000)) * (stepMs / 3_600_000));
  const out: number[] = [];
  for (let t = d.getTime(); t <= to; t += stepMs) if (t >= from) out.push(t);
  return out;
}

/**
 * Время — числовая ось: перерывы без heartbeat видны как пустое место. Между точками, далёкими больше
 * чем на два шага, вставляется пустая точка — линия там разрывается, а не тянется через перерыв.
 */
function withGaps(pts: Point[], stepMs: number): Row[] {
  const out: Row[] = [];
  let prev = 0;
  for (const p of pts) {
    const ts = Date.parse(p.t);
    if (prev && ts - prev > 2 * stepMs) {
      out.push({ t: "", ts: prev + stepMs, battery: null, charging: null, battery_temp_c: null, thermal_status: null, tx_bytes_today: null, queue_items: null, count: 0 });
    }
    out.push({ ...p, ts });
    prev = ts;
  }
  return out;
}
type RangeKey = keyof typeof RANGES;

function useSeries(id: string, range: RangeKey) {
  const { from, to, fromMs, toMs } = useMemo(() => {
    const to = new Date();
    const fromMs = to.getTime() - RANGES[range].ms;
    return { from: new Date(fromMs).toISOString(), to: to.toISOString(), fromMs, toMs: to.getTime() };
    // Границы пересчитываются при смене диапазона; обновление — вместе с запросом.
  }, [range]);
  const q = useQuery({
    queryKey: ["heartbeats", id, range],
    queryFn: () => api<{ points: Point[] }>(`/devices/${id}/heartbeats?${new URLSearchParams({ from, to, step: RANGES[range].step })}`),
    refetchInterval: 60_000,
  });
  return { ...q, domain: [fromMs, toMs] as [number, number] };
}

// Цвета — токены темы: графики одного ряда, заголовок называет ряд (легенда не нужна).
const INK = "var(--primary)";
const GRID = "var(--border)";
const AXIS = "var(--muted-foreground)";
const OFF = "var(--sev-critical)";

const axisProps = { stroke: AXIS, fontSize: 11, tickLine: false, axisLine: false } as const;

function tickTime(range: RangeKey) {
  return (ms: number) =>
    new Date(ms).toLocaleString("ru-RU", range === "24h" ? { hour: "2-digit", minute: "2-digit" } : { day: "2-digit", month: "2-digit" });
}

function Tip({ active, payload, label, fmt }: { active?: boolean; payload?: { value: number }[]; label?: number; fmt: (v: number) => string }) {
  if (!active || !payload?.length || payload[0].value == null) return null;
  return (
    <div className="rounded-md border bg-popover px-2 py-1 text-xs shadow">
      <p className="text-muted-foreground">{label != null && new Date(label).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}</p>
      <p className="font-medium text-foreground">{fmt(payload[0].value)}</p>
    </div>
  );
}

function Chart({ title, children, className }: { title: string; children: React.ReactElement; className?: string }) {
  return (
    <div className={cn("space-y-1", className)}>
      <p className="text-xs font-medium text-muted-foreground">{title}</p>
      <div className="h-32">
        <ResponsiveContainer width="100%" height="100%">
          {children}
        </ResponsiveContainer>
      </div>
    </div>
  );
}

/** Графики на «Обзоре» (docs/06-panel.md §4.1): заряд, питание, температура, трафик — по данным heartbeat. */
export function DeviceCharts({ deviceId }: { deviceId: string }) {
  const [range, setRange] = useState<RangeKey>("24h");
  const q = useSeries(deviceId, range);
  const pts = useMemo(() => withGaps(q.data?.points ?? [], RANGES[range].stepMs), [q.data, range]);

  // Трафик за интервал = прирост счётчика «с начала суток»; в полночь счётчик обнуляется.
  const traffic = useMemo(
    () =>
      {
        let prev: number | null = null; // последнее известное значение счётчика
        return pts.map((p) => {
          const cur = p.tx_bytes_today;
          const d = cur == null ? null : prev == null || cur < prev ? cur : cur - prev;
          if (cur != null) prev = cur;
          return { ts: p.ts, mb: d == null ? null : d / 1e6 };
        });
      },
    [pts],
  );
  const tf = tickTime(range);
  const ticks = useMemo(() => timeTicks(q.domain, RANGES[range].tickMs), [q.domain, range]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Графики</CardTitle>
        <div className="flex gap-1">
          {(Object.keys(RANGES) as RangeKey[]).map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => setRange(k)}
              className={cn("rounded-md px-2 py-1 text-xs", range === k ? "bg-muted font-medium" : "text-muted-foreground hover:bg-muted/60")}
            >
              {RANGES[k].label}
            </button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {q.isPending ? (
          <Skeleton className="h-64" />
        ) : pts.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">Данных за период нет</p>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            <Chart title="Заряд, %">
              <LineChart data={pts} margin={{ top: 4, right: 4, bottom: 0, left: -24 }}>
                <CartesianGrid stroke={GRID} strokeDasharray="2 4" vertical={false} />
                <XAxis dataKey="ts" type="number" scale="time" domain={q.domain} ticks={ticks} tickFormatter={tf} {...axisProps} />
                <YAxis domain={[0, 100]} ticks={[0, 50, 100]} {...axisProps} />
                <Tooltip content={<Tip fmt={(v) => `${Math.round(v)}%`} />} cursor={{ stroke: AXIS, strokeDasharray: "3 3" }} />
                <Line dataKey="battery" stroke={INK} strokeWidth={2} dot={false} connectNulls={false} isAnimationActive={false} />
              </LineChart>
            </Chart>
            <Chart title="Питание (доля времени на зарядке)">
              <AreaChart data={pts} margin={{ top: 4, right: 4, bottom: 0, left: -24 }}>
                <CartesianGrid stroke={GRID} strokeDasharray="2 4" vertical={false} />
                <XAxis dataKey="ts" type="number" scale="time" domain={q.domain} ticks={ticks} tickFormatter={tf} {...axisProps} />
                <YAxis domain={[0, 1]} ticks={[0, 1]} tickFormatter={(v) => (v ? "есть" : "нет")} {...axisProps} />
                <Tooltip
                  content={<Tip fmt={(v) => (v >= 0.99 ? "питание есть" : v <= 0.01 ? "без питания" : `питание ${Math.round(v * 100)}% времени`)} />}
                  cursor={{ stroke: AXIS, strokeDasharray: "3 3" }}
                />
                <Area dataKey="charging" type="stepAfter" stroke={INK} strokeWidth={2} fill={INK} fillOpacity={0.12} isAnimationActive={false} />
              </AreaChart>
            </Chart>
            <Chart title="Температура батареи, °C">
              <LineChart data={pts} margin={{ top: 4, right: 4, bottom: 0, left: -24 }}>
                <CartesianGrid stroke={GRID} strokeDasharray="2 4" vertical={false} />
                <XAxis dataKey="ts" type="number" scale="time" domain={q.domain} ticks={ticks} tickFormatter={tf} {...axisProps} />
                <YAxis domain={["dataMin - 2", "dataMax + 2"]} allowDecimals={false} {...axisProps} />
                <Tooltip content={<Tip fmt={(v) => `${v.toFixed(1)} °C`} />} cursor={{ stroke: AXIS, strokeDasharray: "3 3" }} />
                <Line dataKey="battery_temp_c" stroke={INK} strokeWidth={2} dot={false} isAnimationActive={false} />
              </LineChart>
            </Chart>
            <Chart title={`Трафик, MB за ${range === "24h" ? "10 мин" : "час"}`}>
              <BarChart data={traffic} margin={{ top: 4, right: 4, bottom: 0, left: -24 }}>
                <CartesianGrid stroke={GRID} strokeDasharray="2 4" vertical={false} />
                <XAxis dataKey="ts" type="number" scale="time" domain={q.domain} ticks={ticks} tickFormatter={tf} {...axisProps} />
                <YAxis allowDecimals {...axisProps} />
                <Tooltip content={<Tip fmt={(v) => `${v.toFixed(2)} MB`} />} cursor={{ fill: "var(--muted)" }} />
                <Bar dataKey="mb" fill={INK} radius={[4, 4, 0, 0]} isAnimationActive={false} />
              </BarChart>
            </Chart>
          </div>
        )}
        {pts.some((p) => p.charging != null && p.charging < 0.99) && (
          <p className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
            <span className="inline-block size-2 rounded-full" style={{ background: OFF }} /> В периоде были отключения питания — см. журнал.
          </p>
        )}
      </CardContent>
    </Card>
  );
}
