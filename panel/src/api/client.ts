export class ApiError extends Error {
  status: number;
  code: string;
  fields?: Record<string, string>;

  constructor(status: number, code: string, message: string, fields?: Record<string, string>) {
    super(message || code);
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

export const UNAUTHORIZED_EVENT = "fw:unauthorized";

/** Запрос к /api/panel. Изменяющие запросы несут X-Requested-With (защита от CSRF на сервере). */
export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const headers: Record<string, string> = { "X-Requested-With": "fuelwatch" };
  if (init.body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(`/api/panel${path}`, {
    method: init.method ?? "GET",
    headers,
    credentials: "same-origin",
    body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
  });
  if (!res.ok) {
    const j = await res.json().catch(() => null);
    if (res.status === 401 && path !== "/auth/login" && path !== "/auth/me") {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    }
    throw new ApiError(res.status, j?.error ?? "http_" + res.status, j?.message ?? res.statusText, j?.fields);
  }
  return res.json() as Promise<T>;
}

export const frameThumbUrl = (id: string) => `/api/panel/frames/${encodeURIComponent(id)}/thumb`;
export const frameImageUrl = (id: string) => `/api/panel/frames/${encodeURIComponent(id)}/image`;

export function errorMessage(e: unknown) {
  if (e instanceof ApiError) {
    if (e.fields) return Object.values(e.fields).join("; ");
    return e.message;
  }
  return e instanceof Error ? e.message : String(e);
}
