import { mkdtemp, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { ApiError, FaultlineClient, UnreachableError, WaitTimeoutError } from '../src/index.js';
import { call, startFaultline, startUpstream, type Faultline, type Upstream } from './faultline.js';

describe('the client against a live faultline', () => {
  let faultline: Faultline;
  let upstream: Upstream;
  let client: FaultlineClient;

  beforeAll(async () => {
    upstream = await startUpstream();
    faultline = await startFaultline(upstream, ['--config', await scenarioConfig(upstream.host)]);
    client = new FaultlineClient(faultline.admin);
  }, 30_000);

  afterAll(async () => {
    await faultline?.stop();
    await upstream?.stop();
  });

  beforeEach(async () => {
    // Each test starts from the same place: nothing observed, every behavior
    // re-armed, and the scenario the previous test may have activated off.
    await client.setScenarioActive('the-upstream-is-down', false);
    await client.resetSession();
  });

  it('injects a fault that the application then meets', async () => {
    const rule = await client.addRule({
      name: 'the upstream is refusing',
      enabled: true,
      match: { host: upstream.host },
      fault: { type: 'status', code: 503 },
      behavior: { type: 'first_n', n: 2 },
    });
    expect(rule.id).toBe('the-upstream-is-refusing');
    expect(rule.warnings ?? []).toEqual([]);

    try {
      const first = await call(faultline.route, '/get');
      expect(first.status).toBe(503);
      expect(first.headers['faultline-fault']).toBe(rule.id);

      const second = await call(faultline.route, '/get');
      expect(second.status).toBe(503);

      // The behavior is spent, so the third call reaches the real upstream.
      const third = await call(faultline.route, '/get');
      expect(third.status).toBe(200);

      const report = await client.report();
      expect(report.total).toBe(3);
      expect(report.faulted).toBe(2);
      expect(report.retries).toBe(2);
    } finally {
      await client.removeRule(rule.id);
    }
  });

  it('waits for the call the application makes next', async () => {
    const waiting = client.waitForEvent({ host: upstream.host }, 10_000);
    await call(faultline.route, '/orders');

    const event = await waiting;
    expect(event.host).toBe(upstream.host);
    expect(event.path).toBe('/orders');
    expect(event.tier).toBe('plain');
  });

  it('ignores the events already recorded when it starts waiting', async () => {
    await call(faultline.route, '/before');

    await expect(client.waitForEvent({ host: upstream.host }, 500)).rejects.toBeInstanceOf(
      WaitTimeoutError,
    );
  });

  it('activates a scenario and applies its rules', async () => {
    const activated = await client.setScenarioActive('the-upstream-is-down', true);
    expect(activated.active).toBe(true);

    const faulted = await call(faultline.route, '/get');
    expect(faulted.status).toBe(503);

    const deactivated = await client.setScenarioActive('the-upstream-is-down', false);
    expect(deactivated.active).toBe(false);
    expect((await call(faultline.route, '/get')).status).toBe(200);
  });

  it('re-arms a spent behavior when the session is reset', async () => {
    await client.setScenarioActive('the-upstream-is-down', true);
    expect((await call(faultline.route, '/get')).status).toBe(503);
    expect((await call(faultline.route, '/get')).status).toBe(200);

    await client.resetSession();

    expect((await call(faultline.route, '/get')).status).toBe(503);
    expect((await client.report()).total).toBe(1);
  });

  it('reports a refusal as an ApiError naming the field', async () => {
    const refused = client.addRule({
      name: 'no such fault',
      enabled: true,
      match: { host: upstream.host },
      fault: { type: 'nonesuch' },
    });

    await expect(refused).rejects.toBeInstanceOf(ApiError);
    await refused.catch((err: ApiError) => {
      expect(err.status).toBe(400);
      expect(err.field).toBe('fault.type');
      expect(err.message).toContain('nonesuch');
    });
  });
});

describe('the client on its own', () => {
  it('accepts an address without a scheme', () => {
    expect(new FaultlineClient('localhost:9000').addr).toBe('http://localhost:9000');
  });

  it('rejects an address it cannot use', () => {
    for (const addr of ['', '   ', 'ftp://localhost:9000']) {
      expect(() => new FaultlineClient(addr)).toThrow();
    }
  });

  it('says so when no instance is running', async () => {
    // Port 1 is reserved and nothing listens on it, so the connection is
    // refused rather than hanging.
    const client = new FaultlineClient('http://127.0.0.1:1');
    await expect(client.report()).rejects.toBeInstanceOf(UnreachableError);
  });
});

/** A configuration file declaring one scenario, so there is one to activate. */
async function scenarioConfig(host: string): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), 'faultline-ts-'));
  const path = join(dir, 'faultline.yaml');
  await writeFile(
    path,
    `rules:
  - id: the-upstream-is-down
    name: The upstream is down
    enabled: false
    match:
      host: ${host}
    fault:
      type: status
      code: 503
    behavior:
      type: first_n
      n: 1
scenarios:
  - name: the-upstream-is-down
    rules: [the-upstream-is-down]
`,
  );
  return path;
}
