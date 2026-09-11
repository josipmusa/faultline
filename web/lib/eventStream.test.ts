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

function newStream(
  opts: { cap?: number; onRulesChanged?: () => void; backlog?: () => Promise<Event[]> } = {},
) {
  FakeSocket.opened = [];
  const stream = new EventStream({
    url: 'ws://localhost:9000/api/events/stream',
    cap: opts.cap ?? 100,
    open: (url) => new FakeSocket(url),
    backlog: opts.backlog,
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

// Event ids are a per-process counter that starts again at 1 on every run, so
// a list spanning a restart holds two different events with the same id: the
// rows collide on their React key and the view stops matching its own data.
// The list therefore belongs to one connection, and every open re-reads it.
describe('re-seeding on connect', () => {
  it('reads the backlog when the socket first opens', async () => {
    const backlog = vi.fn(() => Promise.resolve([event('2'), event('1')]));
    const { stream, socket } = newStream({ backlog });

    socket().onopen?.();
    await vi.waitFor(() => expect(stream.events.map((e) => e.id)).toEqual(['2', '1']));
    expect(backlog).toHaveBeenCalledTimes(1);
  });

  it('replaces the list when the binary restarts under it', async () => {
    // The old process got to event 3; the new one starts again at 1.
    let answer: Event[] = [event('3'), event('2'), event('1')];
    const { stream, socket } = newStream({ backlog: () => Promise.resolve(answer) });

    socket().onopen?.();
    await vi.waitFor(() => expect(stream.events).toHaveLength(3));

    // The reconnect is on a timer, which the test drives rather than waits on.
    answer = [event('1', { path: '/after-the-restart' })];
    vi.useFakeTimers();
    try {
      socket().onclose?.();
      vi.runOnlyPendingTimers();
    } finally {
      vi.useRealTimers();
    }
    expect(FakeSocket.opened).toHaveLength(2);
    socket().onopen?.();

    await vi.waitFor(() => {
      expect(stream.events.map((e) => e.id)).toEqual(['1']);
      expect(stream.events[0].path).toBe('/after-the-restart');
    });
  });

  it('keeps events that arrived while the backlog was in flight', async () => {
    let release: (events: Event[]) => void = () => {};
    const backlog = () => new Promise<Event[]>((resolve) => (release = resolve));
    const { stream, socket } = newStream({ backlog });

    socket().onopen?.();
    socket().send({ type: 'event', event: event('9', { path: '/raced-in' }) });
    release([event('8'), event('7')]);

    await vi.waitFor(() => expect(stream.events.map((e) => e.id)).toEqual(['9', '8', '7']));
  });

  it('does not duplicate an event that is in the backlog as well', async () => {
    let release: (events: Event[]) => void = () => {};
    const backlog = () => new Promise<Event[]>((resolve) => (release = resolve));
    const { stream, socket } = newStream({ backlog });

    socket().onopen?.();
    socket().send({ type: 'event', event: event('8') });
    release([event('8'), event('7')]);

    await vi.waitFor(() => expect(stream.events.map((e) => e.id)).toEqual(['8', '7']));
  });

  it('is seeded once the first backlog has answered, and stays so across a reconnect', async () => {
    let release: (events: Event[]) => void = () => {};
    const backlog = () => new Promise<Event[]>((resolve) => (release = resolve));
    const { stream, socket } = newStream({ backlog });

    // Not yet: the socket is open but nothing has been read, so the view
    // cannot tell "no traffic" from "have not looked".
    expect(stream.seeded).toBe(false);
    socket().onopen?.();
    expect(stream.seeded).toBe(false);

    release([]);
    await vi.waitFor(() => expect(stream.seeded).toBe(true));

    // A drop later is a reconnect, not a fresh start.
    socket().onclose?.();
    expect(stream.seeded).toBe(true);
  });

  it('is not seeded by a backlog that could not be read', async () => {
    const { stream, socket } = newStream({ backlog: () => Promise.reject(new Error('offline')) });

    socket().onopen?.();
    await vi.waitFor(() => expect(FakeSocket.opened).toHaveLength(1));
    await Promise.resolve();
    expect(stream.seeded).toBe(false);
  });

  it('leaves the list alone when the backlog cannot be read', async () => {
    const { stream, socket } = newStream({ backlog: () => Promise.reject(new Error('offline')) });

    socket().send({ type: 'event', event: event('1') });
    socket().onopen?.();

    await vi.waitFor(() => expect(stream.events.map((e) => e.id)).toEqual(['1']));
  });
});
