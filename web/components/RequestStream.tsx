'use client';

import type { Event } from '@/types';
import { cn } from '@/lib/utils';

const methodColors: Record<string, string> = {
  GET: 'bg-blue-500/10 text-blue-400 border-blue-500/20',
  POST: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
  PUT: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20',
  PATCH: 'bg-orange-500/10 text-orange-400 border-orange-500/20',
  DELETE: 'bg-red-500/10 text-red-400 border-red-500/20',
  CONNECT: 'bg-violet-500/10 text-violet-400 border-violet-500/20',
};

const fallbackMethod = 'bg-zinc-500/10 text-zinc-400 border-zinc-500/20';

function statusColor(status: number): string {
  if (status === 0) return 'text-zinc-500';
  if (status < 300) return 'text-emerald-400';
  if (status < 400) return 'text-blue-400';
  if (status < 500) return 'text-yellow-400';
  return 'text-red-400';
}

function time(timestamp: string): string {
  const at = new Date(timestamp);
  return Number.isNaN(at.getTime()) ? '--:--:--' : at.toLocaleTimeString('en-GB', { hour12: false });
}

interface RequestStreamProps {
  events: Event[];
}

export function RequestStream({ events }: RequestStreamProps) {
  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="flex-1 overflow-auto">
        <table className="w-full text-sm">
          <thead className="sticky top-0 border-b border-zinc-800 bg-zinc-900 text-xs text-zinc-400">
            <tr>
              <th scope="col" className="w-20 px-4 py-2 text-left font-medium">Time</th>
              <th scope="col" className="w-24 px-4 py-2 text-left font-medium">Method</th>
              <th scope="col" className="px-4 py-2 text-left font-medium">Host</th>
              <th scope="col" className="px-4 py-2 text-left font-medium">Path</th>
              <th scope="col" className="w-20 px-4 py-2 text-left font-medium">Status</th>
              <th scope="col" className="w-24 px-4 py-2 text-right font-medium">Duration</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              // The animation plays as the row mounts, so only a new row
              // announces itself; the rows already on screen stay still.
              <tr key={event.id} className="row-enter border-b border-zinc-800/50">
                <td className="px-4 py-2 font-mono text-xs text-zinc-500">{time(event.timestamp)}</td>
                <td className="px-4 py-2">
                  <span
                    className={cn(
                      'inline-flex rounded border px-2 py-0.5 text-xs font-medium',
                      methodColors[event.method] ?? fallbackMethod,
                    )}
                  >
                    {event.method}
                  </span>
                </td>
                <td className="max-w-xs truncate px-4 py-2 font-mono text-xs text-zinc-300">{event.host}</td>
                <td className="max-w-md truncate px-4 py-2 font-mono text-xs text-zinc-400">{event.path}</td>
                <td className={cn('px-4 py-2 font-semibold', statusColor(event.status))}>
                  {event.status === 0 ? '-' : event.status}
                </td>
                <td className="px-4 py-2 text-right font-mono text-xs text-zinc-400">{event.duration_ms}ms</td>
              </tr>
            ))}
          </tbody>
        </table>

        {events.length === 0 && (
          <p className="flex h-64 items-center justify-center text-sm text-zinc-500">
            No requests yet. Waiting for traffic.
          </p>
        )}
      </div>
    </div>
  );
}
