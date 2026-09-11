import { describe, expect, it } from 'vitest';

import { readSidebarExpanded, sidebarKey, writeSidebarExpanded } from './sidebar';

/** The part of Storage the preference uses, driven by the test. */
function store(values: Record<string, string> = {}) {
  const map = new Map(Object.entries(values));
  return {
    map,
    getItem: (key: string) => map.get(key) ?? null,
    setItem: (key: string, value: string) => void map.set(key, value),
  };
}

describe('readSidebarExpanded', () => {
  it('is expanded on a first visit, when nothing is stored', () => {
    expect(readSidebarExpanded(store())).toBe(true);
  });

  it('is collapsed when that was the last choice', () => {
    expect(readSidebarExpanded(store({ [sidebarKey]: 'collapsed' }))).toBe(false);
  });

  it('is expanded when that was the last choice', () => {
    expect(readSidebarExpanded(store({ [sidebarKey]: 'expanded' }))).toBe(true);
  });

  it('is expanded when the store is missing or refuses to be read', () => {
    expect(readSidebarExpanded(null)).toBe(true);
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {},
    };
    expect(readSidebarExpanded(broken)).toBe(true);
  });

  it('treats a value it did not write as unset', () => {
    expect(readSidebarExpanded(store({ [sidebarKey]: 'sideways' }))).toBe(true);
  });
});

describe('writeSidebarExpanded', () => {
  it('writes the choice so the next visit reads it back', () => {
    const s = store();
    writeSidebarExpanded(s, false);
    expect(readSidebarExpanded(s)).toBe(false);
    writeSidebarExpanded(s, true);
    expect(readSidebarExpanded(s)).toBe(true);
  });

  it('swallows a store that refuses to be written', () => {
    const broken = {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota');
      },
    };
    expect(() => writeSidebarExpanded(broken, false)).not.toThrow();
    expect(() => writeSidebarExpanded(null, false)).not.toThrow();
  });
});
