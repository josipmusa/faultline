import { ApiError, UnreachableError, WaitTimeoutError } from './errors.js';
import type { Event, EventQuery, ReportResult, Rule, RuleResult, Scenario } from './types.js';

/** Where `faultline serve` puts the admin API. */
export const DEFAULT_ADDR = 'http://localhost:9000';

/** How long an ordinary API call is given before it is abandoned. */
const REQUEST_TIMEOUT_MS = 30_000;

/**
 * How often a wait re-reads the events. A test waiting for a call to happen
 * cannot tell a tenth of a second from none.
 */
const POLL_EVERY_MS = 100;

export interface ClientOptions {
  /** Bounds a single API call. Defaults to 30 seconds. */
  timeoutMs?: number;
  /** The fetch to use. Defaults to the global one, and exists for tests of this client. */
  fetch?: typeof globalThis.fetch;
}

/**
 * A thin typed client for one running Faultline, for driving it from a test:
 * inject a fault, let the application meet it, and read what it did.
 *
 * Every method is one API call. There is no business logic here and no state
 * beyond the address, so what a test asserts is what the API said.
 */
export class FaultlineClient {
  readonly addr: string;
  private readonly timeoutMs: number;
  private readonly fetchImpl: typeof globalThis.fetch;

  /**
   * A bare host and port is taken as http, so both `localhost:9000` and
   * `http://localhost:9000` work.
   */
  constructor(addr: string = DEFAULT_ADDR, options: ClientOptions = {}) {
    this.addr = normalizeAddr(addr);
    this.timeoutMs = options.timeoutMs ?? REQUEST_TIMEOUT_MS;
    this.fetchImpl = options.fetch ?? globalThis.fetch;
  }

  /** Creates a rule and returns it as stored, with its id and any warnings about it. */
  async addRule(rule: Rule): Promise<RuleResult> {
    return this.request<RuleResult>('POST', '/api/rules', rule);
  }

  /** Removes a rule. */
  async removeRule(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/rules/${encodeURIComponent(id)}`);
  }

  /** Turns a rule on or off, which also re-arms its behavior state. */
  async setRuleEnabled(id: string, enabled: boolean): Promise<RuleResult> {
    const action = enabled ? 'enable' : 'disable';
    return this.request<RuleResult>('POST', `/api/rules/${encodeURIComponent(id)}/${action}`);
  }

  /** Lists the scenarios the configuration file declares, and says which one is active. */
  async scenarios(): Promise<Scenario[]> {
    return this.request<Scenario[]>('GET', '/api/scenarios');
  }

  /**
   * Activates or deactivates a scenario. Activating one deactivates whichever
   * was active and re-arms the behavior state of the rules it turns on, so a
   * rehearsal starts from the beginning.
   */
  async setScenarioActive(name: string, active: boolean): Promise<Scenario> {
    const action = active ? 'activate' : 'deactivate';
    return this.request<Scenario>('POST', `/api/scenarios/${encodeURIComponent(name)}/${action}`);
  }

  /** Reads what the ring buffer holds, oldest first. */
  async events(query: EventQuery = {}): Promise<Event[]> {
    return this.request<Event[]>('GET', `/api/events${queryString(query)}`);
  }

  /**
   * Resolves with the first event matching the query that is recorded after the
   * call begins, or rejects with a {@link WaitTimeoutError} if none arrives in
   * time. Events already in the ring are ignored: the question is what happens
   * next.
   */
  async waitForEvent(query: EventQuery = {}, timeoutMs = 10_000): Promise<Event> {
    const deadline = Date.now() + timeoutMs;
    const watermark = await this.latestId();
    const { limit: _limit, ...filter } = query;

    for (;;) {
      // Read before the first pause, so an event recorded between the watermark
      // and here is found without waiting out an interval.
      for (const event of await this.events(filter)) {
        if (isAfter(event.id, watermark)) {
          return event;
        }
      }
      if (Date.now() >= deadline) {
        throw new WaitTimeoutError(timeoutMs);
      }
      await sleep(Math.min(POLL_EVERY_MS, deadline - Date.now()));
    }
  }

  /**
   * Reads the current session's report: what Faultline has seen since it
   * started, or since the session was last reset.
   */
  async report(): Promise<ReportResult> {
    return this.request<ReportResult>('GET', '/api/sessions/current/report');
  }

  /**
   * Puts the session back to the start of a measurement: what Faultline
   * observed is cleared and every rule's behavior state is re-armed, so a spent
   * `first_n` applies again. The rules themselves survive, which is what makes
   * this the right call between two tests sharing one instance.
   */
  async resetSession(): Promise<void> {
    await this.request<void>('POST', '/api/sessions/current/reset');
  }

  /**
   * The id of the newest event in the ring, which is the watermark a wait counts
   * from. The read is unfiltered: any event advances the counter.
   */
  private async latestId(): Promise<number> {
    const [newest] = await this.events({ limit: 1 });
    return newest ? Number.parseInt(newest.id, 10) : 0;
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { accept: 'application/json' };
    if (body !== undefined) {
      headers['content-type'] = 'application/json';
    }

    let response: Response;
    try {
      response = await this.fetchImpl(this.addr + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: AbortSignal.timeout(this.timeoutMs),
      });
    } catch (cause) {
      throw this.reachError(cause);
    }

    if (!response.ok) {
      throw await apiErrorFrom(response);
    }
    if (response.status === 204 || response.headers.get('content-length') === '0') {
      return undefined as T;
    }
    return (await response.json()) as T;
  }

  /** Keeps a transport failure to one line, and names the usual cause of it. */
  private reachError(cause: unknown): Error {
    if (cause instanceof DOMException && cause.name === 'TimeoutError') {
      return new Error(`faultline at ${this.addr} did not answer within ${this.timeoutMs}ms`, { cause });
    }
    return new UnreachableError(this.addr, cause);
  }
}

/** Turns a refusal into an ApiError, falling back to the status line for a body that is not the documented shape. */
async function apiErrorFrom(response: Response): Promise<ApiError> {
  try {
    const body = (await response.json()) as { error?: string; field?: string };
    if (body?.error) {
      return new ApiError(response.status, body.error, body.field);
    }
  } catch {
    // Not the documented error shape; the status line is all there is.
  }
  return new ApiError(response.status, `faultline answered ${response.status} ${response.statusText}`.trim());
}

function normalizeAddr(addr: string): string {
  const trimmed = addr.trim();
  if (trimmed === '') {
    throw new Error(`the address is empty, want something like ${DEFAULT_ADDR}`);
  }

  const url = new URL(trimmed.includes('://') ? trimmed : `http://${trimmed}`);
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(`address ${JSON.stringify(addr)} needs an http or https scheme`);
  }
  return `${url.protocol}//${url.host}${url.pathname.replace(/\/$/, '')}`;
}

function queryString(query: EventQuery): string {
  const params = new URLSearchParams();
  if (query.limit !== undefined && query.limit > 0) {
    params.set('limit', String(query.limit));
  }
  if (query.host !== undefined && query.host !== '') {
    params.set('host', query.host);
  }
  if (query.faulted !== undefined) {
    params.set('faulted', String(query.faulted));
  }
  const encoded = params.toString();
  return encoded === '' ? '' : `?${encoded}`;
}

/**
 * Whether an id was minted after the watermark. Ids are decimal counters, so
 * they are compared as numbers: `10` is after `9`, which string order gets
 * backwards. An id that is not a number cannot be placed and counts as new, so
 * a wait never stalls on one.
 */
function isAfter(id: string, watermark: number): boolean {
  const n = Number.parseInt(id, 10);
  return Number.isNaN(n) || n > watermark;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, Math.max(ms, 0)));
}
