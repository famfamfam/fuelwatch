import { useMemo, useState } from "react";
import { useDevices } from "@/api/hooks";
import { useCosts } from "@/api/visits";
import type { CostBucket } from "@/api/types";
import { Card, CardContent, CardHeader, CardTitle, EmptyState, Skeleton } from "@/components/ui/primitives";

const usd = (v: number) => `$${v.toFixed(v < 1 ? 4 : 2)}`;

function Table({ rows, label, name }: { rows: CostBucket[]; label: string; name: (b: CostBucket) => string }) {
  const max = Math.max(...rows.map((r) => r.cost), 1e-9);
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs text-muted-foreground">
          <tr>
            <th className="py-2 pr-3 font-medium">{label}</th>
            <th className="py-2 pr-3 text-right font-medium">Вызовов</th>
            <th className="py-2 pr-3 text-right font-medium">Ошибок</th>
            <th className="py-2 pr-3 text-right font-medium">Токенов</th>
            <th className="py-2 text-right font-medium">Стоимость</th>
            <th className="w-1/4 py-2 pl-3" />
          </tr>
        </thead>
        <tbody className="divide-y">
          {rows.map((r) => (
            <tr key={name(r)}>
              <td className="py-1.5 pr-3 whitespace-nowrap">{name(r)}</td>
              <td className="py-1.5 pr-3 text-right tabular-nums">{r.calls}</td>
              <td className="py-1.5 pr-3 text-right tabular-nums">{r.errors || ""}</td>
              <td className="py-1.5 pr-3 text-right tabular-nums">{(r.tokens_in + r.tokens_out).toLocaleString("ru-RU")}</td>
              <td className="py-1.5 text-right tabular-nums">{usd(r.cost)}</td>
              <td className="py-1.5 pl-3">
                <div className="h-2 rounded-full bg-primary/70" style={{ width: `${(r.cost / max) * 100}%` }} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Расходы на VLM (docs/06-panel.md §2): вызовы и стоимость по дням, моделям и устройствам. */
export function CostsPage() {
  const [days, setDays] = useState(7);
  const range = useMemo(() => {
    const to = new Date();
    const from = new Date(to.getTime() - days * 86_400_000);
    return { from: from.toISOString(), to: to.toISOString() };
  }, [days]);
  const costs = useCosts(range.from, range.to);
  const devices = useDevices();
  const name = (id?: string) => devices.data?.find((d) => d.id === id)?.name ?? id ?? "";
  const total = costs.data?.by_day.reduce((s, b) => s + b.cost, 0) ?? 0;
  const calls = costs.data?.by_day.reduce((s, b) => s + b.calls, 0) ?? 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">Расходы</h1>
        <select className="h-9 rounded-md border bg-card px-2 text-sm" value={days} onChange={(e) => setDays(Number(e.target.value))}>
          <option value={1}>Сутки</option>
          <option value={7}>7 дней</option>
          <option value={30}>30 дней</option>
        </select>
      </div>
      {costs.isPending ? (
        <Skeleton className="h-64" />
      ) : !calls ? (
        <EmptyState title="Вызовов VLM за период не было" />
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            {[
              ["Стоимость", usd(total)],
              ["Вызовов", calls.toLocaleString("ru-RU")],
              ["В среднем за вызов", usd(total / calls)],
            ].map(([k, v]) => (
              <Card key={k}>
                <CardContent className="pt-4">
                  <p className="text-sm text-muted-foreground">{k}</p>
                  <p className="text-2xl font-semibold tabular-nums">{v}</p>
                </CardContent>
              </Card>
            ))}
          </div>
          <Card>
            <CardHeader>
              <CardTitle>По дням</CardTitle>
            </CardHeader>
            <CardContent>
              <Table rows={costs.data!.by_day} label="День" name={(b) => b.day ?? ""} />
            </CardContent>
          </Card>
          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>По моделям</CardTitle>
              </CardHeader>
              <CardContent>
                <Table rows={costs.data!.by_model} label="Модель" name={(b) => b.model ?? ""} />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>По устройствам</CardTitle>
              </CardHeader>
              <CardContent>
                <Table rows={costs.data!.by_device} label="Устройство" name={(b) => name(b.device_id)} />
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
