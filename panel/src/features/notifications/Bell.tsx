import { Link } from "react-router";
import { Bell as BellIcon, CheckCheck } from "lucide-react";
import { useMarkRead, useNotifications, useUnreadCount } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/overlay";
import { NotificationItem } from "./NotificationItem";

export function Bell() {
  const unread = useUnreadCount();
  const list = useNotifications({}, 10);
  const markRead = useMarkRead();
  const count = unread.data ?? 0;
  const items = list.data?.pages[0]?.items ?? [];

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" aria-label={`Уведомления: ${count} непрочитанных`}>
          <BellIcon />
          {count > 0 && (
            <span className="absolute -top-0.5 -right-0.5 flex min-w-4 items-center justify-center rounded-full bg-sev-critical px-1 text-[10px] leading-4 font-semibold text-white">
              {count > 99 ? "99+" : count}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[min(24rem,calc(100vw-2rem))] p-0">
        <div className="flex items-center justify-between border-b px-3 py-2">
          <span className="text-sm font-semibold">Уведомления</span>
          {count > 0 && (
            <Button variant="ghost" size="sm" onClick={() => markRead.mutate("all")}>
              <CheckCheck /> Прочитать все
            </Button>
          )}
        </div>
        <div className="max-h-96 overflow-y-auto p-1">
          {items.length === 0 ? (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">Уведомлений нет</p>
          ) : (
            items.map((n) => <NotificationItem key={n.id} n={n} compact />)
          )}
        </div>
        <div className="border-t p-1">
          <Button variant="ghost" size="sm" className="w-full" asChild>
            <Link to="/notifications">Все уведомления</Link>
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
