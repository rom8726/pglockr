import { useEffect, useMemo, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import { Forest } from "./Forest";
import { DetailPanel } from "./DetailPanel";
import { LocksView } from "./LocksView";
import { HotObjectsView } from "./HotObjectsView";
import { AuditView } from "./AuditView";
import { Scrubber } from "./Scrubber";
import { Login } from "./Login";
import { useStream } from "./useStream";
import { usePolled } from "./usePolled";
import { clearToken, fetchHistory, fetchMe, fetchSnapshot, getToken } from "./api";
import { canAct, type Principal, type Snapshot, type SnapshotMeta } from "./types";

const CLUSTER = "default";

type View = "forest" | "locks" | "hot" | "audit";

const BASE_TABS: { id: View; label: string }[] = [
  { id: "forest", label: "Blocking forest" },
  { id: "locks", label: "Lock inspector" },
  { id: "hot", label: "Hot objects" },
];

const AUDIT_TAB: { id: View; label: string } = { id: "audit", label: "Audit" };

export default function App() {
  const [authed, setAuthed] = useState(() => !!getToken());
  const [me, setMe] = useState<Principal | null>(null);
  const [view, setView] = useState<View>("forest");
  const [selectedPid, setSelectedPid] = useState<number | null>(null);

  const { snapshot: live, state } = useStream(CLUSTER, authed);
  const [liveSnapshot, setLiveSnapshot] = useState<Snapshot | null>(null);

  // History scrubber state.
  const { data: metas } = usePolled<SnapshotMeta[]>(
    () => fetchHistory(CLUSTER),
    1500,
    authed && view === "forest",
  );
  const [paused, setPaused] = useState(false);
  const [index, setIndex] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [histSnapshot, setHistSnapshot] = useState<Snapshot | null>(null);

  const n = metas?.length ?? 0;
  const effectiveIndex = paused ? Math.min(index, Math.max(0, n - 1)) : Math.max(0, n - 1);

  // Resolve the authenticated principal (name + role) for the UI.
  useEffect(() => {
    if (!authed) {
      setMe(null);
      return;
    }
    fetchMe()
      .then(setMe)
      .catch(() => {});
  }, [authed]);

  // Seed with a REST snapshot before the first WebSocket frame.
  useEffect(() => {
    if (!authed) return;
    fetchSnapshot(CLUSTER)
      .then(setLiveSnapshot)
      .catch(() => {});
  }, [authed]);

  useEffect(() => {
    if (live) setLiveSnapshot(live);
  }, [live]);

  // While paused, fetch the historical snapshot at the selected timestamp.
  const targetTs = paused && metas && metas[effectiveIndex] ? metas[effectiveIndex].takenAt : null;
  useEffect(() => {
    if (!targetTs) return;
    let cancelled = false;
    fetchSnapshot(CLUSTER, targetTs)
      .then((s) => !cancelled && setHistSnapshot(s))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [targetTs]);

  // Replay: advance through history while playing.
  useEffect(() => {
    if (!playing) return;
    const id = setInterval(() => setIndex((i) => Math.min(i + 1, Math.max(0, n - 1))), 1000);
    return () => clearInterval(id);
  }, [playing, n]);

  // When replay reaches the end, snap back to live.
  useEffect(() => {
    if (playing && paused && n > 0 && index >= n - 1) {
      setPlaying(false);
      setPaused(false);
      setHistSnapshot(null);
    }
  }, [playing, paused, index, n]);

  if (!authed) {
    return <Login onAuthed={() => setAuthed(true)} />;
  }

  const displayed = paused ? histSnapshot : liveSnapshot;
  const rootCount = displayed?.roots?.length ?? 0;
  const waiterCount = displayed?.edges?.length ?? 0;

  const goLive = () => {
    setPaused(false);
    setPlaying(false);
    setHistSnapshot(null);
  };
  const seek = (i: number) => {
    setPaused(true);
    setIndex(i);
  };
  const togglePlay = () => {
    if (playing) {
      setPlaying(false);
      return;
    }
    setPaused(true);
    if (index >= n - 1) setIndex(0);
    setPlaying(true);
  };

  return (
    <div className="app">
      <header className="topbar">
        <span className="topbar__brand">pglockr</span>
        <span className="topbar__cluster">{CLUSTER}</span>
        <nav className="tabs">
          {(me?.role === "admin" ? [...BASE_TABS, AUDIT_TAB] : BASE_TABS).map((t) => (
            <button
              key={t.id}
              className={`tab ${view === t.id ? "tab--active" : ""}`}
              onClick={() => setView(t.id)}
            >
              {t.label}
            </button>
          ))}
        </nav>
        <span className={`topbar__conn topbar__conn--${state}`}>{state}</span>
        {view === "forest" && (
          <>
            <span className="topbar__stat">{rootCount} root blocker(s)</span>
            <span className="topbar__stat">{waiterCount} blocked edge(s)</span>
          </>
        )}
        <span className="topbar__spacer" />
        {me && (
          <span className="topbar__user" title={`role: ${me.role}`}>
            {me.name} <span className={`rolebadge rolebadge--${me.role}`}>{me.role}</span>
          </span>
        )}
        <button
          className="btn btn--ghost"
          onClick={() => {
            clearToken();
            setMe(null);
            setAuthed(false);
          }}
        >
          Sign out
        </button>
      </header>

      <main className="main">
        {view === "forest" && (
          <>
            <div className="forest-pane">
              <div className="graph">
                <ForestArea snapshot={displayed} selectedPid={selectedPid} onSelect={setSelectedPid} paused={paused} />
              </div>
              <Scrubber
                metas={metas ?? []}
                index={effectiveIndex}
                paused={paused}
                playing={playing}
                onSeek={seek}
                onLive={goLive}
                onTogglePlay={togglePlay}
              />
            </div>
            {selectedPid !== null && displayed && (
              <DetailPanel
                snapshot={displayed}
                pid={selectedPid}
                canAct={canAct(me?.role)}
                onClose={() => setSelectedPid(null)}
              />
            )}
          </>
        )}

        {view === "locks" && <LocksView cluster={CLUSTER} />}
        {view === "hot" && <HotObjectsView cluster={CLUSTER} />}
        {view === "audit" && me?.role === "admin" && <AuditView />}
      </main>
    </div>
  );
}

function ForestArea({
  snapshot,
  selectedPid,
  onSelect,
  paused,
}: {
  snapshot: Snapshot | null;
  selectedPid: number | null;
  onSelect: (pid: number) => void;
  paused: boolean;
}) {
  const hasGraph = useMemo(
    () => !!snapshot && ((snapshot.roots?.length ?? 0) > 0 || (snapshot.edges?.length ?? 0) > 0),
    [snapshot],
  );
  if (!snapshot) return <div className="empty">Waiting for first snapshot…</div>;
  if (!hasGraph) {
    return <div className="empty">{paused ? "No blocking at this moment." : "No blocking right now. 🎉"}</div>;
  }
  return (
    <ReactFlowProvider>
      <Forest snapshot={snapshot} selectedPid={selectedPid} onSelect={onSelect} />
    </ReactFlowProvider>
  );
}
