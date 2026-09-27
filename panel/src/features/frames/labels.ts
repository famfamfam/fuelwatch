import type { FrameKind } from "@/api/types";

export const kindLabel: Record<FrameKind, string> = {
  keyframe: "плановый",
  change: "изменение",
  snapshot: "снимок",
  reference: "эталон",
};

export const kindTone: Record<FrameKind, "neutral" | "info" | "warning" | "ok"> = {
  keyframe: "neutral",
  change: "warning",
  snapshot: "info",
  reference: "ok",
};

/** Цвета отметок на ленте — те же тона, что у бейджей. */
export const kindColor: Record<FrameKind, string> = {
  keyframe: "var(--muted-foreground)",
  change: "var(--sev-warning)",
  snapshot: "var(--sev-info)",
  reference: "var(--sev-ok)",
};
