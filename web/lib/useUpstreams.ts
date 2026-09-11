'use client';

import { useCallback, useEffect, useState } from 'react';

import type { Upstream } from '@/types';

import { getUpstreams } from './api';

/** How often the list is re-read while it is on screen. The rows are counts
 * rather than a live feed, so a couple of seconds is soon enough to feel
 * current, and the panel refreshes itself after every action anyway. */
const pollMS = 2000;

export interface UpstreamsState {
  /** Null until the first read answers, so the panel can tell "not looked
   * yet" from "nothing seen". */
  upstreams: Upstream[] | null;
  error: string | null;
  refresh: () => void;
}

/** Reads the upstream list while active, and again every couple of seconds.
 *
 * The list is not derived from the event stream: bypassed hosts produce no
 * events at all, and the CA-trust hint is the server's reading of a handshake
 * that failed. Both exist only in the API's answer.
 *
 * A read that fails leaves the rows standing and reports the reason, so a
 * blink of the API does not empty the panel. */
export function useUpstreams(active: boolean): UpstreamsState {
  const [upstreams, setUpstreams] = useState<Upstream[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [asked, setAsked] = useState(0);

  const refresh = useCallback(() => setAsked((n) => n + 1), []);

  useEffect(() => {
    if (!active) {
      return;
    }

    let live = true;
    const read = () => {
      getUpstreams().then(
        (list) => {
          if (live) {
            setUpstreams(list);
            setError(null);
          }
        },
        (err: unknown) => {
          if (live) {
            setError(err instanceof Error ? err.message : String(err));
          }
        },
      );
    };

    read();
    const timer = setInterval(read, pollMS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [active, asked]);

  return { upstreams, error, refresh };
}
