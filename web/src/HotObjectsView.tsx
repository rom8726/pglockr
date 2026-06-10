import { fetchHotObjects } from "./api";
import { usePolled } from "./usePolled";
import type { HotObject } from "./types";

// HotObjectsView lists the most contended relations (those with waiters).
export function HotObjectsView({ cluster }: { cluster: string }) {
  const { data, error, loading } = usePolled<HotObject[]>(() => fetchHotObjects(cluster), 2000);

  if (error) return <div className="tab-msg tab-msg--err">Error: {error}</div>;
  if (loading && !data) return <div className="tab-msg">Loading hot objects…</div>;
  if (!data || data.length === 0) return <div className="tab-msg">No contended objects right now. 🎉</div>;

  const maxWaiters = Math.max(...data.map((o) => o.waiters), 1);

  return (
    <div className="tab-scroll">
      <table className="tbl">
        <thead>
          <tr>
            <th>Object</th>
            <th className="num">Waiters</th>
            <th className="num">Holders</th>
            <th>Contention</th>
          </tr>
        </thead>
        <tbody>
          {data.map((o) => (
            <tr key={o.object}>
              <td className="mono">{o.object}</td>
              <td className="num">{o.waiters}</td>
              <td className="num">{o.holders}</td>
              <td>
                <div className="bar">
                  <div className="bar__fill" style={{ width: `${(o.waiters / maxWaiters) * 100}%` }} />
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
