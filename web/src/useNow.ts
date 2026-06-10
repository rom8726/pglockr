import { useEffect, useState } from "react";

// useNow returns Date.now(), refreshed every second via a single shared timer,
// so all live wait-timers across the UI tick together without one interval per
// node.
let current = Date.now();
const subscribers = new Set<(n: number) => void>();
let timer: ReturnType<typeof setInterval> | null = null;

function ensureTimer() {
  if (timer) return;
  timer = setInterval(() => {
    current = Date.now();
    for (const fn of subscribers) fn(current);
  }, 1000);
}

export function useNow(): number {
  const [now, setNow] = useState(current);
  useEffect(() => {
    ensureTimer();
    subscribers.add(setNow);
    return () => {
      subscribers.delete(setNow);
      if (subscribers.size === 0 && timer) {
        clearInterval(timer);
        timer = null;
      }
    };
  }, []);
  return now;
}
