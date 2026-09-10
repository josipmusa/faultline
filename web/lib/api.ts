import type { Capture, CreatedRule, Event, Rule, Upstream } from '@/types';

/** Where the API lives. Empty means same origin, which is the embedded build:
 * the binary serves both the UI and the API on the admin port. `npm run dev`
 * on :3000 sets the override, because Next rewrites are unavailable under
 * `output: 'export'` and so cannot be the mechanism here. */
const BASE = process.env.NEXT_PUBLIC_FAULTLINE_API ?? '';

export function apiUrl(path: string): string {
  return `${BASE}${path}`;
}

/** The event stream's address. With no override it follows the page, so the
 * UI keeps working on a non-default admin port or through a tunnel. */
export function streamUrl(href: string): string {
  const base = new URL(BASE || href, href);
  base.protocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
  return new URL('/api/events/stream', base).toString();
}

/** A failed request, carrying the API's own explanation. `field` is the dotted
 * path into the request that was rejected, when the API named one. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly field?: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

interface ErrorEnvelope {
  error?: string;
  field?: string;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(apiUrl(path), { headers: { Accept: 'application/json' }, ...init });

  if (!res.ok) {
    throw await apiError(res);
  }
  // 204 carries no body, so parsing one would throw. DELETE /api/events is the
  // one such endpoint the UI calls today.
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

async function apiError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as ErrorEnvelope;
    if (body.error) {
      return new ApiError(body.error, res.status, body.field);
    }
  } catch {
    // Not an error envelope: a proxy or a crash answered instead of the API.
  }
  return new ApiError(`request to ${res.url || 'the API'} failed with ${res.status}`, res.status);
}

/** The most recent events, newest first. The API returns them oldest first,
 * trimming to the newest `limit`; the stream prepends, so the list is ordered
 * the same way whichever it came from. */
export async function getEvents(limit: number): Promise<Event[]> {
  const events = await request<Event[]>(`/api/events?limit=${limit}`);
  return events.reverse();
}

export function clearEvents(): Promise<void> {
  return request<void>('/api/events', { method: 'DELETE' });
}

export function getRules(): Promise<Rule[]> {
  return request<Rule[]>('/api/rules');
}

/** The headers and bodies recorded for one event. A 404 means the event is
 * unknown, or its capture has aged out of the capture budget, or the traffic
 * was encrypted and there was never one; the message says which. */
export function getCapture(id: string): Promise<Capture> {
  return request<Capture>(`/api/events/${encodeURIComponent(id)}/capture`);
}

/** The hosts Faultline has seen, in the order the API sorts them. A bypassed
 * host is in the list too, which is how the UI can say that nothing is being
 * recorded for it on purpose. */
export function getUpstreams(): Promise<Upstream[]> {
  return request<Upstream[]>('/api/upstreams');
}

/** Puts a host on the forward proxy's bypass list, so its traffic is passed
 * through untouched from the next request on. */
export function addBypass(host: string): Promise<{ host: string }> {
  return request<{ host: string }>('/api/bypass', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ host }),
  });
}

/** Takes a host off the bypass list, so it is proxied and recorded again. */
export function removeBypass(host: string): Promise<void> {
  return request<void>(`/api/bypass/${encodeURIComponent(host)}`, { method: 'DELETE' });
}

/** Creates a rule. The id is the API's to derive when the rule does not carry
 * one, and the response may carry warnings: a response-tier fault on a host
 * only ever seen encrypted is accepted and cannot apply yet. */
export function createRule(rule: Omit<Rule, 'id'> & { id?: string }): Promise<CreatedRule> {
  return request<CreatedRule>('/api/rules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rule),
  });
}

export function deleteRule(id: string): Promise<void> {
  return request<void>(`/api/rules/${encodeURIComponent(id)}`, { method: 'DELETE' });
}
