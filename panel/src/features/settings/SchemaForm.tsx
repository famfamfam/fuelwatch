import { useMemo, useState } from "react";
import { RotateCcw, RefreshCw, Crosshair, Smartphone } from "lucide-react";
import { toast } from "sonner";
import { ApiError, errorMessage } from "@/api/client";
import type { SettingDef, SettingSource, SettingValue } from "@/api/types";
import type { SettingsChanges } from "@/api/settings";
import { Button } from "@/components/ui/button";
import { Badge, Input } from "@/components/ui/primitives";
import { Dialog, DialogContent } from "@/components/ui/overlay";
import { cn } from "@/lib/utils";
import { validateSetting, formatValue, sameValue } from "./values";

export interface SchemaFormProps {
  defs: SettingDef[];
  /** Действующее значение и его источник по каждому ключу. */
  current: Record<string, { value: SettingValue; source: SettingSource }>;
  /** "global" — сброс удаляет глобальное значение; "device" — удаляет переопределение устройства. */
  level: "global" | "device";
  onSave: (changes: SettingsChanges) => Promise<unknown>;
  /** Вызывается после успешного сохранения с изменёнными ключами. */
  onSaved?: (changed: SettingDef[]) => void;
}

const sourceLabel: Record<SettingSource, string> = {
  default: "по умолчанию",
  global: "глобально",
  device: "своё",
};

/**
 * Форма настроек по реестру (D-15, docs/06-panel.md §10.1). Поля строятся по типу из схемы,
 * значения проверяются по min/max/вариантам до отправки; сервер проверяет ещё раз.
 */
export function SchemaForm({ defs, current, level, onSave, onSaved }: SchemaFormProps) {
  // undefined — не менялось, null — сбросить (вернуть значение уровнем выше).
  const [draft, setDraft] = useState<Record<string, SettingValue | null>>({});
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({});
  const [confirm, setConfirm] = useState(false);
  const [saving, setSaving] = useState(false);

  const byKey = useMemo(() => Object.fromEntries(defs.map((d) => [d.key, d])), [defs]);
  const changedKeys = Object.keys(draft).filter((k) => k in byKey);
  const clientErrors = Object.fromEntries(
    changedKeys
      .map((k) => [k, draft[k] === null ? null : validateSetting(byKey[k], draft[k] as SettingValue)] as const)
      .filter(([, e]) => e),
  ) as Record<string, string>;
  const hasErrors = Object.keys(clientErrors).length > 0;

  const set = (key: string, v: SettingValue | null) => {
    setServerErrors((e) => ({ ...e, [key]: "" }));
    setDraft((d) => {
      const cur = current[key];
      // Вернули исходное значение — это не изменение.
      if (v !== null && cur && sameValue(v, cur.value) && cur.source === level) {
        const { [key]: _, ...rest } = d;
        return rest;
      }
      return { ...d, [key]: v };
    });
  };

  const save = async () => {
    setSaving(true);
    try {
      await onSave(Object.fromEntries(changedKeys.map((k) => [k, draft[k]])));
      onSaved?.(changedKeys.map((k) => byKey[k]));
      setDraft({});
      setServerErrors({});
      setConfirm(false);
    } catch (e) {
      if (e instanceof ApiError && e.fields) setServerErrors(e.fields);
      else toast.error(errorMessage(e));
      setConfirm(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    // @container: раскладка зависит от ширины формы, а не окна (форма бывает в узкой колонке).
    <div className="@container space-y-1">
      {defs.map((d) => {
        const cur = current[d.key];
        const readOnly = level === "device" && !d.overridable;
        const edited = d.key in draft;
        const value = edited ? (draft[d.key] ?? d.default) : (cur?.value ?? d.default);
        const canReset = !readOnly && (edited ? draft[d.key] !== null && cur?.source === level : cur?.source === level);
        const error = clientErrors[d.key] || serverErrors[d.key];
        return (
          <div
            key={d.key}
            className={cn(
              "grid gap-2 rounded-md px-3 py-3 @2xl:grid-cols-[1fr_minmax(0,18rem)] @2xl:items-center",
              edited && "bg-primary/5",
            )}
          >
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-sm font-medium">{d.label}</span>
                {d.scope === "device" && (
                  <Badge title="Применится на телефоне в течение одного heartbeat (~20 с)">
                    <Smartphone className="size-3" /> телефон
                  </Badge>
                )}
                {d.camera_restart && (
                  <Badge tone="info" title="Телефон перезапустит камеру">
                    <RefreshCw className="size-3" /> камера
                  </Badge>
                )}
                {d.new_reference && (
                  <Badge tone="warning" title="После изменения нужен новый эталон и проверка зон">
                    <Crosshair className="size-3" /> эталон
                  </Badge>
                )}
                {cur && (
                  <Badge tone={cur.source === "default" ? "neutral" : "info"}>
                    {edited ? (draft[d.key] === null ? "будет сброшено" : "изменено") : sourceLabel[cur.source]}
                  </Badge>
                )}
              </div>
              {d.help && <p className="text-xs text-muted-foreground">{d.help}</p>}
              <p className="font-mono text-[11px] text-muted-foreground/70">
                {d.key} · по умолчанию {formatValue(d, d.default)}
                {readOnly && " · задаётся только глобально"}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1">
                <Field def={d} value={value} disabled={readOnly} onChange={(v) => set(d.key, v)} invalid={!!error} />
                {error && <p className="mt-1 text-xs text-sev-critical">{error}</p>}
              </div>
              <Button
                variant="ghost"
                size="icon"
                className={cn(!canReset && "invisible")}
                title={level === "device" ? "Убрать своё значение" : "Вернуть значение по умолчанию"}
                onClick={() => set(d.key, null)}
              >
                <RotateCcw />
              </Button>
            </div>
          </div>
        );
      })}

      {changedKeys.length > 0 && (
        <div className="sticky bottom-0 z-10 flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-card p-3 shadow-lg">
          <span className="text-sm">Изменено: {changedKeys.length}</span>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={() => (setDraft({}), setServerErrors({}))}>
              Отменить
            </Button>
            <Button disabled={hasErrors} onClick={() => setConfirm(true)}>
              Сохранить
            </Button>
          </div>
        </div>
      )}

      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent title="Сохранить изменения?" className="max-w-lg">
          <ul className="max-h-80 space-y-2 overflow-y-auto text-sm">
            {changedKeys.map((k) => {
              const d = byKey[k];
              const next = draft[k];
              return (
                <li key={k} className="flex flex-wrap items-baseline justify-between gap-2">
                  <span>{d.label}</span>
                  <span className="text-muted-foreground">
                    {formatValue(d, current[k]?.value ?? d.default)} →{" "}
                    <b className="text-foreground">{next === null ? `сброс (${sourceLabel[level === "device" ? "global" : "default"]})` : formatValue(d, next)}</b>
                  </span>
                </li>
              );
            })}
          </ul>
          {changedKeys.some((k) => byKey[k].scope === "device") && (
            <p className="text-sm text-muted-foreground">Телефон получит изменения в течение ~20 с.</p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirm(false)}>
              Назад
            </Button>
            <Button onClick={save} disabled={saving}>
              Сохранить
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

const selectCls = "h-9 w-full rounded-md border bg-card px-2 text-sm disabled:opacity-60";

function Field({
  def,
  value,
  onChange,
  disabled,
  invalid,
}: {
  def: SettingDef;
  value: SettingValue;
  onChange: (v: SettingValue) => void;
  disabled?: boolean;
  invalid?: boolean;
}) {
  const ring = invalid ? "border-sev-critical" : "";
  switch (def.type) {
    case "bool":
      return (
        <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="peer sr-only"
            checked={value === true}
            disabled={disabled}
            onChange={(e) => onChange(e.target.checked)}
          />
          <span className="relative h-5 w-9 rounded-full bg-muted transition-colors peer-checked:bg-primary peer-disabled:opacity-60 after:absolute after:top-0.5 after:left-0.5 after:size-4 after:rounded-full after:bg-white after:shadow after:transition-transform peer-checked:after:translate-x-4" />
          {value ? "да" : "нет"}
        </label>
      );
    case "enum":
      return (
        <select className={cn(selectCls, ring)} value={String(value)} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
          {!(def.options ?? []).includes(String(value)) && <option value={String(value)}>{String(value)}</option>}
          {(def.options ?? []).map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      );
    case "enum_list": {
      const list = Array.isArray(value) ? value : [];
      return (
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          {(def.options ?? []).map((o) => (
            <label key={o} className="flex items-center gap-1.5 text-xs">
              <input
                type="checkbox"
                className="accent-primary"
                disabled={disabled}
                checked={list.includes(o)}
                onChange={(e) => onChange(e.target.checked ? [...list, o] : list.filter((x) => x !== o))}
              />
              {o}
            </label>
          ))}
        </div>
      );
    }
    case "string_list":
      return (
        <Input
          className={ring}
          disabled={disabled}
          value={Array.isArray(value) ? value.join(", ") : ""}
          placeholder="через запятую"
          onChange={(e) =>
            onChange(
              e.target.value
                .split(",")
                .map((s) => s.trim())
                .filter(Boolean),
            )
          }
        />
      );
    case "string":
      return <Input className={ring} disabled={disabled} value={String(value)} onChange={(e) => onChange(e.target.value)} />;
    default:
      return <NumberField def={def} value={Number(value)} onChange={onChange} disabled={disabled} className={ring} />;
  }
}

function NumberField({
  def,
  value,
  onChange,
  disabled,
  className,
}: {
  def: SettingDef;
  value: number;
  onChange: (v: number) => void;
  disabled?: boolean;
  className?: string;
}) {
  const [text, setText] = useState<string | null>(null);
  const hasRange = def.min != null && def.max != null;
  return (
    <div className="flex items-center gap-2">
      {hasRange && (
        <input
          type="range"
          className="min-w-0 flex-1 accent-primary"
          min={def.min}
          max={def.max}
          step={def.step ?? (def.type === "int" ? 1 : 0.01)}
          value={value}
          disabled={disabled}
          onChange={(e) => (setText(null), onChange(Number(e.target.value)))}
        />
      )}
      <div className="flex items-center gap-1">
        <Input
          className={cn("w-20 text-right", className)}
          inputMode="decimal"
          disabled={disabled}
          value={text ?? String(value)}
          onChange={(e) => {
            setText(e.target.value);
            const n = Number(e.target.value.replace(",", "."));
            if (e.target.value.trim() !== "" && !Number.isNaN(n)) onChange(n);
          }}
          onBlur={() => setText(null)}
        />
        <span className="w-8 text-xs text-muted-foreground">{def.unit}</span>
      </div>
    </div>
  );
}
