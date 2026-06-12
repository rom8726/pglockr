import { fetchAudit } from "./api";
import { usePolled } from "./usePolled";
import type { AuditEntry } from "./types";

// AuditView lists recent cancel/terminate actions, newest first (admin only —
// the tab is hidden for other roles and the API enforces it server-side).
export function AuditView() {
  const { data, error, loading } = usePolled<AuditEntry[]>(() => fetchAudit(200), 5000);

  if (error) return <div className="tab-msg tab-msg--err">Error: {error}</div>;
  if (loading && !data) return <div className="tab-msg">Loading audit…</div>;
  if (!data || data.length === 0) return <div className="tab-msg">No actions recorded yet.</div>;

  return (
    <div className="tab-scroll">
      <table className="tbl">
        <thead>
          <tr>
            <th>Time</th>
            <th>Actor</th>
            <th>Action</th>
            <th className="num">PID</th>
            <th>Result</th>
            <th>Victim query</th>
          </tr>
        </thead>
        <tbody>
          {data.map((e, i) => (
            <tr key={`${e.at}-${e.pid}-${i}`}>
              <td className="mono">{new Date(e.at).toLocaleString()}</td>
              <td>{e.actor}</td>
              <td>
                <span className={`pill ${e.action === "terminate" ? "pill--danger" : "pill--warn"}`}>
                  {e.action}
                </span>
              </td>
              <td className="num mono">{e.pid}</td>
              <td>
                {e.error ? (
                  <span className="pill pill--danger" title={e.error}>error</span>
                ) : e.delivered ? (
                  <span className="pill pill--ok">delivered</span>
                ) : (
                  <span className="pill">not delivered</span>
                )}
              </td>
              <td className="tbl__query mono" title={e.victimQuery}>
                {e.victimQuery || "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
