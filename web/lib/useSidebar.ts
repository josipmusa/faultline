'use client';

import { useCallback, useSyncExternalStore } from 'react';

import { readSidebarExpanded, writeSidebarExpanded, type PreferenceStore } from './sidebar';

const listeners = new Set<() => void>();

/** The choice made this page load, which wins over the store: a browser that
 * blocks storage still gets a nav that opens and closes, it just forgets. */
let chosen: boolean | null = null;

/** The browser's storage, or null where reaching for it throws: a private
 * window with storage blocked, or a sandboxed frame. */
function storage(): PreferenceStore | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function snapshot(): boolean {
  return chosen ?? readSidebarExpanded(storage());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Whether the nav shows its labels, and a toggle. An external store rather
 * than state: the page is a static export, so the first render has no storage
 * to read, and this is how React reconciles a server value with the
 * browser's without a flash or a hydration mismatch. */
export function useSidebarExpanded(): [boolean, () => void] {
  const expanded = useSyncExternalStore(subscribe, snapshot, () => true);

  const toggle = useCallback(() => {
    chosen = !snapshot();
    writeSidebarExpanded(storage(), chosen);
    for (const listener of listeners) {
      listener();
    }
  }, []);

  return [expanded, toggle];
}
