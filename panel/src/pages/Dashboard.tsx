import { Link } from "react-router";
import { useDevices, useNotifications } from "@/api/hooks";
import type { DeviceSummary } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, EmptyState, Skeleton } from "@/components/ui/primitives";
import { FrameImage, IssueBadges, LastSeen, ModeBadge, PowerStats } from "@/features/devices/DeviceBits";
import { AddDeviceDialog } from "@/features/devices/PairingDialogs";
import { NotificationItem } from "@/features/notifications/NotificationItem";
import { formatAgo, formatTime, useNow } from "@/lib/utils";
import { visitMinutes } from "@/features/visits/Visits";

function DeviceCard({ d, now }: { d: DeviceSummary; now: number }) {
  return (
    <Link to={`/devices/${d.id}`} className="group block">
      <Card className="overflow-hidden transition-shadow group-hover:shadow-md">
        <div className="relative">
          <FrameImage frameId={d.last_frame_id} alt={`Последний кадр ${d.name}`} />
          {d.last_frame_at && (
            <span className="absolute right-2 bottom-2 rounded bg-black/60 px-1.5 py-0.5 text-xs text-white">
              кадр {formatAgo(d.last_frame_at, now)}
            </span>
          )}
        </div>
        <div className="space-y-2 p-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="truncate font-semibold">{d.name}</h3>
            <ModeBadge mode={d.mode} />
          </div>
          <LastSeen d={d} now={now} />
          {d.paired && <PowerStats d={d} />}
          {d.visit && (
            <p className="text-sm font-medium text-sev-info">
              ⛽ бензовоз с {formatTime(d.visit.started_at)} · {visitMinutes({ ...d.visit, ended_at: null }, now)} мин
            </p>
          )}
          <IssueBadges issues={d.open_issues} />
          {d.disabled && <p className="text-sm text-sev-critical">Устройство отключено</p>}
        </div>
      </Card>
    </Link>
  );
}

export function Dashboard() {
  const devices = useDevices();
  const notifications = useNotifications({}, 20);
  const now = useNow();
  const recent = notifications.data?.pages[0]?.items ?? [];

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
      <section className="min-w-0 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-xl font-semibold">Устройства</h1>
          <AddDeviceDialog />
        </div>
        {devices.isPending ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-72" />
            ))}
          </div>
        ) : devices.data?.length ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {devices.data.map((d) => (
              <DeviceCard key={d.id} d={d} now={now} />
            ))}
          </div>
        ) : (
          <EmptyState title="Устройств пока нет">
            <p className="max-w-sm text-sm text-muted-foreground">
              Добавьте устройство, получите код привязки и введите его в приложении FuelWatch на телефоне.
            </p>
          </EmptyState>
        )}
      </section>

      <Card className="h-fit">
        <CardHeader>
          <CardTitle>Последние уведомления</CardTitle>
          <Button variant="link" size="sm" asChild>
            <Link to="/notifications">Все</Link>
          </Button>
        </CardHeader>
        <CardContent className="px-1">
          {recent.length === 0 ? (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">Уведомлений нет</p>
          ) : (
            recent.map((n) => <NotificationItem key={n.id} n={n} compact />)
          )}
        </CardContent>
      </Card>
    </div>
  );
}
