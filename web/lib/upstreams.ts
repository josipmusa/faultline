import type { Rule, Upstream } from '@/types';

/** The faults the panel has a button for. A delay long enough to be felt and
 * the error every dependency eventually returns: the two things somebody
 * reaches for first, without opening the rule editor to choose numbers. */
export const quickFaultTypes = ['delay', 'status'] as const;

export type QuickFault = (typeof quickFaultTypes)[number];

const quickFaults: Record<QuickFault, { label: string; fault: Rule['fault'] }> = {
  delay: { label: 'delay', fault: { type: 'delay', ms: 2000 } },
  status: { label: '503', fault: { type: 'status', code: 503 } },
};

/** The share of a host's requests that went wrong, or null when there is
 * nothing to divide by.
 *
 * A host with requests passed through has no rate: those requests are counted,
 * because they happened, but nothing about them was recorded, so any rate over
 * them would be wrong in one direction or the other. Merely being on the
 * bypass list is not that - a routed host's traffic is recorded - so this
 * reads what was skipped, not what the list says. */
export function errorRate(u: Upstream): number | null {
  if (u.bypassed || u.requests === 0) {
    return null;
  }
  return u.errors / u.requests;
}

export interface BypassState {
  /** The forward proxy would pass this host through untouched. */
  on: boolean;
  /** The entry that does it is the host itself, so one click can take it off
   * the list. False for a broader entry - a portless one, or a wildcard -
   * which covers hosts this row does not speak for. */
  ownEntry: boolean;
}

/** What the bypass toggle should show for a host: whether it is covered, and
 * whether the entry covering it is the host's own. */
export function bypassState(u: Upstream): BypassState {
  const entry = u.bypass_entry ?? '';
  return { on: entry !== '', ownEntry: entry.toLowerCase() === u.host.toLowerCase() };
}

/** The rule that has this host under this fault already, if there is one.
 *
 * The panel finds its own work by shape rather than by a generated id: a rule
 * id may not hold a colon and a host may carry a port. Matching on shape also
 * lights the button for the same rule written by hand or in faultline.yaml,
 * which is the truth the row should be telling.
 *
 * Only a rule that matches this host and nothing else counts. A narrower rule
 * is somebody's own, and an unconditional one faults every host, so showing
 * either as this row's would claim something untrue. */
export function quickRuleFor(rules: Rule[], host: string, type: QuickFault): Rule | undefined {
  return rules.find(
    (rule) =>
      rule.enabled &&
      rule.fault.type === quickFaults[type].fault.type &&
      hostOnly(rule.match) &&
      rule.match.host?.toLowerCase() === host.toLowerCase(),
  );
}

function hostOnly(match: Rule['match']): boolean {
  return (
    match.host !== undefined &&
    match.host !== '' &&
    match.method === undefined &&
    match.path === undefined &&
    match.header === undefined
  );
}

/** The rule one of the panel's buttons creates. The id is left out, so the API
 * derives a readable one from the name. */
export function quickRulePayload(host: string, type: QuickFault): Omit<Rule, 'id'> {
  const { label, fault } = quickFaults[type];
  return { name: `${label} ${host}`, enabled: true, match: { host }, fault };
}

/** What a button says it will do, for its label and its title. */
export function quickFaultLabel(type: QuickFault): string {
  return quickFaults[type].label;
}
