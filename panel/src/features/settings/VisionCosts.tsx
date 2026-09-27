import { useMemo } from "react";
import { Link } from "react-router";
import { useCosts } from "@/api/visits";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";

const WEEK_MS = 7 * 86_400_000;

/** Стоимость VLM за последние 7 дней по моделям (docs/06-panel.md §10.3). */
export function VisionCosts() {
  const range = useMemo(() => {
    const to = new Date();
    return { from: new Date(to.getTime() - WEEK_MS).toISOString(), to: to.toISOString() };
  }, []);
  const costs = useCosts(range.from, range.to);
  const rows = costs.data?.by_model ?? [];
  if (!rows.length) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Расходы за 7 дней</CardTitle>
        <Link to="/costs" className="text-sm text-primary hover:underline">
          Подробно
        </Link>
      </CardHeader>
      <CardContent className="space-y-1 text-sm">
        {rows.map((r) => (
          <div key={r.model} className="flex justify-between gap-3">
            <span className="truncate">{r.model}</span>
            <span className="text-muted-foreground tabular-nums">
              {r.calls} выз. · ${r.cost.toFixed(4)}
            </span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
