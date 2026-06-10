import type { ActionResult, Snapshot } from "./types";

// The bearer token is kept in localStorage and also mirrored into a cookie so
// the browser can authenticate the WebSocket upgrade (which cannot set custom
// headers). This is the MVP single-token scheme.
const TOKEN_KEY = "pglockr_token";

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
  // Session cookie, SameSite=Strict for CSRF resistance.
  document.cookie = `pglockr_token=${encodeURIComponent(token)}; path=/; SameSite=Strict`;
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
  document.cookie = "pglockr_token=; path=/; max-age=0";
}

function authHeaders(): HeadersInit {
  const t = getToken();
  return t ? { Authorization: `Bearer ${t}` } : {};
}

export class AuthError extends Error {}

async function handle<T>(res: Response): Promise<T> {
  if (res.status === 401) throw new AuthError("unauthorized");
  if (!res.ok) throw new Error(await res.text());
  return res.json() as Promise<T>;
}

export async function fetchSnapshot(cluster: string): Promise<Snapshot> {
  const res = await fetch(`/api/snapshot?cluster=${encodeURIComponent(cluster)}`, {
    headers: authHeaders(),
  });
  return handle<Snapshot>(res);
}

export async function signalSession(
  pid: number,
  action: "cancel" | "terminate",
): Promise<ActionResult> {
  const res = await fetch(`/api/sessions/${pid}/${action}`, {
    method: "POST",
    headers: authHeaders(),
  });
  return handle<ActionResult>(res);
}

// streamURL builds the ws:// or wss:// URL for the live snapshot stream.
export function streamURL(cluster: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/api/stream?cluster=${encodeURIComponent(cluster)}`;
}
