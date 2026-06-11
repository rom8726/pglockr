import { useState } from "react";
import type { Snapshot } from "./types";
import { signalSession } from "./api";
import { useNow } from "./useNow";
import { ageSeconds, formatDuration } from "./time";

type Action = "cancel" | "terminate";

export function DetailPanel({
  snapshot,
  pid,
  canAct,
  onClose,
}: {
  snapshot: Snapshot;
  pid: number;
  canAct: boolean;
  onClose: () => void;
}) {
  const session = snapshot.sessions[String(pid)];
  const now = useNow();
  const [confirm, setConfirm] = useState<Action | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | null>(null);

  if (!session) {
    return (
      <aside className="panel">
        <div className="panel__head">
          <span>pid {pid}</span>
          <button onClick={onClose}>✕</button>
        </div>
        <p className="panel__gone">Session gone — it is no longer in the latest snapshot.</p>
      </aside>
    );
  }

  const doAction = async (action: Action) => {
    setBusy(true);
    setResult(null);
    try {
      const res = await signalSession(pid, action);
      setResult(res.delivered ? `${action} delivered` : `${action} not delivered (backend gone or protected)`);
    } catch (e) {
      setResult(`error: ${(e as Error).message}`);
    } finally {
      setBusy(false);
      setConfirm(null);
    }
  };

  const xactAge = ageSeconds(session.xactStart, now);
  const queryAge = ageSeconds(session.queryStart, now);

  return (
    <aside className="panel">
      <div className="panel__head">
        <span>
          pid {session.pid} {session.isRoot && <span className="badge badge--root">ROOT</span>}
        </span>
        <button onClick={onClose} aria-label="Close">✕</button>
      </div>

      <dl className="panel__grid">
        <dt>User</dt><dd>{session.user || "—"}</dd>
        <dt>App</dt><dd>{session.appName || "—"}</dd>
        <dt>Client</dt><dd>{session.clientAddr || "—"}</dd>
        <dt>State</dt>
        <dd className={session.state === "idle in transaction" ? "alert" : ""}>{session.state || "—"}</dd>
        <dt>Wait</dt><dd>{session.waitEventType ? `${session.waitEventType} / ${session.waitEvent}` : "—"}</dd>
        <dt>Txn age</dt><dd>{formatDuration(xactAge)}</dd>
        <dt>Query age</dt><dd>{formatDuration(queryAge)}</dd>
        <dt>Blocked by</dt><dd>{session.blockedBy?.length ? session.blockedBy.join(", ") : "—"}</dd>
      </dl>

      <h3 className="panel__label">Query</h3>
      <pre className="panel__query">{session.query || "(no query text — pg_monitor required)"}</pre>

      <div className="panel__actions">
        {!canAct ? (
          <p className="panel__note">Read-only (viewer role): cancel/terminate disabled.</p>
        ) : confirm === null ? (
          <>
            <button className="btn btn--warn" disabled={busy} onClick={() => setConfirm("cancel")}>
              Cancel query
            </button>
            <button className="btn btn--danger" disabled={busy} onClick={() => setConfirm("terminate")}>
              Terminate backend
            </button>
          </>
        ) : (
          <div className="confirm">
            <p>
              {confirm === "cancel" ? "Cancel the running query" : "Terminate the connection"} of{" "}
              <strong>pid {pid}</strong> ({session.user})?
            </p>
            <pre className="confirm__query">{session.query || "(no query text)"}</pre>
            <div className="confirm__buttons">
              <button className={`btn ${confirm === "terminate" ? "btn--danger" : "btn--warn"}`} disabled={busy} onClick={() => doAction(confirm)}>
                {busy ? "…" : `Yes, ${confirm}`}
              </button>
              <button className="btn" disabled={busy} onClick={() => setConfirm(null)}>
                No
              </button>
            </div>
          </div>
        )}
      </div>

      {result && <p className="panel__result">{result}</p>}
      {canAct && (
        <p className="panel__note">
          Note: superuser backends cannot be signalled with <code>pg_signal_backend</code>.
        </p>
      )}
    </aside>
  );
}
