'use client';

import { useState } from 'react';
import { Ban, ShieldAlert, Timer, TriangleAlert, Zap } from 'lucide-react';

import type { Rule, Upstream } from '@/types';
import { addBypass, createRule, deleteRule, removeBypass } from '@/lib/api';
import { panelState } from '@/lib/panelState';
import { cn } from '@/lib/utils';
import {
  bypassState,
  errorRate,
  quickFaultLabel,
  quickRuleFor,
  quickRulePayload,
  type QuickFault,
} from '@/lib/upstreams';

import { TierBadge } from './badges';
import { Badge } from './ui/Badge';
import { Button } from './ui/Button';
import { EmptyState } from './ui/EmptyState';
import { Notice } from './ui/Notice';
import { Toolbar } from './ui/Toolbar';

interface UpstreamsPanelProps {
  /** Null until the first read answers. */
  upstreams: Upstream[] | null;
  /** The rules as the stream keeps them, which is where a quick action reads
   * whether it is already in force. */
  rules: Rule[];
  /** Set when the list could not be read, so the panel can say why rather
   * than look empty. */
  error: string | null;
  /** Re-reads the list, after an action changed something the rows show. */
  onChanged: () => void;
}

/** What one row is waiting for, or what came back from it. Keyed by host, so
 * two rows never share a spinner or a message. */
type Pending = Record<string, string>;

export function UpstreamsPanel({ upstreams, rules, error, onChanged }: UpstreamsPanelProps) {
  const [pending, setPending] = useState<Pending>({});
  const [notices, setNotices] = useState<Pending>({});

  /** Runs one row's action, keeping the row busy until it answers and leaving
   * whatever it has to say on the row itself. */
  const act = async (host: string, what: string, run: () => Promise<string | void>) => {
    setPending((p) => ({ ...p, [host]: what }));
    setNotices((n) => without(n, host));
    try {
      const said = await run();
      if (said) {
        setNotices((n) => ({ ...n, [host]: said }));
      }
    } catch (err: unknown) {
      setNotices((n) => ({ ...n, [host]: err instanceof Error ? err.message : String(err) }));
    } finally {
      setPending((p) => without(p, host));
      onChanged();
    }
  };

  const toggleFault = (u: Upstream, type: QuickFault) => {
    const existing = quickRuleFor(rules, u.host, type);
    return act(u.host, type, async () => {
      if (existing) {
        await deleteRule(existing.id);
        return;
      }
      const created = await createRule(quickRulePayload(u.host, type));
      return created.warnings?.join(' ');
    });
  };

  const toggleBypass = (u: Upstream) =>
    act(u.host, 'bypass', async () => {
      if (bypassState(u).ownEntry) {
        await removeBypass(u.host);
        return;
      }
      await addBypass(u.host);
    });

  const state = panelState(upstreams, error);
  const rows = upstreams ?? [];

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <Toolbar
        summary={
          state === 'loading' || state === 'error'
            ? 'Upstreams'
            : rows.length === 0
              ? 'No upstreams seen yet'
              : `${rows.length} upstream${rows.length === 1 ? '' : 's'} seen, one row per host`
        }
      />

      {error && (
        <Notice tone="error" className="mx-4 mt-3">
          The upstream list could not be read: {error}
        </Notice>
      )}

      <div className="flex flex-1 flex-col overflow-auto">
        <table className="w-full table-fixed text-sm">
          <thead className="sticky top-0 z-10 border-b border-zinc-800 bg-zinc-900 text-xs text-zinc-400">
            <tr>
              <th scope="col" className="px-4 py-2 text-left font-medium">Host</th>
              <th scope="col" className="w-36 px-4 py-2 text-left font-medium">Tier</th>
              <th scope="col" className="w-24 px-4 py-2 text-right font-medium">Requests</th>
              <th scope="col" className="w-24 px-4 py-2 text-right font-medium">Faulted</th>
              <th scope="col" className="w-24 px-4 py-2 text-right font-medium">Errors</th>
              <th scope="col" className="w-24 px-4 py-2 text-right font-medium">Last seen</th>
              <th scope="col" className="w-64 px-4 py-2 text-left font-medium">Quick actions</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((u) => (
              <Row
                key={u.host}
                upstream={u}
                rules={rules}
                busy={pending[u.host]}
                notice={notices[u.host]}
                onFault={(type) => void toggleFault(u, type)}
                onBypass={() => void toggleBypass(u)}
              />
            ))}
          </tbody>
        </table>

        {state === 'loading' && <EmptyState>Loading…</EmptyState>}
        {state === 'empty' && (
          <EmptyState>
            No upstreams seen yet. Start your application with{' '}
            <code className="rounded bg-zinc-800 px-1 py-0.5 font-mono text-xs text-zinc-300">
              faultline run -- &lt;your start command&gt;
            </code>{' '}
            and every host it calls is listed here.
          </EmptyState>
        )}
      </div>
    </div>
  );
}

interface RowProps {
  upstream: Upstream;
  rules: Rule[];
  busy?: string;
  notice?: string;
  onFault: (type: QuickFault) => void;
  onBypass: () => void;
}

function Row({ upstream: u, rules, busy, notice, onFault, onBypass }: RowProps) {
  const rate = errorRate(u);
  const failing = rate !== null && rate > 0;
  const bypass = bypassState(u);
  const routed = routedPastTheList(u);
  /** Whether anything is said under the row, which is also what decides where
   * the line between rows goes: a strip belongs to the row above it, so the
   * two are one block with one border underneath. */
  const details = Boolean(u.hint || notice || routed);

  return (
    <>
      <tr className={cn(!details && 'border-b border-zinc-800/50', u.bypassed && 'text-zinc-500')}>
        <td className="truncate px-4 py-2 font-mono text-xs text-zinc-300" title={u.host}>
          {u.host}
        </td>
        {/* Both can be true at once: a host bypassed part way through has a
            tier from what was recorded before and requests passed through
            since, and the row is the same host either way. */}
        <td className="px-4 py-2">
          <div className="flex flex-wrap items-center gap-1">
            {u.bypassed && (
              <Badge
                tone="neutral"
                icon={Ban}
                title="Passed through untouched, so nothing about those requests was recorded"
              >
                bypassed
              </Badge>
            )}
            {u.tier && <TierBadge tier={u.tier} />}
          </div>
        </td>
        <td className="px-4 py-2 text-right font-mono text-xs text-zinc-300">{u.requests}</td>
        <td className="px-4 py-2 text-right font-mono text-xs text-amber-300/90">{u.faulted || '-'}</td>
        <td className={cn('px-4 py-2 text-right font-mono text-xs', failing ? 'text-red-400' : 'text-zinc-500')}>
          {rate === null ? '-' : `${u.errors} · ${(rate * 100).toFixed(0)}%`}
        </td>
        <td className="px-4 py-2 text-right font-mono text-xs text-zinc-500">{time(u.last_seen)}</td>
        <td className="px-4 py-2">
          <div className="flex items-center gap-1.5">
            <Action
              icon={Timer}
              label={quickFaultLabel('delay')}
              title={`Delay every request to ${u.host} by 2s`}
              on={quickRuleFor(rules, u.host, 'delay') !== undefined}
              busy={busy === 'delay'}
              disabled={u.bypassed}
              why="Nothing is recorded for a bypassed host, so a rule cannot apply"
              onClick={() => onFault('delay')}
            />
            <Action
              icon={Zap}
              label={quickFaultLabel('status')}
              title={`Answer every request to ${u.host} with 503`}
              on={quickRuleFor(rules, u.host, 'status') !== undefined}
              busy={busy === 'status'}
              disabled={u.bypassed}
              why="Nothing is recorded for a bypassed host, so a rule cannot apply"
              onClick={() => onFault('status')}
            />
            <Action
              icon={Ban}
              label="bypass"
              title={
                bypass.on
                  ? bypass.ownEntry
                    ? `Proxy and record ${u.host} again`
                    : `On the bypass list through ${u.bypass_entry}, which covers more than this host: edit that entry to change it`
                  : `Pass ${u.host} through untouched: no rules, no recording`
              }
              on={bypass.on}
              // A broader entry covers hosts this row does not speak for, so
              // taking it off the list is not this click's to make.
              disabled={bypass.on && !bypass.ownEntry}
              busy={busy === 'bypass'}
              onClick={onBypass}
            />
          </div>
        </td>
      </tr>

      {details && (
        <tr className="border-b border-zinc-800/50">
          <td colSpan={7} className="px-4 pb-2">
            <div className="space-y-1.5 border-l-2 border-zinc-700 pl-3">
              {u.hint && (
                <Notice tone="warning" icon={ShieldAlert} host={u.host}>
                  {said(u.hint, u.host)}
                </Notice>
              )}
              {routed && (
                <Notice tone="info" icon={Ban} host={u.host}>
                  on the bypass list through {u.bypass_entry}, and these requests were recorded anyway: an
                  explicit route does not consult the list. Through the forward proxy this host would be passed
                  through untouched.
                </Notice>
              )}
              {notice && (
                <Notice tone="info" icon={TriangleAlert} host={u.host}>
                  {said(notice, u.host)}
                </Notice>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

interface ActionProps {
  icon: typeof Timer;
  label: string;
  title: string;
  /** Lit when this action is already in force, and clicking undoes it. */
  on: boolean;
  busy: boolean;
  disabled?: boolean;
  /** Shown in place of the title when the action is disabled, to say why. */
  why?: string;
  onClick: () => void;
}

/** One quick action. It is a toggle rather than a button that only adds: a
 * second click has to undo the first, or the panel would stack up rules with
 * no way back until the rule editor exists. */
function Action({ icon, label, title, on, busy, disabled, why, onClick }: ActionProps) {
  return (
    <Button
      variant={on ? 'primary' : 'secondary'}
      icon={icon}
      onClick={onClick}
      busy={busy}
      disabled={disabled}
      title={disabled && why ? why : title}
      aria-pressed={on}
    >
      {label}
    </Button>
  );
}

/** The message without the host it already names, since the notice names it
 * itself. A warning from the API leads with the host; a notice written here
 * does not. */
function said(text: string, host: string): string {
  return text.startsWith(`${host} `) ? text.slice(host.length + 1) : text;
}

/** Whether this row's traffic reached Faultline despite the host being on the
 * bypass list, which is what an explicit route to a bypassed host looks like.
 * Worth saying: the row shows faults working on a host the forward proxy would
 * skip, and nothing else on the row explains that. */
function routedPastTheList(u: Upstream): boolean {
  return bypassState(u).on && !u.bypassed && u.requests > 0;
}

/** The map without one host's entry, which is how a row stops being busy or
 * drops the message it was carrying. */
function without(map: Pending, host: string): Pending {
  const rest = { ...map };
  delete rest[host];
  return rest;
}

function time(timestamp: string): string {
  const at = new Date(timestamp);
  return Number.isNaN(at.getTime()) ? '-' : at.toLocaleTimeString('en-GB', { hour12: false });
}
