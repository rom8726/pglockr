import type { SnapshotMeta } from "./types";

// Scrubber is the history timeline: a strip of per-snapshot bars (height by
// blocked-edge count, red when there are root blockers) plus play/live
// controls. Clicking a bar seeks; "Live" returns to the live stream.
export function Scrubber({
  metas,
  index,
  paused,
  playing,
  onSeek,
  onLive,
  onTogglePlay,
}: {
  metas: SnapshotMeta[];
  index: number; // effective index being viewed
  paused: boolean;
  playing: boolean;
  onSeek: (i: number) => void;
  onLive: () => void;
  onTogglePlay: () => void;
}) {
  const n = metas.length;
  const maxEdges = Math.max(1, ...metas.map((m) => m.edges));
  const current = metas[index];

  return (
    <div className="scrubber">
      <div className="scrubber__controls">
        <button className="btn" onClick={onTogglePlay} disabled={n === 0} title="Replay history">
          {playing ? "⏸" : "▶"}
        </button>
        <button
          className={`btn ${paused ? "" : "btn--live"}`}
          onClick={onLive}
          title="Follow live"
        >
          ● Live
        </button>
      </div>

      <div className="scrubber__track" role="slider" aria-valuemin={0} aria-valuemax={Math.max(0, n - 1)} aria-valuenow={index}>
        {metas.map((m, i) => {
          const h = 20 + (m.edges / maxEdges) * 80; // 20%..100%
          const cls = [
            "scrubber__bar",
            m.roots > 0 ? "scrubber__bar--danger" : m.edges > 0 ? "scrubber__bar--warn" : "",
            i === index ? "scrubber__bar--current" : "",
          ].join(" ");
          return (
            <button
              key={m.takenAt + i}
              className={cls}
              style={{ height: `${h}%` }}
              onClick={() => onSeek(i)}
              title={`${new Date(m.takenAt).toLocaleTimeString()} · ${m.roots} roots, ${m.edges} blocked`}
            />
          );
        })}
        {n === 0 && <span className="scrubber__empty">collecting history…</span>}
      </div>

      <div className="scrubber__label">
        {paused ? (
          <>
            <span className="scrubber__badge scrubber__badge--paused">PAUSED</span>
            {current && <span className="mono">{new Date(current.takenAt).toLocaleTimeString()}</span>}
            <span className="scrubber__pos">
              {n > 0 ? index + 1 : 0}/{n}
            </span>
          </>
        ) : (
          <>
            <span className="scrubber__badge scrubber__badge--live">LIVE</span>
            {current && <span className="mono">{new Date(current.takenAt).toLocaleTimeString()}</span>}
          </>
        )}
      </div>
    </div>
  );
}
