import { lazy, Suspense, useState, type ReactNode } from "react";
import { Link, NavLink, useParams } from "react-router";
import { FramesTab } from "@/features/frames/FramesTab";
import { CameraTab } from "@/features/camera/CameraTab";
import { DeviceSettingsTab } from "@/features/settings/SettingsPages";
import { VisitList, visitMinutes } from "@/features/visits/Visits";
import { ArrowLeft, Camera, KeyRound, LoaderCircle, Pause, Play, Power, RefreshCw, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import {
  useCommands,
  useDevice,
  useNewPairingCode,
  usePatchDevice,
  useSendCommand,
  useSetMode,
} from "@/api/hooks";
import { api, errorMessage, frameImageUrl } from "@/api/client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { DeviceLog } from "@/features/devices/DeviceLog";
import { DeleteDevice, RenameDevice } from "@/features/devices/DeviceManage";
import { useZones } from "@/api/zones";
import { useFrame } from "@/api/frames";
import { ZonesOverlay } from "@/features/frames/FrameViewer";
import type { Command, CommandType, DeviceDetail, PairingCode } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Badge, Card, CardContent, CardHeader, CardTitle, EmptyState, Skeleton } from "@/components/ui/primitives";
import { FrameImage, LastSeen, ModeBadge } from "@/features/devices/DeviceBits";
import { PairingCodeView } from "@/features/devices/PairingDialogs";
import {
  commandLabel,
  commandStatusLabel,
  commandStatusTone,
  issueLabel,
  issueSeverity,
  thermalLabel,
} from "@/features/devices/labels";
import { cn, formatAgo, formatDateTime, useNow } from "@/lib/utils";

const busy = (c?: Command) => c && (c.status === "pending" || c.status === "sent");

function formatDuration(s: number) {
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  return d > 0 ? `${d} д ${h} ч` : h > 0 ? `${h} ч ${m} мин` : `${m} мин`;
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-1.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}

function StatusCard({ detail, now }: { detail: DeviceDetail; now: number }) {
  const d = detail.device;
  const hb = detail.last_heartbeat;
  const synced = d.config_version_device >= d.config_version_server;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Статус</CardTitle>
      </CardHeader>
      <CardContent className="divide-y">
        <Row label="Связь">
          <LastSeen d={d} now={now} />
        </Row>
        <Row label="Заряд">
          {d.battery != null ? `${Math.round(d.battery)}%` : "—"}
          {d.charging != null && <span className="text-muted-foreground"> · {d.charging ? "заряжается" : "от батареи"}</span>}
        </Row>
        <Row label="Температура батареи">{d.battery_temp_c != null ? `${d.battery_temp_c.toFixed(1)} °C` : "—"}</Row>
        <Row label="Нагрев (Android)">{d.thermal_status != null ? (thermalLabel[d.thermal_status] ?? d.thermal_status) : "—"}</Row>
        <Row label="Сеть">{d.network ?? "—"}</Row>
        {hb?.tx_bytes_today != null && <Row label="Трафик сегодня">{(hb.tx_bytes_today / 1e6).toFixed(1)} MB</Row>}
        {hb?.queue_items != null && <Row label="Очередь на телефоне">{hb.queue_items}</Row>}
        <Row label="Последний кадр">{formatAgo(d.last_frame_at, now)}</Row>
        <Row label="Конфигурация">
          {synced ? (
            <Badge tone="ok">применена · v{d.config_version_server}</Badge>
          ) : (
            <Badge tone="warning">
              ожидает · v{d.config_version_device} → v{d.config_version_server}
            </Badge>
          )}
        </Row>
        {hb?.uptime_s != null && <Row label="Работает без перезапуска">{formatDuration(hb.uptime_s)}</Row>}
        <Row label="Телефон">{[d.model, d.android && `Android ${d.android}`].filter(Boolean).join(" · ") || "—"}</Row>
        <Row label="Приложение">{d.app_version || "—"}</Row>
        {d.last_error && (
          <Row label="Последняя ошибка">
            <span className="text-sev-critical">{d.last_error}</span>
          </Row>
        )}
      </CardContent>
    </Card>
  );
}

const RECENT_COMMANDS = 10;

function CommandLog({ id }: { id: string }) {
  const cmds = useCommands(id);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Последние команды</CardTitle>
        <Button variant="link" size="sm" asChild>
          <Link to={`/devices/${id}/log`}>Весь журнал</Link>
        </Button>
      </CardHeader>
      <CardContent className="px-0">
        {cmds.data?.length ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Когда</th>
                  <th className="px-4 py-2 font-medium">Команда</th>
                  <th className="px-4 py-2 font-medium">Кто</th>
                  <th className="px-4 py-2 font-medium">Статус</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {cmds.data.slice(0, RECENT_COMMANDS).map((c) => (
                  <tr key={c.id}>
                    <td className="px-4 py-2 whitespace-nowrap">{formatDateTime(c.created_at)}</td>
                    <td className="px-4 py-2">{commandLabel[c.type] ?? c.type}</td>
                    <td className="px-4 py-2 text-muted-foreground">{c.created_by ?? "—"}</td>
                    <td className="px-4 py-2">
                      <Badge tone={commandStatusTone[c.status]} title={c.error ?? undefined}>
                        {busy(c) && <LoaderCircle className="size-3 animate-spin" />}
                        {commandStatusLabel[c.status]}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="px-4 py-6 text-center text-sm text-muted-foreground">Команд ещё не было</p>
        )}
      </CardContent>
    </Card>
  );
}

function Actions({ detail }: { detail: DeviceDetail }) {
  const d = detail.device;
  const send = useSendCommand(d.id);
  const setMode = useSetMode(d.id);
  const patch = usePatchDevice(d.id);
  const pairing = useNewPairingCode(d.id);
  const [code, setCode] = useState<PairingCode | null>(null);
  const onErr = (e: unknown) => toast.error(errorMessage(e));

  const cmd = (type: CommandType, confirm?: string) => {
    if (confirm && !window.confirm(confirm)) return;
    send.mutate(type, { onSuccess: () => toast.info(`«${commandLabel[type]}» — выполнится при следующем heartbeat`), onError: onErr });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Управление</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-2 gap-2">
          {d.mode === "armed" ? (
            <Button variant="outline" onClick={() => setMode.mutate("paused", { onError: onErr })}>
              <Pause /> Пауза
            </Button>
          ) : (
            <Button variant="outline" onClick={() => setMode.mutate("armed", { onError: onErr })} disabled={!d.paired}>
              <Play /> Включить
            </Button>
          )}
          <Button variant="outline" onClick={() => cmd("restart_camera")} disabled={!d.paired}>
            <RefreshCw /> Камера
          </Button>
          <Button
            variant="outline"
            onClick={() => cmd("restart_app", "Перезапустить приложение на телефоне?")}
            disabled={!d.paired}
          >
            <RotateCcw /> Приложение
          </Button>
          <Button
            variant="outline"
            onClick={() => pairing.mutate(undefined, { onSuccess: setCode, onError: onErr })}
            title="Новый код привязки (перепривязать телефон)"
          >
            <KeyRound /> Код
          </Button>
        </div>
        {code && <PairingCodeView code={code} />}
        <Button
          variant={d.disabled ? "outline" : "ghost"}
          className="w-full text-sev-critical"
          onClick={() => {
            if (!d.disabled && !window.confirm("Отключить устройство? Телефон перестанет отправлять данные.")) return;
            patch.mutate({ disabled: !d.disabled }, { onError: onErr });
          }}
        >
          <Power /> {d.disabled ? "Включить устройство" : "Отключить устройство"}
        </Button>
        <DeleteDevice d={d} />
      </CardContent>
    </Card>
  );
}

// Редактор зон тянет Konva (~350 KB) — грузится только на вкладке «Зоны».
const ZonesTab = lazy(() => import("@/features/zones/ZoneEditor").then((m) => ({ default: m.ZonesTab })));

// Графики тянут Recharts (~400 KB) — грузятся отдельно.
const DeviceCharts = lazy(() => import("@/features/devices/DeviceCharts").then((m) => ({ default: m.DeviceCharts })));

const TABS = [
  { id: "overview", label: "Обзор" },
  { id: "frames", label: "Кадры" },
  { id: "visits", label: "Визиты" },
  { id: "zones", label: "Зоны" },
  { id: "camera", label: "Камера" },
  { id: "settings", label: "Настройки" },
  { id: "log", label: "Журнал" },
] as const;

export function Device() {
  const { id = "", tab = "overview" } = useParams();
  const detail = useDevice(id);
  const cmds = useCommands(id);
  const send = useSendCommand(id);
  const now = useNow();
  const [snapId, setSnapId] = useState<string | null>(null);
  const [showZones, setShowZones] = useState(false);
  const zones = useZones(id);
  const lastFrame = useFrame(detail.data?.device.last_frame_id ?? null);
  const qc = useQueryClient();
  const closeIssue = useMutation({
    mutationFn: (issueId: number) => api(`/issues/${issueId}/close`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["device", id] }),
  });
  const snapCmd = cmds.data?.find((c) => c.id === snapId);

  if (detail.isPending) return <Skeleton className="h-96" />;
  if (detail.isError)
    return (
      <EmptyState title="Устройство не найдено">
        <Button variant="outline" asChild>
          <Link to="/">На дашборд</Link>
        </Button>
      </EmptyState>
    );

  const d = detail.data.device;
  const snapBusy = busy(snapCmd) || send.isPending;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="ghost" size="icon" asChild>
          <Link to="/" aria-label="Назад">
            <ArrowLeft />
          </Link>
        </Button>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-xl font-semibold">{d.name}</h1>
            <RenameDevice d={d} />
            <ModeBadge mode={d.mode} />
            {d.disabled && <Badge tone="critical">отключено</Badge>}
          </div>
          <p className="text-sm text-muted-foreground">{d.id}</p>
        </div>
      </div>

      {detail.data.issues.length > 0 && (
        <Card className="border-sev-warning/50">
          <CardContent className="space-y-2 pt-4">
            {detail.data.issues.map((i) => (
              <div key={i.id} className="flex flex-wrap items-center gap-2 text-sm">
                <Badge tone={issueSeverity(i.type)}>{issueLabel[i.type] ?? i.type}</Badge>
                <span className="flex-1 text-muted-foreground">с {formatDateTime(i.opened_at)}</span>
                {i.type !== "OFFLINE" && (
                <Button
                  size="sm"
                  variant="ghost"
                  title="Закрыть вручную: проблема проверена на месте"
                  onClick={() =>
                    window.confirm(`Закрыть «${issueLabel[i.type] ?? i.type}» вручную?`) &&
                    closeIssue.mutate(i.id, { onError: (e) => toast.error(errorMessage(e)) })
                  }
                >
                  Закрыть
                </Button>
                )}
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <nav className="flex gap-1 overflow-x-auto border-b">
        {TABS.map((t) => (
          <NavLink
            key={t.id}
            to={t.id === "overview" ? `/devices/${id}` : `/devices/${id}/${t.id}`}
            end
            className={({ isActive }) =>
              cn(
                "shrink-0 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors",
                isActive ? "border-primary font-medium" : "border-transparent text-muted-foreground hover:text-foreground",
              )
            }
          >
            {t.label}
          </NavLink>
        ))}
      </nav>

      {tab === "frames" && <FramesTab deviceId={id} />}
      {tab === "zones" && (
        <Suspense fallback={<Skeleton className="h-96" />}>
          <ZonesTab deviceId={id} />
        </Suspense>
      )}
      {tab === "camera" && <CameraTab deviceId={id} />}
      {tab === "visits" && <VisitList deviceId={id} />}
      {tab === "log" && <DeviceLog deviceId={id} />}
      {tab === "settings" && <DeviceSettingsTab deviceId={id} />}
      {tab === "overview" && (
      <>
      {d.visit && (
        <Card className="border-sev-info/50">
          <CardContent className="flex flex-wrap items-center justify-between gap-2 pt-4">
            <p className="text-sm">
              ⛽ <b>Бензовоз на месте</b> с {formatDateTime(d.visit.started_at)} · {visitMinutes({ ...d.visit, ended_at: null }, now)} мин
            </p>
            <Button size="sm" variant="outline" asChild>
              <Link to={`/devices/${id}/visits`}>Визиты</Link>
            </Button>
          </CardContent>
        </Card>
      )}

      {!d.paired && (
        <Card>
          <CardContent className="space-y-2 pt-4">
            <p className="font-medium">Телефон ещё не привязан</p>
            <p className="text-sm text-muted-foreground">
              Получите код привязки кнопкой «Код» в разделе «Управление» и введите его в приложении FuelWatch.
            </p>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 lg:grid-cols-[1fr_22rem]">
        <div className="min-w-0 space-y-4">
          <Card className="overflow-hidden">
            <CardHeader>
              <CardTitle>
                Последний кадр
                {d.last_frame_at && (
                  <span className="ml-2 font-normal text-muted-foreground">{formatAgo(d.last_frame_at, now)}</span>
                )}
              </CardTitle>
              <Button
                size="sm"
                disabled={!d.paired || d.disabled || snapBusy}
                onClick={() =>
                  send.mutate("snapshot", {
                    onSuccess: (c) => setSnapId(c.id),
                    onError: (e) => toast.error(errorMessage(e)),
                  })
                }
              >
                {snapBusy ? <LoaderCircle className="animate-spin" /> : <Camera />}
                {snapBusy ? (snapCmd?.status === "sent" ? "Снимаем…" : "Ждём телефон…") : "Снимок"}
              </Button>
            </CardHeader>
            {d.last_frame_id ? (
              // Контейнер по размеру самой картинки: иначе зоны у кадра другой ориентации легли бы мимо.
              <div className="flex justify-center bg-muted">
                <a href={frameImageUrl(d.last_frame_id)} target="_blank" rel="noreferrer" title="Открыть в полном размере" className="relative inline-block">
                  <img src={frameImageUrl(d.last_frame_id)} alt="Последний кадр" className="block max-h-[60vh] w-auto max-w-full" />
                  {showZones && zones.data?.zones && lastFrame.data && (
                    <ZonesOverlay zones={zones.data.zones} crop={lastFrame.data.frame.crop_rect} />
                  )}
                </a>
              </div>
            ) : (
              <FrameImage frameId={null} />
            )}
            {d.last_frame_id && (zones.data?.zones?.length ?? 0) > 0 && (
              <CardContent className="pt-3">
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" className="accent-primary" checked={showZones} onChange={(e) => setShowZones(e.target.checked)} />
                  Показать зоны
                </label>
              </CardContent>
            )}
          </Card>
          <Suspense fallback={<Skeleton className="h-64" />}>
            <DeviceCharts deviceId={id} />
          </Suspense>
          <CommandLog id={id} />
        </div>
        <div className="space-y-4">
          <Actions detail={detail.data} />
          <StatusCard detail={detail.data} now={now} />
        </div>
      </div>
      </>
      )}
    </div>
  );
}
