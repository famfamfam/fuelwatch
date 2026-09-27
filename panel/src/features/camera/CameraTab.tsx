import { useState } from "react";
import { Camera, Crosshair, LoaderCircle, Radio, Square } from "lucide-react";
import { toast } from "sonner";
import { errorMessage, frameImageUrl } from "@/api/client";
import { useCommands, useDevice, useSendCommand } from "@/api/hooks";
import { useDeviceSettings, usePutDeviceSettings, useSettingsSchema } from "@/api/settings";
import { useSetLive } from "@/api/zones";
import type { SettingDef } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Badge, Card, CardContent, CardHeader, CardTitle, Skeleton } from "@/components/ui/primitives";
import { FrameImage } from "@/features/devices/DeviceBits";
import { SchemaForm } from "@/features/settings/SchemaForm";
import { formatAgo, useNow } from "@/lib/utils";

const CAMERA_GROUPS = ["camera", "live"];

function countdown(ms: number) {
  const s = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** Вкладка «Камера» (docs/06-panel.md §6): живой режим, снимок, эталон, настройки камеры в пределах возможностей телефона. */
export function CameraTab({ deviceId }: { deviceId: string }) {
  const detail = useDevice(deviceId);
  const schema = useSettingsSchema(deviceId);
  const values = useDeviceSettings(deviceId);
  const put = usePutDeviceSettings(deviceId);
  const live = useSetLive(deviceId);
  const send = useSendCommand(deviceId);
  const cmds = useCommands(deviceId);
  const now = useNow(1000);
  const [needRef, setNeedRef] = useState<SettingDef[]>([]);
  const [refCmd, setRefCmd] = useState<string | null>(null);

  if (detail.isPending || schema.isPending || values.isPending) return <Skeleton className="h-96" />;
  if (!detail.data || !schema.data || !values.data) return null;

  const d = detail.data.device;
  const caps = detail.data.camera_caps;
  const liveLeft = d.live_until ? new Date(d.live_until).getTime() - now : 0;
  const liveOn = liveLeft > 0;
  const refBusy = cmds.data?.some((c) => c.id === refCmd && (c.status === "pending" || c.status === "sent"));
  const defs = schema.data.settings.filter((s) => CAMERA_GROUPS.includes(s.group));
  const onErr = (e: unknown) => toast.error(errorMessage(e));

  const captureReference = () =>
    send.mutate("capture_reference", {
      onSuccess: (c) => {
        setRefCmd(c.id);
        setNeedRef([]);
        toast.info("Эталон будет снят при следующем heartbeat. Потом проверьте зоны.");
      },
      onError: onErr,
    });

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_26rem]">
      <div className="min-w-0 space-y-4">
        <Card className="overflow-hidden">
          <CardHeader className="flex-wrap">
            <CardTitle className="flex items-center gap-2">
              {liveOn ? (
                <Badge tone="critical">
                  <Radio className="size-3 animate-pulse" /> живой режим · {countdown(liveLeft)}
                </Badge>
              ) : (
                "Кадр"
              )}
              {d.last_frame_at && <span className="font-normal text-muted-foreground">{formatAgo(d.last_frame_at, now)}</span>}
            </CardTitle>
            <div className="flex flex-wrap gap-2">
              {liveOn ? (
                <>
                  <Button size="sm" variant="outline" onClick={() => live.mutate(undefined, { onError: onErr })}>
                    Продлить
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => live.mutate(0, { onError: onErr })}>
                    <Square /> Выключить
                  </Button>
                </>
              ) : (
                <Button size="sm" onClick={() => live.mutate(undefined, { onError: onErr })} disabled={!d.paired || d.disabled || live.isPending}>
                  <Radio /> Живой режим
                </Button>
              )}
              <Button size="sm" variant="outline" disabled={!d.paired || send.isPending} onClick={() => send.mutate("snapshot", { onError: onErr })}>
                <Camera /> Снимок
              </Button>
              <Button size="sm" variant="outline" disabled={!d.paired || refBusy} onClick={captureReference}>
                {refBusy ? <LoaderCircle className="animate-spin" /> : <Crosshair />} Сделать эталон
              </Button>
            </div>
          </CardHeader>
          {d.last_frame_id ? (
            <a href={frameImageUrl(d.last_frame_id)} target="_blank" rel="noreferrer">
              <FrameImage frameId={d.last_frame_id} full className="object-contain" />
            </a>
          ) : (
            <FrameImage frameId={null} />
          )}
          <CardContent className="pt-3 text-sm text-muted-foreground">
            {liveOn
              ? "Кадр обновляется сам, пока включён живой режим. Двигайте телефон и меняйте zoom — результат виден здесь."
              : "Живой режим присылает снимок каждые несколько секунд — удобно при установке и подборе zoom. Выключается сам."}
          </CardContent>
        </Card>

        {needRef.length > 0 && (
          <Card className="border-sev-warning/60">
            <CardContent className="flex flex-wrap items-center justify-between gap-3 pt-4">
              <p className="text-sm">
                Изменено: {needRef.map((s) => s.label.toLowerCase()).join(", ")}. Вид камеры поменялся — сделайте новый эталон и проверьте зоны.
              </p>
              <div className="flex gap-2">
                <Button size="sm" onClick={captureReference}>
                  <Crosshair /> Сделать эталон
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setNeedRef([])}>
                  Позже
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {caps && (
          <Card>
            <CardHeader>
              <CardTitle>Возможности камеры</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-1 text-sm sm:grid-cols-2">
              <span className="text-muted-foreground">Zoom</span>
              <span>
                {caps.zoom_min}× – {caps.zoom_max}×
              </span>
              <span className="text-muted-foreground">Разрешения</span>
              <span>{caps.resolutions.slice(0, 4).join(", ")}{caps.resolutions.length > 4 ? ` и ещё ${caps.resolutions.length - 4}` : ""}</span>
              <span className="text-muted-foreground">Ручной фокус</span>
              <span>{caps.manual_focus ? "есть" : "нет"}</span>
              <span className="text-muted-foreground">Ориентация сенсора</span>
              <span>{caps.sensor_orientation}°</span>
            </CardContent>
          </Card>
        )}
      </div>

      <Card className="h-fit">
        <CardHeader>
          <CardTitle>Настройки камеры</CardTitle>
        </CardHeader>
        <CardContent className="px-1">
          <SchemaForm
            defs={defs}
            current={values.data}
            level="device"
            onSave={(changes) => put.mutateAsync(changes)}
            onSaved={(changed) => {
              toast.success("Сохранено. Телефон применит настройки в течение ~20 с");
              const ref = changed.filter((s) => s.new_reference);
              if (ref.length) setNeedRef(ref);
            }}
          />
        </CardContent>
      </Card>
    </div>
  );
}
