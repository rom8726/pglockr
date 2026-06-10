import { Handle, Position } from "@xyflow/react";
import { memo } from "react";
import type { Session } from "./types";
import { useNow } from "./useNow";
import { ageSeconds, formatDuration } from "./time";

// waitAge returns how long the session has been waiting: from waitStart (PG14+)
// or approximated from queryStart on older versions.
function waitAge(s: Session, now: number): number | null {
  const w = ageSeconds(s.waitStart, now);
  if (w !== null) return w;
  return ageSeconds(s.queryStart, now);
}

function role(s: Session): "root" | "waiting" | "neutral" {
  if (s.isRoot) return "root";
  if (s.blockedBy && s.blockedBy.length > 0) return "waiting";
  return "neutral";
}

export const SessionNode = memo(function SessionNode({
  data,
  selected,
}: {
  data: { session: Session };
  selected: boolean;
}) {
  const s = data.session;
  const now = useNow();
  const r = role(s);
  const idleInTxnRoot = r === "root" && s.state === "idle in transaction";
  const age = waitAge(s, now);

  return (
    <div
      className={[
        "node",
        `node--${r}`,
        idleInTxnRoot ? "node--idle-txn" : "",
        selected ? "node--selected" : "",
      ].join(" ")}
    >
      <Handle type="target" position={Position.Top} />
      <div className="node__head">
        <span className="node__role">{r === "root" ? "ROOT" : r === "waiting" ? "WAITING" : ""}</span>
        <span className="node__pid">pid {s.pid}</span>
      </div>
      <div className="node__user">
        {s.user || "?"}
        {s.appName ? ` · ${s.appName}` : ""}
      </div>
      <div className="node__meta">
        <span className={idleInTxnRoot ? "node__state node__state--alert" : "node__state"}>
          {s.state || "—"}
        </span>
        {age !== null && <span className="node__age">⏱ {formatDuration(age)}</span>}
      </div>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
});
