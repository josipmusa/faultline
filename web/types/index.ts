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

/** Every frame on the event stream is a tagged envelope, so a client switches
 * on `type` instead of sniffing for fields. A new kind arrives as a new type
 * with its own payload field, never as a widened existing one. */
export type StreamMessage =
  | { type: 'event'; event: Event }
  | { type: 'rules_changed' };
