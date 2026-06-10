import { useEffect, useRef, useState } from "react";
import { getToken, streamURL } from "./api";
import type { Snapshot } from "./types";

export type ConnState = "connecting" | "open" | "closed";

// useStream subscribes to the live snapshot WebSocket with automatic reconnect
// and exponential backoff. It returns the latest snapshot and connection state.
export function useStream(cluster: string, enabled: boolean) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [state, setState] = useState<ConnState>("closed");
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    if (!enabled || !getToken()) {
      setState("closed");
      return;
    }

    let closedByUs = false;
    let backoff = 500;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    const connect = () => {
      setState("connecting");
      const ws = new WebSocket(streamURL(cluster));
      wsRef.current = ws;

      ws.onopen = () => {
        backoff = 500;
        setState("open");
      };
      ws.onmessage = (ev) => {
        try {
          setSnapshot(JSON.parse(ev.data) as Snapshot);
        } catch {
          /* ignore malformed frame */
        }
      };
      ws.onclose = () => {
        setState("closed");
        if (closedByUs) return;
        retryTimer = setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, 10_000);
      };
      ws.onerror = () => ws.close();
    };

    connect();

    return () => {
      closedByUs = true;
      if (retryTimer) clearTimeout(retryTimer);
      wsRef.current?.close();
    };
  }, [cluster, enabled]);

  return { snapshot, state };
}
