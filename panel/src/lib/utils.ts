import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Текущее время, обновляется каждые intervalMs — для «N с назад». */
export function useNow(intervalMs = 5000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

/** Эмодзи в начале заголовка уведомления нужны для Telegram; в панели их заменяет иконка. */
export const stripEmoji = (s: string) => s.replace(/^\p{Extended_Pictographic}\u{FE0F}?\s*/u, "");

/** Значение параметра адреса как состояние (null — нет). Смена не добавляет запись в историю. */
export function useQueryParam(name: string): [string | null, (v: string | null) => void] {
  const [params, setParams] = useSearchParams();
  const set = useCallback(
    (v: string | null) =>
      setParams(
        (p) => {
          const n = new URLSearchParams(p);
          if (v == null) n.delete(name);
          else n.set(name, v);
          return n;
        },
        { replace: true },
      ),
    [name, setParams],
  );
  return [params.get(name), set];
}

export function formatAgo(iso: string | null | undefined, now: number) {
  if (!iso) return "никогда";
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 10) return "только что";
  if (s < 60) return `${s} с назад`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} мин назад`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} ч назад`;
  return formatDateTime(iso);
}

export function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

export function formatDateTime(iso: string) {
  const d = new Date(iso);
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  return sameDay
    ? formatTime(iso)
    : d.toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
}
