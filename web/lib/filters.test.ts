import { describe, expect, it } from 'vitest';

import type { Event } from '@/types';

import { emptyFilters, filterEvents, hostsOf, statusClassOf } from './filters';

function event(over: Partial<Event> = {}): Event {
  return {
    id: '1',
    timestamp: '2026-09-10T12:00:00Z',
    host: 'httpbin.org',
    method: 'GET',
    path: '/get',
    status: 200,
    duration_ms: 12,
    bytes_in: 0,
    bytes_out: 42,
    faulted: false,
    tier: 'plain',
    ...over,
  };
}

describe('statusClassOf', () => {
  it('groups a status by its hundreds', () => {
    expect(statusClassOf(204)).toBe('2xx');
    expect(statusClassOf(301)).toBe('3xx');
    expect(statusClassOf(404)).toBe('4xx');
    expect(statusClassOf(503)).toBe('5xx');
  });

  // A request that never got a response is not a 0xx; it is its own thing,
  // and it is exactly what somebody hunting a connection fault filters for.
  it('calls a request with no response none', () => {
    expect(statusClassOf(0)).toBe('none');
  });
});

describe('filterEvents', () => {
  const events = [
    event({ id: '1', host: 'httpbin.org', method: 'GET', status: 200 }),
    event({ id: '2', host: 'api.stripe.com', method: 'POST', status: 503, faulted: true, rule_id: 'boom' }),
    event({ id: '3', host: 'httpbin.org', method: 'POST', status: 0, tier: 'encrypted' }),
  ];

  it('keeps everything when nothing is filtered', () => {
    expect(filterEvents(events, emptyFilters).map((e) => e.id)).toEqual(['1', '2', '3']);
  });

  it('filters by host', () => {
    expect(filterEvents(events, { ...emptyFilters, host: 'httpbin.org' }).map((e) => e.id)).toEqual(['1', '3']);
  });

  it('filters by method', () => {
    expect(filterEvents(events, { ...emptyFilters, method: 'POST' }).map((e) => e.id)).toEqual(['2', '3']);
  });

  it('filters by status class', () => {
    expect(filterEvents(events, { ...emptyFilters, statusClass: '5xx' }).map((e) => e.id)).toEqual(['2']);
    expect(filterEvents(events, { ...emptyFilters, statusClass: 'none' }).map((e) => e.id)).toEqual(['3']);
  });

  it('filters to faulted only', () => {
    expect(filterEvents(events, { ...emptyFilters, faultedOnly: true }).map((e) => e.id)).toEqual(['2']);
  });

  it('applies every filter at once', () => {
    const got = filterEvents(events, { host: 'httpbin.org', method: 'POST', statusClass: 'none', faultedOnly: false });
    expect(got.map((e) => e.id)).toEqual(['3']);
  });
});

describe('hostsOf', () => {
  it('lists the distinct hosts seen, in alphabetical order', () => {
    const events = [event({ host: 'httpbin.org' }), event({ host: 'api.stripe.com' }), event({ host: 'httpbin.org' })];
    expect(hostsOf(events)).toEqual(['api.stripe.com', 'httpbin.org']);
  });
});
