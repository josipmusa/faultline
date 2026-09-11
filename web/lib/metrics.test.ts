import { describe, expect, it } from 'vitest';

import type { Event } from '@/types';

import { bucketCount, series, windowMs } from './metrics';

const now = Date.parse('2026-09-11T12:01:00Z');

function event(secondsAgo: number, over: Partial<Event> = {}): Event {
  return {
    id: String(secondsAgo),
    timestamp: new Date(now - secondsAgo * 1000).toISOString(),
    host: 'httpbin.org',
    method: 'GET',
    path: '/get',
    status: 200,
    duration_ms: 12,
    bytes_in: 0,
    bytes_out: 42,
    faulted: false,
    tier: 'plain',
    ...over,
  };
}

describe('series', () => {
  it('covers the last sixty seconds, one bucket per second', () => {
    const points = series([], now, false);

    expect(points).toHaveLength(bucketCount);
    expect(points[points.length - 1].start).toBe(now - 1000);
    expect(points[0].start).toBe(now - windowMs);
  });

  it('counts a second of traffic and averages its latency', () => {
    const points = series(
      [event(5, { duration_ms: 10 }), event(5, { duration_ms: 30 }), event(9)],
      now,
      false,
    );
    const at = (secondsAgo: number) => points[bucketCount - secondsAgo];

    expect(at(5).requests).toBe(2);
    expect(at(5).avg_ms).toBe(20);
    expect(at(9).requests).toBe(1);
  });

  it('leaves an empty second without a latency rather than at zero', () => {
    const points = series([event(5)], now, false);

    expect(points[0]).toEqual({ start: now - windowMs, requests: 0, avg_ms: null });
  });

  it('drops events older than the window and any not yet a full second old', () => {
    const points = series([event(61), event(600), event(0)], now, false);

    expect(points.every((point) => point.requests === 0)).toBe(true);
  });

  it('counts every event, faulted and errored alike', () => {
    const points = series(
      [event(3, { faulted: true, status: 503, duration_ms: 2 }), event(3, { status: 0, duration_ms: 8 })],
      now,
      false,
    );

    expect(points[bucketCount - 3].requests).toBe(2);
    expect(points[bucketCount - 3].avg_ms).toBe(5);
  });

  it('starts at the oldest held event once the browser cap is reached', () => {
    const points = series([event(20), event(4)], now, true);

    expect(points).toHaveLength(20);
    expect(points[0].start).toBe(now - 20 * 1000);
    expect(points[points.length - 1].start).toBe(now - 1000);
  });

  it('shows the full window below the cap, where the empty seconds are real', () => {
    expect(series([event(20)], now, false)).toHaveLength(bucketCount);
  });

  it('keeps one bucket when the cap is reached inside a single second', () => {
    expect(series([event(1)], now, true)).toHaveLength(1);
  });

  it('holds the window open when the cap is reached with nothing in it', () => {
    expect(series([], now, true)).toHaveLength(bucketCount);
  });
});
