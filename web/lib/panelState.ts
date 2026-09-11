/** What a list panel should show in its rows area.
 *
 * `loading` until the first read answers, so a panel never claims "nothing
 * yet" before it knows; `error` when that first read failed and there is
 * nothing else to show; `empty` once a read has answered with nothing; `rows`
 * otherwise. A read that fails after rows are on screen leaves them standing,
 * with the panel's notice saying why, so the failure is not a state here. */
export type PanelState = 'loading' | 'error' | 'empty' | 'rows';

export function panelState(list: readonly unknown[] | null, error: string | null): PanelState {
  if (list === null) {
    return error === null ? 'loading' : 'error';
  }
  return list.length === 0 ? 'empty' : 'rows';
}
