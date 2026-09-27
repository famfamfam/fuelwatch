import { useState } from "react";
import { NavLink, Outlet } from "react-router";
import { Bell as BellIcon, BellRing, Fuel, LayoutDashboard, LogOut, Menu, SlidersHorizontal, UserRound, Users, Volume2, Wallet, X } from "lucide-react";
import { toast } from "sonner";
import { browserNotifySupported, enableBrowserNotify, setPref, usePref } from "@/features/notifications/alerts";
import { useDevices, useLogout, useMe } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/overlay";
import { Bell } from "@/features/notifications/Bell";
import { OnlineDot } from "@/features/devices/DeviceBits";
import { useEventStream } from "@/stream/useEventStream";
import { cn } from "@/lib/utils";

const nav = [
  { to: "/", label: "Дашборд", icon: LayoutDashboard, end: true },
  { to: "/visits", label: "Визиты", icon: Fuel, end: false },
  { to: "/notifications", label: "Уведомления", icon: BellIcon, end: false },
  { to: "/settings", label: "Настройки", icon: SlidersHorizontal, end: false },
  { to: "/costs", label: "Расходы", icon: Wallet, end: false },
  { to: "/users", label: "Пользователи", icon: Users, end: false },
];

function SideNav({ onNavigate }: { onNavigate?: () => void }) {
  const devices = useDevices();
  const linkCls = ({ isActive }: { isActive: boolean }) =>
    cn(
      "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors hover:bg-muted",
      isActive && "bg-muted font-medium",
    );
  return (
    <nav className="flex flex-col gap-1 p-3" onClick={onNavigate}>
      {nav.map((n) => (
        <NavLink key={n.to} to={n.to} end={n.end} className={linkCls}>
          <n.icon className="size-4" /> {n.label}
        </NavLink>
      ))}
      {(devices.data?.length ?? 0) > 0 && (
        <>
          <p className="mt-4 px-3 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Устройства</p>
          {devices.data!.map((d) => (
            <NavLink key={d.id} to={`/devices/${d.id}`} className={linkCls}>
              <OnlineDot online={d.online} className={cn(!d.paired && "opacity-30")} />
              <span className="truncate">{d.name}</span>
            </NavLink>
          ))}
        </>
      )}
    </nav>
  );
}

export function Layout() {
  const me = useMe();
  const logout = useLogout();
  const stream = useEventStream();
  const [menuOpen, setMenuOpen] = useState(false);
  const browserNotify = usePref("fw.browserNotify");
  const sound = usePref("fw.sound");

  return (
    <div className="min-h-dvh md:grid md:grid-cols-[15rem_1fr]">
      <aside className="hidden border-r bg-card md:block">
        <div className="flex h-14 items-center gap-2 border-b px-5 font-semibold">⛽ FuelWatch</div>
        <SideNav />
      </aside>

      {menuOpen && (
        <div className="fixed inset-0 z-40 md:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setMenuOpen(false)} />
          <aside className="absolute inset-y-0 left-0 w-64 overflow-y-auto bg-card shadow-lg">
            <div className="flex h-14 items-center justify-between border-b px-4 font-semibold">
              ⛽ FuelWatch
              <Button variant="ghost" size="icon" onClick={() => setMenuOpen(false)} aria-label="Закрыть меню">
                <X />
              </Button>
            </div>
            <SideNav onNavigate={() => setMenuOpen(false)} />
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b bg-background/90 px-4 backdrop-blur">
          <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setMenuOpen(true)} aria-label="Меню">
            <Menu />
          </Button>
          <span className="font-semibold md:hidden">⛽ FuelWatch</span>
          <div className="flex-1" />
          {stream === "reconnecting" && (
            <span className="text-xs text-sev-warning" title="Нет соединения с сервером, переподключаемся">
              нет связи с сервером…
            </span>
          )}
          <Bell />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" aria-label="Пользователь">
                <UserRound />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuLabel>{me.data?.login}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {browserNotifySupported && (
                <DropdownMenuItem
                  onSelect={async () => {
                    if (browserNotify) setPref("fw.browserNotify", false);
                    else if (!(await enableBrowserNotify())) toast.error("Браузер не разрешил уведомления");
                  }}
                >
                  <BellRing /> {browserNotify ? "✓ " : ""}Уведомления браузера
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onSelect={() => setPref("fw.sound", !sound)}>
                <Volume2 /> {sound ? "✓ " : ""}Звук для критических
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => logout.mutate()}>
                <LogOut /> Выйти
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </header>
        <main className="mx-auto w-full max-w-7xl flex-1 p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
