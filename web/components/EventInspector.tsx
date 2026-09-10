'use client';

import { Lock, X, Zap } from 'lucide-react';
import { useEffect, useState } from 'react';

import { getCapture } from '@/lib/api';
import { decodeBody, type DecodedBody } from '@/lib/body';
import { cn } from '@/lib/utils';
import type { Capture, CaptureSide, Event, Rule } from '@/types';

interface EventInspectorProps {
  event: Event;
  /** The rule that faulted this event, when the UI knows it. */
  rule?: Rule;
  onClose: () => void;
}

type Load =
  | { state: 'loading' }
  | { state: 'loaded'; capture: Capture }
  | { state: 'unavailable'; reason: string };

/** Encrypted traffic was tunnelled, not terminated, so there was never
 * anything to capture. The tier says so on the event itself, and the panel
 * explains it rather than showing an empty body and letting it look broken. */
const encryptedNotice =
  'This request was tunnelled without interception, so Faultline saw only the host and the connection. Run `faultline ca init` and trust the CA to read headers and bodies for this upstream.';

export function EventInspector({ event, rule, onClose }: EventInspectorProps) {
  const encrypted = event.tier === 'encrypted';
  const [fetched, setFetched] = useState<Load>({ state: 'loading' });

  // Encrypted traffic has no capture to ask for, so it never reaches the API
  // and its notice is decided while rendering rather than in an effect.
  const load: Load = encrypted ? { state: 'unavailable', reason: encryptedNotice } : fetched;

  useEffect(() => {
    if (encrypted) {
      return;
    }

    let current = true;
    getCapture(event.id)
      .then((capture) => {
        if (current) setFetched({ state: 'loaded', capture });
      })
      .catch((err: unknown) => {
        if (current) setFetched({ state: 'unavailable', reason: err instanceof Error ? err.message : String(err) });
      });

    // The panel is mounted per event, so this guards only against an answer
    // arriving after the panel has closed.
    return () => {
      current = false;
    };
  }, [event.id, encrypted]);

  return (
    <aside className="flex w-[32rem] shrink-0 flex-col overflow-hidden border-l border-zinc-800 bg-zinc-950">
      <header className="flex items-start justify-between gap-3 border-b border-zinc-800 px-5 py-4">
        <div className="min-w-0">
          <p className="truncate font-mono text-sm text-zinc-100">
            {event.method} {event.host}
            {event.path}
          </p>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-xs text-zinc-500">
            <span>{event.status === 0 ? 'no response' : event.status}</span>
            <span>{event.duration_ms}ms</span>
            <TierBadge tier={event.tier} />
            {event.faulted && <FaultBadge ruleId={event.rule_id} rule={rule} />}
          </p>
        </div>
        <button
          type="button"
          aria-label="Close"
          onClick={onClose}
          className="rounded-md p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <div className="flex-1 overflow-auto px-5 py-4">
        {event.error && (
          <p className="mb-4 rounded-md border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-300">
            {event.error}
          </p>
        )}

        {load.state === 'loading' && <p className="text-sm text-zinc-500">Loading the capture…</p>}

        {load.state === 'unavailable' && (
          <div className="flex gap-2 rounded-md border border-zinc-800 bg-zinc-900/60 px-3 py-3 text-xs text-zinc-400">
            {encrypted && <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0 text-violet-400" />}
            <p>{load.reason}</p>
          </div>
        )}

        {load.state === 'loaded' && (
          <>
            <Side title="Request" side={load.capture.request} />
            <Side title="Response" side={load.capture.response} />
          </>
        )}
      </div>
    </aside>
  );
}

function Side({ title, side }: { title: string; side: CaptureSide }) {
  const names = Object.keys(side.headers ?? {}).sort();
  const body = decodeBody(side.body);

  return (
    <section className="mb-6 last:mb-0">
      <h2 className="mb-2 text-xs font-semibold tracking-wide text-zinc-400 uppercase">{title}</h2>

      {names.length === 0 ? (
        <p className="text-xs text-zinc-600">No headers.</p>
      ) : (
        <dl className="mb-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 font-mono text-xs">
          {names.map((name) => (
            <div key={name} className="contents">
              <dt className="text-zinc-500">{name}</dt>
              <dd className="break-all text-zinc-300">{side.headers[name].join(', ')}</dd>
            </div>
          ))}
        </dl>
      )}

      <Body body={body} truncated={side.truncated} />
    </section>
  );
}

function Body({ body, truncated }: { body: DecodedBody; truncated: boolean }) {
  if (body.kind === 'empty') {
    return <p className="text-xs text-zinc-600">No body.</p>;
  }
  return (
    <>
      {body.kind === 'binary' ? (
        <p className="text-xs text-zinc-500">Binary body, {body.bytes} bytes.</p>
      ) : (
        <pre className="max-h-80 overflow-auto rounded-md border border-zinc-800 bg-zinc-900/60 p-3 font-mono text-xs whitespace-pre-wrap text-zinc-300">
          {body.text}
        </pre>
      )}
      {truncated && (
        <p className="mt-1 text-xs text-amber-400/80">
          Capture cut at {body.bytes} bytes; the body sent was longer.
        </p>
      )}
    </>
  );
}

const tierStyles: Record<Event['tier'], string> = {
  plain: 'border-zinc-600/40 bg-zinc-500/10 text-zinc-400',
  intercepted: 'border-sky-500/30 bg-sky-500/10 text-sky-300',
  encrypted: 'border-violet-500/30 bg-violet-500/10 text-violet-300',
};

export function TierBadge({ tier }: { tier: Event['tier'] }) {
  return (
    <span className={cn('inline-flex rounded border px-1.5 py-0.5 text-[0.65rem] font-medium', tierStyles[tier])}>
      {tier}
    </span>
  );
}

/** The rule that faulted a request, named where the UI knows the name. The
 * bolt is what makes a faulted row readable at a glance: a rule name alone is
 * just more text in a dense table, and rule names get long. The badge is
 * inert; there is no rule editor to open until 6.4 builds one. */
export function FaultBadge({ ruleId, rule }: { ruleId?: string; rule?: Rule }) {
  const name = rule?.name ?? ruleId ?? 'faulted';

  return (
    <span
      title={ruleId ? `Faulted by rule ${ruleId}` : 'Faulted'}
      className="inline-flex max-w-full items-center gap-1 rounded border border-amber-500/30 bg-amber-500/15 py-0.5 pr-2 pl-1.5 text-[0.7rem] font-medium text-amber-300"
    >
      <Zap className="h-3 w-3 shrink-0" fill="currentColor" />
      <span className="truncate">{name}</span>
    </span>
  );
}
