import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError, apiUrl, clearEvents, getEvents, getRules, streamUrl } from './api';

function respond(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });
}

function stubFetch(res: Response | Promise<Response>) {
  const fetchMock = vi.fn(() => Promise.resolve(res));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('base URL resolution', () => {
  // The embedded build is served by the binary itself, so a same-origin path
  // is what works there; the override exists for `npm run dev` on :3000.
  it('is same-origin when no override is set', () => {
    expect(apiUrl('/api/events')).toBe('/api/events');
  });

  it('derives the stream URL from the page when no override is set', () => {
    expect(streamUrl('http://localhost:9000/')).toBe('ws://localhost:9000/api/events/stream');
  });

  it('keeps the secure scheme on an https page', () => {
    expect(streamUrl('https://faultline.test/')).toBe('wss://faultline.test/api/events/stream');
  });
});

describe('getEvents', () => {
  it('asks for the newest events and returns them newest first', async () => {
    const fetchMock = stubFetch(
      respond([
        { id: '1', host: 'a', method: 'GET', path: '/', status: 200 },
        { id: '2', host: 'a', method: 'GET', path: '/', status: 200 },
      ]),
    );

    const events = await getEvents(50);

    expect(fetchMock).toHaveBeenCalledWith('/api/events?limit=50', expect.anything());
    expect(events.map((e) => e.id)).toEqual(['2', '1']);
  });
});

describe('clearEvents', () => {
  // DELETE /api/events answers 204 with no body, so parsing one would throw.
  it('accepts an empty response', async () => {
    stubFetch(new Response(null, { status: 204 }));

    await expect(clearEvents()).resolves.toBeUndefined();
  });
});

describe('getRules', () => {
  it('returns the rule list', async () => {
    stubFetch(respond([{ id: 'slow', name: 'Slow', enabled: true, match: {}, fault: { type: 'delay', ms: 10 } }]));

    const rules = await getRules();

    expect(rules).toHaveLength(1);
    expect(rules[0].fault.type).toBe('delay');
  });
});

describe('failures', () => {
  // Errors are `{ "error": ..., "field": ... }`, and the field is the useful
  // half: it names what the caller has to fix.
  it('throws the API error with its message and field', async () => {
    stubFetch(respond({ error: 'ms must be positive', field: 'fault.ms' }, { status: 400 }));

    const err = await getRules().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe('ms must be positive');
    expect((err as ApiError).field).toBe('fault.ms');
    expect((err as ApiError).status).toBe(400);
  });

  it('falls back to the status when the body is not an error envelope', async () => {
    stubFetch(new Response('<html>gateway</html>', { status: 502 }));

    const err = await getRules().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toContain('502');
  });
});
