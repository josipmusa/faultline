'use client';

import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';

import type { Event, Rule } from '@/types';

import { clearEvents as clearRecorder, getEvents, getRules, streamUrl } from './api';
import { EventStream } from './eventStream';

/** How many events the view keeps. The recorder's own ring is larger; this is
 * about what a browser can render, not about what Faultline remembers. */
const eventCap = 500;

export interface LiveState {
  events: Event[];
  rules: Rule[];
  connected: boolean;
  /** The view is holding as many events as it keeps, so there may be older
   * traffic it has already dropped. What the chart reads to know how far back
   * it can honestly draw. */
  atCap: boolean;
  /** Set when the API could not be reached, so the view can say why. */
  error: string | null;
  clear: () => Promise<void>;
}

/** Subscribes to the read-only event stream and keeps the backlog, the rule
 * list and the connection state in sync. A thin binding: the logic it drives
 * lives in EventStream and api, which are testable without React. */
export function useEventStream(): LiveState {
  const [rules, setRules] = useState<Rule[]>([]);
  const [error, setError] = useState<string | null>(null);

  const [stream] = useState(
    () =>
      new EventStream({
        // The address depends on the page's own location, which does not exist
        // while the export is prerendered. Nothing connects until the effect
        // below runs, and by then hydration has re-created this on the client.
        url: typeof window === 'undefined' ? '' : streamUrl(window.location.href),
        cap: eventCap,
        // Read on every connect, not only the first: the list has to belong
        // to the process now on the other end of the socket, since event ids
        // start again at 1 whenever the binary restarts.
        backlog: () =>
          getEvents(eventCap).then(
            (events) => {
              setError(null);
              return events;
            },
            (err: unknown) => {
              setError(message(err));
              throw err;
            },
          ),
      }),
  );

  const readRules = useCallback(() => {
    getRules()
      .then(setRules)
      .catch((err: unknown) => setError(message(err)));
  }, []);

  useEffect(() => stream.subscribeRulesChanged(readRules), [stream, readRules]);

  useEffect(() => {
    // The socket only carries what happens after it connects; the backlog
    // comes from the API, read by the stream itself on every open.
    readRules();
    stream.start();

    return () => stream.stop();
  }, [stream, readRules]);

  const events = useSyncExternalStore(
    (listener) => stream.subscribe(listener),
    () => stream.events,
    () => stream.events,
  );
  const connected = useSyncExternalStore(
    (listener) => stream.subscribe(listener),
    () => stream.connected,
    () => false,
  );

  const clear = useCallback(async () => {
    try {
      await clearRecorder();
      stream.clear();
      setError(null);
    } catch (err: unknown) {
      setError(message(err));
    }
  }, [stream]);

  return { events, rules, connected, atCap: events.length >= eventCap, error, clear };
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
