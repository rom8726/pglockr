import { isZeroTime } from "./types";

// ageSeconds returns whole seconds elapsed since an RFC3339 timestamp, or null
// if the timestamp is unset/zero.
export function ageSeconds(t: string, now: number): number | null {
  if (isZeroTime(t)) return null;
  const ms = now - new Date(t).getTime();
  return ms >= 0 ? Math.floor(ms / 1000) : 0;
}

// formatDuration renders seconds as a compact human string (e.g. "1m 05s").
export function formatDuration(secs: number | null): string {
  if (secs === null) return "—";
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  if (m < 60) return `${m}m ${String(s).padStart(2, "0")}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${String(m % 60).padStart(2, "0")}m`;
}
