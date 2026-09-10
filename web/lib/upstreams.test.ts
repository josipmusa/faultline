import { describe, expect, it } from 'vitest';

import type { Rule, Upstream } from '@/types';

import { bypassState, errorRate, quickFaultTypes, quickRuleFor, quickRulePayload } from './upstreams';

function upstream(over: Partial<Upstream> = {}): Upstream {
  return {
    host: 'api.stripe.com',
    tier: 'plain',
    requests: 0,
    faulted: 0,
    errors: 0,
    bypassed: false,
    last_seen: '2026-09-10T10:00:00Z',
    ...over,
  };
}

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: 'r',
    name: 'r',
    enabled: true,
    match: { host: 'api.stripe.com' },
    fault: { type: 'delay', ms: 2000 },
    ...over,
  };
}

describe('errorRate', () => {
  it('is the share of requests that went wrong', () => {
    expect(errorRate(upstream({ requests: 4, errors: 1 }))).toBe(0.25);
  });

  // A host with no requests has no rate, and 0% would claim it is healthy.
  it('is null when nothing was recorded', () => {
    expect(errorRate(upstream({ requests: 0 }))).toBeNull();
    expect(errorRate(upstream({ requests: 3, bypassed: true, bypass_entry: 'localhost' }))).toBeNull();
  });

  // A route does not consult the bypass list, so a host can be on the list
  // with traffic that was really recorded. That traffic has a rate.
  it('is a rate for a recorded host that is on the bypass list', () => {
    expect(errorRate(upstream({ requests: 2, errors: 1, bypass_entry: '127.0.0.1' }))).toBe(0.5);
  });
});

describe('quickRuleFor', () => {
  it('finds an enabled host-only rule with that fault', () => {
    const found = quickRuleFor([rule({ id: 'slow' })], 'api.stripe.com', 'delay');
    expect(found?.id).toBe('slow');
  });

  it('ignores a rule for another host, fault or state', () => {
    const rules = [
      rule({ id: 'other-host', match: { host: 'httpbin.org' } }),
      rule({ id: 'other-fault', fault: { type: 'status', code: 503 } }),
      rule({ id: 'off', enabled: false }),
    ];
    expect(quickRuleFor(rules, 'api.stripe.com', 'delay')).toBeUndefined();
  });

  // A narrower rule is somebody's own work: the panel's toggle would not
  // create it and must not offer to delete it.
  it('ignores a rule that matches more than the host', () => {
    const rules = [
      rule({ id: 'narrow', match: { host: 'api.stripe.com', method: 'POST' } }),
      rule({ id: 'pathed', match: { host: 'api.stripe.com', path: '/v1/*' } }),
      rule({ id: 'headed', match: { host: 'api.stripe.com', header: { 'X-Test': '1' } } }),
    ];
    expect(quickRuleFor(rules, 'api.stripe.com', 'delay')).toBeUndefined();
  });

  // Every host is not this host: an unconditional rule faults everything, and
  // showing it lit on one row would say it belongs to that row.
  it('ignores a rule that matches every host', () => {
    expect(quickRuleFor([rule({ match: {} })], 'api.stripe.com', 'delay')).toBeUndefined();
  });

  it('matches the host case-insensitively, the way the proxies do', () => {
    expect(quickRuleFor([rule({ match: { host: 'API.Stripe.com' } })], 'api.stripe.com', 'delay')).toBeDefined();
  });
});

describe('quickRulePayload', () => {
  it('writes a host-only rule naming what it does', () => {
    expect(quickRulePayload('api.stripe.com', 'delay')).toEqual({
      name: 'delay api.stripe.com',
      enabled: true,
      match: { host: 'api.stripe.com' },
      fault: { type: 'delay', ms: 2000 },
    });
  });

  it('writes the 503 as a status fault', () => {
    expect(quickRulePayload('api.stripe.com', 'status')).toEqual({
      name: '503 api.stripe.com',
      enabled: true,
      match: { host: 'api.stripe.com' },
      fault: { type: 'status', code: 503 },
    });
  });

  it('offers exactly the two faults the panel has buttons for', () => {
    expect(quickFaultTypes).toEqual(['delay', 'status']);
  });
});

describe('bypassState', () => {
  it('is off when nothing covers the host', () => {
    expect(bypassState(upstream())).toEqual({ on: false, ownEntry: false });
  });

  it('is the host is own entry when the entry is the host', () => {
    expect(bypassState(upstream({ bypass_entry: 'api.stripe.com' }))).toEqual({ on: true, ownEntry: true });
    expect(bypassState(upstream({ bypass_entry: 'API.Stripe.com' }))).toEqual({ on: true, ownEntry: true });
  });

  // A broader entry is not this row's to remove: un-bypassing the host would
  // mean editing an entry that covers hosts nobody asked about.
  it('is on but not the host own entry when something broader covers it', () => {
    expect(bypassState(upstream({ host: 'db.internal', bypass_entry: '*.internal' }))).toEqual({
      on: true,
      ownEntry: false,
    });
    expect(bypassState(upstream({ host: '127.0.0.1:8777', bypass_entry: '127.0.0.1' }))).toEqual({
      on: true,
      ownEntry: false,
    });
  });
});
