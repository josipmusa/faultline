import { describe, expect, it } from 'vitest';

import type { Report, Rule, Scenario } from '@/types';

import { enabledRuleIDs, reportTiles, resolveScenarioRules, scenarioSaveBlocked } from './scenarios';

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

function scenario(over: Partial<Scenario> = {}): Scenario {
  return { name: 'payments-down', rules: [], active: false, ...over };
}

const report: Report = {
  total: 42,
  faulted: 12,
  retries: 8,
  max_retry_wait_ms: 1002,
  abandoned: 0,
};

describe('resolveScenarioRules', () => {
  const rules = [rule({ id: 'slow', name: 'Stripe is slow' }), rule({ id: 'down', name: 'Stripe is down', enabled: false })];

  it('names each rule and says whether it is on right now', () => {
    const got = resolveScenarioRules(scenario({ rules: ['slow', 'down'] }), rules);

    expect(got).toEqual([
      { id: 'slow', name: 'Stripe is slow', enabled: true, known: true },
      { id: 'down', name: 'Stripe is down', enabled: false, known: true },
    ]);
  });

  it('keeps the order the scenario names them in', () => {
    const got = resolveScenarioRules(scenario({ rules: ['down', 'slow'] }), rules);
    expect(got.map((r) => r.id)).toEqual(['down', 'slow']);
  });

  // The rule list and the scenario list are read separately, so one can be a
  // moment behind the other. A row says the id rather than dropping it.
  it('falls back to the id for a rule the list does not hold', () => {
    const got = resolveScenarioRules(scenario({ rules: ['ghost'] }), rules);
    expect(got).toEqual([{ id: 'ghost', name: 'ghost', enabled: false, known: false }]);
  });
});

describe('enabledRuleIDs', () => {
  // The form opens on what is on right now, which is the "save what you did"
  // case: somebody arranged a situation by hand and wants it named.
  it('is the rules that are on, in store order', () => {
    const got = enabledRuleIDs([
      rule({ id: 'a' }),
      rule({ id: 'b', enabled: false }),
      rule({ id: 'c' }),
    ]);
    expect(got).toEqual(['a', 'c']);
  });

  it('is empty when nothing is on', () => {
    expect(enabledRuleIDs([rule({ enabled: false })])).toEqual([]);
  });
});

describe('reportTiles', () => {
  it('reads the five numbers the CLI prints, in the same order', () => {
    expect(reportTiles(report).map((tile) => tile.label)).toEqual([
      'Requests',
      'Faulted',
      'Retries',
      'Max retry wait',
      'Abandoned',
    ]);
  });

  it('writes the longest retry wait as a duration', () => {
    expect(reportTiles(report)[3].value).toBe('1002ms');
  });

  it('is all zeroes for a session that has seen nothing', () => {
    const empty: Report = { total: 0, faulted: 0, retries: 0, max_retry_wait_ms: 0, abandoned: 0 };
    expect(reportTiles(empty).map((tile) => tile.value)).toEqual(['0', '0', '0', '0ms', '0']);
  });
});

describe('scenarioSaveBlocked', () => {
  it('needs a name', () => {
    expect(scenarioSaveBlocked('', ['a'])).toBe('Give the scenario a name.');
    expect(scenarioSaveBlocked('   ', ['a'])).toBe('Give the scenario a name.');
  });

  it('needs at least one rule, since a scenario is a name for a set of rules', () => {
    expect(scenarioSaveBlocked('payments-down', [])).toBe('Tick at least one rule.');
  });

  it('is clear to save with both', () => {
    expect(scenarioSaveBlocked('payments-down', ['a'])).toBeNull();
  });
});
