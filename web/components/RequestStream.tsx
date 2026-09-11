'use client';

import { X } from 'lucide-react';

import type { Event, Rule } from '@/types';
import { cn } from '@/lib/utils';

import { FaultBadge, MethodBadge, TierBadge } from './badges';
import { Button } from './ui/Button';
import { EmptyState } from './ui/EmptyState';
import { Notice } from './ui/Notice';

/** Green for an answer, blue for a redirect, orange for a client error, red for
 * a server error. Orange rather than yellow for 4xx: yellow sits next to the
 * amber a faulted row wears, and a 404 is not a fault. */
function statusColor(status: number): string {
  if (status === 0) return 'text-zinc-500';
  if (status < 300) return 'text-emerald-400';
  if (status < 400) return 'text-blue-400';
  if (status < 500) return 'text-orange-400';
  return 'text-red-400';
}

function time(timestamp: string): string {
  const at = new Date(timestamp);
  return Number.isNaN(at.getTime()) ? '--:--:--' : at.toLocaleTimeString('en-GB', { hour12: false });
}

interface RequestStreamProps {
  events: Event[];
  /** Rules by id, so a faulted row can name the rule rather than number it. */
  rules: Map<string, Rule>;
  selectedId?: string;
  onSelect: (event: Event) => void;
  /** Set when filters are hiding everything, so the empty state can say so
   * rather than claiming no traffic has arrived. */
  filtering: boolean;
  onClearFilters: () => void;
  /** A backlog has been read at least once. Before that an empty list means
   * the page has not looked yet, and the view says so instead of "no
   * requests". */
  seeded: boolean;
  /** Why the API could not be reached, when it could not. */
  error: string | null;
}

export function RequestStream({
  events,
  rules,
  selectedId,
  onSelect,
  filtering,
  onClearFilters,
  seeded,
  error,
}: RequestStreamProps) {
  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {error && (
        <Notice tone="error" className="mx-4 mt-3">
          {error}
        </Notice>
      )}

      {/* A container query rather than a viewport one: what squeezes the
          table is the inspector opening beside it, not the window. Below
          64rem the Time and Tier columns go, since the inspector's header
          carries the tier and the rows keep their order without the clock. */}
      <div className="@container flex flex-1 flex-col overflow-auto">
        <table className="w-full table-fixed text-sm">
          <thead className="sticky top-0 z-10 border-b border-zinc-800 bg-zinc-900 text-xs text-zinc-400">
            <tr>
              <th scope="col" className="w-24 px-4 py-2 text-left font-medium @max-5xl:hidden">Time</th>
              <th scope="col" className="w-24 px-4 py-2 text-left font-medium">Method</th>
              <th scope="col" className="w-[24%] px-4 py-2 text-left font-medium">Host</th>
              <th scope="col" className="px-4 py-2 text-left font-medium">Path</th>
              <th scope="col" className="w-20 px-4 py-2 text-left font-medium">Status</th>
              <th scope="col" className="w-24 px-4 py-2 text-right font-medium">Duration</th>
              <th scope="col" className="w-28 px-4 py-2 text-left font-medium @max-5xl:hidden">Tier</th>
              <th scope="col" className="w-44 px-4 py-2 text-left font-medium @max-5xl:w-36">Fault</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => {
              const selected = event.id === selectedId;
              return (
                // The animation plays as the row mounts, so only a new row
                // announces itself; the rows already on screen stay still.
                <tr
                  key={event.id}
                  onClick={() => onSelect(event)}
                  aria-selected={selected}
                  className={cn(
                    'row-enter cursor-pointer border-b border-zinc-800/50 transition-colors hover:bg-zinc-800/40',
                    // A faulted row is tinted and barred, so it reads as
                    // faulted before any of its text does. The bar is an
                    // inset shadow on the row rather than a border on its
                    // first cell, so it stays put when that cell is hidden.
                    event.faulted && 'bg-amber-500/[0.06] shadow-[inset_2px_0_0_0_theme(--color-amber-500)]',
                    // Selected wins the bar; the tint still says faulted.
                    selected && 'bg-zinc-800 shadow-[inset_2px_0_0_0_theme(--color-zinc-200)]',
                  )}
                >
                  <td className="truncate px-4 py-2 font-mono text-xs text-zinc-500 @max-5xl:hidden">
                    {time(event.timestamp)}
                  </td>
                  <td className="px-4 py-2">
                    <MethodBadge method={event.method} />
                  </td>
                  <td className="truncate px-4 py-2 font-mono text-xs text-zinc-300" title={event.host}>
                    {event.host}
                  </td>
                  <td className="px-4 py-2">
                    {/* The opener. The whole row takes a click, but a row is
                        not something a keyboard can reach; this is. It stops
                        its click so the row's handler does not undo it. */}
                    <button
                      type="button"
                      aria-expanded={selected}
                      title={event.path}
                      onClick={(e) => {
                        e.stopPropagation();
                        onSelect(event);
                      }}
                      className="block w-full cursor-pointer truncate rounded-sm text-left font-mono text-xs text-zinc-400"
                    >
                      {event.path}
                    </button>
                  </td>
                  <td className={cn('px-4 py-2 font-semibold', statusColor(event.status))}>
                    {event.status === 0 ? '-' : event.status}
                  </td>
                  <td className="px-4 py-2 text-right font-mono text-xs text-zinc-400">{event.duration_ms}ms</td>
                  <td className="px-4 py-2 @max-5xl:hidden">
                    <TierBadge tier={event.tier} />
                  </td>
                  <td className="px-4 py-2">
                    {event.faulted && (
                      <FaultBadge ruleId={event.rule_id} rule={event.rule_id ? rules.get(event.rule_id) : undefined} />
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>

        {events.length === 0 && (
          <Empty seeded={seeded} filtering={filtering} onClearFilters={onClearFilters} />
        )}
      </div>
    </div>
  );
}

function Empty({
  seeded,
  filtering,
  onClearFilters,
}: {
  seeded: boolean;
  filtering: boolean;
  onClearFilters: () => void;
}) {
  if (filtering) {
    return (
      <EmptyState
        action={
          <Button variant="ghost" icon={X} onClick={onClearFilters}>
            Clear filters
          </Button>
        }
      >
        No requests match these filters.
      </EmptyState>
    );
  }
  if (!seeded) {
    return <EmptyState>Connecting to Faultline…</EmptyState>;
  }
  return (
    <EmptyState>
      No requests yet. Start your application with{' '}
      <code className="rounded bg-zinc-800 px-1 py-0.5 font-mono text-xs text-zinc-300">
        faultline run -- &lt;your start command&gt;
      </code>{' '}
      and its outbound calls appear here.
    </EmptyState>
  );
}
