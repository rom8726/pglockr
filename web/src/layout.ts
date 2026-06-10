import dagre from "dagre";
import type { Edge as RFEdge, Node as RFNode } from "@xyflow/react";
import { MarkerType, Position } from "@xyflow/react";
import type { Session, Snapshot } from "./types";

export const NODE_W = 240;
export const NODE_H = 96;

export type SessionNode = RFNode<{ session: Session }>;

// buildGraph turns a snapshot into React Flow nodes/edges laid out top-down with
// dagre. Only sessions involved in the blocking forest (a root, or appearing in
// an edge) are shown. Edges run blocker -> waiter so roots sit at the top.
export function buildGraph(snap: Snapshot): { nodes: SessionNode[]; edges: RFEdge[] } {
  const edges = snap.edges ?? [];
  const roots = new Set(snap.roots ?? []);

  const involved = new Set<number>(roots);
  for (const e of edges) {
    involved.add(e.waiterPid);
    involved.add(e.blockerPid);
  }

  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: "TB", nodesep: 40, ranksep: 80 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const pid of involved) {
    g.setNode(String(pid), { width: NODE_W, height: NODE_H });
  }
  for (const e of edges) {
    // dagre source is the upper rank: blocker on top, waiter below.
    g.setEdge(String(e.blockerPid), String(e.waiterPid));
  }
  dagre.layout(g);

  const nodes: SessionNode[] = [];
  for (const pid of involved) {
    const session = snap.sessions[String(pid)];
    if (!session) continue;
    const pos = g.node(String(pid));
    nodes.push({
      id: String(pid),
      type: "session",
      position: { x: pos.x - NODE_W / 2, y: pos.y - NODE_H / 2 },
      data: { session },
      sourcePosition: Position.Bottom,
      targetPosition: Position.Top,
    });
  }

  const rfEdges: RFEdge[] = edges.map((e) => ({
    id: `${e.blockerPid}->${e.waiterPid}`,
    source: String(e.blockerPid),
    target: String(e.waiterPid),
    label: edgeLabel(e.relation, e.blockerMode, e.waiterMode, e.lockType),
    labelBgPadding: [6, 3],
    labelBgBorderRadius: 4,
    markerEnd: { type: MarkerType.ArrowClosed },
    animated: true,
  }));

  return { nodes, edges: rfEdges };
}

function edgeLabel(rel: string, blockerMode: string, waiterMode: string, lockType: string): string {
  const obj = rel || lockType || "?";
  const modes = blockerMode && waiterMode ? `${shortMode(blockerMode)} ⛔ ${shortMode(waiterMode)}` : "";
  return modes ? `${obj}\n${modes}` : obj;
}

// shortMode trims the "Lock" suffix for readability (AccessExclusiveLock -> AccessExclusive).
function shortMode(m: string): string {
  return m.endsWith("Lock") ? m.slice(0, -4) : m;
}
