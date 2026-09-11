'use client';

import { useEffect, useMemo, useState } from 'react';

import type { Event } from '@/types';

import { bucketMs, series, type Point } from './metrics';

/**
 * The trailing window over the events it is given, recomputed as each second
 * completes and whenever an event arrives.
 *
 * Nothing is read from the server: the chart is the same traffic the rows
 * below it are, counted a second at a time, so the two cannot tell different
 * stories about one session. `atCap` belongs to the whole event list rather
 * than to the filtered slice passed in here, since it is about what the
 * browser has dropped, not about what a filter is hiding.
 */
export function useMetrics(events: Event[], atCap: boolean): Point[] {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), bucketMs);
    return () => clearInterval(timer);
  }, []);

  return useMemo(() => series(events, now, atCap), [events, now, atCap]);
}
