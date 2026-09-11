import type { Event } from '@/types';

/** How far back the chart looks. Sixty seconds is long enough that a rule
 * switched on part way through shows as an edge with traffic on both sides of
 * it, and short enough that the edge is still on screen a moment later. */
export const windowMs = 60_000;

/** One point per second, so a change in traffic arrives as a step rather than
 * drifting in over the whole window. */
export const bucketMs = 1_000;

export const bucketCount = windowMs / bucketMs;

/** One second of traffic. `start` is epoch ms, which is what the chart's x
 * axis is drawn against. `avg_ms` is null for a second with no requests: an
 * empty second has no latency, and drawing it as zero would invent a dip the
 * traffic never had. */
export interface Point {
  start: number;
  requests: number;
  avg_ms: number | null;
}

/**
 * Buckets the events into the trailing window, newest bucket last.
 *
 * Every event counts, faulted and errored alike: a refused request is a real
 * request with a real, short wait, and that contrast against a delay is what
 * the chart is for. Latency is `duration_ms` as recorded, so the chart and the
 * stream's duration column cannot disagree.
 *
 * The second in progress is left out. It is only partly over, so its count
 * would read as a drop at the leading edge on every tick.
 *
 * `atCap` says the browser is holding as many events as it keeps, which means
 * there may be older traffic it has already dropped. The window then begins at
 * the oldest event still held, rather than drawing seconds it cannot see as
 * empty ones. Below the cap the whole window is shown, because those empty
 * seconds are real.
 */
export function series(events: Event[], now: number, atCap: boolean): Point[] {
  const end = Math.floor(now / bucketMs) * bucketMs - bucketMs;
  const count = atCap ? cappedCount(events, end) : bucketCount;
  const start = end - (count - 1) * bucketMs;

  const requests = new Array<number>(count).fill(0);
  const total = new Array<number>(count).fill(0);

  for (const event of events) {
    const at = Date.parse(event.timestamp);
    const index = Math.floor((at - start) / bucketMs);
    if (index < 0 || index >= count) {
      continue;
    }
    requests[index] += 1;
    total[index] += event.duration_ms;
  }

  return requests.map((n, index) => ({
    start: start + index * bucketMs,
    requests: n,
    avg_ms: n === 0 ? null : total[index] / n,
  }));
}

/** How many seconds back the held events actually reach. A cap reached with
 * nothing in it says nothing about what is missing, so the window stays whole;
 * a cap reached inside a single second still gets one bucket, since the
 * traffic that filled it is the busiest the chart will ever show. */
function cappedCount(events: Event[], end: number): number {
  if (events.length === 0) {
    return bucketCount;
  }
  const oldest = Math.min(...events.map((event) => Date.parse(event.timestamp)));
  const reach = Math.floor((end - oldest) / bucketMs) + 1;
  return Math.min(bucketCount, Math.max(1, reach));
}
