import { CircleAlert, Fuel } from "lucide-react";
import type { ObservationBrief, ObservationResult } from "@/api/types";
import { Badge } from "@/components/ui/primitives";

/** Бейдж ответа модели: ⛽ в зоне / нет / ошибка. */
export function VlmBadge({ o, className }: { o: ObservationBrief | null; className?: string }) {
  if (!o) return null;
  if (o.error)
    return (
      <Badge tone="critical" className={className} title={o.error}>
        <CircleAlert className="size-3" /> ошибка VLM
      </Badge>
    );
  if (o.tanker_present && o.tanker_in_zone)
    return (
      <Badge tone="info" className={className} title={o.note ?? undefined}>
        <Fuel className="size-3" /> бензовоз {o.confidence != null && `${Math.round(o.confidence * 100)}%`}
      </Badge>
    );
  if (o.tanker_present)
    return (
      <Badge tone="warning" className={className} title={o.note ?? undefined}>
        вне зоны
      </Badge>
    );
  return (
    <Badge tone="neutral" className={className} title={o.note ?? undefined}>
      нет
    </Badge>
  );
}

const yesNo = (v: boolean) => (v ? "да" : "нет");

/** Поля ответа модели списком. */
export function ObservationFields({ r }: { r: ObservationResult }) {
  const rows: [string, string, boolean?][] = [
    ["Бензовоз", yesNo(r.tanker_present), r.tanker_present],
    ["В зоне", yesNo(r.tanker_in_zone), r.tanker_in_zone],
    ["Уверенность", `${Math.round(r.confidence * 100)}%`],
    ["Вид как у эталона", yesNo(r.view_matches_reference), !r.view_matches_reference],
    ["Вид закрыт", yesNo(r.view_obstructed), r.view_obstructed],
  ];
  return (
    <div className="space-y-1 text-sm">
      {rows.map(([k, v, hl]) => (
        <div key={k} className="flex justify-between gap-3">
          <span className="text-muted-foreground">{k}</span>
          <span className={hl ? "font-medium" : undefined}>{v}</span>
        </div>
      ))}
      {r.note && <p className="rounded bg-muted px-2 py-1 text-xs">{r.note}</p>}
    </div>
  );
}
