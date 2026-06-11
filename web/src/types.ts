// Mirrors the Go JSON contract in internal/graph/types.go.

export interface Session {
  pid: number;
  user: string;
  appName: string;
  clientAddr: string;
  state: string;
  waitEventType: string;
  waitEvent: string;
  backendType: string;
  xactStart: string; // RFC3339; zero value "0001-01-01T00:00:00Z"
  queryStart: string;
  waitStart: string;
  query: string;
  blockedBy: number[] | null;
  isRoot: boolean;
}

export interface Edge {
  waiterPid: number;
  blockerPid: number;
  lockType: string;
  relation: string;
  waiterMode: string;
  blockerMode: string;
}

export interface Snapshot {
  cluster: string;
  takenAt: string;
  sessions: Record<string, Session>;
  edges: Edge[] | null;
  roots: number[] | null;
}

export interface ActionResult {
  action: "cancel" | "terminate";
  pid: number;
  delivered: boolean;
  at: string;
}

export interface SnapshotMeta {
  takenAt: string;
  roots: number;
  edges: number;
  sessions: number;
}

export interface LockRow {
  lockType: string;
  object: string;
  mode: string;
  granted: boolean;
  pid: number;
}

export interface HotObject {
  object: string;
  waiters: number;
  holders: number;
}

// PostgreSQL's zero timestamp, used to detect unset time fields.
export const ZERO_TIME = "0001-01-01T00:00:00Z";

export function isZeroTime(t: string): boolean {
  return !t || t === ZERO_TIME;
}
