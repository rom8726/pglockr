import { useEffect, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import { Forest } from "./Forest";
import { DetailPanel } from "./DetailPanel";
import { Login } from "./Login";
import { useStream } from "./useStream";
import { clearToken, fetchSnapshot, getToken } from "./api";
import type { Snapshot } from "./types";

const CLUSTER = "default";

export default function App() {
  const [authed, setAuthed] = useState(() => !!getToken());
  const [selectedPid, setSelectedPid] = useState<number | null>(null);
  const { snapshot: live, state } = useStream(CLUSTER, authed);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);

  // Seed with a REST snapshot so there is immediate content before the first
  // WebSocket frame arrives; the stream then takes over.
  useEffect(() => {
    if (!authed) return;
    fetchSnapshot(CLUSTER)
      .then(setSnapshot)
      .catch(() => {
        /* stream will fill in; ignore initial fetch errors */
      });
  }, [authed]);

  useEffect(() => {
    if (live) setSnapshot(live);
  }, [live]);

  if (!authed) {
    return <Login onAuthed={() => setAuthed(true)} />;
  }

  const rootCount = snapshot?.roots?.length ?? 0;
  const waiterCount = snapshot?.edges?.length ?? 0;

  return (
    <div className="app">
      <header className="topbar">
        <span className="topbar__brand">pglockr</span>
        <span className="topbar__cluster">{CLUSTER}</span>
        <span className={`topbar__conn topbar__conn--${state}`}>{state}</span>
        <span className="topbar__stat">{rootCount} root blocker(s)</span>
        <span className="topbar__stat">{waiterCount} blocked edge(s)</span>
        <span className="topbar__spacer" />
        <button
          className="btn btn--ghost"
          onClick={() => {
            clearToken();
            setAuthed(false);
          }}
        >
          Sign out
        </button>
      </header>

      <main className="main">
        <div className="graph">
          {snapshot && (rootCount > 0 || Object.keys(snapshot.sessions).length > 0) ? (
            rootCount > 0 || waiterCount > 0 ? (
              <ReactFlowProvider>
                <Forest snapshot={snapshot} selectedPid={selectedPid} onSelect={setSelectedPid} />
              </ReactFlowProvider>
            ) : (
              <div className="empty">No blocking right now. 🎉</div>
            )
          ) : (
            <div className="empty">Waiting for first snapshot…</div>
          )}
        </div>

        {selectedPid !== null && snapshot && (
          <DetailPanel snapshot={snapshot} pid={selectedPid} onClose={() => setSelectedPid(null)} />
        )}
      </main>
    </div>
  );
}
