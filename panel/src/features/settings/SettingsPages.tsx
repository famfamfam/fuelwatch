import { useState } from "react";
import { toast } from "sonner";
import { useDeviceSettings, useGlobalSettings, usePutDeviceSettings, usePutGlobalSettings, useSettingsSchema } from "@/api/settings";
import type { SettingSource, SettingValue } from "@/api/types";
import { Card, CardContent, Skeleton } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";
import { SchemaForm } from "./SchemaForm";
import { TelegramCard } from "./TelegramCard";
import { VisionCosts } from "./VisionCosts";

function GroupTabs({
  groups,
  active,
  onChange,
}: {
  groups: { id: string; label: string }[];
  active: string;
  onChange: (id: string) => void;
}) {
  return (
    <div className="flex gap-1 overflow-x-auto border-b">
      {groups.map((g) => (
        <button
          key={g.id}
          type="button"
          onClick={() => onChange(g.id)}
          className={cn(
            "shrink-0 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors",
            active === g.id ? "border-primary font-medium" : "border-transparent text-muted-foreground hover:text-foreground",
          )}
        >
          {g.label}
        </button>
      ))}
    </div>
  );
}

/** Глобальные настройки (docs/06-panel.md §10): вкладки по группам реестра. */
export function GlobalSettingsPage() {
  const schema = useSettingsSchema();
  const global = useGlobalSettings();
  const put = usePutGlobalSettings();
  const [group, setGroup] = useState("camera");

  if (schema.isPending || global.isPending) return <Skeleton className="h-96" />;
  if (!schema.data || !global.data) return null;

  const current: Record<string, { value: SettingValue; source: SettingSource }> = {};
  for (const d of schema.data.settings) {
    current[d.key] = d.key in global.data ? { value: global.data[d.key], source: "global" } : { value: d.default, source: "default" };
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold">Настройки</h1>
        <p className="text-sm text-muted-foreground">Действуют на все устройства. Для отдельного телефона их можно переопределить на его вкладке «Настройки».</p>
      </div>
      <GroupTabs groups={schema.data.groups} active={group} onChange={setGroup} />
      {group === "notify" && <TelegramCard />}
      {group === "vision" && (
        <p className="text-sm text-muted-foreground">
          Ключ OpenRouter задаётся только в .env сервера (VLM_API_KEY). Модель можно сравнить на реальном кадре: «Кадры» → кадр →
          «Проверить модель», или прогоном <code className="rounded bg-muted px-1">fuelwatch replay</code>.
        </p>
      )}
      {group === "vision" && <VisionCosts />}
      <Card>
        <CardContent className="px-1 pt-2">
          <SchemaForm
            key={group}
            defs={schema.data.settings.filter((d) => d.group === group)}
            current={current}
            level="global"
            onSave={(c) => put.mutateAsync(c)}
            onSaved={() => toast.success("Сохранено")}
          />
        </CardContent>
      </Card>
    </div>
  );
}

/** Настройки устройства: действующее значение, откуда оно, своё переопределение. */
export function DeviceSettingsTab({ deviceId }: { deviceId: string }) {
  const schema = useSettingsSchema(deviceId);
  const values = useDeviceSettings(deviceId);
  const put = usePutDeviceSettings(deviceId);
  const [group, setGroup] = useState("camera");

  if (schema.isPending || values.isPending) return <Skeleton className="h-96" />;
  if (!schema.data || !values.data) return null;

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Бейдж «своё» — значение только для этого телефона. Кнопка ↺ убирает его, и снова действует глобальное.
      </p>
      <GroupTabs groups={schema.data.groups} active={group} onChange={setGroup} />
      <Card>
        <CardContent className="px-1 pt-2">
          <SchemaForm
            key={group}
            defs={schema.data.settings.filter((d) => d.group === group)}
            current={values.data}
            level="device"
            onSave={(c) => put.mutateAsync(c)}
            onSaved={() => toast.success("Сохранено")}
          />
        </CardContent>
      </Card>
    </div>
  );
}
