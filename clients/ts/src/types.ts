/**
 * The wire types of the Faultline admin API, as JSON, snake_case. They are
 * written out here rather than generated so a test suite reading this file can
 * see the whole shape it is working with.
 */

/** The traffic a rule applies to. Every field is optional; an omitted one matches anything. */
export interface Match {
  host?: string;
  method?: string;
  /** A glob over the request path, such as `/v1/charges/*`. */
  path?: string;
  /** Header names mapped to the exact value a request must carry. */
  header?: Record<string, string>;
}

/**
 * What goes wrong, named by type with its parameters beside it:
 * `{ type: "delay", ms: 2000 }`, `{ type: "status", code: 503 }`.
 *
 * The parameters are open because the catalogue is: a fault added to Faultline
 * is usable here without a new release of this client. `faultline rule add
 * --fault <name>` names them, and the API refuses a wrong one by field.
 */
export type Fault = { type: string } & Record<string, unknown>;

/**
 * When a fault applies: `{ type: "first_n", n: 2 }`, `{ type: "percent", percent: 50 }`,
 * `{ type: "for_duration", seconds: 30 }`, `{ type: "pattern", pattern: "FFP" }`.
 */
export type Behavior = { type: string } & Record<string, unknown>;

/** A rule: the traffic it matches, the fault it injects, and optionally the behavior gating it. */
export interface Rule {
  id?: string;
  name: string;
  enabled: boolean;
  match: Match;
  fault: Fault;
  behavior?: Behavior;
}

/** A rule as stored, which is where an id the server minted from the name appears. */
export interface StoredRule extends Rule {
  id: string;
}

/**
 * A rule plus anything worth knowing about it that is not part of it. A warning
 * says the rule is stored and in force but cannot do anything yet, such as a
 * response fault on a host Faultline has only ever seen encrypted. Read it: an
 * empty faulted count beside a warning means the fault never applied, not that
 * the application coped with it.
 */
export interface RuleResult extends StoredRule {
  warnings?: string[];
}

/** How much of a call Faultline could see. */
export type Tier = 'plain' | 'intercepted' | 'encrypted';

/** One call that went through Faultline. */
export interface Event {
  id: string;
  timestamp: string;
  host: string;
  method: string;
  path: string;
  status: number;
  duration_ms: number;
  bytes_in?: number;
  bytes_out?: number;
  faulted: boolean;
  rule_id?: string;
  tier: Tier;
  error?: string;
  /** The id of the attempt this call repeats, when it looks like a retry of one. */
  retry_of?: string;
}

/** What the application did while Faultline watched. */
export interface Report {
  total: number;
  faulted: number;
  retries: number;
  /** The longest any retry waited after the attempt it repeats. */
  max_retry_wait_ms: number;
  abandoned: number;
}

/** The session report, plus a warning for every enabled rule that cannot do anything as things stand. */
export interface ReportResult extends Report {
  warnings?: string[];
}

/** A named group of rules the configuration file declares, and whether it is the active one. */
export interface Scenario {
  name: string;
  rules: string[];
  active: boolean;
}

/** Narrows what `events` returns, the same way the query string on GET /api/events does. */
export interface EventQuery {
  /** Keep the most recent N events. */
  limit?: number;
  /** Keep only events for one upstream. */
  host?: string;
  /** Keep only faulted, or only unfaulted, events. */
  faulted?: boolean;
}
