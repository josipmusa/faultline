/** The part of Storage the preference needs. Narrow so the tests can hand in
 * a map, and so a browser that blocks storage can hand in nothing. */
export interface PreferenceStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export const sidebarKey = 'faultline.sidebar';

/** Whether the nav should show its labels. Expanded on a first visit, since
 * somebody new is the person who most needs to read what the icons are; the
 * choice is then remembered per browser. A store that is missing, refuses to
 * be read, or holds something this code never wrote counts as unset. */
export function readSidebarExpanded(store: PreferenceStore | null): boolean {
  try {
    return store?.getItem(sidebarKey) !== 'collapsed';
  } catch {
    return true;
  }
}

/** Remembers the choice. A store that refuses the write loses only the
 * memory of it: the nav is already in the state the user asked for. */
export function writeSidebarExpanded(store: PreferenceStore | null, expanded: boolean): void {
  try {
    store?.setItem(sidebarKey, expanded ? 'expanded' : 'collapsed');
  } catch {
    // Storage full or blocked; the preference is not worth an error.
  }
}
