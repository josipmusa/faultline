'use client';

import { useEffect, useState } from 'react';

import type { ConfigInfo } from '@/types';

import { getConfig } from './api';

/** Reads where rule changes go, once.
 *
 * Which file is in use is fixed when Faultline starts: the file's contents
 * change while the page is open, and the rule list follows them over the
 * stream, but the path cannot move, so there is nothing to poll for. A failed
 * read leaves this null and the header says nothing, which is the honest
 * answer: the error already reaches the user through whichever panel needed
 * the API to work. */
export function useConfig(): ConfigInfo | null {
  const [config, setConfig] = useState<ConfigInfo | null>(null);

  useEffect(() => {
    let live = true;
    getConfig().then(
      (read) => {
        if (live) {
          setConfig(read);
        }
      },
      () => {},
    );

    return () => {
      live = false;
    };
  }, []);

  return config;
}
