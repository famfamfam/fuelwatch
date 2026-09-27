import { useState } from "react";
import { CheckCheck } from "lucide-react";
import { useDevices, useMarkRead, useNotifications, type NotificationFilter } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import { Card, EmptyState, Skeleton } from "@/components/ui/primitives";
import { NotificationItem } from "@/features/notifications/NotificationItem";

const categories = [
  { id: "", label: "Все" },
  { id: "visits", label: "Визиты" },
  { id: "health", label: "Здоровье" },
  { id: "security", label: "Безопасность" },
  { id: "resolved", label: "Восстановление" },
];

const selectCls = "h-9 rounded-md border bg-card px-2 text-sm";

export function Notifications() {
  const [filter, setFilter] = useState<NotificationFilter>({});
  const list = useNotifications(filter);
  const devices = useDevices();
  const markRead = useMarkRead();
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">Уведомления</h1>
        <Button variant="outline" size="sm" onClick={() => markRead.mutate("all")}>
          <CheckCheck /> Прочитать все
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <select
          className={selectCls}
          value={filter.category ?? ""}
          onChange={(e) => setFilter({ ...filter, category: e.target.value || undefined })}
          aria-label="Категория"
        >
          {categories.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </select>
        <select
          className={selectCls}
          value={filter.device ?? ""}
          onChange={(e) => setFilter({ ...filter, device: e.target.value || undefined })}
          aria-label="Устройство"
        >
          <option value="">Все устройства</option>
          {devices.data?.map((d) => (
            <option key={d.id} value={d.id}>
              {d.name}
            </option>
          ))}
        </select>
        <select
          className={selectCls}
          value={filter.severity ?? ""}
          onChange={(e) => setFilter({ ...filter, severity: e.target.value || undefined })}
          aria-label="Важность"
        >
          <option value="">Любая важность</option>
          <option value="critical">Критические</option>
          <option value="warning">Предупреждения</option>
          <option value="info">Информация</option>
          <option value="ok">Восстановление</option>
        </select>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-primary"
            checked={!!filter.unread}
            onChange={(e) => setFilter({ ...filter, unread: e.target.checked || undefined })}
          />
          Только непрочитанные
        </label>
      </div>

      {list.isPending ? (
        <Skeleton className="h-64" />
      ) : items.length === 0 ? (
        <EmptyState title="Ничего не найдено" />
      ) : (
        <Card className="p-1">
          {items.map((n) => (
            <NotificationItem key={n.id} n={n} />
          ))}
        </Card>
      )}
      {list.hasNextPage && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Показать ещё
          </Button>
        </div>
      )}
    </div>
  );
}
