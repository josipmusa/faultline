import { describe, expect, it, vi } from 'vitest';

import type { Event } from '@/types';

import { EventStream, type Socket } from './eventStream';

/** A stand-in for the browser's WebSocket, driven by the test. */
class FakeSocket implements Socket {
  static opened: FakeSocket[] = [];

  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeSocket.opened.push(this);
  }

  close() {
    this.closed = true;
  }

  send(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  sendRaw(data: string) {
    this.onmessage?.({ data });
  }
}

function event(id: string, over: Partial<Event> = {}): Event {
  return {
    id,
    timestamp: '2026-09-10T10:00:00Z',
    host: 'api.stripe.com',
    method: 'GET',
    path: '/v1/charges',
    status: 200,
    duration_ms: 12,
    bytes_in: 0,
    bytes_out: 30,
    faulted: false,
    tier: 'plain',
    ...over,
  };
}

function newStream(opts: { cap?: number; onRulesChanged?: () => void } = {}) {
  FakeSocket.opened = [];
  const stream = new EventStream({
    url: 'ws://localhost:9000/api/events/stream',
    cap: opts.cap ?? 100,
    open: (url) => new FakeSocket(url),
  });
  if (opts.onRulesChanged) {
    stream.subscribeRulesChanged(opts.onRulesChanged);
  }
  stream.start();
  const socket = () => FakeSocket.opened[FakeSocket.opened.length - 1];
  return { stream, socket };
}

describe('event messages', () => {
  it('prepends each event, newest first', () => {
    const { stream, socket } = newStream();

    socket().send({ type: 'event', event: event('1') });
    socket().send({ type: 'event', event: event('2') });

    expect(stream.events.map((e) => e.id)).toEqual(['2', '1']);
  });

  it('caps the list, dropping the oldest', () => {
    const { stream, socket } = newStream({ cap: 2 });

    for (const id of ['1', '2', '3']) {
      socket().send({ type: 'event', event: event(id) });
    }

    expect(stream.events.map((e) => e.id)).toEqual(['3', '2']);
  });

  it('notifies subscribers', () => {
    const { stream, socket } = newStream();
    const seen = vi.fn();
    stream.subscribe(seen);

    socket().send({ type: 'event', event: event('1') });

    expect(seen).toHaveBeenCalled();
  });
});

describe('rules_changed', () => {
  // Rules never travel over the socket: the notice only says a cached list is
  // stale, and the client re-reads GET /api/rules.
  it('reports the notice without carrying a rule list', () => {
    const onRulesChanged = vi.fn();
    const { socket } = newStream({ onRulesChanged });

    socket().send({ type: 'rules_changed' });

    expect(onRulesChanged).toHaveBeenCalledTimes(1);
  });
});

describe('unknown frames', () => {
  // A new message kind arrives as a new type. Reading an unrecognised frame as
  // an event is what would make adding one a breaking change.
  it('ignores a type it does not know', () => {
    const { stream, socket } = newStream();

    socket().send({ type: 'upstreams_changed', upstreams: [] });

    expect(stream.events).toHaveLength(0);
  });

  it('ignores a frame that is not JSON', () => {
    const { stream, socket } = newStream();

    socket().sendRaw('not json');

    expect(stream.events).toHaveLength(0);
  });

  it('ignores an event frame with no event', () => {
    const { stream, socket } = newStream();

    socket().send({ type: 'event' });

    expect(stream.events).toHaveLength(0);
  });
});

describe('connection state', () => {
  it('is disconnected until the socket opens', () => {
    const { stream, socket } = newStream();
    expect(stream.connected).toBe(false);

    socket().onopen?.();

    expect(stream.connected).toBe(true);
  });

  it('reconnects after a close', () => {
    vi.useFakeTimers();
    try {
      const { stream, socket } = newStream();
      socket().onopen?.();

      socket().onclose?.();
      expect(stream.connected).toBe(false);
      vi.runOnlyPendingTimers();

      expect(FakeSocket.opened).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('backs off between attempts', () => {
    vi.useFakeTimers();
    try {
      const { socket } = newStream();

      socket().onclose?.();
      vi.advanceTimersByTime(1000);
      expect(FakeSocket.opened).toHaveLength(2);

      socket().onclose?.();
      vi.advanceTimersByTime(1000);
      expect(FakeSocket.opened).toHaveLength(2);
      vi.advanceTimersByTime(1000);
      expect(FakeSocket.opened).toHaveLength(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it('stops reconnecting once stopped', () => {
    vi.useFakeTimers();
    try {
      const { stream, socket } = newStream();
      const open = socket();

      stream.stop();
      open.onclose?.();
      vi.runOnlyPendingTimers();

      expect(open.closed).toBe(true);
      expect(FakeSocket.opened).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('clear', () => {
  it('empties the list and notifies', () => {
    const { stream, socket } = newStream();
    socket().send({ type: 'event', event: event('1') });
    const seen = vi.fn();
    stream.subscribe(seen);

    stream.clear();

    expect(stream.events).toHaveLength(0);
    expect(seen).toHaveBeenCalled();
  });
});
