import { Battery, BatteryCharging, ImageOff, Plug, Thermometer } from "lucide-react";
import type { DeviceSummary } from "@/api/types";
import { frameThumbUrl, frameImageUrl } from "@/api/client";
import { Badge } from "@/components/ui/primitives";
import { cn, formatAgo } from "@/lib/utils";
import { issueLabel, issueSeverity, modeLabel } from "./labels";

export function OnlineDot({ online, className }: { online: boolean; className?: string }) {
  return (
    <span className={cn("relative inline-flex size-2.5", className)} title={online ? "online" : "offline"}>
      {online && <span className="absolute inset-0 animate-ping rounded-full bg-sev-ok opacity-50" />}
      <span className={cn("relative inline-flex size-2.5 rounded-full", online ? "bg-sev-ok" : "bg-sev-critical")} />
    </span>
  );
}

export function ModeBadge({ mode }: { mode: DeviceSummary["mode"] }) {
  const tone = mode === "armed" ? "ok" : mode === "paused" ? "warning" : "neutral";
  return <Badge tone={tone}>{modeLabel[mode]}</Badge>;
}

export function IssueBadges({ issues }: { issues: string[] }) {
  if (issues.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1">
      {issues.map((t) => (
        <Badge key={t} tone={issueSeverity(t)}>
          {issueLabel[t] ?? t}
        </Badge>
      ))}
    </div>
  );
}

export function PowerStats({ d }: { d: DeviceSummary }) {
  const lowBattery = d.battery != null && d.battery <= 25 && !d.charging;
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
      <span className={cn("inline-flex items-center gap-1", lowBattery && "text-sev-critical")}>
        {d.charging ? <BatteryCharging className="size-4" /> : <Battery className="size-4" />}
        {d.battery != null ? `${Math.round(d.battery)}%` : "—"}
      </span>
      <span className={cn("inline-flex items-center gap-1", d.charging === false && "text-sev-warning")}>
        <Plug className="size-4" />
        {d.charging == null ? "—" : d.charging ? "питание" : "от батареи"}
      </span>
      <span className="inline-flex items-center gap-1">
        <Thermometer className="size-4" />
        {d.battery_temp_c != null ? `${d.battery_temp_c.toFixed(1)}°` : "—"}
      </span>
    </div>
  );
}

export function LastSeen({ d, now }: { d: DeviceSummary; now: number }) {
  if (!d.paired) return <span className="text-sm text-muted-foreground">не привязан</span>;
  return (
    <span className="inline-flex items-center gap-2 text-sm text-muted-foreground">
      <OnlineDot online={d.online} />
      {d.online ? "online" : "offline"} · {formatAgo(d.last_seen_at, now)}
    </span>
  );
}

export function FrameImage({
  frameId,
  full = false,
  className,
  alt = "Кадр",
}: {
  frameId: string | null;
  full?: boolean;
  className?: string;
  alt?: string;
}) {
  if (!frameId) {
    return (
      <div className={cn("flex aspect-video items-center justify-center bg-muted text-muted-foreground", className)}>
        <ImageOff className="size-6" />
      </div>
    );
  }
  return (
    <img
      src={full ? frameImageUrl(frameId) : frameThumbUrl(frameId)}
      alt={alt}
      loading="lazy"
      className={cn("aspect-video w-full bg-muted object-cover", className)}
    />
  );
}
