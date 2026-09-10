/** How much of a request Faultline could see, which decides what faults were
 * even possible. Every event carries one, so a fault that could not apply can
 * explain itself rather than looking like it did nothing. */
export type Tier = 'plain' | 'intercepted' | 'encrypted';

/** One proxied request, recorded whether or not a fault applied. */
export interface Event {
  id: string;
  timestamp: string;
  host: string;
  method: string;
  path: string;
  /** The response status, or 0 when the request never got one. */
  status: number;
  duration_ms: number;
  bytes_in: number;
  bytes_out: number;
  faulted: boolean;
  rule_id?: string;
  /** Why the request never reached the upstream, when that is worth saying. */
  error?: string;
  /** The attempt this request repeats, when it looks like a retry. */
  retry_of?: string;
  tier: Tier;
}

/** One half of a captured exchange. `body` is base64, the way Go marshals
 * bytes; `decodeBody` in `lib/body` is what reads it. `truncated` says the
 * body was longer than the capture cap and only its first part was kept. */
export interface CaptureSide {
  headers: Record<string, string[]>;
  body?: string;
  truncated: boolean;
}

/** What went over the wire for one event. Captures live apart from events and
 * are fetched by event id: an event is small and every one of them travels on
 * the stream, a capture is large and is wanted only when somebody opens it.
 * Encrypted traffic has none, which is why the inspector explains the tier. */
export interface Capture {
  event_id: string;
  request: CaptureSide;
  response: CaptureSide;
}

/** One host Faultline has seen, as `GET /api/upstreams` reports it. A
 * bypassed host was passed through on purpose: nothing was recorded for it, so
 * it has no tier, and the counts it carries are from before it was bypassed.
 * Being on the list is a separate fact: the list belongs to the forward proxy,
 * and an explicit route does not consult it, so a routed host can be on the
 * list and recorded all the same.
 * `hint` is advice for something wrong that no status code explains, such as a
 * client that refused the interception certificate. */
export interface Upstream {
  host: string;
  tier?: Tier;
  requests: number;
  /** Requests a rule acted on, on purpose. */
  faulted: number;
  /** Requests that went wrong: a 5xx, or one that never got a status. */
  errors: number;
  /** Its requests were passed through untouched, so nothing was recorded. */
  bypassed: boolean;
  /** The entry on the bypass list that covers this host, absent when none
   * does. Often not the host itself: a portless entry covers every port and
   * `*.internal` covers every subdomain, so this is the entry somebody would
   * have to take off the list to have the host proxied again. */
  bypass_entry?: string;
  last_seen: string;
  hint?: string;
}

export interface Match {
  host?: string;
  method?: string;
  path?: string;
  header?: Record<string, string>;
}

/** A fault on the wire is flat: `{ "type": "delay", "ms": 2000 }`. The
 * catalogue of types lives in the binary, and 6.4 drives the editor's fields
 * from it, so the parameters stay open here rather than being enumerated. */
export type Fault = { type: string } & Record<string, unknown>;

/** A behavior is flat too: `{ "type": "first_n", "n": 2 }`. */
export type Behavior = { type: string } & Record<string, unknown>;

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  match: Match;
  fault: Fault;
  behavior?: Behavior;
}

/** A rule as an endpoint about one rule returns it: the rule, plus anything
 * worth saying about it that is not part of it. A warning is something in the
 * rule's way that does not make it invalid, such as a response-tier fault on a
 * host Faultline has only ever seen encrypted. */
export interface CreatedRule extends Rule {
  warnings?: string[];
}

/** Every frame on the event stream is a tagged envelope, so a client switches
 * on `type` instead of sniffing for fields. A new kind arrives as a new type
 * with its own payload field, never as a widened existing one. */
export type StreamMessage =
  | { type: 'event'; event: Event }
  | { type: 'rules_changed' };

/** What one parameter of a fault or a behavior holds. The editor maps a kind
 * to a control, which is the whole reason a fault the UI was never told about
 * still renders: only a new kind would need work here. */
export type FieldKind = 'integer' | 'string' | 'string map' | 'string list';

/** One parameter, as the binary describes it. The constraints are the ones the
 * server validates against, carried so the form can apply them as input
 * attributes and give an answer before a round trip. */
export interface CatalogueField {
  name: string;
  kind: FieldKind;
  description: string;
  required?: boolean;
  min?: number;
  max?: number;
  /** The alphabet a text parameter is spelled with, absent when any text
   * will do. `pattern` is written with F and P. */
  chars?: string;
  /** The other parameter this one is written with. `exclusive` says exactly
   * one of the two rather than at least one: `truncate` takes `after_bytes`
   * or `percent`, `headers` takes `set`, `remove`, or both. */
  partner?: string;
  exclusive?: boolean;
}

/** One fault or one behavior. A behavior has no tier: it decides when a fault
 * applies, not how much of the traffic Faultline has to see. */
export interface CatalogueEntry {
  name: string;
  tier?: 'connection' | 'response';
  fields: CatalogueField[];
}

/** Everything the binary can do to traffic, as `GET /api/catalogue` reports
 * it. The editor renders its form from this rather than from a list of faults
 * written out again in TypeScript. */
export interface Catalogue {
  faults: CatalogueEntry[];
  behaviors: CatalogueEntry[];
}

/** One named situation, as `GET /api/scenarios` reports it: the rules that go
 * on and off together, named by id in the order the file writes them, and
 * whether this is the one that is on. Only one is at a time. */
export interface Scenario {
  name: string;
  rules: string[];
  active: boolean;
}

/** What the application did while Faultline watched, as
 * `GET /api/sessions/current/report` reports it. It cannot be derived in the
 * browser: retries and abandoned attempts are the recorder's reading of its
 * whole ring, not of the events this view happens to keep. */
export interface Report {
  total: number;
  faulted: number;
  /** Requests that look like a repeat of an earlier attempt. */
  retries: number;
  /** The longest a retry waited after the attempt it repeats, so the backoff
   * the application actually used. Zero when nothing retried. */
  max_retry_wait_ms: number;
  /** Attempts worth retrying that nothing repeated before their window
   * closed. Almost always zero while a run is still going: an attempt whose
   * window is still open counts as neither. */
  abandoned: number;
}
