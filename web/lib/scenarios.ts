import type { Report, Rule, Scenario } from '@/types';

/** One rule a scenario names, ready to show: what it is called, and whether it
 * is on right now. `known` is false for an id the rule list does not hold,
 * which happens for a moment because the two lists are read separately. */
export interface ScenarioRule {
  id: string;
  name: string;
  enabled: boolean;
  known: boolean;
}

/** The rules a scenario names, in the order it names them, resolved against
 * the rule list the stream already keeps.
 *
 * A row shows whether each rule is enabled rather than only whether the
 * scenario is active, because the two can disagree: a rule can be switched by
 * hand, and only one scenario is active at a time, so "on" is a fact about the
 * rules and not about the name. */
export function resolveScenarioRules(scenario: Scenario, rules: Rule[]): ScenarioRule[] {
  const byId = new Map(rules.map((rule) => [rule.id, rule]));

  return scenario.rules.map((id) => {
    const rule = byId.get(id);
    return {
      id,
      name: rule?.name ?? id,
      enabled: rule?.enabled ?? false,
      known: rule !== undefined,
    };
  });
}

/** The ids of the rules that are on, in store order. This is what the new
 * scenario form opens with: somebody who has arranged a situation by hand is
 * one name away from being able to rehearse it again. */
export function enabledRuleIDs(rules: Rule[]): string[] {
  return rules.filter((rule) => rule.enabled).map((rule) => rule.id);
}

export interface ReportTile {
  label: string;
  value: string;
  /** What the number means, for the tile's title. */
  hint: string;
}

/** The report as the five figures the CLI prints, in the same order, so the
 * browser and `faultline run` say the same thing about one session. */
export function reportTiles(report: Report): ReportTile[] {
  return [
    { label: 'Requests', value: String(report.total), hint: 'Calls Faultline saw this session' },
    { label: 'Faulted', value: String(report.faulted), hint: 'Calls a rule acted on, on purpose' },
    { label: 'Retries', value: String(report.retries), hint: 'Calls that look like a repeat of an earlier attempt' },
    {
      label: 'Max retry wait',
      value: `${report.max_retry_wait_ms}ms`,
      hint: 'The longest a retry waited after the attempt it repeats, so the backoff the application used',
    },
    {
      label: 'Abandoned',
      value: String(report.abandoned),
      hint: 'Failures worth retrying that nothing repeated before their window closed',
    },
  ];
}
