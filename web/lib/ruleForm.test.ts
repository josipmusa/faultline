import { describe, expect, it } from 'vitest';

import type { CatalogueEntry, Rule } from '@/types';

import {
  blankForm,
  blankParams,
  describeFault,
  describeMatch,
  formToRule,
  paramsFromValues,
  paramsToValues,
  ruleToForm,
} from './ruleForm';

const delay: CatalogueEntry = {
  name: 'delay',
  tier: 'connection',
  fields: [
    { name: 'ms', kind: 'integer', description: 'How long', required: true, min: 1 },
    { name: 'jitter_ms', kind: 'integer', description: 'Random extra', min: 0 },
  ],
};

const headers: CatalogueEntry = {
  name: 'headers',
  tier: 'response',
  fields: [
    { name: 'set', kind: 'string map', description: 'Set these', partner: 'remove' },
    { name: 'remove', kind: 'string list', description: 'Strip these' },
  ],
};

const firstN: CatalogueEntry = {
  name: 'first_n',
  fields: [{ name: 'n', kind: 'integer', description: 'How many', required: true, min: 1 }],
};

const catalogue = { faults: [delay, headers], behaviors: [firstN] };

describe('blankParams', () => {
  it('gives every parameter an empty value of its kind', () => {
    expect(blankParams(delay)).toEqual({ ms: '', jitter_ms: '' });
  });

  it('starts a map or a list with one empty row to type into', () => {
    expect(blankParams(headers)).toEqual({ set: [{ name: '', value: '' }], remove: [''] });
  });

  it('has nothing to fill for a fault with no parameters', () => {
    expect(blankParams({ name: 'refuse', tier: 'connection', fields: [] })).toEqual({});
  });

  // A fault the catalogue does not describe cannot be rendered at all, which
  // the form reports rather than crashing on.
  it('is empty when there is no entry', () => {
    expect(blankParams(undefined)).toEqual({});
  });
});

describe('reading a saved fault into the form', () => {
  it('holds numbers as text, because that is what an input carries', () => {
    expect(paramsFromValues(delay, { type: 'delay', ms: 2000, jitter_ms: 0 })).toEqual({
      ms: '2000',
      jitter_ms: '0',
    });
  });

  it('leaves a parameter the saved fault omits empty', () => {
    expect(paramsFromValues(delay, { type: 'delay', ms: 2000 })).toEqual({ ms: '2000', jitter_ms: '' });
  });

  it('turns a map and a list into rows', () => {
    expect(
      paramsFromValues(headers, {
        type: 'headers',
        set: { 'Cache-Control': 'no-store' },
        remove: ['ETag'],
      }),
    ).toEqual({ set: [{ name: 'Cache-Control', value: 'no-store' }], remove: ['ETag'] });
  });
});

describe('writing the form back to a fault', () => {
  it('sends whole numbers, not the text of them', () => {
    expect(paramsToValues(delay, { ms: '2000', jitter_ms: '500' })).toEqual({ ms: 2000, jitter_ms: 500 });
  });

  // An empty input means the parameter was left out. Sending 0 instead would
  // be a different rule: `reset` with after_bytes 0 breaks the connection
  // before anything, which is not what an untouched field asked for.
  it('leaves out a parameter that was not filled in', () => {
    expect(paramsToValues(delay, { ms: '2000', jitter_ms: '' })).toEqual({ ms: 2000 });
  });

  it('keeps a zero that was actually typed', () => {
    expect(paramsToValues(delay, { ms: '2000', jitter_ms: '0' })).toEqual({ ms: 2000, jitter_ms: 0 });
  });

  // The server decides what is valid. Text in a number field travels as it was
  // typed so the answer comes back naming the field, rather than the form
  // quietly turning it into something else.
  it('sends text that is not a number unchanged', () => {
    expect(paramsToValues(delay, { ms: 'soon', jitter_ms: '' })).toEqual({ ms: 'soon' });
  });

  it('drops the empty rows of a map and a list', () => {
    expect(
      paramsToValues(headers, {
        set: [
          { name: 'Cache-Control', value: 'no-store' },
          { name: '', value: '' },
        ],
        remove: ['ETag', ''],
      }),
    ).toEqual({ set: { 'Cache-Control': 'no-store' }, remove: ['ETag'] });
  });

  it('leaves out a map or a list with no rows left', () => {
    expect(paramsToValues(headers, { set: [{ name: '', value: '' }], remove: [''] })).toEqual({});
  });
});

describe('formToRule', () => {
  const base = blankForm(catalogue);

  it('builds the flat wire shape', () => {
    const rule = formToRule(
      { ...base, name: 'Stripe is slow', match: { ...base.match, host: 'api.stripe.com' }, faultParams: { ms: '2000' } },
      catalogue,
    );

    expect(rule).toEqual({
      name: 'Stripe is slow',
      enabled: true,
      match: { host: 'api.stripe.com' },
      fault: { type: 'delay', ms: 2000 },
    });
  });

  // The API derives an id from the name when the rule does not carry one, so
  // an untouched id field must not travel as an empty string.
  it('leaves the id out when it was not given', () => {
    expect(formToRule(base, catalogue)).not.toHaveProperty('id');
    expect(formToRule({ ...base, id: 'slow-stripe' }, catalogue)).toHaveProperty('id', 'slow-stripe');
  });

  it('leaves out the parts of the match that were not filled in', () => {
    const rule = formToRule({ ...base, match: { ...base.match, method: 'post' } }, catalogue);
    expect(rule.match).toEqual({ method: 'post' });
  });

  it('carries the match headers as a mapping', () => {
    const rule = formToRule(
      { ...base, match: { ...base.match, header: [{ name: 'X-Test', value: '1' }] } },
      catalogue,
    );
    expect(rule.match).toEqual({ header: { 'X-Test': '1' } });
  });

  it('has no behavior when none was chosen', () => {
    expect(formToRule(base, catalogue).behavior).toBeUndefined();
  });

  it('carries the chosen behavior', () => {
    const rule = formToRule({ ...base, behaviorType: 'first_n', behaviorParams: { n: '2' } }, catalogue);
    expect(rule.behavior).toEqual({ type: 'first_n', n: 2 });
  });
});

describe('ruleToForm', () => {
  const saved: Rule = {
    id: 'slow-stripe',
    name: 'Stripe is slow',
    enabled: false,
    match: { host: 'api.stripe.com', path: '/v1/charges/*', header: { 'X-Test': '1' } },
    fault: { type: 'delay', ms: 2000 },
    behavior: { type: 'first_n', n: 2 },
  };

  it('round-trips a rule through the form unchanged', () => {
    expect(formToRule(ruleToForm(saved, catalogue), catalogue)).toEqual({
      id: 'slow-stripe',
      name: 'Stripe is slow',
      enabled: false,
      match: { host: 'api.stripe.com', path: '/v1/charges/*', header: { 'X-Test': '1' } },
      fault: { type: 'delay', ms: 2000 },
      behavior: { type: 'first_n', n: 2 },
    });
  });

  it('offers an empty row to add a match header to a rule that has none', () => {
    const form = ruleToForm({ ...saved, match: { host: 'a' } }, catalogue);
    expect(form.match.header).toEqual([{ name: '', value: '' }]);
  });

  // A rule can name a fault this binary does not have: an older UI against a
  // newer file, or a hand-edited faultline.yaml. The form keeps the type so
  // the panel can say so instead of silently rewriting the rule.
  it('keeps a fault type the catalogue does not describe', () => {
    const form = ruleToForm({ ...saved, fault: { type: 'wormhole', depth: 3 } }, catalogue);
    expect(form.faultType).toBe('wormhole');
    expect(form.faultParams).toEqual({});
  });
});

describe('describing a rule in a row', () => {
  it('names the fault and its parameters', () => {
    expect(describeFault({ type: 'delay', ms: 2000, jitter_ms: 500 })).toBe('delay ms=2000 jitter_ms=500');
  });

  it('is just the name for a fault with no parameters', () => {
    expect(describeFault({ type: 'refuse' })).toBe('refuse');
  });

  it('reads a match left to right', () => {
    expect(describeMatch({ host: 'api.stripe.com', method: 'POST', path: '/v1/*' })).toBe(
      'api.stripe.com POST /v1/*',
    );
  });

  it('says so when a rule matches everything', () => {
    expect(describeMatch({})).toBe('all traffic');
  });

  it('counts the headers a match requires rather than listing them', () => {
    expect(describeMatch({ host: 'a', header: { 'X-Test': '1', 'X-Other': '2' } })).toBe('a +2 headers');
    expect(describeMatch({ host: 'a', header: { 'X-Test': '1' } })).toBe('a +1 header');
  });
});
