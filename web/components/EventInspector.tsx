'use client';

import { Lock } from 'lucide-react';
import { useEffect, useState } from 'react';

import { getCapture } from '@/lib/api';
import { decodeBody, type DecodedBody } from '@/lib/body';
import type { Capture, CaptureSide, Event, Rule } from '@/types';

import { FaultBadge, TierBadge } from './badges';
import { Notice } from './ui/Notice';
import { SidePanel } from './ui/SidePanel';

interface EventInspectorProps {
  event: Event;
  /** The rule that faulted this event, when the UI knows it. */
  rule?: Rule;
  /** Opens a rule in the editor, which is where the fault badge leads. */
  onOpenRule: (id: string) => void;
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

export function EventInspector({ event, rule, onOpenRule, onClose }: EventInspectorProps) {
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
    <SidePanel
      onClose={onClose}
      title={
        <span className="font-mono">
          {event.method} {event.host}
          {event.path}
        </span>
      }
      subtitle={
        <>
          <span>{event.status === 0 ? 'no response' : event.status}</span>
          <span>{event.duration_ms}ms</span>
          <TierBadge tier={event.tier} />
          {event.faulted && <FaultBadge ruleId={event.rule_id} rule={rule} onOpen={onOpenRule} />}
        </>
      }
    >
      <div className="flex-1 space-y-4 overflow-auto px-4 py-4">
        {event.error && <Notice tone="error">{event.error}</Notice>}

        {load.state === 'loading' && <p className="text-sm text-zinc-500">Loading the capture…</p>}

        {load.state === 'unavailable' && (
          <Notice tone="info" icon={encrypted ? Lock : undefined}>
            {load.reason}
          </Notice>
        )}

        {load.state === 'loaded' && (
          <>
            <Side title="Request" side={load.capture.request} />
            <Side title="Response" side={load.capture.response} />
          </>
        )}
      </div>
    </SidePanel>
  );
}

function Side({ title, side }: { title: string; side: CaptureSide }) {
  const names = Object.keys(side.headers ?? {}).sort();
  const body = decodeBody(side.body);

  return (
    <section>
      <h2 className="mb-2 text-xs font-medium tracking-wide text-zinc-400 uppercase">{title}</h2>

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
