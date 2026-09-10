import type { Event, StreamMessage } from '@/types';

/** The part of WebSocket this client uses. Narrow on purpose: it keeps the
 * stream testable without a DOM, and there is nothing to send anyway. */
export interface Socket {
  close(): void;
  onopen: (() => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  onmessage: ((e: { data: string }) => void) | null;
}

export interface EventStreamOptions {
  url: string;
  /** How many events to keep. Older ones fall off the end. */
  cap: number;
  open?: (url: string) => Socket;
}

const reconnectBaseMs = 1000;
const reconnectMaxMs = 30_000;

/** A read-only client for GET /api/events/stream.
 *
 * The socket takes no commands, by design: every mutation goes through the
 * REST API. This class holds no React, so its logic is testable on its own;
 * useEventStream is the binding.
 */
export class EventStream {
  events: Event[] = [];
  connected = false;

  private readonly opts: EventStreamOptions;
  private readonly openSocket: (url: string) => Socket;
  private readonly listeners = new Set<() => void>();
  private readonly ruleListeners = new Set<() => void>();
  private socket: Socket | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private attempts = 0;
  private stopped = false;

  constructor(opts: EventStreamOptions) {
    this.opts = opts;
    this.openSocket = opts.open ?? ((url) => new WebSocket(url) as unknown as Socket);
  }

  /** Called on every change, so a view can re-read `events` and `connected`. */
  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** Called when the server says a cached rule list is stale. The notice
   * carries no rules: the listener re-reads GET /api/rules. */
  subscribeRulesChanged(listener: () => void): () => void {
    this.ruleListeners.add(listener);
    return () => this.ruleListeners.delete(listener);
  }

  start() {
    this.stopped = false;
    this.connect();
  }

  stop() {
    this.stopped = true;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    this.socket?.close();
    this.socket = null;
    this.setConnected(false);
  }

  /** Drops the local list. Clearing the recorder is DELETE /api/events; this
   * is the half that empties what the view is holding. */
  clear() {
    this.events = [];
    this.emit();
  }

  /** Seeds the list from GET /api/events, which is where the backlog comes
   * from: the socket only sends what happens after it connects. */
  seed(events: Event[]) {
    this.events = events.slice(0, this.opts.cap);
    this.emit();
  }

  private connect() {
    const socket = this.openSocket(this.opts.url);
    this.socket = socket;

    socket.onopen = () => {
      this.attempts = 0;
      this.setConnected(true);
    };
    socket.onmessage = (e) => this.receive(e.data);
    socket.onclose = () => {
      this.setConnected(false);
      this.reconnect();
    };
    socket.onerror = () => this.setConnected(false);
  }

  private reconnect() {
    if (this.stopped || this.timer !== null) {
      return;
    }
    // Exponential, capped. A closed socket here usually means the binary went
    // away, and it comes back on the same port when it returns.
    const delay = Math.min(reconnectBaseMs * 2 ** this.attempts, reconnectMaxMs);
    this.attempts += 1;
    this.timer = setTimeout(() => {
      this.timer = null;
      if (!this.stopped) {
        this.connect();
      }
    }, delay);
  }

  private receive(data: string) {
    let message: StreamMessage;
    try {
      message = JSON.parse(data) as StreamMessage;
    } catch {
      return;
    }

    switch (message.type) {
      case 'event':
        if (message.event?.id) {
          this.record(message.event);
        }
        return;
      case 'rules_changed':
        for (const listener of this.ruleListeners) {
          listener();
        }
        return;
      default:
        // An unrecognised type is a message kind this build does not know
        // about. Ignoring it is what keeps adding one non-breaking.
        return;
    }
  }

  private record(event: Event) {
    this.events = [event, ...this.events].slice(0, this.opts.cap);
    this.emit();
  }

  private setConnected(connected: boolean) {
    if (this.connected !== connected) {
      this.connected = connected;
      this.emit();
    }
  }

  private emit() {
    for (const listener of this.listeners) {
      listener();
    }
  }
}
