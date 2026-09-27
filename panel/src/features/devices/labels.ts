import type { CommandStatus, CommandType, Mode, Severity } from "@/api/types";

export const modeLabel: Record<Mode, string> = {
  setup: "Настройка",
  armed: "Мониторинг",
  paused: "Пауза",
};

export const issueLabel: Record<string, string> = {
  OFFLINE: "Нет связи",
  POWER_OFF: "Нет питания",
  LOW_BATTERY: "Низкий заряд",
  OVERHEAT: "Перегрев",
  CAMERA_STALE: "Камера молчит",
  MOVED: "Сдвинут",
  VIEW_CHANGED: "Вид изменился",
  VIEW_BLOCKED: "Вид закрыт",
  THEFT_SUSPECTED: "Возможная кража",
};

export const issueSeverity = (type: string): Severity =>
  type === "OFFLINE" || type === "THEFT_SUSPECTED" ? "critical" : "warning";

export const commandLabel: Record<CommandType, string> = {
  snapshot: "Снимок",
  capture_reference: "Сделать эталон",
  arm: "Включить мониторинг",
  pause: "Пауза",
  restart_camera: "Перезапуск камеры",
  restart_app: "Перезапуск приложения",
  live: "Живой режим",
};

export const commandStatusLabel: Record<CommandStatus, string> = {
  pending: "ожидает",
  sent: "отправлена",
  done: "выполнена",
  failed: "ошибка",
  expired: "истекла",
};

export const commandStatusTone: Record<CommandStatus, Severity | "neutral"> = {
  pending: "neutral",
  sent: "info",
  done: "ok",
  failed: "critical",
  expired: "warning",
};

// Android PowerManager.THERMAL_STATUS_*
export const thermalLabel = ["нет", "лёгкий", "умеренный", "сильный", "критический", "аварийный", "отключение"];
