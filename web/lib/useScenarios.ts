'use client';

import { useCallback, useEffect, useState } from 'react';

import type { Report, Scenario } from '@/types';

import { getReport, getScenarios } from './api';

/** How often the panel re-reads. The report is a live reading of the recorder
 * and moves with the traffic, so a second keeps the counts honest without
 * making the panel a poller of anything expensive: both endpoints answer from
 * memory. */
const pollMS = 1000;

export interface SessionState {
  /** Null until the first read answers, so the panel can tell "not looked
   * yet" from "no scenarios". */
  scenarios: Scenario[] | null;
  /** Null until the first read answers, so the panel can tell "nothing yet"
   * from "a session that has seen nothing". */
  report: Report | null;
  error: string | null;
  refresh: () => void;
}

/** Reads the scenarios and the session report while the panel is on screen.
 *
 * Neither comes off the event stream. Which scenario is active is per run
 * state the socket never carries, and the report counts retries and abandoned
 * attempts over the recorder's whole ring rather than over the events this
 * view keeps, so both are the API's answer or nothing.
 *
 * A read that fails leaves what is on screen standing and says why, so a blink
 * of the API does not empty the panel. */
export function useScenarios(active: boolean): SessionState {
  const [scenarios, setScenarios] = useState<Scenario[] | null>(null);
  const [report, setReport] = useState<Report | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [asked, setAsked] = useState(0);

  const refresh = useCallback(() => setAsked((n) => n + 1), []);

  useEffect(() => {
    if (!active) {
      return;
    }

    let live = true;
    const read = () => {
      Promise.all([getScenarios(), getReport()]).then(
        ([list, current]) => {
          if (live) {
            setScenarios(list);
            setReport(current);
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

  return { scenarios, report, error, refresh };
}
