import { useMemo } from "react";
import { fetchLocks } from "./api";
import { usePolled } from "./usePolled";
import type { LockRow } from "./types";

// LocksView is the lock inspector: raw pg_locks grouped by object, granted
// holders first then waiters.
export function LocksView({ cluster }: { cluster: string }) {
  const { data, error, loading } = usePolled<LockRow[]>(() => fetchLocks(cluster), 2000);

  const groups = useMemo(() => groupByObject(data ?? []), [data]);

  if (error) return <div className="tab-msg tab-msg--err">Error: {error}</div>;
  if (loading && !data) return <div className="tab-msg">Loading locks…</div>;
  if (groups.length === 0) return <div className="tab-msg">No locks held.</div>;

  return (
    <div className="tab-scroll">
      <table className="tbl">
        <thead>
          <tr>
            <th>Object</th>
            <th>Mode</th>
            <th>State</th>
            <th className="num">PID</th>
            <th>Lock type</th>
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => (
            <GroupRows key={g.object} object={g.object} rows={g.rows} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function GroupRows({ object, rows }: { object: string; rows: LockRow[] }) {
  const waiters = rows.filter((r) => !r.granted).length;
  return (
    <>
      <tr className="tbl__group">
        <td colSpan={5}>
          {object} <span className="tbl__count">{rows.length} locks{waiters > 0 ? `, ${waiters} waiting` : ""}</span>
        </td>
      </tr>
      {rows.map((r, i) => (
        <tr key={`${object}-${r.pid}-${r.mode}-${i}`} className={r.granted ? "" : "tbl__waiting"}>
          <td></td>
          <td className="mono">{r.mode}</td>
          <td>{r.granted ? <span className="pill pill--ok">granted</span> : <span className="pill pill--warn">waiting</span>}</td>
          <td className="num mono">{r.pid}</td>
          <td className="tbl__dim">{r.lockType}</td>
        </tr>
      ))}
    </>
  );
}

function groupByObject(rows: LockRow[]): { object: string; rows: LockRow[] }[] {
  const m = new Map<string, LockRow[]>();
  for (const r of rows) {
    const arr = m.get(r.object) ?? [];
    arr.push(r);
    m.set(r.object, arr);
  }
  // Objects with waiters first, then by number of locks.
  return [...m.entries()]
    .map(([object, rs]) => ({ object, rows: rs }))
    .sort((a, b) => {
      const aw = a.rows.some((r) => !r.granted) ? 1 : 0;
      const bw = b.rows.some((r) => !r.granted) ? 1 : 0;
      if (aw !== bw) return bw - aw;
      return b.rows.length - a.rows.length;
    });
}
