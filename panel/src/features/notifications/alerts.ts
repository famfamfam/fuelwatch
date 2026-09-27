import { useSyncExternalStore } from "react";
import type { Notification } from "@/api/types";
import { stripEmoji } from "@/lib/utils";

// Личные настройки оповещений — в localStorage этого браузера (docs/06-panel.md §8).
// Хранилище может быть недоступно (приватный режим) — тогда просто выключено.

type Key = "fw.browserNotify" | "fw.sound";
const listeners = new Set<() => void>();

function read(key: Key): boolean {
  try {
    return localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

export function setPref(key: Key, on: boolean) {
  try {
    localStorage.setItem(key, on ? "1" : "0");
  } catch {
    /* хранилище недоступно */
  }
  listeners.forEach((l) => l());
}

export function usePref(key: Key) {
  return useSyncExternalStore(
    (cb) => (listeners.add(cb), () => listeners.delete(cb)),
    () => read(key),
  );
}

export const browserNotifySupported = typeof window !== "undefined" && "Notification" in window;

/** Включить системные уведомления: спрашивает разрешение браузера. */
export async function enableBrowserNotify(): Promise<boolean> {
  if (!browserNotifySupported) return false;
  const p = Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
  setPref("fw.browserNotify", p === "granted");
  return p === "granted";
}

let audio: AudioContext | null = null;

/** Короткий двойной сигнал через WebAudio (без файлов). */
function beep() {
  try {
    audio ??= new AudioContext();
    const t = audio.currentTime;
    for (const [start, freq] of [
      [0, 880],
      [0.25, 660],
    ] as const) {
      const o = audio.createOscillator();
      const g = audio.createGain();
      o.frequency.value = freq;
      g.gain.setValueAtTime(0.2, t + start);
      g.gain.exponentialRampToValueAtTime(0.001, t + start + 0.2);
      o.connect(g).connect(audio.destination);
      o.start(t + start);
      o.stop(t + start + 0.2);
    }
  } catch {
    /* звук недоступен */
  }
}


/** Системное уведомление (если вкладка не на виду) и звук для критических. */
export function alertNotification(n: Notification) {
  if (read("fw.sound") && n.severity === "critical") beep();
  if (read("fw.browserNotify") && browserNotifySupported && Notification.permission === "granted" && document.hidden) {
    const sn = new Notification(stripEmoji(n.title), { body: n.body || undefined, tag: `fw-${n.id}` });
    sn.onclick = () => {
      window.focus();
      if (n.device_id) window.location.assign(`/devices/${n.device_id}`);
    };
  }
}
