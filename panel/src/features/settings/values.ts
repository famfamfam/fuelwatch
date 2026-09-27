import type { SettingDef, SettingValue } from "@/api/types";

/** Проверка значения по схеме — те же правила, что на сервере (settings.Def.Normalize). */
export function validateSetting(d: SettingDef, v: SettingValue): string | null {
  switch (d.type) {
    case "int":
    case "float": {
      if (typeof v !== "number" || Number.isNaN(v)) return "нужно число";
      if (d.type === "int" && !Number.isInteger(v)) return "нужно целое число";
      if (d.min != null && v < d.min) return `не меньше ${d.min}`;
      if (d.max != null && v > d.max) return `не больше ${d.max}`;
      return null;
    }
    case "enum":
      return d.options && !d.options.includes(String(v)) ? "недопустимое значение" : null;
    case "enum_list":
      return Array.isArray(v) && v.every((x) => d.options?.includes(x)) ? null : "недопустимое значение";
    default:
      return null;
  }
}

export function formatValue(d: SettingDef, v: SettingValue | undefined): string {
  if (v === undefined) return "—";
  if (typeof v === "boolean") return v ? "да" : "нет";
  if (Array.isArray(v)) return v.length ? v.join(", ") : "пусто";
  if (v === "") return "пусто";
  return d.unit ? `${v} ${d.unit}` : String(v);
}

export function sameValue(a: SettingValue, b: SettingValue): boolean {
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => x === b[i]);
  return a === b;
}
