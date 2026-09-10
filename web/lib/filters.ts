import type { Event } from '@/types';

/** The class a status falls into, as the filter offers them. A request that
 * never got a response is `none` rather than a class of its own numbers: it is
 * what somebody hunting a connection fault is looking for. */
export type StatusClass = 'none' | '1xx' | '2xx' | '3xx' | '4xx' | '5xx';

export const statusClasses: StatusClass[] = ['none', '1xx', '2xx', '3xx', '4xx', '5xx'];

export interface Filters {
  /** Empty means every host. */
  host: string;
  /** Empty means every method. */
  method: string;
  /** Empty means every status. */
  statusClass: StatusClass | '';
  faultedOnly: boolean;
}

export const emptyFilters: Filters = { host: '', method: '', statusClass: '', faultedOnly: false };

export function statusClassOf(status: number): StatusClass {
  if (status === 0) return 'none';
  return `${Math.floor(status / 100)}xx` as StatusClass;
}

/** Narrows the events already held in the browser. Filtering happens here
 * rather than on the API because events arrive live over the socket: a
 * refetch would drop everything that came in since. */
export function filterEvents(events: Event[], f: Filters): Event[] {
  return events.filter(
    (e) =>
      (f.host === '' || e.host === f.host) &&
      (f.method === '' || e.method === f.method) &&
      (f.statusClass === '' || statusClassOf(e.status) === f.statusClass) &&
      (!f.faultedOnly || e.faulted),
  );
}

/** The distinct hosts seen so far, for the host filter to offer. */
export function hostsOf(events: Event[]): string[] {
  return [...new Set(events.map((e) => e.host))].sort();
}

/** The distinct methods seen so far, for the method filter to offer. */
export function methodsOf(events: Event[]): string[] {
  return [...new Set(events.map((e) => e.method))].sort();
}
