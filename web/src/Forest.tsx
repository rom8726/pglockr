import { useEffect, useMemo, useRef } from "react";
import {
  Background,
  Controls,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type Edge as RFEdge,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { Snapshot } from "./types";
import { buildGraph, type SessionNode } from "./layout";
import { SessionNode as SessionNodeComponent } from "./SessionNode";

const nodeTypes = { session: SessionNodeComponent };

// structureSignature changes only when the set of nodes or edges changes, so we
// can keep existing node positions stable across ticks (no flicker) and only
// re-run the dagre layout when the forest's shape actually changes.
function structureSignature(snap: Snapshot): string {
  const involved = new Set<number>(snap.roots ?? []);
  for (const e of snap.edges ?? []) {
    involved.add(e.waiterPid);
    involved.add(e.blockerPid);
  }
  const pids = [...involved].sort((a, b) => a - b).join(",");
  const edges = (snap.edges ?? [])
    .map((e) => `${e.blockerPid}>${e.waiterPid}`)
    .sort()
    .join("|");
  return `${pids}#${edges}`;
}

export function Forest({
  snapshot,
  selectedPid,
  onSelect,
}: {
  snapshot: Snapshot;
  selectedPid: number | null;
  onSelect: (pid: number) => void;
}) {
  const [nodes, setNodes, onNodesChange] = useNodesState<SessionNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<RFEdge>([]);
  const lastSig = useRef<string>("");

  const sig = useMemo(() => structureSignature(snapshot), [snapshot]);

  useEffect(() => {
    const built = buildGraph(snapshot);
    if (sig !== lastSig.current) {
      // Shape changed: relayout from scratch.
      lastSig.current = sig;
      setNodes(built.nodes);
      setEdges(built.edges);
    } else {
      // Same shape: keep positions, refresh session data so timers/state update.
      const byId = new Map(built.nodes.map((n) => [n.id, n]));
      setNodes((prev) =>
        prev.map((n) => {
          const fresh = byId.get(n.id);
          return fresh ? { ...n, data: fresh.data } : n;
        }),
      );
      setEdges(built.edges);
    }
  }, [snapshot, sig, setNodes, setEdges]);

  const styledNodes = nodes.map((n) => ({ ...n, selected: n.id === String(selectedPid) }));

  return (
    <ReactFlow
      nodes={styledNodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onNodeClick={(_, node) => onSelect(Number(node.id))}
      fitView
      minZoom={0.2}
      proOptions={{ hideAttribution: true }}
    >
      <Background />
      <Controls />
    </ReactFlow>
  );
}
