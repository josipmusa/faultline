'use client';

import { useEffect, useState } from 'react';

import type { Catalogue } from '@/types';

import { getCatalogue } from './api';

export interface CatalogueState {
  catalogue: Catalogue | null;
  error: string | null;
}

/** Reads what the binary can do to traffic, once.
 *
 * The catalogue is compiled into the binary serving this page, so it cannot
 * change while the page is open the way rules and upstreams do: there is
 * nothing to poll for. A failed read leaves the editor with nothing to render
 * and says why, since a form guessed at without the catalogue is exactly the
 * hardcoded list this endpoint exists to avoid. */
export function useCatalogue(active: boolean): CatalogueState {
  const [catalogue, setCatalogue] = useState<Catalogue | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!active || catalogue) {
      return;
    }

    let live = true;
    getCatalogue().then(
      (read) => {
        if (live) {
          setCatalogue(read);
          setError(null);
        }
      },
      (err: unknown) => {
        if (live) {
          setError(err instanceof Error ? err.message : String(err));
        }
      },
    );

    return () => {
      live = false;
    };
  }, [active, catalogue]);

  return { catalogue, error };
}
