import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  addBypass,
  ApiError,
  apiUrl,
  clearEvents,
  createRule,
  deleteRule,
  disableRule,
  enableRule,
  getCapture,
  getCatalogue,
  getEvents,
  getRule,
  getReport,
  getRules,
  getScenarios,
  getUpstreams,
  createScenario,
  removeBypass,
  setScenarioActive,
  streamUrl,
  updateRule,
} from './api';

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

describe('getCapture', () => {
  it('fetches the capture filed against an event', async () => {
    const fetchMock = stubFetch(
      respond({ event_id: '7', request: { headers: {}, truncated: false }, response: { headers: {}, truncated: false } }),
    );

    const got = await getCapture('7');

    expect(fetchMock).toHaveBeenCalledWith('/api/events/7/capture', expect.anything());
    expect(got.event_id).toBe('7');
  });

  // An event whose capture has aged out of the budget is a 404 the inspector
  // shows as a notice, so the error has to arrive intact rather than as a
  // generic failure.
  it('reports a capture the server no longer holds', async () => {
    stubFetch(respond({ error: 'the capture for event 7 is no longer held' }, { status: 404 }));

    await expect(getCapture('7')).rejects.toMatchObject({
      status: 404,
      message: 'the capture for event 7 is no longer held',
    });
  });

  it('escapes an id that is not URL safe', async () => {
    const fetchMock = stubFetch(respond({ event_id: 'a/b' }));

    await getCapture('a/b');

    expect(fetchMock).toHaveBeenCalledWith('/api/events/a%2Fb/capture', expect.anything());
  });
});

describe('getUpstreams', () => {
  it('reads the list as the API orders it', async () => {
    const fetchMock = stubFetch(
      respond([
        { host: 'api.stripe.com', requests: 2, faulted: 1, errors: 0, bypassed: false, last_seen: '' },
      ]),
    );

    const list = await getUpstreams();

    expect(fetchMock).toHaveBeenCalledWith('/api/upstreams', expect.anything());
    expect(list.map((u) => u.host)).toEqual(['api.stripe.com']);
  });
});

describe('the bypass list', () => {
  it('adds a host', async () => {
    const fetchMock = stubFetch(respond({ host: 'api.stripe.com' }, { status: 201 }));

    await addBypass('api.stripe.com');

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/bypass',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ host: 'api.stripe.com' }) }),
    );
  });

  // A host carries a port often enough that escaping it is the whole point.
  it('removes a host, escaping it into the path', async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(removeBypass('localhost:8080')).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/bypass/localhost%3A8080',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('reports what the API refused', async () => {
    stubFetch(respond({ error: 'localhost stays bypassed', field: 'host' }, { status: 409 }));

    await expect(removeBypass('localhost')).rejects.toThrow(ApiError);
  });
});

describe('rule writes', () => {
  it('creates a rule and returns it with any warnings', async () => {
    const fetchMock = stubFetch(
      respond(
        {
          id: 'delay-api-stripe-com',
          name: 'delay api.stripe.com',
          enabled: true,
          match: { host: 'api.stripe.com' },
          fault: { type: 'delay', ms: 2000 },
          warnings: ['api.stripe.com has only been seen encrypted'],
        },
        { status: 201 },
      ),
    );

    const created = await createRule({
      name: 'delay api.stripe.com',
      enabled: true,
      match: { host: 'api.stripe.com' },
      fault: { type: 'delay', ms: 2000 },
    });

    expect(fetchMock).toHaveBeenCalledWith('/api/rules', expect.objectContaining({ method: 'POST' }));
    expect(created.id).toBe('delay-api-stripe-com');
    expect(created.warnings).toHaveLength(1);
  });

  it('deletes a rule by id', async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(deleteRule('slow-stripe')).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/rules/slow-stripe',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
});

describe('the catalogue', () => {
  it('reads what the binary can do to traffic', async () => {
    const fetchMock = stubFetch(
      respond({
        faults: [
          {
            name: 'delay',
            tier: 'connection',
            fields: [{ name: 'ms', kind: 'integer', description: 'How long', required: true, min: 1 }],
          },
        ],
        behaviors: [
          { name: 'first_n', fields: [{ name: 'n', kind: 'integer', description: 'How many', required: true }] },
        ],
      }),
    );

    const catalogue = await getCatalogue();

    expect(fetchMock).toHaveBeenCalledWith('/api/catalogue', expect.anything());
    expect(catalogue.faults[0].fields[0].min).toBe(1);
    expect(catalogue.behaviors[0].name).toBe('first_n');
  });
});

describe('editing one rule', () => {
  const rule = {
    id: 'slow-stripe',
    name: 'Stripe is slow',
    enabled: true,
    match: { host: 'api.stripe.com' },
    fault: { type: 'delay', ms: 2000 },
  };

  it('reads a rule, which carries the warnings the list does not', async () => {
    const fetchMock = stubFetch(respond({ ...rule, warnings: ['only seen encrypted'] }));

    const got = await getRule('slow-stripe');

    expect(fetchMock).toHaveBeenCalledWith('/api/rules/slow-stripe', expect.anything());
    expect(got.warnings).toEqual(['only seen encrypted']);
  });

  it('replaces a rule at its id', async () => {
    const fetchMock = stubFetch(respond(rule));

    await updateRule('slow-stripe', rule);

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/rules/slow-stripe',
      expect.objectContaining({ method: 'PUT' }),
    );
  });

  it('enables and disables without sending the rest of the rule', async () => {
    // A Response body can only be read once, so each call needs its own.
    const fetchMock = vi.fn(() => Promise.resolve(respond({ ...rule, enabled: false })));
    vi.stubGlobal('fetch', fetchMock);

    await enableRule('slow-stripe');
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/rules/slow-stripe/enable',
      expect.objectContaining({ method: 'POST' }),
    );

    await disableRule('slow-stripe');
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/rules/slow-stripe/disable',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('escapes an id that is not URL safe', async () => {
    const fetchMock = stubFetch(respond(rule));

    await getRule('a/b');

    expect(fetchMock).toHaveBeenCalledWith('/api/rules/a%2Fb', expect.anything());
  });
});

describe('scenarios', () => {
  it('reads the list in the order the file declares it', async () => {
    const fetchMock = stubFetch(
      respond([
        { name: 'payments-down', rules: ['slow-stripe'], active: true },
        { name: 'orders-flaky', rules: [], active: false },
      ]),
    );

    const list = await getScenarios();

    expect(fetchMock).toHaveBeenCalledWith('/api/scenarios', expect.anything());
    expect(list.map((s) => s.name)).toEqual(['payments-down', 'orders-flaky']);
    expect(list[0].active).toBe(true);
  });

  it('turns one on and off through its own endpoints', async () => {
    const on = stubFetch(respond({ name: 'payments-down', rules: [], active: true }));
    await setScenarioActive('payments-down', true);
    expect(on).toHaveBeenCalledWith(
      '/api/scenarios/payments-down/activate',
      expect.objectContaining({ method: 'POST' }),
    );

    const off = stubFetch(respond({ name: 'payments-down', rules: [], active: false }));
    await setScenarioActive('payments-down', false);
    expect(off).toHaveBeenCalledWith(
      '/api/scenarios/payments-down/deactivate',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('escapes a name on its way into the path', async () => {
    const fetchMock = stubFetch(respond({ name: 'orders/flaky', rules: [], active: true }));

    await setScenarioActive('orders/flaky', true);

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/scenarios/orders%2Fflaky/activate',
      expect.anything(),
    );
  });

  it('creates one from a name and the rules it names', async () => {
    const fetchMock = stubFetch(
      respond({ name: 'everything-slow', rules: ['slow-stripe'], active: false }, { status: 201 }),
    );

    const created = await createScenario({ name: 'everything-slow', rules: ['slow-stripe'] });

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/scenarios',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ name: 'everything-slow', rules: ['slow-stripe'] }),
      }),
    );
    expect(created.active).toBe(false);
  });
});

describe('the session report', () => {
  it('reads the five figures the CLI prints', async () => {
    const fetchMock = stubFetch(
      respond({ total: 42, faulted: 12, retries: 8, max_retry_wait_ms: 1002, abandoned: 0 }),
    );

    const report = await getReport();

    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/current/report', expect.anything());
    expect(report.total).toBe(42);
    expect(report.max_retry_wait_ms).toBe(1002);
  });
});
